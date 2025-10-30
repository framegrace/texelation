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
	"runtime/pprof"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/statusbar"
	"texelation/apps/texelterm"
	"texelation/apps/welcome"
	"texelation/internal/runtime/server"
	"texelation/texel"
)

func main() {
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)

	socketPath := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	title := flag.String("title", "Texel Server", "Title for the main pane")
	snapshotPath := flag.String("snapshot", "", "Optional path to persist pane snapshots")
	cpuProfile := flag.String("pprof-cpu", "", "Write CPU profile to file")
	memProfile := flag.String("pprof-mem", "", "Write heap profile to file on exit")
	flag.Parse()

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

		// Normal terminal (no menu)
		return texelterm.New(title, defaultShell)

	}
	welcomeFactory := func() texel.App {
		return welcome.NewWelcomeApp()
	}

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create desktop: %v\n", err)
		os.Exit(1)
	}

	status := statusbar.New()
	desktop.AddStatusPane(status, texel.SideTop, 1)

	srv := server.NewServer(*socketPath, manager)
	metrics := server.NewFocusMetrics(log.Default())
	srv.SetFocusMetrics(metrics)
	statsLogger := server.NewSessionStatsLogger(log.Default())
	server.SetSessionStatsObserver(statsLogger)
	publishLogger := server.NewPublishLogger(log.Default())
	sink := server.NewDesktopSink(desktop)
	srv.SetEventSink(sink)
	srv.SetPublisherFactory(func(sess *server.Session) *server.DesktopPublisher {
		publisher := server.NewDesktopPublisher(desktop, sess)
		sink.SetPublisher(publisher)
		publisher.SetObserver(publishLogger)
		return publisher
	})
	if *snapshotPath != "" {
		store := server.NewSnapshotStore(*snapshotPath)
		srv.SetSnapshotStore(store, 5*time.Second)
	}

	go func() {
		if err := srv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	fmt.Printf("Texel server harness listening on %s\n", *socketPath)
	fmt.Println("Use the integration test client or proto harness to connect and send key events.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

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
