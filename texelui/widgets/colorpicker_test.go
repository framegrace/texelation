package widgets

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestColorPickerEnterWithOnChange(t *testing.T) {
	cp := NewColorPicker(ColorPickerConfig{
		EnableSemantic: true,
		EnablePalette:  true,
		EnableOKLCH:    true,
		Label:          "Test",
	})
	cp.SetValue("#ff0000")

	// Set up an OnChange callback that simulates config editor behavior
	callbackCalled := false
	callbackBlocked := make(chan struct{})
	cp.OnChange = func(result ColorPickerResult) {
		callbackCalled = true
		// Simulate some work (but don't block forever)
		select {
		case <-time.After(10 * time.Millisecond):
		case <-callbackBlocked:
		}
	}

	// Expand the ColorPicker
	cp.Expand()
	if !cp.IsExpanded() {
		t.Fatal("ColorPicker should be expanded after Expand()")
	}
	if !cp.IsModal() {
		t.Fatal("ColorPicker should be modal when expanded")
	}

	// Move focus to content area (needed for Enter to commit)
	cp.focus = focusContent

	// Press Enter to select/commit - this should complete without blocking
	done := make(chan struct{})
	go func() {
		ev := tcell.NewEventKey(tcell.KeyEnter, 0, 0)
		cp.HandleKey(ev)
		close(done)
	}()

	// Wait for completion with timeout
	select {
	case <-done:
		// Good - HandleKey completed
	case <-time.After(1 * time.Second):
		close(callbackBlocked) // unblock callback if stuck
		t.Fatal("HandleKey blocked for too long - potential freeze issue")
	}

	// Verify the callback was called
	if !callbackCalled {
		t.Error("OnChange callback should have been called")
	}

	// Verify ColorPicker is now collapsed
	if cp.IsExpanded() {
		t.Error("ColorPicker should be collapsed after Enter")
	}
	if cp.IsModal() {
		t.Error("ColorPicker should not be modal when collapsed")
	}
}

func TestColorPickerEnterWithSlowOnChange(t *testing.T) {
	cp := NewColorPicker(ColorPickerConfig{
		EnableSemantic: true,
		EnablePalette:  true,
	})
	cp.SetValue("#ff0000")

	// Set up an OnChange callback that takes some time
	callbackDuration := 50 * time.Millisecond
	cp.OnChange = func(result ColorPickerResult) {
		time.Sleep(callbackDuration)
	}

	// Expand and focus content
	cp.Expand()
	cp.focus = focusContent

	// Time the Enter key handling
	start := time.Now()
	ev := tcell.NewEventKey(tcell.KeyEnter, 0, 0)
	cp.HandleKey(ev)
	elapsed := time.Since(start)

	// Should complete in a reasonable time (callback duration + some overhead)
	maxExpected := callbackDuration + 100*time.Millisecond
	if elapsed > maxExpected {
		t.Errorf("HandleKey took too long: %v (expected < %v)", elapsed, maxExpected)
	}

	// Should be collapsed
	if cp.IsExpanded() {
		t.Error("ColorPicker should be collapsed after Enter")
	}
}
