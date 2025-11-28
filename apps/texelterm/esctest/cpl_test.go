// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CPL (Cursor Previous Line) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cpl.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CPL_DefaultParam tests that CPL moves the cursor up 1 with no parameter given.
func Test_CPL_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 3))
	CPL(d)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 2)
}

// Test_CPL_ExplicitParam tests that CPL moves the cursor up by the passed-in number of lines.
func Test_CPL_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(6, 5))
	CPL(d, 2)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 3)
}

// Test_CPL_StopsAtTopLine tests that CPL moves the cursor up, stopping at the first line.
func Test_CPL_StopsAtTopLine(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(6, 3))
	height := d.GetScreenSize().Height
	CPL(d, height)
	position := d.GetCursorPosition()
	AssertEQ(t, position.X, 1)
	AssertEQ(t, position.Y, 1)
}

// Test_CPL_StopsAtTopLineWhenBegunAboveScrollRegion tests that when the cursor starts above
// the scroll region, CPL moves it up to the top of the screen.
func Test_CPL_StopsAtTopLineWhenBegunAboveScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 4, 5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Position the cursor above the scroll region
	CUP(d, NewPoint(7, 3))

	// Move it up by a lot
	height := d.GetScreenSize().Height
	CPL(d, height)

	// Ensure it stopped at the top of the screen
	position := d.GetCursorPosition()
	AssertEQ(t, position.Y, 1)
	AssertEQ(t, position.X, 1) // CPL always moves to column 1
}

// Test_CPL_StopsAtTopMarginInScrollRegion tests that when the cursor starts within the scroll
// region, CPL moves it up to the top margin but no farther.
func Test_CPL_StopsAtTopMarginInScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	// Set a scroll region. This must be done first because DECSTBM moves the cursor to the origin.
	DECSTBM(d, 2, 4)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Position the cursor within the scroll region
	CUP(d, NewPoint(7, 3))

	// Move it up by more than the height of the scroll region
	CPL(d, 99)

	// Ensure it stopped at the top of the scroll region.
	position := d.GetCursorPosition()
	AssertEQ(t, position.Y, 2)
	AssertEQ(t, position.X, 1) // CPL always moves to column 1
}
