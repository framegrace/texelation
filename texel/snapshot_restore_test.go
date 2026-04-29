// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texel

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// panickingStopApp implements App but panics in Stop. Used to verify
// the orphan filter in ApplyTreeCapture does not abort boot snapshot
// restore when a factory-built app's Stop misbehaves (Plan D2 17.A).
type panickingStopApp struct {
	title string
}

func (p *panickingStopApp) Run() error                       { return nil }
func (p *panickingStopApp) Stop()                            { panic("orphan Stop must not abort restore") }
func (p *panickingStopApp) Resize(cols, rows int)            {}
func (p *panickingStopApp) Render() [][]Cell                 { return [][]Cell{{{Ch: ' '}}} }
func (p *panickingStopApp) GetTitle() string                 { return p.title }
func (p *panickingStopApp) HandleKey(*tcell.EventKey)        {}
func (p *panickingStopApp) SetRefreshNotifier(c chan<- bool) {}

// passthroughLifecycle calls Stop on the app directly; used so the
// panickingStopApp's panic actually fires through StopApp.
type passthroughLifecycle struct{}

func (passthroughLifecycle) StartApp(_ App, _ func(error)) {}
func (passthroughLifecycle) StopApp(app App) {
	if app != nil {
		app.Stop()
	}
}

// TestStopOrphanAppSafely_RecoversFromPanic confirms the helper
// recovers from a panicking Stop so callers can call it from the
// orphan filter without aborting boot.
func TestStopOrphanAppSafely_RecoversFromPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("stopOrphanAppSafely propagated panic: %v", r)
		}
	}()
	stopOrphanAppSafely(passthroughLifecycle{}, &panickingStopApp{title: "boom"})
}

// TestApplyTreeCapture_OrphanStopPanicDoesNotAbortRestore is the
// integration regression: if a status-orphan pane's app panics in
// Stop during the orphan filter, ApplyTreeCapture must still return
// nil and complete the rest of the restore (Plan D2 17.A).
func TestApplyTreeCapture_OrphanStopPanicDoesNotAbortRestore(t *testing.T) {
	driver := &stubScreenDriver{width: 80, height: 24}
	lifecycle := passthroughLifecycle{}
	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}
	desktop.SwitchToWorkspace(1)

	// Add a real status pane so its title is in statusTitles. Its app
	// is *not* the panicking one — we want the orphan in the snapshot
	// to be filtered out via title match.
	desktop.AddStatusPane(newFakeApp("statuspane-title"), SideTop, 1)

	// Register a snapshot factory for "panic-bar" that returns a
	// panicking-Stop app. The orphan filter checks AppType=="statusbar"
	// or title-in-statusTitles. We use the title path for portability.
	desktop.RegisterSnapshotFactory("panic-bar", func(title string, _ map[string]interface{}) App {
		return &panickingStopApp{title: title}
	})

	// Build a TreeCapture with two panes:
	//  - pane 0: a regular shell pane (kept by orphan filter)
	//  - pane 1: an orphan with title matching the live status pane
	capture := TreeCapture{
		Panes: []PaneSnapshot{
			{
				ID:      [16]byte{0x01},
				Title:   "shell",
				Buffer:  [][]Cell{{{Ch: ' '}}},
				Rect:    Rectangle{X: 0, Y: 1, Width: 80, Height: 23},
				AppType: "",
			},
			{
				ID:      [16]byte{0x02},
				Title:   "statuspane-title", // matches AddStatusPane above
				Buffer:  [][]Cell{{{Ch: ' '}}},
				Rect:    Rectangle{X: 0, Y: 0, Width: 80, Height: 1},
				AppType: "panic-bar",
			},
		},
		WorkspaceRoots: map[int]*TreeNodeCapture{
			1: {
				PaneIndex: 0, // only pane 0 is referenced; pane 1 is the orphan
			},
		},
		ActiveWorkspaceID: 1,
	}

	// Apply; must NOT panic, must NOT return error.
	err = desktop.ApplyTreeCapture(capture)
	if err != nil {
		t.Fatalf("ApplyTreeCapture returned error after orphan-Stop panic: %v", err)
	}

	// Verify the desktop is still usable: the active workspace exists
	// and has a pane.
	if desktop.activeWorkspace == nil {
		t.Fatal("active workspace is nil after restore")
	}
	if desktop.activeWorkspace.tree == nil || desktop.activeWorkspace.tree.Root == nil {
		t.Fatal("workspace tree empty after restore")
	}
}
