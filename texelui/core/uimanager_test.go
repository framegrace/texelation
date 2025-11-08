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
	mw := &miniWidget{}
	mw.SetPosition(2, 1)
	mw.Resize(3, 1)
	ui.AddWidget(mw)

	// Invalidate overlapping cell; widget draws 'X' at (2,1)
	ui.Invalidate(core.Rect{X: 2, Y: 1, W: 1, H: 1})
	buf := ui.Render()
	if got := buf[1][2].Ch; got != 'X' {
		t.Fatalf("expected draw at (2,1) to be 'X', got %q", string(got))
	}
}
