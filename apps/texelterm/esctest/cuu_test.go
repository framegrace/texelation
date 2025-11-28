// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CUU (Cursor Up) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cuu.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CUU_DefaultParam tests that CUU moves the cursor up 1 with no parameter given.
func Test_CUU_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CUU(d)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 5)
	AssertEQ(t, position.Y, 2)
}

// Test_CUU_ExplicitParam tests that CUU moves the cursor up by the passed-in number of lines.
func Test_CUU_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 3))
	CUU(d, 2)
	AssertEQ(t, d.GetCursorPosition().Y, 1)
}

// Test_CUU_StopsAtTopLine tests that CUU moves the cursor up, stopping at the first line.
func Test_CUU_StopsAtTopLine(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 3))
	CUU(d, 99)
	AssertEQ(t, d.GetCursorPosition().Y, 1)
}

// Test_CUU_StopsAtTopLineWhenBegunAboveScrollRegion tests that when the cursor starts above
// the scroll region, CUU moves it up to the top of the screen.
func Test_CUU_StopsAtTopLineWhenBegunAboveScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 4, 5)

	// Position the cursor above the scroll region
	CUP(d, NewPoint(1, 3))

	// Move it up by a lot
	CUU(d, 99)

	// Ensure it stopped at the top of the screen
	AssertEQ(t, d.GetCursorPosition().Y, 1)
}

// Test_CUU_StopsAtTopMarginInScrollRegion tests that when the cursor starts within the scroll
// region, CUU moves it up to the top margin but no farther.
func Test_CUU_StopsAtTopMarginInScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 2, 4)

	// Position the cursor within the scroll region
	CUP(d, NewPoint(1, 3))

	// Move it up by more than the height of the scroll region
	CUU(d, 99)

	// Ensure it stopped at the top of the scroll region.
	AssertEQ(t, d.GetCursorPosition().Y, 2)
}
