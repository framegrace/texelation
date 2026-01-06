// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/devshell/runner_test.go
// Summary: Exercises devshell runner behaviour to ensure the standalone harness remains reliable.
// Usage: Executed during `go test` to guard against regressions.

package devshell_test

import (
	"errors"
	texelcore "github.com/framegrace/texelui/core"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/internal/devshell"
)

type stubApp struct {
	mu           sync.Mutex
	renderCount  int
	resizes      [][2]int
	keys         []*tcell.EventKey
	stopCalled   bool
	stopCh       chan struct{}
	runStarted   chan struct{}
	runCompleted chan struct{}
	refresh      chan<- bool
	runErr       error
}

func newStubApp() *stubApp {
	return &stubApp{
		stopCh:       make(chan struct{}),
		runStarted:   make(chan struct{}),
		runCompleted: make(chan struct{}),
	}
}

func (a *stubApp) Run() error {
	close(a.runStarted)
	<-a.stopCh
	close(a.runCompleted)
	return a.runErr
}

func (a *stubApp) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopCalled {
		return
	}
	a.stopCalled = true
	close(a.stopCh)
}

func (a *stubApp) Resize(cols, rows int) {
	a.mu.Lock()
	a.resizes = append(a.resizes, [2]int{cols, rows})
	a.mu.Unlock()
}

func (a *stubApp) Render() [][]texelcore.Cell {
	a.mu.Lock()
	a.renderCount++
	a.mu.Unlock()
	return [][]texelcore.Cell{{{Ch: 'X'}}}
}

func (a *stubApp) HandleKey(ev *tcell.EventKey) {
	a.mu.Lock()
	a.keys = append(a.keys, ev)
	a.mu.Unlock()
}

func (a *stubApp) SetRefreshNotifier(ch chan<- bool) { a.refresh = ch }
func (a *stubApp) GetTitle() string                  { return "stub" }

func (a *stubApp) waitRunStarted(t *testing.T) {
	t.Helper()
	select {
	case <-a.runStarted:
	case <-time.After(time.Second):
		t.Fatal("app.Run was not invoked")
	}
}

func (a *stubApp) waitRunCompleted(t *testing.T) {
	t.Helper()
	select {
	case <-a.runCompleted:
	case <-time.After(time.Second):
		t.Fatal("app was not stopped")
	}
}

func (a *stubApp) renderCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.renderCount
}

func (a *stubApp) lastResize() (int, int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.resizes) == 0 {
		return 0, 0, false
	}
	last := a.resizes[len(a.resizes)-1]
	return last[0], last[1], true
}

func (a *stubApp) recordedKeys() []*tcell.EventKey {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*tcell.EventKey, len(a.keys))
	copy(out, a.keys)
	return out
}

func (a *stubApp) requestRefresh() {
	if a.refresh == nil {
		return
	}
	select {
	case a.refresh <- true:
	default:
	}
}

func TestRunHandlesInputRefreshAndShutdown(t *testing.T) {
	defer devshell.SetScreenFactory(nil)

	screen := tcell.NewSimulationScreen("UTF-8")
	devshell.SetScreenFactory(func() (tcell.Screen, error) {
		return screen, nil
	})

	app := newStubApp()
	builder := func(args []string) (texelcore.App, error) {
		if len(args) != 1 || args[0] != "demo" {
			return nil, errors.New("unexpected args")
		}
		return app, nil
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- devshell.Run(builder, []string{"demo"})
	}()

	app.waitRunStarted(t)

	// Initial draw should have rendered at least once.
	if calls := app.renderCalls(); calls == 0 {
		t.Fatalf("expected initial render, got %d", calls)
	}

	// Trigger a refresh and expect another render.
	app.requestRefresh()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if app.renderCalls() > 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if app.renderCalls() <= 1 {
		t.Fatalf("expected render after refresh, got %d", app.renderCalls())
	}

	// Send key event and verify it reaches the app.
	screen.PostEvent(tcell.NewEventKey(tcell.KeyRune, 'x', 0))
	waitFor(func() bool {
		keys := app.recordedKeys()
		return len(keys) > 0 && keys[0].Rune() == 'x'
	}, 500*time.Millisecond, t, "key press to be handled")

	// Send resize and confirm app receives new size.
	screen.PostEvent(tcell.NewEventResize(50, 12))
	waitFor(func() bool {
		w, h, ok := app.lastResize()
		return ok && w == 50 && h == 12
	}, 500*time.Millisecond, t, "resize event to be handled")

	// Exit via Ctrl-C.
	screen.PostEvent(tcell.NewEventKey(tcell.KeyCtrlC, 0, 0))

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not exit after Ctrl-C")
	}

	app.waitRunCompleted(t)
	if !app.stopCalled {
		t.Fatal("app.Stop was not invoked")
	}
}

func TestRunAppUnknownReturnsError(t *testing.T) {
	if err := devshell.RunApp("does-not-exist", nil); err == nil {
		t.Fatal("expected error for unknown app")
	}
}

func waitFor(cond func() bool, timeout time.Duration, t *testing.T, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", msg)
}
