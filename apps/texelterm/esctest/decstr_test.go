// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests DECSTR (Soft Terminal Reset).
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/decstr.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// DECSTR resets terminal modes and settings but does NOT clear the screen or move the cursor.
// This differs from RIS (Reset to Initial State) which does a full hard reset.
package esctest

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// Test_DECSTR_DECSC tests that DECSTR resets saved cursor position to origin.
func Test_DECSTR_DECSC(t *testing.T) {
	d := NewDriver(80, 24)

	// Save cursor at position (5, 6)
	CUP(d, NewPoint(5, 6))
	DECSC(d)

	// Perform soft reset
	DECSTR(d)

	// Restore cursor - should be at origin (1,1) not (5,6)
	DECRC(d)
	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(1, 1))
}

// Test_DECSTR_IRM tests that DECSTR resets insert mode to replace mode.
func Test_DECSTR_IRM(t *testing.T) {
	d := NewDriver(80, 24)

	// Turn on insert mode
	SM(d, IRM)

	// Perform soft reset
	DECSTR(d)

	// Ensure replace mode is active (not insert mode)
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("a")
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("b")

	// 'b' should have overwritten 'a', not inserted before it
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 1), []string{"b"})
}

// Test_DECSTR_DECOM tests that DECSTR resets origin mode to off.
func Test_DECSTR_DECOM(t *testing.T) {
	d := NewDriver(80, 24)

	// Define scroll region and turn on origin mode
	DECSTBM(d, 3, 4)
	DECSET(d, DECOM)

	// Perform soft reset
	DECSTR(d)

	// Define new margins
	DECSET(d, DECLRMM)
	DECSLRM(d, 3, 4)
	DECSTBM(d, 4, 5)

	// Move to (1,1) - with origin mode off, this is absolute (1,1), not relative to margins
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("X")

	// Turn off margins to check where X ended up
	DECRESET(d, DECOM)
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	// X should be at absolute (1,1), proving origin mode was off
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 4), []string{
		"X  ",
		"   ",
		"   ",
		"   ",
	})
}

// Test_DECSTR_DECAWM tests that DECSTR keeps autowrap ON (xterm compatibility).
func Test_DECSTR_DECAWM(t *testing.T) {
	d := NewDriver(80, 24)

	// Turn on autowrap (should already be on by default)
	DECSET(d, DECAWM)

	// Perform soft reset
	DECSTR(d)

	// Autowrap should still be on
	// Write past right edge - should wrap to next line
	CUP(d, NewPoint(79, 1))
	d.WriteRaw("xxx")

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 2) // Should be at column 2 on line 2 (wrapped)
}

// Test_DECSTR_STBM tests that DECSTR resets top/bottom margins to full screen.
func Test_DECSTR_STBM(t *testing.T) {
	d := NewDriver(80, 24)

	// Set top and bottom margins
	DECSTBM(d, 3, 4)

	// Perform soft reset
	DECSTR(d)

	// Margins should be gone - moving down from line 4 should go to line 5
	CUP(d, NewPoint(1, 4))
	CR(d)
	LF(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.Y, 5) // Should be at line 5 (no scroll region constraint)
}

// Test_DECSTR_DECLRMM tests that DECSTR resets left/right margins.
func Test_DECSTR_DECLRMM(t *testing.T) {
	d := NewDriver(80, 24)

	// Set left/right margins
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 6)

	// Perform soft reset
	DECSTR(d)

	// Margins should be gone - writing "ab" at column 5 should reach column 7
	CUP(d, NewPoint(5, 5))
	d.WriteRaw("ab")

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 7) // Should be at column 7 (no left/right margin constraint)
}

// Test_DECSTR_CursorStaysPut tests that DECSTR does NOT move the cursor.
func Test_DECSTR_CursorStaysPut(t *testing.T) {
	d := NewDriver(80, 24)

	// Move cursor to (5, 6)
	CUP(d, NewPoint(5, 6))

	// Perform soft reset
	DECSTR(d)

	// Cursor should still be at (5, 6)
	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 5)
	AssertEQ(t, pos.Y, 6)
}

// Test_DECSTR_SGR tests that DECSTR resets SGR attributes to normal.
func Test_DECSTR_SGR(t *testing.T) {
	d := NewDriver(80, 24)

	// Set some attributes
	SGR(d, SGR_BOLD, SGR_FG_RED)

	// Perform soft reset
	DECSTR(d)

	// Write text - should have no attributes or colors
	d.WriteRaw("X")

	// Check that text has default attributes and colors
	cell := d.GetCellAt(NewPoint(1, 1))
	if cell.Attr != 0 {
		t.Errorf("Expected no attributes, got %v", cell.Attr)
	}
	AssertCellForegroundColor(t, d, NewPoint(1, 1), parser.DefaultFG, "Should have default FG color")
}
