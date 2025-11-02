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
}

// StartApp launches the app's Run method asynchronously.
func (l *LocalAppLifecycle) StartApp(app App) {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		_ = app.Run()
	}()
}

// StopApp forwards the stop request to the app implementation.
func (l *LocalAppLifecycle) StopApp(app App) {
	app.Stop()
}

// Wait blocks until all started apps have exited. Primarily useful for tests.
func (l *LocalAppLifecycle) Wait() {
	l.wg.Wait()
}

// NoopAppLifecycle is a helper used in tests where the app run loop is stubbed
// out and should not be invoked.
type NoopAppLifecycle struct{}

func (NoopAppLifecycle) StartApp(app App) {}
func (NoopAppLifecycle) StopApp(app App)  {}
