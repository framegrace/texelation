package widgets

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

// TestStatusBarShowMessageDuringHandleKey reproduces the freeze that occurs
// when StatusBar.ShowSuccess is called from within a widget's OnChange callback
// during HandleKey processing.
func TestStatusBarShowMessageDuringHandleKey(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(80, 24)

	// Create and set up StatusBar
	sb := NewStatusBar(0, 22, 80)
	ui.SetStatusBar(sb)

	// Create a button that calls StatusBar.ShowSuccess when clicked
	btn := NewButton(10, 10, 20, 1, "Test")
	btn.OnClick = func() {
		// This simulates what happens when a widget's OnChange calls showSuccess
		sb.ShowSuccess("Success!")
	}

	ui.AddWidget(btn)
	ui.Focus(btn)

	// Set up a refresh notifier (like the standalone runner does)
	refreshCh := make(chan bool, 1)
	ui.SetRefreshNotifier(refreshCh)

	// Simulate pressing Enter on the button - this should trigger OnClick
	// which calls StatusBar.ShowSuccess
	done := make(chan struct{})
	go func() {
		ev := tcell.NewEventKey(tcell.KeyEnter, 0, 0)
		ui.HandleKey(ev)
		close(done)
	}()

	// Wait for completion with timeout
	select {
	case <-done:
		// Good - HandleKey completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("HandleKey blocked - potential deadlock in StatusBar.ShowSuccess during HandleKey")
	}

	// Clean up
	sb.Stop()
}

// TestStatusBarShowMessageFromCallback tests calling ShowSuccess from a callback
// that's triggered during key handling, similar to how config editor uses it.
func TestStatusBarShowMessageFromCallback(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(80, 24)

	// Create and set up StatusBar
	sb := NewStatusBar(0, 22, 80)
	ui.SetStatusBar(sb)

	// Create an input that calls StatusBar.ShowSuccess on change
	input := NewInput(10, 10, 30)
	input.OnChange = func(text string) {
		sb.ShowSuccess("Changed to: " + text)
	}

	ui.AddWidget(input)
	ui.Focus(input)

	// Set up a refresh notifier
	refreshCh := make(chan bool, 1)
	ui.SetRefreshNotifier(refreshCh)

	// Simulate typing a character - this should trigger OnChange
	done := make(chan struct{})
	go func() {
		ev := tcell.NewEventKey(tcell.KeyRune, 'a', 0)
		ui.HandleKey(ev)
		close(done)
	}()

	// Wait for completion with timeout
	select {
	case <-done:
		// Good - HandleKey completed
	case <-time.After(2 * time.Second):
		t.Fatal("HandleKey blocked - deadlock when calling StatusBar.ShowSuccess from OnChange")
	}

	// Verify the message was added
	if len(sb.messages) == 0 {
		t.Error("Expected message to be added to StatusBar")
	}

	// Clean up
	sb.Stop()
}

// TestStatusBarTickerDuringHandleKey tests that the StatusBar ticker doesn't
// cause issues when HandleKey is in progress.
func TestStatusBarTickerDuringHandleKey(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(80, 24)

	// Create and set up StatusBar with a short message that will expire
	sb := NewStatusBar(0, 22, 80)
	sb.DefaultMessageDuration = 50 * time.Millisecond // Very short duration
	ui.SetStatusBar(sb)

	// Add a message that will expire soon
	sb.ShowMessage("Expiring soon")

	// Create a simple focusable widget
	btn := NewButton(10, 10, 20, 1, "Test")
	ui.AddWidget(btn)
	ui.Focus(btn)

	// Set up a refresh notifier
	refreshCh := make(chan bool, 1)
	ui.SetRefreshNotifier(refreshCh)

	// Drain refresh channel in background
	stopDrain := make(chan struct{})
	go func() {
		for {
			select {
			case <-refreshCh:
			case <-stopDrain:
				return
			}
		}
	}()

	// Simulate multiple key events while the ticker might be expiring messages
	for i := 0; i < 20; i++ {
		done := make(chan struct{})
		go func() {
			ev := tcell.NewEventKey(tcell.KeyRune, 'x', 0)
			ui.HandleKey(ev)
			close(done)
		}()

		select {
		case <-done:
			// Good
		case <-time.After(1 * time.Second):
			t.Fatalf("HandleKey blocked on iteration %d", i)
		}

		// Small delay to let ticker potentially fire
		time.Sleep(10 * time.Millisecond)
	}

	// Clean up - stop StatusBar first, then stop draining
	sb.Stop()
	close(stopDrain)
}

// TestStatusBarRenderDuringShowMessage tests that calling Render while
// ShowMessage is in progress doesn't cause a deadlock.
func TestStatusBarRenderDuringShowMessage(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(80, 24)

	sb := NewStatusBar(0, 22, 80)
	ui.SetStatusBar(sb)

	btn := NewButton(10, 10, 20, 1, "Test")
	ui.AddWidget(btn)
	ui.Focus(btn)

	refreshCh := make(chan bool, 1)
	ui.SetRefreshNotifier(refreshCh)

	// Simulate the standalone runner pattern: render after each event
	for i := 0; i < 10; i++ {
		done := make(chan struct{})
		go func() {
			// Show a message (like config editor does)
			sb.ShowSuccess("Saved.")
			// Then render (like standalone runner does after HandleKey)
			ui.Render()
			close(done)
		}()

		select {
		case <-done:
			// Good
		case <-time.After(2 * time.Second):
			t.Fatalf("ShowSuccess + Render blocked on iteration %d", i)
		}
	}

	sb.Stop()
}
