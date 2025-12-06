// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the VPA (Vertical Position Absolute) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/vpa.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_VPA_DefaultParams tests that with no params, VPA moves to 1st line.
func Test_VPA_DefaultParams(t *testing.T) {
	d := NewDriver(80, 24)
	VPA(d, 6)

	position := d.GetCursorPosition()
	AssertEQ(t, position.Y, 6)

	VPA(d)

	position = d.GetCursorPosition()
	AssertEQ(t, position.Y, 1)
}

// Test_VPA_StopsAtBottomEdge tests that VPA won't go past the bottom edge.
func Test_VPA_StopsAtBottomEdge(t *testing.T) {
	d := NewDriver(80, 24)
	// Position on 5th row
	CUP(d, NewPoint(6, 5))

	// Try to move 10 past the bottom edge
	size := d.GetScreenSize()
	VPA(d, size.Height+10)

	// Ensure at the bottom edge on same column
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 6)
	AssertEQ(t, position.Y, size.Height)
}

// Test_VPA_DoesNotChangeColumn tests that VPA moves to the specified line and does not change the column.
func Test_VPA_DoesNotChangeColumn(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(6, 5))
	VPA(d, 2)

	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 6)
	AssertEQ(t, position.Y, 2)
}

// Test_VPA_IgnoresOriginMode tests that VPA does not respect origin mode.
func Test_VPA_IgnoresOriginMode(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region.
	DECSTBM(d, 6, 11)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Move to center of region
	CUP(d, NewPoint(7, 9))
	position := d.GetCursorPosition()
	AssertEQ(t, position.Y, 9)
	AssertEQ(t, position.X, 7)

	// Turn on origin mode.
	DECSET(d, DECOM)

	// Move to 2nd line
	VPA(d, 2)

	position = d.GetCursorPosition()
	AssertEQ(t, position.Y, 2)
}
