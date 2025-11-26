// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CHA (Cursor Horizontal Absolute) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cha.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CHA_DefaultParam tests that CHA moves to first column of active line by default.
func Test_CHA_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CHA(d)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 3)
}

// Test_CHA_ExplicitParam tests that CHA moves to specified column of active line.
func Test_CHA_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CHA(d, 10)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 10)
	AssertEQ(t, position.Y, 3)
}

// Test_CHA_OutOfBoundsLarge tests that CHA moves as far as possible when given a too-large parameter.
func Test_CHA_OutOfBoundsLarge(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CHA(d, 9999)
	position := d.GetCursorPosition()
	width := d.GetScreenSize().Width
	AssertEQ(t, position.X, width)
	AssertEQ(t, position.Y, 3)
}

// Test_CHA_ZeroParam tests that CHA moves as far left as possible when given a zero parameter.
func Test_CHA_ZeroParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CHA(d, 0)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 3)
}

// Test_CHA_IgnoresScrollRegion tests that CHA ignores scroll regions.
func Test_CHA_IgnoresScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region.
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)
	CUP(d, NewPoint(5, 3))
	CHA(d, 1)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 3)
}

// Test_CHA_RespectsOriginMode tests that CHA is relative to left margin in origin mode.
func Test_CHA_RespectsOriginMode(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region.
	DECSTBM(d, 6, 11)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Move to center of region
	CUP(d, NewPoint(7, 9))
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 7)
	AssertEQ(t, position.Y, 9)

	// Turn on origin mode.
	DECSET(d, DECOM)

	// Move to top but not the left, so CHA has something to do.
	CUP(d, NewPoint(2, 1))

	// Move to leftmost column in the scroll region.
	CHA(d, 1)

	// Check relative position while still in origin mode.
	position = d.GetCursorPosition()
	AssertEQ(t, position.X, 1)

	d.Write("X")

	// Turn off origin mode. This moves the cursor.
	DECRESET(d, DECOM)

	// Turn off scroll regions so checksum can work.
	DECSTBM(d, 0, 0) // Reset margins
	DECRESET(d, DECLRMM)

	// Make sure there's an X at 5,6
	AssertScreenCharsInRectEqual(t, d, NewRect(5, 6, 5, 6),
		[]string{"X"})
}
