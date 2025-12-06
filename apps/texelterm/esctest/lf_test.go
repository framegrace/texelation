// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the LF (Line Feed) control character.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/lf.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: LNM (Line Feed/New Line Mode) is tested in SM tests, not duplicated here.
// These tests are the same as those for IND (Index).
package esctest

import "testing"

// Test_LF_Basic tests that LF moves the cursor down one line.
func Test_LF_Basic(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(5, 3))
	LF(d)
	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 5)
	AssertEQ(t, pos.Y, 4)
}

// Test_LF_Scrolls tests that LF scrolls when it hits the bottom.
func Test_LF_Scrolls(t *testing.T) {
	d := NewDriver(80, 24)

	// Put a and b on the last two lines
	CUP(d, NewPoint(2, 23))
	d.Write("a")
	CUP(d, NewPoint(2, 24))
	d.Write("b")

	// Move to penultimate line
	CUP(d, NewPoint(2, 23))

	// Move down, ensure no scroll yet
	LF(d)
	AssertEQ(t, d.GetCursorPosition().Y, 24)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 22, 2, 24), []string{" ", "a", "b"})

	// Move down, ensure scroll
	LF(d)
	AssertEQ(t, d.GetCursorPosition().Y, 24)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 22, 2, 24), []string{"a", "b", " "})
}

// Test_LF_ScrollsInTopBottomRegionStartingAbove tests that LF scrolls when it hits
// the bottom region (starting above top).
func Test_LF_ScrollsInTopBottomRegionStartingAbove(t *testing.T) {
	d := NewDriver(80, 24)

	DECSTBM(d, 4, 5)
	CUP(d, NewPoint(2, 5))
	d.Write("x")

	CUP(d, NewPoint(2, 3))
	LF(d)
	LF(d)
	LF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 5))
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 4, 2, 5), []string{"x", " "})
}

// Test_LF_ScrollsInTopBottomRegionStartingWithin tests that LF scrolls when it hits
// the bottom region (starting within region).
func Test_LF_ScrollsInTopBottomRegionStartingWithin(t *testing.T) {
	d := NewDriver(80, 24)

	DECSTBM(d, 4, 5)
	CUP(d, NewPoint(2, 5))
	d.Write("x")

	CUP(d, NewPoint(2, 4))
	LF(d)
	LF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 5))
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 4, 2, 5), []string{"x", " "})
}

// Test_LF_MovesDoesNotScrollOutsideLeftRight tests that cursor moves down but won't scroll
// when outside left-right region.
func Test_LF_MovesDoesNotScrollOutsideLeftRight(t *testing.T) {
	d := NewDriver(80, 24)

	DECSTBM(d, 2, 5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)
	CUP(d, NewPoint(3, 5))
	d.Write("x")

	// Move past bottom margin but to the right of the left-right region
	CUP(d, NewPoint(6, 5))
	LF(d)
	// Cursor won't pass bottom or scroll
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 5))
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Cursor can move down
	CUP(d, NewPoint(6, 4))
	LF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 5))

	// Try to move past the bottom of the screen but to the right of the left-right region
	CUP(d, NewPoint(6, 24))
	LF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 24))
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Move past bottom margin but to the left of the left-right region
	CUP(d, NewPoint(1, 5))
	LF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 5))
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Try to move past the bottom of the screen but to the left of the left-right region
	CUP(d, NewPoint(1, 24))
	LF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 24))
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})
}

// Test_LF_StopsAtBottomLineWhenBegunBelowScrollRegion tests that when the cursor starts below
// the scroll region, LF moves it down to the bottom of the screen but won't scroll.
func Test_LF_StopsAtBottomLineWhenBegunBelowScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 4, 5)

	// Position the cursor below the scroll region
	CUP(d, NewPoint(1, 6))
	d.Write("x")

	// Move it down by a lot
	for i := 0; i < 24; i++ {
		LF(d)
	}

	// Ensure it stopped at the bottom of the screen
	AssertEQ(t, d.GetCursorPosition().Y, 24)

	// Ensure no scroll
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 6, 1, 6), []string{"x"})
}
