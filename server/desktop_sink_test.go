package server

import (
	"testing"

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

func TestDesktopSinkForwardsKeyEvents(t *testing.T) {
	driver := sinkScreenDriver{}
	recorder := &recordingApp{title: "welcome"}

	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &recordingApp{title: "shell"} }
	welcomeFactory := func() texel.App { return recorder }

	desktop, err := texel.NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
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

	desktop, err := texel.NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	session := NewSession([16]byte{2})
	publisher := NewDesktopPublisher(desktop, session)

	sink := NewDesktopSink(desktop)
	sink.SetPublisher(publisher)

	sink.HandleKeyEvent(session, protocol.KeyEvent{KeyCode: uint32(tcell.KeyRune), RuneValue: 'x', Modifiers: 0})

	if len(session.Pending(0)) == 0 {
		t.Fatalf("expected diffs after key event")
	}
}

func TestDesktopSinkHandlesAdditionalEvents(t *testing.T) {
	driver := sinkScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &recordingApp{title: "shell"} }
	welcomeFactory := func() texel.App { return &recordingApp{title: "welcome"} }

	desktop, err := texel.NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
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
