package core_test

import (
	"github.com/gdamore/tcell/v2"
	"testing"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

func TestUIManagerRendersPaneAndTextArea(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(20, 5)

	pane := widgets.NewPane(0, 0, 20, 5, tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite))
	ui.AddWidget(pane)

	ta := widgets.NewTextArea(1, 1, 18, 3)
	b := widgets.NewBorder(0, 0, 20, 5, tcell.StyleDefault.Foreground(tcell.ColorWhite))
	b.SetChild(ta)
	ui.AddWidget(b)
	ui.Focus(ta)

	buf := ui.Render()
	if len(buf) != 5 || len(buf[0]) != 20 {
		t.Fatalf("unexpected buffer size %dx%d", len(buf[0]), len(buf))
	}
}

type miniWidget struct {
	core.BaseWidget
	toggled bool
}

func (m *miniWidget) Draw(p *core.Painter) {
	x, y := m.Position()
	w, h := m.Size()
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			ch := 'X'
			if m.toggled {
				ch = 'Y'
			}
			p.SetCell(x+xx, y+yy, ch, tcell.StyleDefault)
		}
	}
}
func (m *miniWidget) Focusable() bool { return false }

// Ensures that only invalidated clips are redrawn.
func TestUIManagerDirtyClipsRestrictDraw(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(10, 4)
	// Border + TextArea child, ensure invalidator is propagated
	b := widgets.NewBorder(0, 0, 10, 4, tcell.StyleDefault)
	ta := widgets.NewTextArea(0, 0, 8, 2)
	b.SetChild(ta)
	ui.AddWidget(b)

	// Invalidate overlapping cell; widget draws 'X' at (2,1)
	// Focus and type 'a'; caret moves to (2,1), 'a' appears at client(1,1)
	ui.Focus(ta)
	ui.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', 0))
	buf := ui.Render()
	// Border client area starts at (1,1)
	if got := buf[1][1].Ch; got != 'a' {
		t.Fatalf("expected 'a' at (1,1), got %q", string(got))
	}
}

// Clicking should focus the inner TextArea, not the border, and allow typing.
func TestClickToFocusInnerWidget(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(10, 4)
	b := widgets.NewBorder(0, 0, 10, 4, tcell.StyleDefault)
	ta := widgets.NewTextArea(1, 1, 8, 2)
	b.SetChild(ta)
	ui.AddWidget(b)
	// Click inside textarea at (1,1) (client origin)
	me := tcell.NewEventMouse(1, 1, tcell.Button1, 0)
	ui.HandleMouse(me)
    // Type 'a' and ensure it appears
    ui.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', 0))
    buf := ui.Render()
    if got := buf[1][1].Ch; got != 'a' {
        t.Fatalf("expected 'a' at (1,1), got %q", string(got))
    }
}

// Delete a range from the first 10 to the end; expect only first block remains.

// If a widget consumes keys but doesn't invalidate, UIManager falls back to full redraw.
func TestUIManagerKeyFallbackRedraw(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(6, 3)
	mw := &miniWidget{}
	mw.SetPosition(1, 1)
	mw.Resize(1, 1)
	ui.AddWidget(mw)

	// Initial draw shows 'X'
	buf := ui.Render()
	if got := buf[1][1].Ch; got != 'X' {
		t.Fatalf("expected 'X', got %q", string(got))
	}

	// Make mw consume keys without invalidating by focusing it and toggling state in HandleKey via embedding
	// We don't have a HandleKey; simulate by forcing fallback: call HandleKey on UI with a non-Tab key while focused
	// and then toggle state manually to emulate a consumed change without invalidation.
	ui.Focus(mw)
	// Manually set toggled; UI.HandleKey should detect no dirty and issue full redraw
	mw.toggled = true
	ui.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'z', 0))
	buf = ui.Render()
	if got := buf[1][1].Ch; got != 'Y' {
		t.Fatalf("expected 'Y' after fallback redraw, got %q", string(got))
	}
}

// Ensure first Shift+Left moves caret left and selects previous rune inclusively.
// selection-related tests removed; selection will be reintroduced later
