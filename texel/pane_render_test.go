// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/pane_render_test.go
// Summary: Regression tests for setDimensions / SetResizing deferral. While
// the user is dragging a pane border (IsResizing=true), the visible pane
// rectangle must update on every motion event but the embedded app's
// Resize must be deferred until the drag releases. This avoids a per-pixel
// terminal grid reflow that visually flickers at the moving edge.

package texel

import (
	"sync"
	"testing"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

// resizeCountingApp is a minimal App that records every Resize call so the
// tests can assert deferral while IsResizing=true and the single deferred
// fire on SetResizing(false). Mirrors the recordingApp shape from
// internal/runtime/server/desktop_sink_test.go but tracks Resize instead
// of HandleKey.
type resizeCountingApp struct {
	mu    sync.Mutex
	calls []resizeCall
	title string
}

type resizeCall struct {
	cols int
	rows int
}

func (a *resizeCountingApp) Run() error { return nil }
func (a *resizeCountingApp) Stop()      {}
func (a *resizeCountingApp) Resize(cols, rows int) {
	a.mu.Lock()
	a.calls = append(a.calls, resizeCall{cols: cols, rows: rows})
	a.mu.Unlock()
}
func (a *resizeCountingApp) Render() [][]texelcore.Cell     { return [][]texelcore.Cell{{}} }
func (a *resizeCountingApp) GetTitle() string               { return a.title }
func (a *resizeCountingApp) HandleKey(*tcell.EventKey)      {}
func (a *resizeCountingApp) SetRefreshNotifier(chan<- bool) {}

func (a *resizeCountingApp) snapshot() []resizeCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]resizeCall, len(a.calls))
	copy(out, a.calls)
	return out
}

// newDeferralTestPane builds a bare pane with a resizeCountingApp attached
// directly to p.app — bypassing AttachApp so we can observe Resize calls
// driven only by setDimensions / SetResizing without the AttachApp-time
// initial Resize muddying the count.
func newDeferralTestPane() (*pane, *resizeCountingApp) {
	p := newPane(nil)
	app := &resizeCountingApp{title: "mock"}
	p.setApp(app)
	return p, app
}

func TestSetDimensions_DefersAppResizeWhileResizing(t *testing.T) {
	p, app := newDeferralTestPane()
	p.IsResizing = true

	p.setDimensions(0, 0, 10, 10)
	p.setDimensions(0, 0, 20, 20)

	calls := app.snapshot()
	if len(calls) != 0 {
		t.Fatalf("expected 0 Resize calls while IsResizing=true, got %d: %+v", len(calls), calls)
	}
	if p.absX1 != 20 || p.absY1 != 20 {
		t.Fatalf("expected abs coords to track latest setDimensions (20,20), got (%d,%d)", p.absX1, p.absY1)
	}
}

func TestSetDimensions_AppResizesWhenNotResizing(t *testing.T) {
	p, app := newDeferralTestPane()
	p.IsResizing = false

	p.setDimensions(0, 0, 20, 20)

	calls := app.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 Resize call, got %d: %+v", len(calls), calls)
	}
	if calls[0].cols != 18 || calls[0].rows != 18 {
		t.Fatalf("expected Resize(18, 18) for drawable interior, got Resize(%d, %d)", calls[0].cols, calls[0].rows)
	}
}

func TestSetResizing_FalseTriggersDeferredResize(t *testing.T) {
	p, app := newDeferralTestPane()
	p.IsResizing = true

	p.setDimensions(0, 0, 20, 20)
	if got := app.snapshot(); len(got) != 0 {
		t.Fatalf("expected no Resize during drag, got %d calls", len(got))
	}

	p.SetResizing(false)

	calls := app.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 deferred Resize on drag release, got %d: %+v", len(calls), calls)
	}
	if calls[0].cols != 18 || calls[0].rows != 18 {
		t.Fatalf("expected deferred Resize(18, 18), got Resize(%d, %d)", calls[0].cols, calls[0].rows)
	}
}
