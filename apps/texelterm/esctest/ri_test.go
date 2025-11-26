// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the RI (Reverse Index) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/ri.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: 8-bit control test skipped as we don't support 8-bit controls.
package esctest

import "testing"

// Test_RI_Basic tests that RI moves cursor up one line.
func Test_RI_Basic(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	RI(d)
	AssertEQ(t, d.GetCursorPosition().X, 5)
	AssertEQ(t, d.GetCursorPosition().Y, 2)
}

// Test_RI_Scrolls tests that RI scrolls when it hits top.
func Test_RI_Scrolls(t *testing.T) {
	d := NewDriver(80, 24)

	// Put a and b on first two lines
	CUP(d, NewPoint(2, 1))
	d.Write("a")
	CUP(d, NewPoint(2, 2))
	d.Write("b")

	// Move to second line
	CUP(d, NewPoint(2, 2))

	// Move up, ensure no scroll yet
	RI(d)
	AssertEQ(t, d.GetCursorPosition().Y, 1)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 1, 2, 3),
		[]string{"a", "b", " "})

	// Move up, ensure scroll
	RI(d)
	AssertEQ(t, d.GetCursorPosition().Y, 1)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 1, 2, 3),
		[]string{" ", "a", "b"})
}

// Test_RI_ScrollsInTopBottomRegionStartingBelow tests RI with scroll region from below.
func Test_RI_ScrollsInTopBottomRegionStartingBelow(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 4, 5)
	CUP(d, NewPoint(2, 4))
	d.Write("x")

	CUP(d, NewPoint(2, 6))
	RI(d) // To 5
	RI(d) // To 4
	RI(d) // Stay at 4 and scroll x down one line

	AssertEQ(t, d.GetCursorPosition().X, 2)
	AssertEQ(t, d.GetCursorPosition().Y, 4)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 4, 2, 5),
		[]string{" ", "x"})
}

// Test_RI_ScrollsInTopBottomRegionStartingWithin tests RI within scroll region.
func Test_RI_ScrollsInTopBottomRegionStartingWithin(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 4, 5)
	CUP(d, NewPoint(2, 4))
	d.Write("x")

	CUP(d, NewPoint(2, 5))
	RI(d) // To 4
	RI(d) // Stay at 4 and scroll x down one line

	AssertEQ(t, d.GetCursorPosition().X, 2)
	AssertEQ(t, d.GetCursorPosition().Y, 4)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 4, 2, 5),
		[]string{" ", "x"})
}

// Test_RI_MovesDoesNotScrollOutsideLeftRight tests RI respects left/right margins.
func Test_RI_MovesDoesNotScrollOutsideLeftRight(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 2, 5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)
	CUP(d, NewPoint(3, 5))
	d.Write("x")

	// Move past top margin but to the right of left-right region
	CUP(d, NewPoint(6, 2))
	RI(d)
	// Cursor won't pass top or scroll
	AssertEQ(t, d.GetCursorPosition().X, 6)
	AssertEQ(t, d.GetCursorPosition().Y, 2)
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Try to move past top of screen but to right of left-right region
	CUP(d, NewPoint(6, 1))
	RI(d)
	AssertEQ(t, d.GetCursorPosition().X, 6)
	AssertEQ(t, d.GetCursorPosition().Y, 1)
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Move past top margin but to left of left-right region
	CUP(d, NewPoint(1, 2))
	RI(d)
	AssertEQ(t, d.GetCursorPosition().X, 1)
	AssertEQ(t, d.GetCursorPosition().Y, 2)
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Try to move past top of screen but to left of left-right region
	CUP(d, NewPoint(1, 1))
	RI(d)
	AssertEQ(t, d.GetCursorPosition().X, 1)
	AssertEQ(t, d.GetCursorPosition().Y, 1)
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})
}

// Test_RI_StopsAtTopLineWhenBegunAboveScrollRegion tests RI above region.
func Test_RI_StopsAtTopLineWhenBegunAboveScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	// Set scroll region
	DECSTBM(d, 4, 5)

	// Position cursor above scroll region
	CUP(d, NewPoint(1, 3))
	d.Write("x")

	// Move up by a lot
	for i := 0; i < 24; i++ {
		RI(d)
	}

	// Should stop at top of screen
	AssertEQ(t, d.GetCursorPosition().Y, 1)

	// Ensure no scroll - x should still be at line 3
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 3, 1, 3), []string{"x"})
}
