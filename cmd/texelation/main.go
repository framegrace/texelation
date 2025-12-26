// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/main.go
// Summary: Unified texelation command for managing server and client lifecycle.
// Usage: Run `texelation` to auto-start server and connect client.

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"texelation/cmd/texelation/lifecycle"
	clientrt "texelation/internal/runtime/client"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse flags
	fs := flag.NewFlagSet("texelation", flag.ContinueOnError)

	// Mode flags
	clientOnly := fs.Bool("client-only", false, "Run client without checking/starting server")
	serverOnly := fs.Bool("server-only", false, "Run server in foreground (used by daemon)")
	stopServer := fs.Bool("stop", false, "Stop running server daemon")
	resetState := fs.Bool("reset-state", false, "Delete all state and start fresh (requires confirmation)")
	showStatus := fs.Bool("status", false, "Show server status and exit")

	// Shared flags
	socketPath := fs.String("socket", "/tmp/texelation.sock", "Unix socket path")

	// Server flags
	snapshotPath := fs.String("snapshot", "", "Path to persist pane snapshots (default: ~/.texelation/snapshot.json)")
	fromScratch := fs.Bool("from-scratch", false, "Start from scratch, ignoring any saved snapshot")
	defaultApp := fs.String("default-app", "", "Default app for new panes")
	verboseLogs := fs.Bool("verbose-logs", false, "Enable verbose server logging")
	title := fs.String("title", "Texel Server", "Title for the main pane")

	// Client flags
	reconnect := fs.Bool("reconnect", false, "Attempt to resume previous session")
	panicLog := fs.String("panic-log", "", "File to append panic stack traces")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	// Resolve config paths
	paths, err := GetPaths()
	if err != nil {
		return fmt.Errorf("resolve config paths: %w", err)
	}

	// Use socket from paths if not overridden
	if *socketPath == "/tmp/texelation.sock" {
		*socketPath = paths.SocketPath
	}

	// Use default snapshot path if not specified
	if *snapshotPath == "" {
		*snapshotPath = paths.SnapshotPath
	}

	// Create config directory if needed
	if err := paths.EnsureConfigDir(); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	ctx := context.Background()

	// Command dispatch
	switch {
	case *showStatus:
		return handleStatus(ctx, paths, *socketPath)

	case *resetState:
		return handleResetState(ctx, paths, *socketPath)

	case *stopServer:
		return handleStopServer(ctx, paths, *socketPath)

	case *serverOnly:
		return handleServerOnly(lifecycle.ServerOptions{
			SocketPath:   *socketPath,
			SnapshotPath: *snapshotPath,
			FromScratch:  *fromScratch,
			DefaultApp:   *defaultApp,
			VerboseLogs:  *verboseLogs,
			LogFilePath:  paths.ServerLogPath,
			Title:        *title,
		})

	case *clientOnly:
		return handleClientOnly(clientrt.Options{
			Socket:    *socketPath,
			Reconnect: *reconnect,
			PanicLog:  *panicLog,
		})

	default:
		// Default: unified mode (ensure server, then connect client)
		return handleUnifiedMode(ctx, paths, lifecycle.ServerOptions{
			SocketPath:   *socketPath,
			SnapshotPath: *snapshotPath,
			FromScratch:  *fromScratch,
			DefaultApp:   *defaultApp,
			VerboseLogs:  *verboseLogs,
			LogFilePath:  paths.ServerLogPath,
			Title:        *title,
		}, clientrt.Options{
			Socket:    *socketPath,
			Reconnect: *reconnect,
			PanicLog:  *panicLog,
		})
	}
}

func handleUnifiedMode(ctx context.Context, paths *Paths, srvOpts lifecycle.ServerOptions, clientOpts clientrt.Options) error {
	health := lifecycle.NewSocketHealthChecker(2 * time.Second)
	pidFile := lifecycle.NewPIDFile(paths.PIDPath)
	daemon := lifecycle.NewDaemonManager(pidFile, srvOpts.SocketPath, health)
	supervisor := lifecycle.NewSupervisor(daemon, health, pidFile, lifecycle.DefaultSupervisorConfig())

	// Ensure server is running
	result, err := supervisor.EnsureRunning(ctx, srvOpts)
	if err != nil {
		return fmt.Errorf("ensure server running: %w", err)
	}

	if result.WasStarted {
		fmt.Printf("Server started (PID %d)\n", result.PID)
		fmt.Printf("  Socket: %s\n", srvOpts.SocketPath)
		fmt.Printf("  Logs: %s\n", paths.ServerLogPath)
	}

	// If server was restarted due to being unresponsive, show notification
	if result.WasRestarted {
		clientOpts.ShowRestartNotification = true
	}

	// Run client
	return clientrt.Run(clientOpts)
}

func handleClientOnly(opts clientrt.Options) error {
	return clientrt.Run(opts)
}

func handleServerOnly(opts lifecycle.ServerOptions) error {
	// This is the path taken by the daemonized server
	// We need to run the actual server code here

	// Get the path to texel-server binary
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Find texel-server in the same directory
	dir := filepath.Dir(exe)
	serverBin := filepath.Join(dir, "texel-server")

	// Check if texel-server exists
	if _, err := os.Stat(serverBin); err != nil {
		// Try to find it relative to current working directory
		if _, err := os.Stat("./bin/texel-server"); err == nil {
			serverBin = "./bin/texel-server"
		} else {
			return fmt.Errorf("texel-server binary not found (looked in %s and ./bin/texel-server)", dir)
		}
	}

	// Build args
	args := []string{
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

	// Execute texel-server (replaces current process)
	cmd := exec.Command(serverBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Forward signals to child
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	// Wait for either the command to finish or a signal
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case sig := <-sigCh:
		// Forward signal to child process
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
		return <-done
	}
}

func handleStopServer(ctx context.Context, paths *Paths, socketPath string) error {
	health := lifecycle.NewSocketHealthChecker(2 * time.Second)
	pidFile := lifecycle.NewPIDFile(paths.PIDPath)
	daemon := lifecycle.NewDaemonManager(pidFile, socketPath, health)

	state, err := daemon.GetState(ctx)
	if err != nil {
		return fmt.Errorf("get server state: %w", err)
	}

	if state == lifecycle.StateStopped {
		fmt.Println("Server is not running")
		return nil
	}

	pid := daemon.GetPID()
	fmt.Printf("Stopping server (PID %d)...\n", pid)

	if err := daemon.Stop(ctx); err != nil {
		return fmt.Errorf("stop server: %w", err)
	}

	fmt.Println("Server stopped")
	return nil
}

func handleResetState(ctx context.Context, paths *Paths, socketPath string) error {
	fmt.Println("WARNING: This will delete all saved state:")
	fmt.Printf("  - %s (snapshot)\n", paths.SnapshotPath)
	fmt.Printf("  - %s (PID file)\n", paths.PIDPath)
	fmt.Printf("  - %s (server logs)\n", paths.ServerLogPath)
	fmt.Println()
	fmt.Print("Type 'yes' to confirm: ")

	reader := bufio.NewReader(os.Stdin)
	confirm, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}

	confirm = strings.TrimSpace(confirm)
	if confirm != "yes" {
		fmt.Println("Aborted")
		return nil
	}

	// Stop server first
	health := lifecycle.NewSocketHealthChecker(2 * time.Second)
	pidFile := lifecycle.NewPIDFile(paths.PIDPath)
	daemon := lifecycle.NewDaemonManager(pidFile, socketPath, health)

	state, _ := daemon.GetState(ctx)
	if state != lifecycle.StateStopped {
		fmt.Println("Stopping server...")
		_ = daemon.Stop(ctx) // Best effort
	}

	// Remove state files
	removed := 0
	if err := os.Remove(paths.SnapshotPath); err == nil {
		removed++
	}
	if err := os.Remove(paths.PIDPath); err == nil {
		removed++
	}
	if err := os.Remove(paths.ServerLogPath); err == nil {
		removed++
	}

	fmt.Printf("State reset complete (%d files removed)\n", removed)
	return nil
}

func handleStatus(ctx context.Context, paths *Paths, socketPath string) error {
	health := lifecycle.NewSocketHealthChecker(2 * time.Second)
	pidFile := lifecycle.NewPIDFile(paths.PIDPath)
	daemon := lifecycle.NewDaemonManager(pidFile, socketPath, health)

	state, err := daemon.GetState(ctx)
	if err != nil {
		return fmt.Errorf("get server state: %w", err)
	}

	fmt.Printf("Server status: %s\n", state)

	if state == lifecycle.StateRunning || state == lifecycle.StateUnresponsive {
		pid := daemon.GetPID()
		fmt.Printf("  PID: %d\n", pid)
	}

	fmt.Printf("  Socket: %s\n", socketPath)
	fmt.Printf("  PID file: %s\n", paths.PIDPath)
	fmt.Printf("  Snapshot: %s\n", paths.SnapshotPath)
	fmt.Printf("  Log file: %s\n", paths.ServerLogPath)

	// Check if snapshot exists
	if info, err := os.Stat(paths.SnapshotPath); err == nil {
		fmt.Printf("  Snapshot size: %d bytes\n", info.Size())
		fmt.Printf("  Snapshot modified: %s\n", info.ModTime().Format("2006-01-02 15:04:05"))
	}

	return nil
}
