// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the VPR (Vertical Position Relative) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/vpr.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_VPR_DefaultParams tests that VPR with no params moves down by 1.
func Test_VPR_DefaultParams(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 6))
	VPR(d)

	AssertEQ(t, d.GetCursorPosition().Y, 7)
}

// Test_VPR_StopsAtBottomEdge tests that VPR won't go past the bottom edge.
func Test_VPR_StopsAtBottomEdge(t *testing.T) {
	d := NewDriver(80, 24)

	// Position on column 5
	CUP(d, NewPoint(5, 6))

	// Try to move 10 past the bottom edge
	VPR(d, 24+10)

	// Should be at bottom edge on same column
	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 5)
	AssertEQ(t, pos.Y, 24)
}

// Test_VPR_DoesNotChangeColumn tests that VPR only changes row, not column.
func Test_VPR_DoesNotChangeColumn(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(5, 6))
	VPR(d, 2)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 5)
	AssertEQ(t, pos.Y, 8)
}

// Test_VPR_IgnoresOriginMode tests that VPR continues to work in origin mode.
func Test_VPR_IgnoresOriginMode(t *testing.T) {
	d := NewDriver(80, 24)

	// Set scroll region
	DECSTBM(d, 6, 11)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Enter origin mode
	DECSET(d, DECOM)

	// Move to center of region (relative coords in origin mode)
	CUP(d, NewPoint(2, 2))
	d.Write("X")

	// Move down by 2
	VPR(d, 2)
	d.Write("Y")

	// Exit origin mode
	DECRESET(d, DECOM)

	// Reset margins
	DECSET(d, DECLRMM)
	DECSTBM(d, 0, 0)

	// Check result - X at row 7, Y at row 9 (7+2)
	AssertScreenCharsInRectEqual(t, d, NewRect(6, 7, 7, 9), []string{
		"X ",
		"  ",
		" Y",
	})
}
