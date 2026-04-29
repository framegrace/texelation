// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texel-server/main.go
// Summary: Implements main capabilities for the server CLI harness.
// Usage: Executed by operators to start the production server that manages sessions.
// Notes: Focuses on wiring flags and lifecycle around the internal runtime.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	texelcore "github.com/framegrace/texelui/core"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"

	_ "github.com/framegrace/texelation/apps/configeditor"
	_ "github.com/framegrace/texelation/apps/help"
	_ "github.com/framegrace/texelation/apps/texeluidemo"
	"github.com/framegrace/texelation/apps/launcher"
	"github.com/framegrace/texelation/apps/statusbar"
	"github.com/framegrace/texelation/apps/texelterm"
	lifecyclepkg "github.com/framegrace/texelation/cmd/texelation/lifecycle"
	"github.com/framegrace/texelation/config"
	"github.com/framegrace/texelation/internal/keybind"
	"github.com/framegrace/texelation/internal/runtime/server"
	runtimeadapter "github.com/framegrace/texelation/internal/runtimeadapter"
	"github.com/framegrace/texelation/registry"
	"github.com/framegrace/texelation/texel"
	"github.com/framegrace/texelui/theme"
)

func main() {
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)

	socketPath := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	pidFilePath := flag.String("pid-file", "", "PID file path; if set, the server writes its PID here and holds an exclusive flock for its lifetime")
	title := flag.String("title", "Texel Server", "Title for the main pane")
	snapshotPath := flag.String("snapshot", "", "Path to persist pane snapshots (default: ~/.texelation/snapshot.json)")
	fromScratch := flag.Bool("from-scratch", false, "Start from scratch, ignoring any saved snapshot")
	cpuProfile := flag.String("pprof-cpu", "", "Write CPU profile to file")
	memProfile := flag.String("pprof-mem", "", "Write heap profile to file on exit")
	pprofAddr := flag.String("pprof-http", "", "Enable live pprof at address (e.g. localhost:6060)")
	verboseLogs := flag.Bool("verbose-logs", false, "Enable verbose server logging")
	defaultApp := flag.String("default-app", "", "Default app for new panes (launcher, texelterm, help) - overrides config file")
	flag.Parse()

	// Acquire exclusive flock on the PID file before any other setup.
	// This is the canonical "a server is alive" signal used by the
	// supervisor in cmd/texelation/lifecycle. Holding the lock for the
	// entire process lifetime means slow shutdowns (e.g. 20s WAL flushes)
	// still look "alive" to the supervisor and are not killed mid-flush.
	// The OS releases the lock automatically on process exit, even on
	// crash, so stale PID files are self-healing.
	var pidLock lifecyclepkg.PIDLock
	if *pidFilePath != "" {
		pf := lifecyclepkg.NewPIDFile(*pidFilePath)
		lock, err := pf.AcquireExclusiveLock(os.Getpid())
		if err != nil {
			fmt.Fprintf(os.Stderr, "texel-server: cannot acquire PID lock at %s: %v\n", *pidFilePath, err)
			os.Exit(1)
		}
		pidLock = lock
		defer func() {
			_ = pidLock.Close()
			// Best-effort remove; OS already released the lock so a
			// new server can start immediately after us regardless.
			_ = os.Remove(*pidFilePath)
		}()
	}

	server.SetVerboseLogging(*verboseLogs)

	cfg := config.System()
	if err := config.Err(); err != nil {
		log.Printf("Warning: Failed to load system config: %v", err)
	}

	// Command-line flag overrides config file.
	if *defaultApp == "" {
		*defaultApp = cfg.GetString("", "defaultApp", "launcher")
	}

	if *pprofAddr != "" {
		go func() {
			log.Printf("pprof HTTP server at http://%s/debug/pprof/", *pprofAddr)
			if err := http.ListenAndServe(*pprofAddr, nil); err != nil {
				log.Printf("pprof HTTP failed: %v", err)
			}
		}()
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create CPU profile: %v\n", err)
			os.Exit(1)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start CPU profile: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		}()
	}

	manager := server.NewManager()

	simScreen := tcell.NewSimulationScreen("ansi")
	driver := texel.NewTcellScreenDriver(simScreen)
	lifecycle := &texel.LocalAppLifecycle{}

	defaultShell := os.Getenv("SHELL")
	if defaultShell == "" {
		defaultShell = "/bin/bash"
	}

	var shellSeq atomic.Int64
	var serverKB *keybind.Registry // set after desktop creation, captured by closures
	shellFactory := func() texelcore.App {
		id := shellSeq.Add(1)
		title := fmt.Sprintf("%s-%d", *title, id)
		app := texelterm.New(title, defaultShell)
		if tt, ok := app.(*texelterm.TexelTerm); ok && serverKB != nil {
			tt.SetKeybindings(serverKB)
		}
		return app
	}

	// Create desktop first (no help app yet - we'll set it after registry is ready)
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, *defaultApp, lifecycle)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create desktop: %v\n", err)
		os.Exit(1)
	}
	go desktop.Run()

	// Register wrapper factory for texelterm
	// This allows wrapper apps to create texelterm instances with custom commands
	desktop.Registry().RegisterWrapperFactory("texelterm", func(m *registry.Manifest) interface{} {
		command := m.Command
		if len(m.Args) > 0 {
			command = command + " " + strings.Join(m.Args, " ")
		}
		return texelterm.New(m.DisplayName, command)
	})

	// Register built-in apps provided by init-time registration.
	registry.RegisterBuiltIns(desktop.Registry())
	desktop.Registry().SetAppWrapper(runtimeadapter.WrapForRegistry(desktop.Registry()))

	// Load keybindings and wire to desktop engine and shell factory.
	serverKB = loadServerKeybindings()
	desktop.SetKeybindings(serverKB)

	// Register snapshot factory for launcher
	desktop.RegisterSnapshotFactory("launcher", func(title string, config map[string]interface{}) texelcore.App {
		return launcher.New(desktop.Registry())
	})

	// Register snapshot factory for texelterm
	desktop.RegisterSnapshotFactory("texelterm", func(title string, config map[string]interface{}) texelcore.App {
		command, _ := config["command"].(string)
		if command == "" {
			command = defaultShell
		}
		app := texelterm.New(title, command)
		if tt, ok := app.(*texelterm.TexelTerm); ok && serverKB != nil {
			tt.SetKeybindings(serverKB)
		}
		return app
	})

	// Check if we'll be loading from a snapshot - if so, don't create the initial app
	// The snapshot restore will create the proper apps
	snapshotExists := false
	if !*fromScratch {
		snapPath := *snapshotPath
		if snapPath == "" {
			if homeDir, err := os.UserHomeDir(); err == nil {
				snapPath = filepath.Join(homeDir, ".texelation", "snapshot.json")
			}
		}
		if snapPath != "" {
			if _, err := os.Stat(snapPath); err == nil {
				snapshotExists = true
				log.Printf("Snapshot file exists, deferring initial app creation")
				desktop.InitAppName = "" // Don't create initial app - snapshot will restore it
			}
		}
	}

	// Create initial workspace (with or without default app based on snapshot existence)
	desktop.SwitchToWorkspace(1)

	// Restore InitAppName for future workspace creation (if user opens new workspace)
	if snapshotExists {
		desktop.InitAppName = *defaultApp
	}

	statusApp := desktop.Registry().CreateApp("statusbar", nil)
	if sb, ok := statusApp.(*statusbar.StatusBarApp); ok {
		sb.SetActions(desktop)
		sb.UI().ClientSideAnimations = true
	}
	desktop.AddStatusPane(statusApp.(texel.App), texel.SideTop, 2)

	srv := server.NewServer(*socketPath, manager)
	metrics := server.NewFocusMetrics(log.Default())
	srv.SetFocusMetrics(metrics)
	statsLogger := server.NewSessionStatsLogger(log.Default())
	server.SetSessionStatsObserver(statsLogger)
	publishLogger := server.NewPublishLogger(log.Default())

	// Enable snapshots BEFORE SetEventSink - SetEventSink triggers applyBootSnapshot
	// which needs the snapshot store to be set first
	if !*fromScratch {
		snapPath := *snapshotPath
		if snapPath == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				log.Printf("Warning: Could not get home directory: %v", err)
			} else {
				configDir := filepath.Join(homeDir, ".texelation")
				if err := os.MkdirAll(configDir, 0755); err != nil {
					log.Printf("Warning: Could not create config directory: %v", err)
				} else {
					snapPath = filepath.Join(configDir, "snapshot.json")
				}
			}
		}
		if snapPath != "" {
			store := server.NewSnapshotStore(snapPath)
			srv.SetSnapshotStore(store, 5*time.Second)
			log.Printf("Session persistence enabled: %s", snapPath)
			// Plan D2: cross-restart session/viewport persistence.
			// MUST run before srv.Start so the persisted-session index
			// is populated before any client can send MsgResumeRequest.
			if err := manager.EnablePersistence(filepath.Dir(snapPath), 250*time.Millisecond); err != nil {
				log.Printf("warning: could not enable persistence: %v", err)
				// Plan D2 17.D: a single warning above is easy to miss
				// in a busy boot log. Emit a follow-up "DISABLED" line
				// and verify via Manager.Stats so operators have an
				// observable surface for the silent-failure mode.
				stats := manager.Stats()
				if !stats.PersistEnabled {
					log.Printf("[BOOT] PERSISTENCE DISABLED: cross-restart session resume will not work this process lifetime")
				}
			}
		}
	} else {
		log.Println("Starting from scratch (--from-scratch flag set)")
	}

	sink := server.NewDesktopSink(desktop)
	srv.SetEventSink(sink)
	srv.SetPublisherFactory(func(sess *server.Session) *server.DesktopPublisher {
		publisher := server.NewDesktopPublisher(desktop, sess)
		sink.SetPublisher(publisher)
		publisher.SetObserver(publishLogger)
		return publisher
	})

	go func() {
		if err := srv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	fmt.Printf("Texel server harness listening on %s\n", *socketPath)
	fmt.Println("Use the integration test client or proto harness to connect and send key events.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for {
		sig := <-sigCh
		if sig == syscall.SIGHUP {
			log.Println("Received SIGHUP, reloading configuration...")
			if err := config.Reload(); err != nil {
				log.Printf("Failed to reload system/app config: %v", err)
			}
			if err := theme.Reload(); err != nil {
				log.Printf("Failed to reload theme: %v", err)
			} else {
				log.Println("Theme reloaded successfully.")
				desktop.ForceRefresh()
			}
			continue
		}
		// SIGINT or SIGTERM -> Exit
		break
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Stop(ctx)
	desktop.Close()

	if *memProfile != "" {
		f, err := os.Create(*memProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create heap profile: %v\n", err)
		} else {
			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write heap profile: %v\n", err)
			}
			_ = f.Close()
		}
	}

	fmt.Println("Server stopped")
}

// loadServerKeybindings loads the keybinding registry from the user's config.
func loadServerKeybindings() *keybind.Registry {
	preset := "auto"
	var extraPreset string
	var overrides map[string][]string

	home, err := os.UserHomeDir()
	if err == nil {
		data, err := os.ReadFile(filepath.Join(home, ".config", "texelation", "keybindings.json"))
		if err == nil {
			var cfg struct {
				Preset      string              `json:"preset"`
				ExtraPreset string              `json:"extraPreset"`
				Actions     map[string][]string `json:"actions"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				if cfg.Preset != "" {
					preset = cfg.Preset
				}
				extraPreset = cfg.ExtraPreset
				overrides = cfg.Actions
			}
		}
	}

	return keybind.NewRegistry(preset, extraPreset, overrides)
}
