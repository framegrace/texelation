// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/app_lifecycle.go
// Summary: Implements app lifecycle capabilities for the core desktop engine.
// Usage: Used throughout the project to implement app lifecycle inside the desktop and panes.

package texel

import "sync"

// LocalAppLifecycle runs apps in-process on the local machine. It spawns each
// app's Run loop in a goroutine and delegates Stop calls directly.
type LocalAppLifecycle struct {
	wg sync.WaitGroup

	// onExitDoneMu protects onExitDoneByApp. Each running app gets a
	// channel closed *after* its onExit callback returns. AttachApp uses
	// this to wait for the outgoing app's exit handler to finish before
	// rewriting pane.app, which the handler reads to perform staleness
	// checks. Without this barrier, the swap races with the handler.
	onExitDoneMu    sync.Mutex
	onExitDoneByApp map[App]chan struct{}
}

// StartApp launches the app's Run method asynchronously.
func (l *LocalAppLifecycle) StartApp(app App, onExit func(error)) {
	onExitDone := make(chan struct{})
	l.onExitDoneMu.Lock()
	if l.onExitDoneByApp == nil {
		l.onExitDoneByApp = make(map[App]chan struct{})
	}
	l.onExitDoneByApp[app] = onExitDone
	l.onExitDoneMu.Unlock()
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		defer func() {
			l.onExitDoneMu.Lock()
			delete(l.onExitDoneByApp, app)
			l.onExitDoneMu.Unlock()
			close(onExitDone)
		}()
		err := app.Run()
		if onExit != nil {
			onExit(err)
		}
	}()
}

// StopApp forwards the stop request to the app implementation. It does not
// wait for the goroutine to exit — pane teardown calls StopApp from inside
// the goroutine itself (via handleAppExit -> Close), so a wait would
// deadlock. WaitForExit is the explicit barrier for callers that need
// synchronization.
func (l *LocalAppLifecycle) StopApp(app App) {
	app.Stop()
}

// WaitForExit blocks until the app's Run goroutine and onExit callback
// have returned, then drops the bookkeeping. Safe to call after StopApp.
// If the app was never started or has already been waited on, returns
// immediately. Callers must not invoke this from inside the app's own
// goroutine — that would self-deadlock.
func (l *LocalAppLifecycle) WaitForExit(app App) {
	l.onExitDoneMu.Lock()
	done := l.onExitDoneByApp[app]
	l.onExitDoneMu.Unlock()
	if done != nil {
		<-done
	}
}

// Wait blocks until all started apps have exited. Primarily useful for tests.
func (l *LocalAppLifecycle) Wait() {
	l.wg.Wait()
}

// NoopAppLifecycle is a helper used in tests where the app run loop is stubbed
// out and should not be invoked.
type NoopAppLifecycle struct{}

func (NoopAppLifecycle) StartApp(app App, _ func(error)) {}
func (NoopAppLifecycle) StopApp(app App)                 {}
