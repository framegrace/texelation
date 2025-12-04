// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop_test.go
// Summary: Exercises desktop behaviour to ensure the core desktop engine remains reliable.
// Usage: Executed during `go test` to guard against regressions.

package texel

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
)

type stubScreenDriver struct {
	width, height int
	initCalled    bool
	finiCalled    bool
	hideCursor    bool
	setStyle      bool
	showCount     int
	content       map[[2]int]Cell
}

func (s *stubScreenDriver) Init() error {
	s.initCalled = true
	return nil
}

func (s *stubScreenDriver) Fini() {
	s.finiCalled = true
}

func (s *stubScreenDriver) Size() (int, int) {
	if s.width == 0 {
		s.width = 80
	}
	if s.height == 0 {
		s.height = 24
	}
	return s.width, s.height
}

func (s *stubScreenDriver) SetStyle(style tcell.Style) {
	s.setStyle = true
}

func (s *stubScreenDriver) HideCursor() {
	s.hideCursor = true
}

func (s *stubScreenDriver) Show() {
	s.showCount++
}

func (s *stubScreenDriver) PollEvent() tcell.Event { return nil }

func (s *stubScreenDriver) SetContent(x, y int, mainc rune, combc []rune, style tcell.Style) {
	if s.content == nil {
		s.content = make(map[[2]int]Cell)
	}
	s.content[[2]int{x, y}] = Cell{Ch: mainc, Style: style}
}

func (s *stubScreenDriver) GetContent(x, y int) (rune, []rune, tcell.Style, int) {
	if s.content != nil {
		if cell, ok := s.content[[2]int{x, y}]; ok {
			return cell.Ch, nil, cell.Style, 1
		}
	}
	return ' ', nil, tcell.StyleDefault, 1
}

type trackingLifecycle struct {
	started       []App
	stopped       []App
	exitCallbacks map[App]func(error)
}

func (l *trackingLifecycle) StartApp(app App, onExit func(error)) {
	l.started = append(l.started, app)
	if onExit != nil {
		if l.exitCallbacks == nil {
			l.exitCallbacks = make(map[App]func(error))
		}
		l.exitCallbacks[app] = onExit
	}
}

func (l *trackingLifecycle) StopApp(app App) {
	l.stopped = append(l.stopped, app)
}

func (l *trackingLifecycle) TriggerExit(app App, err error) {
	if l.exitCallbacks == nil {
		return
	}
	if cb, ok := l.exitCallbacks[app]; ok {
		cb(err)
		delete(l.exitCallbacks, app)
	}
}

type fakeApp struct {
	title    string
	cols     int
	rows     int
	stopped  bool
	notifier chan<- bool
}

func newFakeApp(title string) *fakeApp {
	return &fakeApp{title: title}
}

func (f *fakeApp) Run() error { return nil }
func (f *fakeApp) Stop()      { f.stopped = true }
func (f *fakeApp) Resize(cols, rows int) {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	f.cols, f.rows = cols, rows
}
func (f *fakeApp) Render() [][]Cell {
	if f.rows == 0 {
		f.rows = 1
	}
	if f.cols == 0 {
		f.cols = 1
	}
	buf := make([][]Cell, f.rows)
	for r := range buf {
		buf[r] = make([]Cell, f.cols)
	}
	return buf
}
func (f *fakeApp) GetTitle() string                  { return f.title }
func (f *fakeApp) HandleKey(ev *tcell.EventKey)      {}
func (f *fakeApp) SetRefreshNotifier(ch chan<- bool) { f.notifier = ch }

func TestDesktopWithInjectedDriverAndLifecycle(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := &trackingLifecycle{}

	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("expected desktop, got error %v", err)
	}

	if !driver.initCalled || !driver.hideCursor || !driver.setStyle {
		t.Fatalf("driver was not initialised correctly: %+v", driver)
	}

	// No initial app with empty initAppName
	if len(lifecycle.started) != 0 {
		t.Fatalf("expected no app to start initially, got %d", len(lifecycle.started))
	}

	statusApp := newFakeApp("status")
	desktop.AddStatusPane(statusApp, SideTop, 1)

	if len(lifecycle.started) != 1 {
		t.Fatalf("expected status app to start, got %d", len(lifecycle.started))
	}

	desktop.Close()

	if len(lifecycle.stopped) != 1 {
		t.Fatalf("expected one app to stop, got %d", len(lifecycle.stopped))
	}

	if !driver.finiCalled {
		t.Fatalf("driver was not closed")
	}
}

func TestWorkspaceRemovesPaneWhenAppExits(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := &trackingLifecycle{}

	var shellCount int
	shellFactory := func() App {
		title := fmt.Sprintf("shell-%d", shellCount)
		shellCount++
		return newFakeApp(title)
	}

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("expected desktop, got error %v", err)
	}

	desktop.SwitchToWorkspace(1)
	welcomeApp := newFakeApp("initial")
	desktop.activeWorkspace.AddApp(welcomeApp)

	ws := desktop.activeWorkspace
	if ws == nil {
		t.Fatalf("workspace should be initialised")
	}

	if len(lifecycle.started) != 1 {
		t.Fatalf("expected initial welcome app to start, got %d", len(lifecycle.started))
	}

	ws.PerformSplit(Vertical)

	if len(lifecycle.started) != 2 {
		t.Fatalf("expected shell app to start after split, got %d", len(lifecycle.started))
	}
	shellApp := lifecycle.started[1]

	leafCount := 0
	ws.tree.Traverse(func(n *Node) {
		if n != nil && n.Pane != nil {
			leafCount++
		}
	})
	if leafCount != 2 {
		t.Fatalf("expected two panes after split, got %d", leafCount)
	}

	lifecycle.TriggerExit(shellApp, nil)

	leafCount = 0
	ws.tree.Traverse(func(n *Node) {
		if n != nil && n.Pane != nil {
			leafCount++
		}
	})
	if leafCount != 1 {
		t.Fatalf("expected one pane after shell exit, got %d", leafCount)
	}

	if ws.tree.ActiveLeaf == nil || ws.tree.ActiveLeaf.Pane == nil {
		t.Fatalf("expected active pane after removing shell")
	}
	if ws.tree.ActiveLeaf.Pane.app != welcomeApp {
		t.Fatalf("expected welcome app to remain active after shell exit")
	}

	lifecycle.TriggerExit(welcomeApp, nil)

	if len(lifecycle.started) != 3 {
		t.Fatalf("expected welcome to restart automatically, got %d apps started", len(lifecycle.started))
	}

	if ws.tree.ActiveLeaf == nil || ws.tree.ActiveLeaf.Pane == nil {
		t.Fatalf("expected active pane after welcome restart")
	}
	newWelcome := lifecycle.started[2]
	if ws.tree.ActiveLeaf.Pane.app != newWelcome {
		t.Fatalf("expected new welcome app to be active after restart")
	}
}

func TestCloseActivePaneRespawnsWelcome(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := &trackingLifecycle{}

	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("expected desktop, got error %v", err)
	}

	desktop.SwitchToWorkspace(1)
	initialWelcome := newFakeApp("initial")
	desktop.activeWorkspace.AddApp(initialWelcome)

	ws := desktop.activeWorkspace
	if ws == nil {
		t.Fatalf("workspace should be initialised")
	}

	if len(lifecycle.started) != 1 {
		t.Fatalf("expected initial welcome app to start, got %d", len(lifecycle.started))
	}

	ws.CloseActivePane()

	if len(lifecycle.stopped) != 1 || lifecycle.stopped[0] != initialWelcome {
		t.Fatalf("expected initial welcome app to be stopped once")
	}
	if len(lifecycle.started) != 2 {
		t.Fatalf("expected welcome app to restart automatically, got %d starts", len(lifecycle.started))
	}

	newWelcome := lifecycle.started[1]

	if ws.tree.Root == nil || ws.tree.ActiveLeaf == nil || ws.tree.ActiveLeaf.Pane == nil {
		t.Fatalf("expected a new pane to exist after closing the last one")
	}

	if ws.tree.ActiveLeaf.Pane.app != newWelcome {
		t.Fatalf("expected the new welcome app to be active after respawn")
	}
}

func TestMouseBorderResizeAdjustsRatios(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := &trackingLifecycle{}

	var shellCount int
	shellFactory := func() App {
		title := fmt.Sprintf("shell-%d", shellCount)
		shellCount++
		return newFakeApp(title)
	}

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("expected desktop, got error %v", err)
	}

	desktop.SwitchToWorkspace(1)
	desktop.activeWorkspace.AddApp(newFakeApp("initial"))

	ws := desktop.activeWorkspace
	if ws == nil {
		t.Fatalf("workspace should be initialised")
	}

	ws.PerformSplit(Vertical)

	root := ws.tree.Root
	if root == nil || root.Split != Vertical || len(root.Children) != 2 {
		t.Fatalf("expected vertical root with two children after split")
	}

	leftPane := root.Children[0].Pane
	rightPane := root.Children[1].Pane
	if leftPane == nil || rightPane == nil {
		t.Fatalf("expected leaf panes on both sides of split")
	}

	initialLeftWidth := leftPane.Width()
	initialLeftRatio := root.SplitRatios[0]

	borderX := leftPane.absX1 - 1
	borderY := leftPane.absY0 + leftPane.Height()/2

	if !ws.handleMouseResize(borderX, borderY, tcell.Button1, tcell.ButtonNone) {
		t.Fatalf("expected mouse resize to start when clicking border")
	}

	moveX := borderX + 4
	ws.handleMouseResize(moveX, borderY, tcell.Button1, tcell.Button1)
	ws.handleMouseResize(moveX, borderY, tcell.ButtonNone, tcell.Button1)

	if ws.mouseResizeBorder != nil {
		t.Fatalf("expected mouse resize state to clear after release")
	}
	if leftPane.IsResizing || rightPane.IsResizing {
		t.Fatalf("expected panes to exit resizing state after mouse release")
	}
	if leftPane.Width() <= initialLeftWidth {
		t.Fatalf("expected left pane width to increase after dragging (before=%d, after=%d)", initialLeftWidth, leftPane.Width())
	}
	if root.SplitRatios[0] <= initialLeftRatio {
		t.Fatalf("expected left ratio to grow after drag (before=%.3f, after=%.3f)", initialLeftRatio, root.SplitRatios[0])
	}
}
