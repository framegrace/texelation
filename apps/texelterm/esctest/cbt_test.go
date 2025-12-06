// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CBT (Cursor Backward Tab) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cbt.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CBT_OneTabStopByDefault tests that CBT with no params goes back one tab stop.
func Test_CBT_OneTabStopByDefault(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(17, 1))
	CBT(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 9)
}

// Test_CBT_ExplicitParameter tests that CBT goes back n tab stops.
func Test_CBT_ExplicitParameter(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(25, 1))
	CBT(d, 2)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 9)
}

// Test_CBT_StopsAtLeftEdge tests that CBT stops at the left edge.
func Test_CBT_StopsAtLeftEdge(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(25, 2))
	CBT(d, 5)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 1)
	AssertEQ(t, pos.Y, 2)
}

// Test_CBT_IgnoresRegion tests that CBT ignores left/right margins.
func Test_CBT_IgnoresRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set left/right margins
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 30)

	// Move to center of region
	CUP(d, NewPoint(7, 9))

	// Tab backwards out of the region
	CBT(d, 2)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 1)
}
