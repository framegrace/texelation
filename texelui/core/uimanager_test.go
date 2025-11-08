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
