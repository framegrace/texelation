// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CNL (Cursor Next Line) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cnl.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CNL_DefaultParam tests that CNL moves the cursor down 1 with no parameter given.
func Test_CNL_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CNL(d)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 4)
}

// Test_CNL_ExplicitParam tests that CNL moves the cursor down by the passed-in number of lines.
func Test_CNL_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(6, 3))
	CNL(d, 2)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 5)
}

// Test_CNL_StopsAtBottomLine tests that CNL moves the cursor down, stopping at the last line.
func Test_CNL_StopsAtBottomLine(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(6, 3))
	height := d.GetScreenSize().Height
	CNL(d, height)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, height)
}

// Test_CNL_StopsAtBottomLineWhenBegunBelowScrollRegion tests that when the cursor starts below
// the scroll region, CNL moves it down to the bottom of the screen.
func Test_CNL_StopsAtBottomLineWhenBegunBelowScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 4, 5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Position the cursor below the scroll region
	CUP(d, NewPoint(7, 6))

	// Move it down by a lot
	height := d.GetScreenSize().Height
	CNL(d, height)

	// Ensure it stopped at the bottom of the screen
	position := d.GetCursorPosition()
	AssertEQ(t, position.Y, height)
	AssertEQ(t, position.X, 1) // CNL always moves to column 1
}

// Test_CNL_StopsAtBottomMarginInScrollRegion tests that when the cursor starts within the scroll
// region, CNL moves it down to the bottom margin but no farther.
func Test_CNL_StopsAtBottomMarginInScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 2, 4)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Position the cursor within the scroll region
	CUP(d, NewPoint(7, 3))

	// Move it down by more than the height of the scroll region
	CNL(d, 99)

	// Ensure it stopped at the bottom of the scroll region.
	position := d.GetCursorPosition()
	AssertEQ(t, position.Y, 4)
	AssertEQ(t, position.X, 1) // CNL always moves to column 1
}
