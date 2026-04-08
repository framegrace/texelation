// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/lifecycle/daemon.go
// Summary: Daemon process management for texelation server.

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// DaemonState represents the lifecycle state of the server daemon
type DaemonState int

const (
	StateUnknown      DaemonState = iota
	StateStopped                  // No PID file or process not running
	StateRunning                  // PID file exists, process responds to health checks
	StateUnresponsive             // PID file exists, process not responding
	StateStale                    // PID file exists, process doesn't exist
)

func (s DaemonState) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateRunning:
		return "running"
	case StateUnresponsive:
		return "unresponsive"
	case StateStale:
		return "stale"
	default:
		return "unknown"
	}
}

// ServerOptions configures server daemon startup
type ServerOptions struct {
	SocketPath   string
	SnapshotPath string
	DefaultApp   string
	VerboseLogs  bool
	LogFilePath  string // Daemon stdout/stderr destination
	Title        string
	PIDFilePath  string // Path passed through to texel-server --pid-file for flock
}

// DaemonManager handles server process lifecycle
type DaemonManager interface {
	// GetState checks current daemon state with health verification
	GetState(ctx context.Context) (DaemonState, error)

	// Start launches the daemon process (fork+detach)
	Start(ctx context.Context, opts ServerOptions) error

	// Stop gracefully stops the daemon (SIGTERM, wait, then SIGKILL)
	Stop(ctx context.Context) error

	// Restart performs stop + start atomically
	Restart(ctx context.Context, opts ServerOptions) error

	// GetPID returns the current PID if running, or 0
	GetPID() int
}

type standardDaemonManager struct {
	pidFile    PIDFile
	socketPath string
	health     HealthChecker
}

// NewDaemonManager creates a new daemon manager
func NewDaemonManager(pidFile PIDFile, socketPath string, health HealthChecker) DaemonManager {
	return &standardDaemonManager{
		pidFile:    pidFile,
		socketPath: socketPath,
		health:     health,
	}
}

func (d *standardDaemonManager) GetState(ctx context.Context) (DaemonState, error) {
	if !d.pidFile.Exists() {
		return StateStopped, nil
	}

	// Canonical "is a server alive" signal: exclusive flock on the PID
	// file. The server holds the lock from startup until OS release at
	// exit, so a slow-flushing server still reports as locked — unlike
	// socket health, which goes false the moment the listener closes.
	locked := d.pidFile.IsLocked()

	if !locked {
		// Lock not held: either no server, or the file is stale from a
		// pre-flock build or crash. Fall back to the legacy process check.
		if !d.pidFile.IsProcessRunning() {
			return StateStale, nil
		}
		// Legacy server without flock support — use socket health.
		healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := d.health.Check(healthCtx, d.socketPath); err != nil {
			return StateUnresponsive, nil
		}
		return StateRunning, nil
	}

	// Lock held → a server is alive (running or mid-shutdown). Treat as
	// running and let the connect path decide whether to wait.
	return StateRunning, nil
}

func (d *standardDaemonManager) GetPID() int {
	pid, err := d.pidFile.Read()
	if err != nil {
		return 0
	}
	return pid
}

func (d *standardDaemonManager) Start(ctx context.Context, opts ServerOptions) error {
	// Check if already running
	if d.pidFile.IsProcessRunning() {
		return fmt.Errorf("server already running (PID %d)", d.GetPID())
	}

	// Get current executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Build server command args. The --pid-file flag tells texel-server
	// where to acquire its exclusive flock; the server writes its own
	// PID there under the lock, so supervisors can reliably distinguish
	// "alive" from "gone" without relying on the socket.
	//
	// Note: the intermediate exec path is
	//     texelation --server-only --pid-file=... --socket=...
	// → handleServerOnly reads --pid-file and forwards it when
	// exec'ing the actual texel-server binary.
	pidPath := opts.PIDFilePath
	if pidPath == "" {
		pidPath = d.pidFile.Path()
	}
	args := []string{
		"--server-only",
		"--socket", opts.SocketPath,
		"--pid-file", pidPath,
	}
	if opts.SnapshotPath != "" {
		args = append(args, "--snapshot", opts.SnapshotPath)
	}
	if opts.DefaultApp != "" {
		args = append(args, "--default-app", opts.DefaultApp)
	}
	if opts.VerboseLogs {
		args = append(args, "--verbose-logs")
	}
	if opts.Title != "" {
		args = append(args, "--title", opts.Title)
	}

	// Open log file for daemon output
	// Note: We intentionally do NOT close this file - the child process inherits it
	// and needs it to remain open for logging. The OS will clean up when the parent exits.
	var logFile *os.File
	if opts.LogFilePath != "" {
		var err error
		logFile, err = os.OpenFile(opts.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
	}

	// Create daemon process
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Detach from parent process group (Linux/macOS)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("fork server: %w", err)
	}

	// The server writes its own PID under the exclusive flock via
	// AcquireExclusiveLock, so we do NOT write the PID file here — doing
	// so would race with the child. Wait briefly for the child to take
	// the lock so we can report startup failures synchronously.
	_ = cmd.Process.Pid

	lockDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(lockDeadline) {
		if d.pidFile.IsLocked() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !d.pidFile.IsLocked() {
		_ = cmd.Process.Kill()
		return fmt.Errorf("server did not acquire PID lock within 3s — likely failed to start")
	}

	// Detach - don't wait for process
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release process: %w", err)
	}

	return nil
}

func (d *standardDaemonManager) Stop(ctx context.Context) error {
	pid, err := d.pidFile.Read()
	if err != nil {
		// No PID file or invalid - nothing to stop
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		// Process doesn't exist
		d.pidFile.Remove()
		return nil
	}

	// Check if process is actually running
	if err := process.Signal(syscall.Signal(0)); err != nil {
		// Process not running
		d.pidFile.Remove()
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	// Wait for the server to release its exclusive flock on the PID file.
	// The server holds the lock from startup until clean exit, so waiting
	// for unlock lets slow WAL flushes (tens of seconds for large bursts)
	// complete without being force-killed mid-write and corrupting the
	// scrollback. Cap at gracefulStopTimeout; after that, escalate.
	const gracefulStopTimeout = 60 * time.Second

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- d.pidFile.WaitForUnlock(gracefulStopTimeout)
	}()

	select {
	case err := <-waitErrCh:
		if err == nil {
			// Lock released → process has exited cleanly.
			d.pidFile.Remove()
			return nil
		}
		// Timeout: the process is still holding the lock after
		// gracefulStopTimeout. Fall through to force-kill.
	case <-ctx.Done():
		_ = process.Kill()
		d.pidFile.Remove()
		return ctx.Err()
	}

	// Force kill after graceful timeout. This is an escape hatch for
	// truly hung servers, not the happy-path behavior it used to be.
	if err := process.Kill(); err != nil {
		// Ignore errors - process might have exited
	}
	d.pidFile.Remove()

	return nil
}

func (d *standardDaemonManager) Restart(ctx context.Context, opts ServerOptions) error {
	// Best effort stop
	_ = d.Stop(ctx)

	// Small delay to ensure socket is released
	time.Sleep(200 * time.Millisecond)

	return d.Start(ctx, opts)
}
