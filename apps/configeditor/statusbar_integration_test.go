package configeditor

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

// TestConfigEditorStatusBarFreeze reproduces the freeze that occurs when
// StatusBar.ShowSuccess is called from the config editor's applyTargetConfig.
func TestConfigEditorStatusBarFreeze(t *testing.T) {
	// Set up temp config dir
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_ = theme.Reload()

	// Create the config editor (this sets up the StatusBar)
	app := New(nil)
	editor := app.(*ConfigEditor)

	// Resize to initialize layout
	editor.Resize(80, 24)

	// Set up a refresh notifier (like standalone runner does)
	refreshCh := make(chan bool, 1)
	editor.SetRefreshNotifier(refreshCh)

	// Drain refresh channel in background (like standalone runner does)
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

	// Simulate the flow: HandleKey triggers a change which calls showSuccess
	done := make(chan struct{})
	go func() {
		// Call showSuccess directly (simulating what happens during a change)
		editor.showSuccess("Test message")

		// Then render (like standalone runner does after HandleKey)
		editor.Render()

		close(done)
	}()

	select {
	case <-done:
		// Good - no freeze
	case <-time.After(2 * time.Second):
		t.Fatal("showSuccess + Render blocked - StatusBar freeze detected")
	}

	// Also test the full HandleKey -> showSuccess flow
	done2 := make(chan struct{})
	go func() {
		// Simulate pressing a key that would trigger a change
		ev := tcell.NewEventKey(tcell.KeyRune, 'x', 0)
		editor.HandleKey(ev)
		editor.Render()
		close(done2)
	}()

	select {
	case <-done2:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("HandleKey + Render blocked")
	}

	// Clean up
	close(stopDrain)
	editor.Stop()
}

// TestConfigEditorApplyWithStatusBar tests the full apply flow with StatusBar messages.
func TestConfigEditorApplyWithStatusBar(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_ = theme.Reload()

	app := New(nil)
	editor := app.(*ConfigEditor)
	editor.Resize(80, 24)

	refreshCh := make(chan bool, 1)
	editor.SetRefreshNotifier(refreshCh)

	// Drain in background
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

	// Get the system target
	var target *configTarget
	for _, tgt := range editor.targets {
		if tgt.kind == targetSystem {
			target = tgt
			break
		}
	}
	if target == nil {
		t.Fatal("No system target found")
	}

	// Test applyTargetConfig which should call showSuccess
	done := make(chan struct{})
	go func() {
		editor.applyTargetConfig(target, applySystem)
		editor.Render()
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("applyTargetConfig blocked - StatusBar freeze")
	}

	close(stopDrain)
	editor.Stop()
}

// TestColorPickerWithStatusBar tests that ColorPicker OnChange calling StatusBar
// doesn't cause a freeze.
func TestColorPickerWithStatusBar(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(80, 24)

	// Create and set up StatusBar
	sb := widgets.NewStatusBar()
	sb.SetPosition(0, 22)
	sb.Resize(80, 2)
	ui.SetStatusBar(sb)

	// Create a ColorPicker that calls StatusBar on change
	cp := widgets.NewColorPicker(widgets.ColorPickerConfig{
		EnableSemantic: true,
		EnablePalette:  true,
		EnableOKLCH:    true,
		Label:          "Test",
	})
	cp.SetPosition(10, 10)
	cp.SetValue("#ff0000")
	cp.OnChange = func(result widgets.ColorPickerResult) {
		// This is what the config editor does
		sb.ShowSuccess("Color changed to: " + result.Source)
	}

	ui.AddWidget(cp)
	ui.Focus(cp)

	refreshCh := make(chan bool, 1)
	ui.SetRefreshNotifier(refreshCh)

	// Drain refresh channel
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

	// Expand the ColorPicker and navigate with Tab to get to content
	cp.Expand()

	// Press Tab a few times to get into content area, then Enter
	done := make(chan struct{})
	go func() {
		// Tab to move through tab bar to content
		for i := 0; i < 5; i++ {
			ev := tcell.NewEventKey(tcell.KeyTab, 0, 0)
			ui.HandleKey(ev)
		}
		// Press Enter to select
		ev := tcell.NewEventKey(tcell.KeyEnter, 0, 0)
		ui.HandleKey(ev)
		ui.Render()
		close(done)
	}()

	select {
	case <-done:
		// Good - no freeze
	case <-time.After(2 * time.Second):
		t.Fatal("ColorPicker Enter with StatusBar.ShowSuccess blocked - freeze detected")
	}

	sb.Stop()
	close(stopDrain)
}

// TestStatusBarAfterHandleKey tests the realistic flow where Render is called
// AFTER HandleKey returns, like the standalone runner does.
func TestStatusBarAfterHandleKey(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(80, 24)

	sb := widgets.NewStatusBar()
	sb.SetPosition(0, 22)
	sb.Resize(80, 2)
	ui.SetStatusBar(sb)

	// Create a button that shows a message in OnClick
	btn := widgets.NewButton("Test")
	btn.OnClick = func() {
		// This is what happens in the config editor
		sb.ShowSuccess("Clicked!")
		// NOTE: We do NOT call Render here - that would be a bug.
		// The standalone runner calls draw() AFTER HandleKey returns.
	}

	ui.AddWidget(btn)
	ui.Focus(btn)

	refreshCh := make(chan bool, 1)
	ui.SetRefreshNotifier(refreshCh)

	// Drain refresh channel (simulates the goroutine in runner.go)
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

	// This is the realistic flow from standalone runner:
	// 1. HandleKey (triggers OnClick which calls ShowSuccess)
	// 2. After HandleKey returns, call Render
	done := make(chan struct{})
	go func() {
		ev := tcell.NewEventKey(tcell.KeyEnter, 0, 0)
		ui.HandleKey(ev) // This triggers OnClick -> ShowSuccess
		ui.Render()      // This is called AFTER HandleKey returns
		close(done)
	}()

	select {
	case <-done:
		// Good - this is the expected flow
	case <-time.After(2 * time.Second):
		t.Fatal("HandleKey + Render (sequential) blocked - unexpected freeze")
	}

	sb.Stop()
	close(stopDrain)
}

// TestStatusBarGetterDuringCallback tests that calling UIManager.StatusBar()
// from within a callback during HandleKey would cause a deadlock if not handled.
// This test verifies the config editor's cached StatusBar approach works.
func TestStatusBarGetterDuringCallback(t *testing.T) {
	ui := core.NewUIManager()
	ui.Resize(80, 24)

	sb := widgets.NewStatusBar()
	sb.SetPosition(0, 22)
	sb.Resize(80, 2)
	ui.SetStatusBar(sb)

	// This is the DANGEROUS pattern that caused the original freeze:
	// Calling ui.StatusBar() from within a callback would try to acquire u.mu
	// while HandleKey already holds it (non-reentrant mutex = deadlock).
	//
	// The fix is to cache the StatusBar reference at initialization, which
	// is what ConfigEditor now does.
	//
	// This test verifies the pattern works with a cached reference.
	cachedSB := sb // Use the concrete *StatusBar directly (same as ConfigEditor caches)

	btn := widgets.NewButton("Test")
	btn.OnClick = func() {
		// Use cached reference instead of calling ui.StatusBar()
		if cachedSB != nil {
			cachedSB.ShowSuccess("Clicked!")
		}
	}

	ui.AddWidget(btn)
	ui.Focus(btn)

	refreshCh := make(chan bool, 1)
	ui.SetRefreshNotifier(refreshCh)

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

	done := make(chan struct{})
	go func() {
		ev := tcell.NewEventKey(tcell.KeyEnter, 0, 0)
		ui.HandleKey(ev)
		ui.Render()
		close(done)
	}()

	select {
	case <-done:
		// Good - cached reference pattern works
	case <-time.After(2 * time.Second):
		t.Fatal("Cached StatusBar reference pattern blocked - unexpected freeze")
	}

	sb.Stop()
	close(stopDrain)
}
