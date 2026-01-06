package statusbar

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelation/texel"
)

func TestStatusBarRender(t *testing.T) {
	app, ok := New().(*StatusBarApp)
	if !ok {
		t.Fatalf("expected *StatusBarApp")
	}
	app.Resize(40, 1)
	app.OnEvent(texel.Event{
		Type: texel.EventStateUpdate,
		Payload: texel.StatePayload{
			AllWorkspaces:  []int{1, 2},
			WorkspaceID:    2,
			InControlMode:  true,
			ActiveTitle:    "shell",
			DesktopBgColor: tcell.ColorGreen,
		},
	})

	buf := app.Render()
	if len(buf) != 1 || len(buf[0]) != 40 {
		t.Fatalf("unexpected buffer dimensions: %dx%d", len(buf), len(buf[0]))
	}

	space := true
	for _, cell := range buf[0] {
		if cell.Ch != ' ' {
			space = false
			break
		}
	}
	if space {
		t.Fatalf("expected status bar to render visible content")
	}

	go func() {
		_ = app.Run()
	}()
	time.Sleep(10 * time.Millisecond)
	app.Stop()
}
