// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests bracketed paste mode (DECSET 2004).
//
// Bracketed paste mode wraps pasted text in special escape sequences:
//   ESC[200~ ... pasted text ... ESC[201~
//
// This allows applications like neovim to:
// - Disable auto-indent during paste
// - Distinguish pasted text from typed text
// - Handle large pastes more efficiently
//
// References:
//   - https://cirw.in/blog/bracketed-paste
//   - xterm control sequences documentation
package esctest

import (
	"testing"
)

// Test_BracketedPaste_Enable tests enabling bracketed paste mode.
func Test_BracketedPaste_Enable(t *testing.T) {
	d := NewDriver(80, 24)

	// Enable bracketed paste mode (CSI ? 2004 h)
	d.WriteRaw("\x1b[?2004h")

	// Verify mode is enabled
	if !d.IsBracketedPasteModeEnabled() {
		t.Error("Bracketed paste mode should be enabled")
	}
}

// Test_BracketedPaste_Disable tests disabling bracketed paste mode.
func Test_BracketedPaste_Disable(t *testing.T) {
	d := NewDriver(80, 24)

	// Enable then disable bracketed paste mode
	d.WriteRaw("\x1b[?2004h")
	d.WriteRaw("\x1b[?2004l")

	// Verify mode is disabled
	if d.IsBracketedPasteModeEnabled() {
		t.Error("Bracketed paste mode should be disabled")
	}
}

// Test_BracketedPaste_DefaultOff tests that mode is off by default.
func Test_BracketedPaste_DefaultOff(t *testing.T) {
	d := NewDriver(80, 24)

	// Verify mode is off by default
	if d.IsBracketedPasteModeEnabled() {
		t.Error("Bracketed paste mode should be off by default")
	}
}

// Test_BracketedPaste_ResetByDECSTR tests that soft reset disables the mode.
func Test_BracketedPaste_ResetByDECSTR(t *testing.T) {
	d := NewDriver(80, 24)

	// Enable bracketed paste mode
	d.WriteRaw("\x1b[?2004h")

	// Perform soft reset
	DECSTR(d)

	// Verify mode is disabled after reset
	if d.IsBracketedPasteModeEnabled() {
		t.Error("Bracketed paste mode should be disabled after DECSTR")
	}
}

// Test_BracketedPaste_ResetByRIS tests that hard reset disables the mode.
func Test_BracketedPaste_ResetByRIS(t *testing.T) {
	d := NewDriver(80, 24)

	// Enable bracketed paste mode
	d.WriteRaw("\x1b[?2004h")

	// Perform hard reset
	RIS(d)

	// Verify mode is disabled after reset
	if d.IsBracketedPasteModeEnabled() {
		t.Error("Bracketed paste mode should be disabled after RIS")
	}
}

// Test_BracketedPaste_StateChangeCallback tests the state change callback.
func Test_BracketedPaste_StateChangeCallback(t *testing.T) {
	d := NewDriver(80, 24)

	// Track state changes
	var changes []bool
	d.SetBracketedPasteCallback(func(enabled bool) {
		changes = append(changes, enabled)
	})

	// Enable
	d.WriteRaw("\x1b[?2004h")

	// Disable
	d.WriteRaw("\x1b[?2004l")

	// Re-enable
	d.WriteRaw("\x1b[?2004h")

	// Verify we got all state changes
	expected := []bool{true, false, true}
	if len(changes) != len(expected) {
		t.Fatalf("Expected %d state changes, got %d", len(expected), len(changes))
	}

	for i, exp := range expected {
		if changes[i] != exp {
			t.Errorf("State change %d: expected %v, got %v", i, exp, changes[i])
		}
	}
}

// Test_BracketedPaste_Documentation is a documentation test showing usage.
func Test_BracketedPaste_Documentation(t *testing.T) {
	d := NewDriver(80, 24)

	// In a real terminal application, you would:
	//
	// 1. Set up a callback to know when the mode changes:
	//    d.SetBracketedPasteCallback(func(enabled bool) {
	//        if enabled {
	//            // When paste is detected, wrap it:
	//            // Send: ESC[200~ + pastedText + ESC[201~
	//        }
	//    })
	//
	// 2. The application (e.g., neovim) enables the mode:
	d.WriteRaw("\x1b[?2004h")
	//
	// 3. When user pastes text, your terminal wraps it:
	//    Input from clipboard: "hello\nworld"
	//    Send to application:  ESC[200~hello\nworldESC[201~
	//
	// 4. The application sees the ESC[200~/ESC[201~ markers and knows
	//    to disable auto-indent, treating the content as a single paste.

	// Just verify the mode is enabled for this documentation test
	if !d.IsBracketedPasteModeEnabled() {
		t.Error("Example should have enabled bracketed paste mode")
	}
}

// Test_BracketedPaste_MultipleEnableDisable tests rapid toggling.
func Test_BracketedPaste_MultipleEnableDisable(t *testing.T) {
	d := NewDriver(80, 24)

	// Toggle multiple times
	for i := 0; i < 5; i++ {
		d.WriteRaw("\x1b[?2004h")
		if !d.IsBracketedPasteModeEnabled() {
			t.Errorf("Iteration %d: mode should be enabled", i)
		}

		d.WriteRaw("\x1b[?2004l")
		if d.IsBracketedPasteModeEnabled() {
			t.Errorf("Iteration %d: mode should be disabled", i)
		}
	}
}

// Test_BracketedPaste_PersistsAcrossScreenSwitch tests that mode persists across alt screen.
func Test_BracketedPaste_PersistsAcrossScreenSwitch(t *testing.T) {
	d := NewDriver(80, 24)

	// Enable bracketed paste mode
	d.WriteRaw("\x1b[?2004h")

	// Switch to alt screen
	DECSET(d, 1049) // Enable alt screen with cursor save

	// Mode should still be enabled
	if !d.IsBracketedPasteModeEnabled() {
		t.Error("Bracketed paste mode should persist in alt screen")
	}

	// Switch back to main screen
	DECRESET(d, 1049)

	// Mode should still be enabled
	if !d.IsBracketedPasteModeEnabled() {
		t.Error("Bracketed paste mode should persist when returning to main screen")
	}
}

// Test_BracketedPaste_CompatibilityNote documents compatibility expectations.
func Test_BracketedPaste_CompatibilityNote(t *testing.T) {
	// This test just documents expected behavior.
	//
	// Applications that use bracketed paste mode:
	// - neovim/vim (when 'paste' option is managed)
	// - emacs
	// - zsh (with BRACKETED_PASTE option)
	// - fish shell
	//
	// The terminal's responsibility:
	// 1. Track the mode state (DECSET/DECRESET 2004)
	// 2. When mode is enabled AND paste is detected:
	//    - Prefix paste with: ESC[200~
	//    - Suffix paste with: ESC[201~
	//
	// The application's responsibility:
	// 1. Enable the mode: CSI ? 2004 h
	// 2. Watch for ESC[200~ to know paste started
	// 3. Watch for ESC[201~ to know paste ended
	// 4. Disable auto-formatting during paste

	// No actual test needed - just documentation
	t.Log("Bracketed paste mode compatibility documented")
}
