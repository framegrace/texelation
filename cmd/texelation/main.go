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
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/cmd/texelation/boot"
	"github.com/framegrace/texelation/cmd/texelation/lifecycle"
	clientrt "github.com/framegrace/texelation/internal/runtime/client"
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
	defaultApp := fs.String("default-app", "", "Default app for new panes")
	verboseLogs := fs.Bool("verbose-logs", false, "Enable verbose server logging")
	title := fs.String("title", "Texel Server", "Title for the main pane")
	// Internal flag used by the supervisor: tells the server where to
	// acquire the exclusive PID flock. Accepted at this layer so
	// --server-only can forward it to the texel-server child.
	pidFile := fs.String("pid-file", "", "PID file path (internal; forwarded to texel-server)")

	// Client flags
	reconnect := fs.Bool("reconnect", false, "Attempt to resume previous session")
	panicLog := fs.String("panic-log", "", "File to append panic stack traces")
	clientName := fs.String("client-name", "", "Client identity slot for persistence (default: $TEXELATION_CLIENT_NAME or \"default\")")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	// Plan D: validate --client-name (or $TEXELATION_CLIENT_NAME)
	// early. Either input expresses the user's intent to use a named
	// persistence slot; silently disabling persistence later when
	// ResolvePath rejects the name is a UX trap. ValidateClientName
	// only checks the name itself — it does not touch $HOME or the
	// socket — so failures here are unambiguously "the user-supplied
	// name is invalid", not "your environment is misconfigured."
	if *clientName != "" {
		if err := clientrt.ValidateClientName(*clientName); err != nil {
			return fmt.Errorf("invalid --client-name %q: %w", *clientName, err)
		}
		// Audible warning when the flag silently overrides a non-empty
		// env var — otherwise a user with $TEXELATION_CLIENT_NAME set
		// in their shell rc could silently end up using a different
		// slot than they expect after typing a one-off --client-name.
		if envName := os.Getenv(clientrt.ClientNameEnvVar); envName != "" && envName != *clientName {
			log.Printf("note: --client-name=%q overrides $%s=%q", *clientName, clientrt.ClientNameEnvVar, envName)
		}
	} else if envName := os.Getenv(clientrt.ClientNameEnvVar); envName != "" {
		if err := clientrt.ValidateClientName(envName); err != nil {
			return fmt.Errorf("invalid $%s %q: %w", clientrt.ClientNameEnvVar, envName, err)
		}
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

	// Wire signal cancellation so Ctrl-C / SIGTERM collapse the
	// supervisor's healthcheck wait, the splash error-display sleep,
	// and any other ctx-aware path inside the bootstrap. Before this
	// the unified-mode flow rooted at context.Background() couldn't
	// be interrupted until the in-flight syscall returned.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
			DefaultApp:   *defaultApp,
			VerboseLogs:  *verboseLogs,
			LogFilePath:  paths.ServerLogPath,
			Title:        *title,
			PIDFilePath:  *pidFile,
		})

	case *clientOnly:
		return handleClientOnly(clientrt.Options{
			Socket:     *socketPath,
			Reconnect:  *reconnect,
			PanicLog:   *panicLog,
			ClientName: *clientName,
		})

	default:
		// Default: unified mode (ensure server, then connect client)
		return handleUnifiedMode(ctx, paths, lifecycle.ServerOptions{
			SocketPath:   *socketPath,
			SnapshotPath: *snapshotPath,
			DefaultApp:   *defaultApp,
			VerboseLogs:  *verboseLogs,
			LogFilePath:  paths.ServerLogPath,
			Title:        *title,
		}, clientrt.Options{
			Socket:     *socketPath,
			Reconnect:  *reconnect,
			PanicLog:   *panicLog,
			ClientName: *clientName,
		})
	}
}

func handleUnifiedMode(ctx context.Context, paths *Paths, srvOpts lifecycle.ServerOptions, clientOpts clientrt.Options) error {
	// Ensure PIDFilePath is populated so daemon.Start can forward it to
	// the server child via --pid-file. Fall back to paths.PIDPath to
	// keep the two sources of truth in sync.
	if srvOpts.PIDFilePath == "" {
		srvOpts.PIDFilePath = paths.PIDPath
	}

	// Boot splash takes the screen for the duration of the bootstrap
	// (server health check + protocol handshake + first snapshot).
	// Cold starts after a daemon stop currently spend 10+ seconds in
	// WAL replay before the socket is healthy; without the splash the
	// terminal looks frozen the whole time. The splash is a lightweight
	// pre-connect harness that Plan F (#199 session recovery) will
	// extend with a session picker.
	screen, splashErr := tcell.NewScreen()
	if splashErr != nil {
		// Fall back to the legacy stdout-only path if we can't even
		// allocate a screen — better the user sees the original
		// "Starting texelation server..." prints than nothing. Log
		// and surface to stderr so a misconfigured environment
		// (missing TTY, unset $TERM, etc.) doesn't get hidden behind
		// a downstream supervisor error.
		log.Printf("boot splash unavailable (tcell.NewScreen: %v); falling back to text bootstrap", splashErr)
		fmt.Fprintf(os.Stderr, "Note: boot splash unavailable (%v); using text mode\n", splashErr)
		return runUnifiedWithoutSplash(ctx, paths, srvOpts, clientOpts)
	}
	if err := screen.Init(); err != nil {
		log.Printf("boot splash unavailable (screen.Init: %v); falling back to text bootstrap", err)
		fmt.Fprintf(os.Stderr, "Note: boot splash unavailable (%v); using text mode\n", err)
		return runUnifiedWithoutSplash(ctx, paths, srvOpts, clientOpts)
	}
	defer screen.Fini()
	// Hide the cursor immediately — without this, a blinking cursor
	// shows in the top-left corner during the splash. clientrt.Run
	// will re-assert HideCursor on handoff but doing it here removes
	// the visible artifact in the bootstrap window.
	screen.HideCursor()

	splashApp := boot.New("Texelation")
	splashApp.SetStage(boot.StageStarting)
	splashRunner := boot.NewRunner(screen, splashApp)
	splashRunner.Start()
	splashStopped := false
	stopSplash := func() {
		if splashStopped {
			return
		}
		splashStopped = true
		splashRunner.Stop()
	}
	defer stopSplash()

	// Supervisor messages flow into the splash's detail line. They are
	// transient by design — the next stage transition clears them.
	health := lifecycle.NewSocketHealthChecker(2 * time.Second)
	pidFile := lifecycle.NewPIDFile(paths.PIDPath)
	daemon := lifecycle.NewDaemonManager(pidFile, srvOpts.SocketPath, health)
	supCfg := lifecycle.DefaultSupervisorConfig()
	// Cold-start WAL replay can run 10+ seconds with deep scrollback;
	// the default 5s ceiling fails the health check before the socket
	// is reachable. With the splash now visible, an honest long wait
	// is better UX than a false "failed to start" — bump the budget.
	supCfg.StartupWait = 60 * time.Second
	supCfg.Reporter = func(msg string) {
		splashApp.SetDetail(msg)
		splashRunner.Wake()
	}
	supervisor := lifecycle.NewSupervisor(daemon, health, pidFile, supCfg)

	result, err := supervisor.EnsureRunning(ctx, srvOpts)
	if err != nil {
		// Log the full error before squeezing it into the splash
		// detail line — drawCenteredText truncates to the box's
		// inner width (~32 cols), so wrapped/multi-line error
		// chains get clipped on screen but stay legible in the log.
		log.Printf("ensure server running failed: %v", err)
		splashApp.SetStage(boot.StageError)
		splashApp.SetDetail(err.Error())
		splashRunner.Wake()
		// Hold the error message on screen briefly so the user can
		// read it before tcell's Fini wipes the dialog. ctx-aware
		// so a Ctrl-C / context cancellation collapses the wait
		// instead of forcing the user to sit through it.
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-ctx.Done():
		}
		return fmt.Errorf("ensure server running: %w", err)
	}

	// If server was restarted due to being unresponsive, show notification
	if result.WasRestarted {
		clientOpts.ShowRestartNotification = true
	}

	// Map clientrt's status callbacks onto the splash. Once StageReady
	// fires the runtime has painted its first frame on the same screen
	// and we can stop the splash without leaving blank cells.
	clientOpts.Screen = screen
	clientOpts.OnStatus = func(stage clientrt.Stage, detail string) {
		switch stage {
		case clientrt.StageConnecting:
			splashApp.SetStage(boot.StageConnecting)
		case clientrt.StageResuming:
			splashApp.SetStage(boot.StageResuming)
		case clientrt.StageReady:
			stopSplash()
			return
		}
		if detail != "" {
			splashApp.SetDetail(detail)
		}
		splashRunner.Wake()
	}

	return clientrt.Run(clientOpts)
}

// runUnifiedWithoutSplash is the pre-splash bootstrap path, retained
// as a fallback for environments where tcell.NewScreen / Init fails
// (very rare — typically only when stdin/stdout aren't a tty). It
// preserves the original behaviour: stdout messages from the
// supervisor, then handing control to clientrt.Run with no pre-init
// screen.
func runUnifiedWithoutSplash(ctx context.Context, paths *Paths, srvOpts lifecycle.ServerOptions, clientOpts clientrt.Options) error {
	health := lifecycle.NewSocketHealthChecker(2 * time.Second)
	pidFile := lifecycle.NewPIDFile(paths.PIDPath)
	daemon := lifecycle.NewDaemonManager(pidFile, srvOpts.SocketPath, health)
	// Mirror the splash path's 60s startup ceiling. Cold-start WAL
	// replay can run 10+ seconds with deep scrollback, and the
	// fallback path shouldn't be a degraded mode where the same
	// daemon spuriously fails to come up just because the splash
	// couldn't initialise.
	supCfg := lifecycle.DefaultSupervisorConfig()
	supCfg.StartupWait = 60 * time.Second
	supervisor := lifecycle.NewSupervisor(daemon, health, pidFile, supCfg)

	result, err := supervisor.EnsureRunning(ctx, srvOpts)
	if err != nil {
		return fmt.Errorf("ensure server running: %w", err)
	}
	if result.WasStarted {
		fmt.Printf("Server started (PID %d)\n", result.PID)
		fmt.Printf("  Socket: %s\n", srvOpts.SocketPath)
		fmt.Printf("  Logs: %s\n", paths.ServerLogPath)
	}
	if result.WasRestarted {
		clientOpts.ShowRestartNotification = true
	}
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
	if opts.PIDFilePath != "" {
		// Forward the PID file path so texel-server can take the
		// exclusive flock that the supervisor uses as its liveness
		// signal.
		args = append(args, "--pid-file", opts.PIDFilePath)
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
	// Enumerate what will be deleted
	fmt.Println("WARNING: This will delete ALL saved state:")
	fmt.Printf("  - %s/ (scrollback, search indices, env, history, storage, logs, snapshot, server-side sessions)\n", paths.ConfigDir)

	// Plan D's client-side persistence lives outside ConfigDir under
	// $XDG_STATE_HOME (defaults to ~/.local/state/texelation/client/).
	// Wipe it too so --reset-state truly produces a clean slate —
	// otherwise the client retains its sessionID + saved viewports
	// across resets, defeating the intent.
	clientStateDir := resolveClientStateDir()

	// Find legacy env/history files still in ~/
	homeDir, _ := os.UserHomeDir()
	var legacyFiles []string
	if homeDir != "" {
		envMatches, _ := filepath.Glob(filepath.Join(homeDir, ".texel-env-*"))
		histMatches, _ := filepath.Glob(filepath.Join(homeDir, ".texel-history-*"))
		legacyFiles = append(envMatches, histMatches...)
	}
	if len(legacyFiles) > 0 {
		fmt.Printf("  - ~/%s, ~/%s (%d legacy pane files)\n", ".texel-env-*", ".texel-history-*", len(legacyFiles))
	}

	if clientStateDir != "" {
		if _, err := os.Stat(clientStateDir); err == nil {
			fmt.Printf("  - %s/ (Plan D client persistence: sessionID, viewports)\n", clientStateDir)
		}
	}

	fmt.Printf("  - %s (socket)\n", socketPath)
	fmt.Println()
	fmt.Println("User configuration (~/.config/texelation/) will NOT be deleted.")
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

	removed := wipeResetStatePaths(os.Stderr, os.Stdout, paths.ConfigDir, clientStateDir, socketPath, legacyFiles)

	fmt.Printf("\nState reset complete (%d items removed)\n", removed)
	return nil
}

// wipeResetStatePaths removes all state covered by --reset-state:
//   - configDir (entire ~/.texelation/ tree, including Plan D2's
//     sessions/ subdir written by atomicjson)
//   - legacy per-pane files (~/.texel-env-*, ~/.texel-history-*)
//   - clientStateDir (Plan D's client-side sessionID + viewports)
//   - the daemon's Unix socket
//
// Returns the count of paths successfully removed. Extracted from
// handleResetState so the wipe can be exercised by tests without
// stdin/daemon plumbing (Plan D2 17.E).
func wipeResetStatePaths(stderr, stdout interface {
	Write(p []byte) (n int, err error)
}, configDir, clientStateDir, socketPath string, legacyFiles []string) int {
	removed := 0

	// Remove the entire state directory (~/.texelation/)
	// This covers: scrollback/*.hist3, scrollback/*.index.db, storage/,
	// texelbrowse/, snapshot.json, server.log, texelation.pid, AND
	// Plan D2's sessions/ subdir (atomicjson session-state files).
	if err := os.RemoveAll(configDir); err != nil {
		fmt.Fprintf(stderr, "  warning: failed to remove %s: %v\n", configDir, err)
	} else {
		fmt.Fprintf(stdout, "  removed %s/\n", configDir)
		removed++
	}

	// Remove legacy per-pane files (~/.texel-env-*, ~/.texel-history-*)
	for _, f := range legacyFiles {
		if err := os.Remove(f); err != nil {
			fmt.Fprintf(stderr, "  warning: failed to remove %s: %v\n", f, err)
		} else {
			removed++
		}
	}
	if len(legacyFiles) > 0 {
		fmt.Fprintf(stdout, "  removed %d legacy pane files\n", len(legacyFiles))
	}

	// Remove Plan D's client persistence directory if it exists.
	// Without this, the client retains its sessionID and saved
	// PaneViewports across --reset-state, which has caused confusing
	// "broken state survives reset" behavior in manual e2e tests.
	if clientStateDir != "" {
		if _, err := os.Stat(clientStateDir); err == nil {
			if err := os.RemoveAll(clientStateDir); err != nil {
				fmt.Fprintf(stderr, "  warning: failed to remove %s: %v\n", clientStateDir, err)
			} else {
				fmt.Fprintf(stdout, "  removed %s/\n", clientStateDir)
				removed++
			}
		}
	}

	// Remove socket file
	if err := os.Remove(socketPath); err == nil {
		fmt.Fprintf(stdout, "  removed %s\n", socketPath)
		removed++
	}

	return removed
}

// resolveClientStateDir returns the path that holds Plan D's client
// persistence files: $XDG_STATE_HOME/texelation/client/, defaulting to
// ~/.local/state/texelation/client/. Mirrors the logic in
// internal/runtime/client/persistence.go ResolvePath. Returns empty
// string if the home directory cannot be determined.
func resolveClientStateDir() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "texelation", "client")
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

	// Plan D2 17.D: report cross-restart session-persistence state so
	// a permissions issue (e.g. sessions/ owned by root after a sudo
	// misadventure) doesn't silently break resume across restarts.
	// We check by inspecting <ConfigDir>/sessions/ since the running
	// daemon doesn't expose Manager.Stats over the socket today.
	sessionsDir := filepath.Join(paths.ConfigDir, "sessions")
	if info, err := os.Stat(sessionsDir); err == nil && info.IsDir() {
		entries, readErr := os.ReadDir(sessionsDir)
		if readErr != nil {
			fmt.Printf("  Persistence: directory unreadable (%v)\n", readErr)
		} else {
			count := 0
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
					count++
				}
			}
			fmt.Printf("  Persistence: enabled (%d persisted session(s) at %s)\n", count, sessionsDir)
		}
	} else if os.IsNotExist(err) {
		fmt.Printf("  Persistence: no sessions directory yet (%s)\n", sessionsDir)
	} else if err != nil {
		fmt.Printf("  Persistence: cannot stat %s: %v\n", sessionsDir, err)
	}

	return nil
}
