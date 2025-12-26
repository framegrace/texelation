// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for save/restore cursor (DECSC/DECRC) escape sequences.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/save_restore_cursor.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: Only basic save/restore tests have been converted. Advanced tests
// involving protection (DECSCA), wrap modes, and insert mode have been omitted.
package esctest

import "testing"

// Test_SaveRestoreCursor_Basic tests basic save and restore functionality.
func Test_SaveRestoreCursor_Basic(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 6))
	DECSC(d) // Save
	CUP(d, NewPoint(1, 1))
	DECRC(d) // Restore
	AssertEQ(t, d.GetCursorPosition(), NewPoint(5, 6))
}

// Test_SaveRestoreCursor_MoveToHomeWhenNotSaved tests that restore without save moves to home.
func Test_SaveRestoreCursor_MoveToHomeWhenNotSaved(t *testing.T) {
	d := NewDriver(80, 24)
	// Don't save cursor, just try to restore
	CUP(d, NewPoint(5, 6))
	DECRC(d) // Restore without save should move to 1,1
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 1))
}

// Test_SaveRestoreCursor_AltVsMain tests that alternate and main screens have separate saved cursor state.
func Test_SaveRestoreCursor_AltVsMain(t *testing.T) {
	d := NewDriver(80, 24)

	// Driver starts in alt screen, so exit to main first
	DECRESET(d, 1049)

	// Save in main screen
	CUP(d, NewPoint(2, 3))
	DECSC(d)

	// Switch to alternate screen
	DECSET(d, 1049) // ALTBUF with cursor save

	// Save different position in alt screen
	CUP(d, NewPoint(6, 7))
	DECSC(d)

	// Switch back to main screen
	DECRESET(d, 1049)

	// Restore in main screen should get main screen's saved position
	DECRC(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 3))

	// Switch back to alt screen
	DECSET(d, 1049)

	// Restore in alt screen should get alt screen's saved position
	DECRC(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 7))
}

// Test_SaveRestoreCursor_ResetsOriginMode tests that restore resets origin mode.
func Test_SaveRestoreCursor_ResetsOriginMode(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(5, 6))
	DECSC(d)

	// Set up margins.
	DECSTBM(d, 5, 7)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 7)

	// Enter origin mode.
	DECSET(d, DECOM)

	// Do DECRC, which should reset origin mode.
	DECRC(d)

	// Move home
	CUP(d, NewPoint(1, 1))

	// Place an X at cursor, which should be at (1, 1) if DECOM was reset.
	d.Write("X")

	// Remove margins and ensure origin mode is off for valid test.
	DECRESET(d, DECLRMM)
	DECSTBM(d, 0, 0)
	DECRESET(d, DECOM)

	// Ensure the X was placed at the true origin
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 1),
		[]string{"X"})
}

// Test_SaveRestoreCursor_WorksInLRM tests that save/restore works when left/right margins are active.
func Test_SaveRestoreCursor_WorksInLRM(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(2, 3))
	DECSC(d)

	DECSET(d, DECLRMM)
	DECSLRM(d, 1, 10)

	CUP(d, NewPoint(5, 6))
	DECSC(d)

	CUP(d, NewPoint(4, 5))
	DECRC(d)

	// Should restore to most recent save (5, 6)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(5, 6))
}
