// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CUB (Cursor Backward) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cub.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: Tests involving reverse-wrap (test_CUB_AfterNoWrappedInlines, test_CUB_AfterOneWrappedInline)
// have been omitted as they test advanced xterm-specific reverse-wrap behavior.
package esctest

import "testing"

// Test_CUB_DefaultParam tests that CUB moves the cursor left 1 with no parameter given.
func Test_CUB_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CUB(d)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 4)
	AssertEQ(t, position.Y, 3)
}

// Test_CUB_ExplicitParam tests that CUB moves the cursor left by the passed-in number of columns.
func Test_CUB_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 4))
	CUB(d, 2)
	AssertEQ(t, d.GetCursorPosition().X, 3)
}

// Test_CUB_StopsAtLeftEdge tests that CUB moves the cursor left, stopping at the first column.
func Test_CUB_StopsAtLeftEdge(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CUB(d, 99)
	AssertEQ(t, d.GetCursorPosition().X, 1)
}

// Test_CUB_StopsAtLeftEdgeWhenBegunLeftOfScrollRegion tests that when the cursor starts left of
// the scroll region, CUB moves it left to the left edge of the screen.
func Test_CUB_StopsAtLeftEdgeWhenBegunLeftOfScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region.
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Position the cursor left of the scroll region
	CUP(d, NewPoint(4, 3))

	// Move it left by a lot
	CUB(d, 99)

	// Ensure it stopped at the left edge of the screen
	AssertEQ(t, d.GetCursorPosition().X, 1)
}

// Test_CUB_StopsAtLeftMarginInScrollRegion tests that when the cursor starts within the scroll
// region, CUB moves it left to the left margin but no farther.
func Test_CUB_StopsAtLeftMarginInScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region.
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Position the cursor within the scroll region
	CUP(d, NewPoint(7, 3))

	// Move it left by more than the width of the scroll region
	CUB(d, 99)

	// Ensure it stopped at the left margin of the scroll region.
	AssertEQ(t, d.GetCursorPosition().X, 5)
}
