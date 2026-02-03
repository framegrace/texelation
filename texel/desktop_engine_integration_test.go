// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop_integration_test.go
// Summary: Exercises desktop integration behaviour to ensure the core desktop engine remains reliable.
// Usage: Executed during `go test` to guard against regressions.

package texel

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelui/theme"
)

func TestDesktopSplitCreatesNewPane(t *testing.T) {
	driver := &stubScreenDriver{width: 120, height: 40}
	lifecycle := NoopAppLifecycle{}

	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.SwitchToWorkspace(1)
	desktop.activeWorkspace.AddApp(newFakeApp("initial"))

	ws := desktop.activeWorkspace
	if ws == nil {
		t.Fatalf("expected active workspace")
	}
	if ws.tree.Root == nil || ws.tree.Root.Pane == nil {
		t.Fatalf("expected initial pane")
	}

	ws.PerformSplit(Horizontal)

	if ws.tree.Root == nil || len(ws.tree.Root.Children) != 2 {
		t.Fatalf("expected root split into two children")
	}
	if ws.tree.ActiveLeaf == nil || ws.tree.ActiveLeaf.Pane == nil {
		t.Fatalf("expected active pane after split")
	}
	if got := ws.tree.ActiveLeaf.Pane.getTitle(); got != "shell" {
		t.Fatalf("expected new pane title shell, got %s", got)
	}
}

func TestDesktopStatusPaneResizesMainArea(t *testing.T) {
	driver := &stubScreenDriver{width: 100, height: 30}
	lifecycle := NoopAppLifecycle{}

	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.SwitchToWorkspace(1)
	desktop.activeWorkspace.AddApp(newFakeApp("initial"))

	statusApp := newFakeApp("status")
	desktop.AddStatusPane(statusApp, SideTop, 2)

	mainX, mainY, mainW, mainH := desktop.getMainArea()
	if mainY != 2 {
		t.Fatalf("expected top offset 2, got %d", mainY)
	}
	if mainH != 28 {
		t.Fatalf("expected workspace height 28, got %d", mainH)
	}
	if mainW != 100 || mainX != 0 {
		t.Fatalf("expected full width main area, got x=%d w=%d", mainX, mainW)
	}
}

func TestDesktopSwitchWorkspaceCreatesNewScreen(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := NoopAppLifecycle{}
	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.SwitchToWorkspace(1)
	desktop.activeWorkspace.AddApp(newFakeApp("ws1"))
	desktop.SwitchToWorkspace(2)
	desktop.activeWorkspace.AddApp(newFakeApp("ws2"))

	if desktop.activeWorkspace == nil || desktop.activeWorkspace.id != 2 {
		t.Fatalf("expected active workspace 2")
	}
	if len(desktop.workspaces) != 2 {
		t.Fatalf("expected two workspaces, got %d", len(desktop.workspaces))
	}
	ws := desktop.activeWorkspace
	if ws.tree.Root == nil || ws.tree.Root.Pane == nil {
		t.Fatalf("expected welcome pane in new workspace")
	}
}

type keyRecordingApp struct {
	title string
	keys  []*tcell.EventKey
}

func (a *keyRecordingApp) Run() error                        { return nil }
func (a *keyRecordingApp) Stop()                             {}
func (a *keyRecordingApp) Resize(cols, rows int)             {}
func (a *keyRecordingApp) Render() [][]Cell                  { return [][]Cell{{}} }
func (a *keyRecordingApp) GetTitle() string                  { return a.title }
func (a *keyRecordingApp) HandleKey(ev *tcell.EventKey)      { a.keys = append(a.keys, ev) }
func (a *keyRecordingApp) SetRefreshNotifier(ch chan<- bool) {}

func TestDesktopInjectKeyEvent(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := NoopAppLifecycle{}
	recorder := &keyRecordingApp{title: "recorder"}

	shellFactory := func() App { return recorder }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.SwitchToWorkspace(1)
	desktop.activeWorkspace.AddApp(recorder)

	if desktop.activeWorkspace == nil {
		t.Fatalf("expected active workspace")
	}

	desktop.activeWorkspace.PerformSplit(Vertical)

	desktop.InjectKeyEvent(tcell.KeyEnter, '\n', tcell.ModMask(0))

	if len(recorder.keys) != 1 {
		t.Fatalf("expected 1 key event, got %d", len(recorder.keys))
	}
	if recorder.keys[0].Key() != tcell.KeyEnter {
		t.Fatalf("unexpected key %v", recorder.keys[0].Key())
	}
}

func TestDesktopInjectMouseEvent(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := NoopAppLifecycle{}
	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.SwitchToWorkspace(1)
	desktop.activeWorkspace.AddApp(newFakeApp("initial"))

	ws := desktop.activeWorkspace
	if ws == nil || ws.tree == nil || ws.tree.Root == nil {
		t.Fatalf("expected active workspace with root pane")
	}

	ws.PerformSplit(Vertical)

	root := ws.tree.Root
	if len(root.Children) != 2 {
		t.Fatalf("expected vertical split to produce two children, got %d", len(root.Children))
	}

	left := root.Children[0]
	right := root.Children[1]
	if left == nil || left.Pane == nil || right == nil || right.Pane == nil {
		t.Fatalf("expected both panes to be present")
	}

	if ws.tree.ActiveLeaf != right {
		t.Fatalf("expected new pane on the right to be active after split")
	}

	clickX := left.Pane.absX0 + left.Pane.Width()/2
	clickY := left.Pane.absY0 + left.Pane.Height()/2

	desktop.InjectMouseEvent(clickX, clickY, tcell.Button1, tcell.ModMask(2))

	if desktop.lastMouseX != clickX || desktop.lastMouseY != clickY {
		t.Fatalf("unexpected mouse position %d,%d", desktop.lastMouseX, desktop.lastMouseY)
	}
	if desktop.lastMouseButtons != tcell.Button1 {
		t.Fatalf("unexpected buttons %v", desktop.lastMouseButtons)
	}
	if desktop.lastMouseModifier != tcell.ModMask(2) {
		t.Fatalf("unexpected modifiers %v", desktop.lastMouseModifier)
	}
	if ws.tree.ActiveLeaf != left {
		t.Fatalf("expected click to activate left pane")
	}
}

func TestDesktopClipboardAndThemeHandling(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := NoopAppLifecycle{}
	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.SwitchToWorkspace(1)
	desktop.activeWorkspace.AddApp(newFakeApp("initial"))

	desktop.SetClipboard("text/plain", []byte("hello"))
	mime, data, ok := desktop.GetClipboard()
	if !ok {
		t.Fatalf("expected clipboard to have content")
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected clipboard data %q", string(data))
	}
	if mime != "text/plain" {
		t.Fatalf("expected clipboard mime 'text/plain', got %q", mime)
	}

	desktop.HandleThemeUpdate("pane", "fg", "#ffffff")
	cfg := theme.Get()
	if section, ok := cfg["pane"]; !ok || section["fg"] != "#ffffff" {
		t.Fatalf("expected theme update to persist")
	}
}
