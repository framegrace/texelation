package cards

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/texel"
)

func TestFlashCardTriggerLifecycle(t *testing.T) {
	flash := NewFlashCard(50*time.Millisecond, tcell.ColorRed)
	bus := newControlBus()
	if err := flash.RegisterControls(bus); err != nil {
		t.Fatalf("register controls: %v", err)
	}
	flash.SetRefreshNotifier(make(chan bool, 1))

	if flash.Active() {
		t.Fatal("flash should be inactive before trigger")
	}
	if err := bus.Trigger(FlashTriggerID, nil); err != nil {
		t.Fatalf("trigger flash: %v", err)
	}

	waitFor(t, 100*time.Millisecond, func() bool { return flash.Active() })
	waitFor(t, 200*time.Millisecond, func() bool { return !flash.Active() })
}

func TestFlashCardRenderOverlay(t *testing.T) {
	flash := NewFlashCard(100*time.Millisecond, tcell.ColorBlue)
	bus := newControlBus()
	if err := flash.RegisterControls(bus); err != nil {
		t.Fatalf("register controls: %v", err)
	}

	input := [][]texel.Cell{{{Ch: 'A', Style: tcell.StyleDefault}}}
	outInactive := flash.Render(input)
	if &outInactive[0][0] != &input[0][0] {
		// When inactive the buffer should be returned as-is (no clone performed).
		t.Fatal("expected inactive render to return original buffer")
	}

	if err := bus.Trigger(FlashTriggerID, nil); err != nil {
		t.Fatalf("trigger flash: %v", err)
	}
	out := flash.Render(input)
	if out == nil {
		t.Fatal("expected render output when active")
	}
	if &out[0][0] == &input[0][0] {
		t.Fatal("expected clone of buffer while flash is active")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}
