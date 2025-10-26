package tviewbridge

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/texel"
)

func TestTViewAppHandleKeyProducesCompleteFrames(t *testing.T) {
	input := tview.NewInputField().SetLabel("Value: ")
	root := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(input, 0, 1, true)

	app := NewTViewApp("tview-key-test", root)
	app.Resize(40, 3)

	if err := app.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	defer app.Stop()

	typed := ""
	for _, r := range []rune{'a', 'b', '1', '2'} {
		typed += string(r)
		app.HandleKey(tcell.NewEventKey(tcell.KeyRune, r, 0))
		expected := "Value: " + typed
		if !eventuallyContains(app, expected, 200*time.Millisecond) {
			t.Fatalf("buffer never contained %q after key %q", expected, r)
		}
	}
}

func eventuallyContains(app *TViewApp, substr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if bufferContains(app.Render(), substr) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func bufferContains(buffer [][]texel.Cell, substr string) bool {
	for _, row := range buffer {
		var b strings.Builder
		for _, cell := range row {
			ch := cell.Ch
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		if strings.Contains(b.String(), substr) {
			return true
		}
	}
	return false
}
