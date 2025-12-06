// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the IND (Index) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/ind.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: 8-bit control test skipped as we don't support 8-bit controls.
package esctest

import "testing"

// Test_IND_Basic tests that IND moves cursor down one line.
func Test_IND_Basic(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	IND(d)
	AssertEQ(t, d.GetCursorPosition().X, 5)
	AssertEQ(t, d.GetCursorPosition().Y, 4)
}

// Test_IND_Scrolls tests that IND scrolls when it hits bottom.
func Test_IND_Scrolls(t *testing.T) {
	d := NewDriver(80, 24)

	// Put a and b on last two lines
	CUP(d, NewPoint(2, 23))
	d.Write("a")
	CUP(d, NewPoint(2, 24))
	d.Write("b")

	// Move to penultimate line
	CUP(d, NewPoint(2, 23))

	// Move down, ensure no scroll yet
	IND(d)
	AssertEQ(t, d.GetCursorPosition().Y, 24)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 22, 2, 24),
		[]string{" ", "a", "b"})

	// Move down, ensure scroll
	IND(d)
	AssertEQ(t, d.GetCursorPosition().Y, 24)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 22, 2, 24),
		[]string{"a", "b", " "})
}

// Test_IND_ScrollsInTopBottomRegionStartingAbove tests IND with scroll region from above.
func Test_IND_ScrollsInTopBottomRegionStartingAbove(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 4, 5)
	CUP(d, NewPoint(2, 5))
	d.Write("x")

	CUP(d, NewPoint(2, 3))
	IND(d) // To 4
	IND(d) // To 5
	IND(d) // Stay at 5 and scroll x up one line

	AssertEQ(t, d.GetCursorPosition().X, 2)
	AssertEQ(t, d.GetCursorPosition().Y, 5)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 4, 2, 5),
		[]string{"x", " "})
}

// Test_IND_ScrollsInTopBottomRegionStartingWithin tests IND within scroll region.
func Test_IND_ScrollsInTopBottomRegionStartingWithin(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 4, 5)
	CUP(d, NewPoint(2, 5))
	d.Write("x")

	CUP(d, NewPoint(2, 4))
	IND(d) // To 5
	IND(d) // Stay at 5 and scroll x up one line

	AssertEQ(t, d.GetCursorPosition().X, 2)
	AssertEQ(t, d.GetCursorPosition().Y, 5)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 4, 2, 5),
		[]string{"x", " "})
}

// Test_IND_MovesDoesNotScrollOutsideLeftRight tests IND respects left/right margins.
func Test_IND_MovesDoesNotScrollOutsideLeftRight(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 2, 5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)
	CUP(d, NewPoint(3, 5))
	d.Write("x")

	// Move past bottom margin but to the right of left-right region
	CUP(d, NewPoint(6, 5))
	IND(d)
	// Cursor won't pass bottom or scroll
	AssertEQ(t, d.GetCursorPosition().X, 6)
	AssertEQ(t, d.GetCursorPosition().Y, 5)
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Try to move past bottom of screen but to right of left-right region
	CUP(d, NewPoint(6, 24))
	IND(d)
	AssertEQ(t, d.GetCursorPosition().X, 6)
	AssertEQ(t, d.GetCursorPosition().Y, 24)
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Move past bottom margin but to left of left-right region
	CUP(d, NewPoint(1, 5))
	IND(d)
	AssertEQ(t, d.GetCursorPosition().X, 1)
	AssertEQ(t, d.GetCursorPosition().Y, 5)
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Try to move past bottom of screen but to left of left-right region
	CUP(d, NewPoint(1, 24))
	IND(d)
	AssertEQ(t, d.GetCursorPosition().X, 1)
	AssertEQ(t, d.GetCursorPosition().Y, 24)
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})
}

// Test_IND_StopsAtBottomLineWhenBegunBelowScrollRegion tests IND below region.
func Test_IND_StopsAtBottomLineWhenBegunBelowScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	// Set scroll region
	DECSTBM(d, 4, 5)

	// Position cursor below scroll region
	CUP(d, NewPoint(1, 6))
	d.Write("x")

	// Move down by a lot
	for i := 0; i < 24; i++ {
		IND(d)
	}

	// Should stop at bottom of screen
	AssertEQ(t, d.GetCursorPosition().Y, 24)

	// Ensure no scroll - x should still be at line 6
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 6, 1, 6), []string{"x"})
}
