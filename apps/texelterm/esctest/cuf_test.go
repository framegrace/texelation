// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CUF (Cursor Forward) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cuf.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CUF_DefaultParam tests that CUF moves the cursor right 1 with no parameter given.
func Test_CUF_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CUF(d)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 6)
	AssertEQ(t, position.Y, 3)
}

// Test_CUF_ExplicitParam tests that CUF moves the cursor right by the passed-in number of columns.
func Test_CUF_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 2))
	CUF(d, 2)
	AssertEQ(t, d.GetCursorPosition().X, 3)
}

// Test_CUF_StopsAtRightSide tests that CUF moves the cursor right, stopping at the right edge.
func Test_CUF_StopsAtRightSide(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 3))
	width := d.GetScreenSize().Width
	CUF(d, width)
	AssertEQ(t, d.GetCursorPosition().X, width)
}

// Test_CUF_StopsAtRightEdgeWhenBegunRightOfScrollRegion tests that when the cursor starts right of
// the scroll region, CUF moves it right to the edge of the screen.
func Test_CUF_StopsAtRightEdgeWhenBegunRightOfScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region.
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Position the cursor right of the scroll region
	CUP(d, NewPoint(12, 3))
	AssertEQ(t, d.GetCursorPosition().X, 12)

	// Move it right by a lot
	width := d.GetScreenSize().Width
	CUF(d, width)

	// Ensure it stopped at the right edge of the screen
	AssertEQ(t, d.GetCursorPosition().X, width)
}

// Test_CUF_StopsAtRightMarginInScrollRegion tests that when the cursor starts within the scroll
// region, CUF moves it right to the right margin but no farther.
func Test_CUF_StopsAtRightMarginInScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region.
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Position the cursor inside the scroll region
	CUP(d, NewPoint(7, 3))

	// Move it right by a lot
	width := d.GetScreenSize().Width
	CUF(d, width)

	// Ensure it stopped at the right margin
	AssertEQ(t, d.GetCursorPosition().X, 10)
}
