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
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/help"
	"texelation/apps/launcher"
	"texelation/apps/statusbar"
	"texelation/apps/texelterm"
	"texelation/config"
	"texelation/internal/runtime/server"
	"texelation/registry"
	"texelation/texel"
	"texelation/texel/theme"
)

func main() {
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)

	socketPath := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	title := flag.String("title", "Texel Server", "Title for the main pane")
	snapshotPath := flag.String("snapshot", "", "Path to persist pane snapshots (default: ~/.texelation/snapshot.json)")
	fromScratch := flag.Bool("from-scratch", false, "Start from scratch, ignoring any saved snapshot")
	cpuProfile := flag.String("pprof-cpu", "", "Write CPU profile to file")
	memProfile := flag.String("pprof-mem", "", "Write heap profile to file on exit")
	verboseLogs := flag.Bool("verbose-logs", false, "Enable verbose server logging")
	defaultApp := flag.String("default-app", "", "Default app for new panes (launcher, texelterm, help) - overrides config file")
	flag.Parse()

	server.SetVerboseLogging(*verboseLogs)

	// Load configuration from file
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: Failed to load config: %v, using defaults", err)
		cfg = config.Default()
	}

	// Command-line flag overrides config file
	if *defaultApp == "" {
		*defaultApp = cfg.DefaultApp
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
	shellFactory := func() texel.App {
		id := shellSeq.Add(1)
		title := fmt.Sprintf("%s-%d", *title, id)
		return texelterm.New(title, defaultShell)
	}

	// Create desktop first (no help app yet - we'll set it after registry is ready)
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, *defaultApp, lifecycle)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create desktop: %v\n", err)
		os.Exit(1)
	}

	// Register wrapper factory for texelterm
	// This allows wrapper apps to create texelterm instances with custom commands
	desktop.Registry().RegisterWrapperFactory("texelterm", func(m *registry.Manifest) interface{} {
		command := m.Command
		if len(m.Args) > 0 {
			command = command + " " + strings.Join(m.Args, " ")
		}
		return texelterm.New(m.DisplayName, command)
	})

	// Register launcher in registry
	desktop.Registry().RegisterBuiltIn("launcher", func() interface{} {
		return launcher.New(desktop.Registry())
	})

	// Register snapshot factory for launcher
	desktop.RegisterSnapshotFactory("launcher", func(title string, config map[string]interface{}) texel.App {
		return launcher.New(desktop.Registry())
	})

	// Register help app
	desktop.Registry().RegisterBuiltIn("help", func() interface{} {
		return help.NewHelpApp()
	})

	// Register snapshot factory for texelterm
	desktop.RegisterSnapshotFactory("texelterm", func(title string, config map[string]interface{}) texel.App {
		command, _ := config["command"].(string)
		if command == "" {
			command = defaultShell
		}
		return texelterm.New(title, command)
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

	status := statusbar.New()
	desktop.AddStatusPane(status, texel.SideTop, 1)

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
