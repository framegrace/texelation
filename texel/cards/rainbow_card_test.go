package cards

import (
	texelcore "github.com/framegrace/texelui/core"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestRainbowCardToggleAndRender(t *testing.T) {
	card := NewRainbowCard(0.5, 0.6)

	original := [][]texelcore.Cell{{{
		Ch:    'a',
		Style: tcell.StyleDefault.Foreground(tcell.ColorBlue),
	}}}

	out := card.Render(original)
	if &out[0][0] != &original[0][0] {
		t.Fatalf("expected disabled card to return original buffer instance")
	}

	ch := make(chan bool, 1)
	card.SetRefreshNotifier(ch)
	card.Toggle()
	select {
	case <-ch:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected refresh notification on toggle")
	}

	if !card.Enabled() {
		t.Fatalf("card should be enabled after toggle")
	}

	tinted := card.Render(original)
	if &tinted[0][0] == &original[0][0] {
		t.Fatalf("expected new buffer after tinting")
	}
	if tinted[0][0].Style == original[0][0].Style {
		t.Fatalf("expected style to change after tinting")
	}

	card.Toggle()
	if card.Enabled() {
		t.Fatalf("card should be disabled after second toggle")
	}
}
