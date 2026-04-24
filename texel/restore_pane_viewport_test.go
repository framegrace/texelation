// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/restore_pane_viewport_test.go
// Summary: Tests for DesktopEngine.RestorePaneViewport capability dispatch.

package texel

import (
	"testing"
)

// fakeViewportRestorerApp satisfies both App (via embedded fakeApp) and
// ViewportRestorer. Records the last RestoreViewport call for assertion.
type fakeViewportRestorerApp struct {
	*fakeApp
	lastViewBottom int64
	lastWrapSeg    uint16
	lastAutoFollow bool
	called         bool
}

func (f *fakeViewportRestorerApp) RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool) {
	f.called = true
	f.lastViewBottom = viewBottom
	f.lastWrapSeg = wrapSeg
	f.lastAutoFollow = autoFollow
}

// newTestDesktopWithOnePane builds a minimal DesktopEngine with a single pane
// hosting a plain fakeApp (no ViewportRestorer). The caller can swap the app
// via swapPaneApp before exercising RestorePaneViewport.
func newTestDesktopWithOnePane(t *testing.T) *DesktopEngine {
	t.Helper()
	driver := &stubScreenDriver{}
	lifecycle := &trackingLifecycle{}
	shellFactory := func() App { return newFakeApp("shell") }

	de, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("NewDesktopEngineWithDriver: %v", err)
	}

	de.SwitchToWorkspace(1)
	de.activeWorkspace.AddApp(newFakeApp("test-pane"))
	return de
}

// firstPaneIDAndApp returns the ID and app of the first pane found by
// forEachPane. Panics if no pane is present (test harness bug).
func (d *DesktopEngine) firstPaneIDAndApp() ([16]byte, App) {
	var id [16]byte
	var app App
	d.forEachPane(func(p *pane) {
		if app != nil {
			return
		}
		id = p.ID()
		app = p.app
	})
	return id, app
}

// swapPaneApp replaces the app in the pane with the given ID. Used by tests to
// inject a capability-bearing fake without going through the full app lifecycle.
func (d *DesktopEngine) swapPaneApp(id [16]byte, newApp App) {
	d.forEachPane(func(p *pane) {
		if p.ID() == id {
			p.app = newApp
		}
	})
}

func TestDesktopEngine_RestorePaneViewport_ForwardsToApp(t *testing.T) {
	de := newTestDesktopWithOnePane(t)
	id, _ := de.firstPaneIDAndApp()

	fake := &fakeViewportRestorerApp{fakeApp: newFakeApp("restorer")}
	de.swapPaneApp(id, fake)

	ok := de.RestorePaneViewport(id, 42, 1, false)
	if !ok {
		t.Fatalf("RestorePaneViewport: want true (pane found and restorer)")
	}
	if !fake.called {
		t.Fatalf("app.RestoreViewport not called")
	}
	if fake.lastViewBottom != 42 || fake.lastWrapSeg != 1 || fake.lastAutoFollow {
		t.Fatalf("forwarded args: got (%d,%d,%v) want (42,1,false)",
			fake.lastViewBottom, fake.lastWrapSeg, fake.lastAutoFollow)
	}
}

func TestDesktopEngine_RestorePaneViewport_UnknownPane(t *testing.T) {
	de := newTestDesktopWithOnePane(t)
	var unknown [16]byte
	unknown[0] = 0xff
	if ok := de.RestorePaneViewport(unknown, 0, 0, true); ok {
		t.Fatalf("RestorePaneViewport unknown id: want false")
	}
}

func TestDesktopEngine_RestorePaneViewport_NonRestorerApp(t *testing.T) {
	de := newTestDesktopWithOnePane(t)
	id, _ := de.firstPaneIDAndApp()
	// Default fakeApp doesn't implement ViewportRestorer.
	if ok := de.RestorePaneViewport(id, 0, 0, true); ok {
		t.Fatalf("RestorePaneViewport on non-restorer app: want false")
	}
}
