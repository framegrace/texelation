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
	welcomeFactory := func() App { return newFakeApp("welcome") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("expected desktop, got error %v", err)
	}

	if !driver.initCalled || !driver.hideCursor || !driver.setStyle {
		t.Fatalf("driver was not initialised correctly: %+v", driver)
	}

	if len(lifecycle.started) != 1 {
		t.Fatalf("expected welcome app to start, got %d", len(lifecycle.started))
	}

	statusApp := newFakeApp("status")
	desktop.AddStatusPane(statusApp, SideTop, 1)

	if len(lifecycle.started) != 2 {
		t.Fatalf("expected status app to start, got %d", len(lifecycle.started))
	}

	desktop.Close()

	if len(lifecycle.stopped) != 2 {
		t.Fatalf("expected two apps to stop, got %d", len(lifecycle.stopped))
	}

	if !driver.finiCalled {
		t.Fatalf("driver was not closed")
	}
}

func TestWorkspaceRemovesPaneWhenAppExits(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := &trackingLifecycle{}

	var welcomeCount int
	welcomeFactory := func() App {
		title := fmt.Sprintf("welcome-%d", welcomeCount)
		welcomeCount++
		return newFakeApp(title)
	}

	var shellCount int
	shellFactory := func() App {
		title := fmt.Sprintf("shell-%d", shellCount)
		shellCount++
		return newFakeApp(title)
	}

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("expected desktop, got error %v", err)
	}

	ws := desktop.activeWorkspace
	if ws == nil {
		t.Fatalf("workspace should be initialised")
	}

	if len(lifecycle.started) != 1 {
		t.Fatalf("expected initial welcome app to start, got %d", len(lifecycle.started))
	}
	welcomeApp := lifecycle.started[0]

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

	var welcomeCount int
	welcomeFactory := func() App {
		title := fmt.Sprintf("welcome-%d", welcomeCount)
		welcomeCount++
		return newFakeApp(title)
	}

	shellFactory := func() App { return newFakeApp("shell") }

	desktop, err := NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("expected desktop, got error %v", err)
	}

	ws := desktop.activeWorkspace
	if ws == nil {
		t.Fatalf("workspace should be initialised")
	}

	if len(lifecycle.started) != 1 {
		t.Fatalf("expected initial welcome app to start, got %d", len(lifecycle.started))
	}
	initialWelcome := lifecycle.started[0]

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
