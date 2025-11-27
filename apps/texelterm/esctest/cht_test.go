// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CHT (Cursor Horizontal Tab) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cht.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CHT_OneTabStopByDefault tests that CHT with no params goes forward one tab stop.
func Test_CHT_OneTabStopByDefault(t *testing.T) {
	d := NewDriver(80, 24)

	CHT(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 9)
}

// Test_CHT_ExplicitParameter tests that CHT goes forward n tab stops.
func Test_CHT_ExplicitParameter(t *testing.T) {
	d := NewDriver(80, 24)

	CHT(d, 2)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 17)
}

// Test_CHT_IgnoresScrollingRegion tests that CHT respects right margin when DECLRMM is active.
// In DEC terminals (and xterm), tabs stop at the right margin.
func Test_CHT_IgnoresScrollingRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set left/right margins
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 30)

	// Move to center of region
	CUP(d, NewPoint(7, 9))

	// Ensure we can tab within the region
	CHT(d, 2)
	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 17)

	// Ensure that we can't tab out of the region - stops at right margin
	CHT(d, 2)
	pos = d.GetCursorPosition()
	AssertEQ(t, pos.X, 30)

	// Try again, starting before the region
	CUP(d, NewPoint(1, 9))
	CHT(d, 9)
	pos = d.GetCursorPosition()
	AssertEQ(t, pos.X, 30)
}
