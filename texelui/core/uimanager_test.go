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
}

func (m *miniWidget) Draw(p *core.Painter) {
	x, y := m.Position()
	w, h := m.Size()
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			p.SetCell(x+xx, y+yy, 'X', tcell.StyleDefault)
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
