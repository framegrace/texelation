// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/desktop_sink_test.go
// Summary: Exercises desktop sink behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/texel"
	"texelation/texel/theme"
)

type sinkScreenDriver struct{}

func (sinkScreenDriver) Init() error            { return nil }
func (sinkScreenDriver) Fini()                  {}
func (sinkScreenDriver) Size() (int, int)       { return 80, 24 }
func (sinkScreenDriver) SetStyle(tcell.Style)   {}
func (sinkScreenDriver) HideCursor()            {}
func (sinkScreenDriver) Show()                  {}
func (sinkScreenDriver) PollEvent() tcell.Event { return nil }
func (sinkScreenDriver) SetContent(x, y int, mainc rune, combc []rune, style tcell.Style) {
}
func (sinkScreenDriver) GetContent(x, y int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type recordingApp struct {
	title string
	keys  []*tcell.EventKey
}

func (r *recordingApp) Run() error                        { return nil }
func (r *recordingApp) Stop()                             {}
func (r *recordingApp) Resize(cols, rows int)             {}
func (r *recordingApp) Render() [][]texel.Cell            { return [][]texel.Cell{{}} }
func (r *recordingApp) GetTitle() string                  { return r.title }
func (r *recordingApp) HandleKey(ev *tcell.EventKey)      { r.keys = append(r.keys, ev) }
func (r *recordingApp) SetRefreshNotifier(ch chan<- bool) {}

type keyPaneApp struct {
	title   string
	content rune
	mu      sync.Mutex
	refresh chan<- bool
}

func newKeyPaneApp(title string) *keyPaneApp {
	return &keyPaneApp{title: title, content: ' '}
}

func (a *keyPaneApp) Run() error            { return nil }
func (a *keyPaneApp) Stop()                 {}
func (a *keyPaneApp) Resize(cols, rows int) {}
func (a *keyPaneApp) Render() [][]texel.Cell {
	a.mu.Lock()
	defer a.mu.Unlock()
	buf := make([][]texel.Cell, 1)
	buf[0] = []texel.Cell{{Ch: a.content, Style: tcell.StyleDefault}}
	return buf
}
func (a *keyPaneApp) GetTitle() string { return a.title }
func (a *keyPaneApp) HandleKey(ev *tcell.EventKey) {
	a.mu.Lock()
	a.content = ev.Rune()
	a.mu.Unlock()
	if a.refresh != nil {
		select {
		case a.refresh <- true:
		default:
		}
	}
}
func (a *keyPaneApp) SetRefreshNotifier(ch chan<- bool) { a.refresh = ch }

type staticPaneApp struct {
	title string
}

func (s *staticPaneApp) Run() error            { return nil }
func (s *staticPaneApp) Stop()                 {}
func (s *staticPaneApp) Resize(cols, rows int) {}
func (s *staticPaneApp) Render() [][]texel.Cell {
	return [][]texel.Cell{{{Ch: 'T', Style: tcell.StyleDefault}}}
}
func (s *staticPaneApp) GetTitle() string                  { return s.title }
func (s *staticPaneApp) HandleKey(ev *tcell.EventKey)      {}
func (s *staticPaneApp) SetRefreshNotifier(ch chan<- bool) {}

func TestDesktopSinkForwardsKeyEvents(t *testing.T) {
	driver := sinkScreenDriver{}
	recorder := &recordingApp{title: "welcome"}

	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &recordingApp{title: "shell"} }
	welcomeFactory := func() texel.App { return recorder }

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	sink := NewDesktopSink(desktop)
	sink.HandleKeyEvent(nil, protocol.KeyEvent{KeyCode: uint32(tcell.KeyEnter), RuneValue: '\n', Modifiers: 0})

	if len(recorder.keys) != 1 {
		t.Fatalf("expected key event forwarded, got %d", len(recorder.keys))
	}
	if recorder.keys[0].Key() != tcell.KeyEnter {
		t.Fatalf("unexpected key received: %v", recorder.keys[0].Key())
	}
}

func TestDesktopSinkPublishesAfterKeyEvent(t *testing.T) {

	driver := sinkScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &recordingApp{title: "shell"} }
	welcomeFactory := func() texel.App { return &recordingApp{title: "welcome"} }

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	session := NewSession([16]byte{2}, 512)
	publisher := NewDesktopPublisher(desktop, session)

	sink := NewDesktopSink(desktop)
	sink.SetPublisher(publisher)

	sink.HandleKeyEvent(session, protocol.KeyEvent{KeyCode: uint32(tcell.KeyRune), RuneValue: 'x', Modifiers: 0})
	time.Sleep(2 * publishFallbackDelay)

	if len(session.Pending(0)) == 0 {
		t.Fatalf("expected diffs after key event")
	}
}

func TestDesktopSinkHandlesAdditionalEvents(t *testing.T) {
	driver := sinkScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &recordingApp{title: "shell"} }
	welcomeFactory := func() texel.App { return &recordingApp{title: "welcome"} }

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	sink := NewDesktopSink(desktop)
	sink.HandleMouseEvent(nil, protocol.MouseEvent{X: 5, Y: 6, ButtonMask: 1, Modifiers: 2})
	x, y := desktop.LastMousePosition()
	if x != 5 || y != 6 {
		t.Fatalf("mouse event not recorded")
	}
	if desktop.LastMouseButtons() != tcell.ButtonMask(1) {
		t.Fatalf("mouse buttons not recorded")
	}

	sink.HandleClipboardSet(nil, protocol.ClipboardSet{MimeType: "text/plain", Data: []byte("data")})
	data := desktop.HandleClipboardGet("text/plain")
	if string(data) != "data" {
		t.Fatalf("clipboard not stored")
	}

	sink.HandleThemeUpdate(nil, protocol.ThemeUpdate{Section: "pane", Key: "fg", Value: "#123456"})
	cfg := theme.Get()
	if section, ok := cfg["pane"]; !ok || section["fg"] != "#123456" {
		t.Fatalf("theme update not applied")
	}
}

func TestOtherPaneInputDoesNotPublishTViewPane(t *testing.T) {
	driver := sinkScreenDriver{}
	shellFactory := func() texel.App { return newKeyPaneApp("shell") }
	welcomeFactory := func() texel.App { return &staticPaneApp{title: "tview"} }

	lifecycle := texel.NoopAppLifecycle{}
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	session := NewSession([16]byte{3}, 512)
	publisher := NewDesktopPublisher(desktop, session)

	sink := NewDesktopSink(desktop)
	sink.SetPublisher(publisher)

	// Split to create the shell pane.
	ctrlA := protocol.KeyEvent{KeyCode: uint32(tcell.KeyCtrlA)}
	sink.HandleKeyEvent(nil, ctrlA)
	sink.HandleKeyEvent(nil, protocol.KeyEvent{KeyCode: uint32(tcell.KeyRune), RuneValue: '|'})
	// Move focus to the shell pane (right pane after vertical split).
	sink.HandleKeyEvent(nil, protocol.KeyEvent{KeyCode: uint32(tcell.KeyRight), Modifiers: uint16(tcell.ModShift)})

	snap, err := sink.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snap.Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(snap.Panes))
	}

	var shellID, tviewID [16]byte
	for _, pane := range snap.Panes {
		switch pane.Title {
		case "shell":
			shellID = pane.PaneID
		case "tview":
			tviewID = pane.PaneID
		}
	}
	if isZeroPaneID(shellID) || isZeroPaneID(tviewID) {
		t.Fatalf("failed to discover pane IDs (shell=%x tview=%x)", shellID, tviewID)
	}

	if activeID, ok := sink.Desktop().ActivePaneID(); !ok || activeID != shellID {
		t.Fatalf("expected shell pane to be active")
	}

	// Drain initial diffs.
	base := len(session.Pending(0))

	for _, r := range []rune{'a', 'b', 'c'} {
		prev := len(session.Pending(0))
		sink.HandleKeyEvent(nil, protocol.KeyEvent{KeyCode: uint32(tcell.KeyRune), RuneValue: r})
		time.Sleep(2 * publishFallbackDelay)
		pending := session.Pending(0)
		if len(pending) <= prev {
			t.Fatalf("no new diffs after key %q", r)
		}
		for _, diff := range pending[prev:] {
			delta, err := protocol.DecodeBufferDelta(diff.Payload)
			if err != nil {
				t.Fatalf("decode delta failed: %v", err)
			}
			if delta.PaneID == tviewID {
				t.Fatalf("tview pane received delta for key %q", r)
			}
			if delta.PaneID != shellID {
				t.Fatalf("unexpected pane %x in delta", delta.PaneID[:4])
			}
		}
	}

	if sink.scheduler != nil && sink.scheduler.FallbackCount() != 0 {
		t.Fatalf("fallback scheduler fired unexpectedly (%d)", sink.scheduler.FallbackCount())
	}

	if len(session.Pending(0)) <= base {
		t.Fatalf("expected new diffs queued")
	}
}
