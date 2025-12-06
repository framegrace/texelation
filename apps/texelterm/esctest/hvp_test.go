// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the HVP (Horizontal and Vertical Position) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/hvp.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_HVP_DefaultParams tests that with no params, HVP moves to 1,1.
func Test_HVP_DefaultParams(t *testing.T) {
	d := NewDriver(80, 24)
	HVP(d, NewPoint(6, 3))

	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 6)
	AssertEQ(t, position.Y, 3)

	HVP(d)

	position = d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 1)
}

// Test_HVP_OutOfBoundsParams tests that with overly large parameters, HVP moves as far as possible.
func Test_HVP_OutOfBoundsParams(t *testing.T) {
	d := NewDriver(80, 24)
	size := d.GetScreenSize()
	HVP(d, NewPoint(size.Width+10, size.Height+10))

	position := d.GetCursorPosition()
	AssertEQ(t, position.X, size.Width)
	AssertEQ(t, position.Y, size.Height)
}

// Test_HVP_RespectsOriginMode tests that HVP is relative to margins in origin mode.
func Test_HVP_RespectsOriginMode(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region.
	DECSTBM(d, 6, 11)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Move to center of region
	HVP(d, NewPoint(7, 9))
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 7)
	AssertEQ(t, position.Y, 9)

	// Turn on origin mode.
	DECSET(d, DECOM)

	// Move to top-left
	HVP(d, NewPoint(1, 1))

	// Check relative position while still in origin mode.
	position = d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 1)

	d.Write("X")

	// Turn off origin mode. This moves the cursor.
	DECRESET(d, DECOM)

	// Turn off scroll regions so checksum can work.
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	// Make sure there's an X at 5,6
	AssertScreenCharsInRectEqual(t, d, NewRect(5, 6, 5, 6),
		[]string{"X"})
}
