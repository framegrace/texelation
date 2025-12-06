// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests FF (Form Feed, \f, 0x0C) behavior.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/ff.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// The tests have been converted from Python to Go to enable offline, deterministic
// testing of the texelterm terminal emulator.
package esctest

import "testing"

// Test_FF_Basic tests that FF moves the cursor down one line.
func Test_FF_Basic(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	FF(d)
	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 5)
	AssertEQ(t, pos.Y, 4)
}

// Test_FF_Scrolls tests that FF scrolls when it hits the bottom.
func Test_FF_Scrolls(t *testing.T) {
	d := NewDriver(80, 24)

	// Put a and b on the last two lines.
	CUP(d, NewPoint(2, 23))
	d.WriteRaw("a")
	CUP(d, NewPoint(2, 24))
	d.WriteRaw("b")

	// Move to penultimate line.
	CUP(d, NewPoint(2, 23))

	// Move down, ensure no scroll yet.
	FF(d)
	AssertEQ(t, d.GetCursorPosition().Y, 24)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 22, 2, 24), []string{Empty(), "a", "b"})

	// Move down, ensure scroll.
	FF(d)
	AssertEQ(t, d.GetCursorPosition().Y, 24)
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 22, 2, 24), []string{"a", "b", Empty()})
}

// Test_FF_ScrollsInTopBottomRegionStartingAbove tests FF scrolls when it hits the bottom region (starting above top).
func Test_FF_ScrollsInTopBottomRegionStartingAbove(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 4, 5)
	CUP(d, NewPoint(2, 5))
	d.WriteRaw("x")

	CUP(d, NewPoint(2, 3))
	FF(d)
	FF(d)
	FF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 5))
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 4, 2, 5), []string{"x", Empty()})
}

// Test_FF_ScrollsInTopBottomRegionStartingWithin tests FF scrolls when it hits the bottom region (starting within region).
func Test_FF_ScrollsInTopBottomRegionStartingWithin(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 4, 5)
	CUP(d, NewPoint(2, 5))
	d.WriteRaw("x")

	CUP(d, NewPoint(2, 4))
	FF(d)
	FF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 5))
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 4, 2, 5), []string{"x", Empty()})
}

// Test_FF_MovesDoesNotScrollOutsideLeftRight tests cursor moves down but won't scroll when outside left-right region.
func Test_FF_MovesDoesNotScrollOutsideLeftRight(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 2, 5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)
	CUP(d, NewPoint(3, 5))
	d.WriteRaw("x")

	// Move past bottom margin but to the right of the left-right region
	CUP(d, NewPoint(6, 5))
	FF(d)
	// Cursor won't pass bottom or scroll.
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 5))
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Cursor can move down
	CUP(d, NewPoint(6, 4))
	FF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 5))

	// Try to move past the bottom of the screen but to the right of the left-right region
	CUP(d, NewPoint(6, 24))
	FF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 24))
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Move past bottom margin but to the left of the left-right region
	CUP(d, NewPoint(1, 5))
	FF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 5))
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})

	// Try to move past the bottom of the screen but to the left of the left-right region
	CUP(d, NewPoint(1, 24))
	FF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 24))
	AssertScreenCharsInRectEqual(t, d, NewRect(3, 5, 3, 5), []string{"x"})
}

// Test_FF_StopsAtBottomLineWhenBegunBelowScrollRegion tests when the cursor starts below the scroll region,
// FF moves it down to the bottom of the screen but won't scroll.
func Test_FF_StopsAtBottomLineWhenBegunBelowScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 4, 5)

	// Position the cursor below the scroll region
	CUP(d, NewPoint(1, 6))
	d.WriteRaw("x")

	// Move it down by a lot
	for i := 0; i < 24; i++ {
		FF(d)
	}

	// Ensure it stopped at the bottom of the screen
	AssertEQ(t, d.GetCursorPosition().Y, 24)

	// Ensure no scroll
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 6, 1, 6), []string{"x"})
}
