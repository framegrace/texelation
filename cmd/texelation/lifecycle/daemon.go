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
	SocketPath    string
	SnapshotPath  string
	FromScratch   bool
	DefaultApp    string
	VerboseLogs   bool
	LogFilePath   string // Daemon stdout/stderr destination
	Title         string
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

	if !d.pidFile.IsProcessRunning() {
		return StateStale, nil
	}

	// Process exists, check health
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := d.health.Check(healthCtx, d.socketPath); err != nil {
		return StateUnresponsive, nil
	}

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

	// Build server command args
	args := []string{
		"--server-only",
		"--socket", opts.SocketPath,
	}
	if opts.SnapshotPath != "" {
		args = append(args, "--snapshot", opts.SnapshotPath)
	}
	if opts.FromScratch {
		args = append(args, "--from-scratch")
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

	pid := cmd.Process.Pid

	// Write PID file before releasing process
	if err := d.pidFile.Write(pid); err != nil {
		// Try to kill the process we just started
		_ = cmd.Process.Kill()
		return fmt.Errorf("write PID file: %w", err)
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

	// Wait for graceful shutdown (up to 5 seconds)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			// Context cancelled - force kill
			_ = process.Kill()
			d.pidFile.Remove()
			return ctx.Err()
		default:
		}

		if err := process.Signal(syscall.Signal(0)); err != nil {
			// Process is gone
			d.pidFile.Remove()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill after timeout
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
