// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the HPR (Horizontal Position Relative) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/hpr.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_HPR_DefaultParams tests that HPR with no params moves right by 1.
func Test_HPR_DefaultParams(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(6, 1))
	HPR(d)

	AssertEQ(t, d.GetCursorPosition().X, 7)
}

// Test_HPR_StopsAtRightEdge tests that HPR won't go past the right edge.
func Test_HPR_StopsAtRightEdge(t *testing.T) {
	d := NewDriver(80, 24)

	// Position on row 6
	CUP(d, NewPoint(5, 6))

	// Try to move 10 past the right edge
	HPR(d, 80+10)

	// Should be at right edge on same row
	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 80)
	AssertEQ(t, pos.Y, 6)
}

// Test_HPR_DoesNotChangeRow tests that HPR only changes column, not row.
func Test_HPR_DoesNotChangeRow(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(5, 6))
	HPR(d, 2)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 7)
	AssertEQ(t, pos.Y, 6)
}

// Test_HPR_IgnoresOriginMode tests that HPR continues to work in origin mode.
func Test_HPR_IgnoresOriginMode(t *testing.T) {
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

	// Move right by 2
	HPR(d, 2)
	d.Write("Y")

	// Exit origin mode
	DECRESET(d, DECOM)

	// Reset margins
	DECSET(d, DECLRMM)
	DECSTBM(d, 0, 0)

	// Check result - X at absolute col 6, Y at absolute col 9 (6+2+1 for X itself)
	AssertScreenCharsInRectEqual(t, d, NewRect(5, 7, 9, 7), []string{" X  Y"})
}
