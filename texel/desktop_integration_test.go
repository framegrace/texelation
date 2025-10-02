package texel

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"

	"texelation/texel/theme"
)

func TestDesktopSplitCreatesNewPane(t *testing.T) {
	driver := &stubScreenDriver{width: 120, height: 40}
	lifecycle := NoopAppLifecycle{}

	var shellCount int
	shellFactory := func() App {
		shellCount++
		return newFakeApp(fmt.Sprintf("shell-%d", shellCount))
	}
	welcomeFactory := func() App { return newFakeApp("welcome") }

	desktop, err := NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	ws := desktop.activeWorkspace
	if ws == nil {
		t.Fatalf("expected active workspace")
	}
	if ws.tree.Root == nil || ws.tree.Root.Pane == nil {
		t.Fatalf("expected initial welcome pane")
	}

	ws.PerformSplit(Horizontal)

	if ws.tree.Root == nil || len(ws.tree.Root.Children) != 2 {
		t.Fatalf("expected root split into two children")
	}
	if shellCount != 1 {
		t.Fatalf("expected shell factory invoked once, got %d", shellCount)
	}
	if ws.tree.ActiveLeaf == nil || ws.tree.ActiveLeaf.Pane == nil {
		t.Fatalf("expected active pane after split")
	}
	if got := ws.tree.ActiveLeaf.Pane.getTitle(); got != "shell-1" {
		t.Fatalf("expected new pane title shell-1, got %s", got)
	}
}

func TestDesktopStatusPaneResizesMainArea(t *testing.T) {
	driver := &stubScreenDriver{width: 100, height: 30}
	lifecycle := NoopAppLifecycle{}

	shellFactory := func() App { return newFakeApp("shell") }
	welcomeFactory := func() App { return newFakeApp("welcome") }

	desktop, err := NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

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
	welcomeFactory := func() App { return newFakeApp("welcome") }

	desktop, err := NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.SwitchToWorkspace(2)

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
	recorder := &keyRecordingApp{title: "shell"}

	shellFactory := func() App { return recorder }
	welcomeFactory := func() App { return newFakeApp("welcome") }

	desktop, err := NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

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
	welcomeFactory := func() App { return newFakeApp("welcome") }

	desktop, err := NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.InjectMouseEvent(12, 34, tcell.Button1, tcell.ModMask(2))

	if desktop.lastMouseX != 12 || desktop.lastMouseY != 34 {
		t.Fatalf("unexpected mouse position %d,%d", desktop.lastMouseX, desktop.lastMouseY)
	}
	if desktop.lastMouseButtons != tcell.Button1 {
		t.Fatalf("unexpected buttons %v", desktop.lastMouseButtons)
	}
	if desktop.lastMouseModifier != tcell.ModMask(2) {
		t.Fatalf("unexpected modifiers %v", desktop.lastMouseModifier)
	}
}

func TestDesktopClipboardAndThemeHandling(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := NoopAppLifecycle{}
	shellFactory := func() App { return newFakeApp("shell") }
	welcomeFactory := func() App { return newFakeApp("welcome") }

	desktop, err := NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	desktop.HandleClipboardSet("text/plain", []byte("hello"))
	data := desktop.HandleClipboardGet("text/plain")
	if string(data) != "hello" {
		t.Fatalf("unexpected clipboard data %q", string(data))
	}
	if desktop.lastClipboardMime != "text/plain" {
		t.Fatalf("expected last clipboard mime recorded")
	}

	desktop.HandleThemeUpdate("pane", "fg", "#ffffff")
	cfg := theme.Get()
	if section, ok := cfg["pane"]; !ok || section["fg"] != "#ffffff" {
		t.Fatalf("expected theme update to persist")
	}
}
