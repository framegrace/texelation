// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CUD (Cursor Down) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cud.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CUD_DefaultParam tests that CUD moves the cursor down 1 with no parameter given.
func Test_CUD_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CUD(d)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 5)
	AssertEQ(t, position.Y, 4)
}

// Test_CUD_ExplicitParam tests that CUD moves the cursor down by the passed-in number of lines.
func Test_CUD_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 3))
	CUD(d, 2)
	AssertEQ(t, d.GetCursorPosition().Y, 5)
}

// Test_CUD_StopsAtBottomLine tests that CUD moves the cursor down, stopping at the last line.
func Test_CUD_StopsAtBottomLine(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 3))
	height := d.GetScreenSize().Height
	CUD(d, height)
	AssertEQ(t, d.GetCursorPosition().Y, height)
}

// Test_CUD_StopsAtBottomLineWhenBegunBelowScrollRegion tests that when the cursor starts below
// the scroll region, CUD moves it down to the bottom of the screen.
func Test_CUD_StopsAtBottomLineWhenBegunBelowScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 4, 5)

	// Position the cursor below the scroll region
	CUP(d, NewPoint(1, 6))

	// Move it down by a lot
	height := d.GetScreenSize().Height
	CUD(d, height)

	// Ensure it stopped at the bottom of the screen
	AssertEQ(t, d.GetCursorPosition().Y, height)
}

// Test_CUD_StopsAtBottomMarginInScrollRegion tests that when the cursor starts within the scroll
// region, CUD moves it down to the bottom margin but no farther.
func Test_CUD_StopsAtBottomMarginInScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 2, 4)

	// Position the cursor within the scroll region
	CUP(d, NewPoint(1, 3))

	// Move it down by more than the height of the scroll region
	CUD(d, 99)

	// Ensure it stopped at the bottom of the scroll region.
	AssertEQ(t, d.GetCursorPosition().Y, 4)
}
