// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the HPA (Horizontal Position Absolute) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/hpa.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_HPA_DefaultParams tests that HPA with no params moves to column 1.
func Test_HPA_DefaultParams(t *testing.T) {
	d := NewDriver(80, 24)

	// Move to column 6
	HPA(d, 6)
	AssertEQ(t, d.GetCursorPosition().X, 6)

	// HPA with no params should move to column 1
	HPA(d)
	AssertEQ(t, d.GetCursorPosition().X, 1)
}

// Test_HPA_StopsAtRightEdge tests that HPA won't go past the right edge.
func Test_HPA_StopsAtRightEdge(t *testing.T) {
	d := NewDriver(80, 24)

	// Position on row 6
	CUP(d, NewPoint(5, 6))

	// Try to move 10 past the right edge
	HPA(d, 80+10)

	// Should be at right edge on same row
	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 80)
	AssertEQ(t, pos.Y, 6)
}

// Test_HPA_DoesNotChangeRow tests that HPA only changes column, not row.
func Test_HPA_DoesNotChangeRow(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(5, 6))
	HPA(d, 2)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 2)
	AssertEQ(t, pos.Y, 6)
}

// Test_HPA_IgnoresOriginMode tests that HPA does not respect origin mode.
func Test_HPA_IgnoresOriginMode(t *testing.T) {
	d := NewDriver(80, 24)

	// Set scroll region
	DECSTBM(d, 6, 11)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Move to center of region
	CUP(d, NewPoint(7, 9))
	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 7)
	AssertEQ(t, pos.Y, 9)

	// Turn on origin mode
	DECSET(d, DECOM)

	// Move to column 2 - should be absolute, not relative to margins
	HPA(d, 2)

	pos = d.GetCursorPosition()
	AssertEQ(t, pos.X, 2)
}
