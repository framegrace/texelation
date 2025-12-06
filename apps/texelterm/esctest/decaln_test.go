// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the DECALN (Screen Alignment Test) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/decaln.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_DECALN_FillsScreen tests that DECALN fills the screen with the letter E.
// Testing the whole screen would be slow so we just check the corners and center.
func Test_DECALN_FillsScreen(t *testing.T) {
	d := NewDriver(80, 24)

	DECALN(d)

	// Check corners and center
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 1), []string{"E"})
	AssertScreenCharsInRectEqual(t, d, NewRect(80, 1, 80, 1), []string{"E"})
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 24, 1, 24), []string{"E"})
	AssertScreenCharsInRectEqual(t, d, NewRect(80, 24, 80, 24), []string{"E"})
	AssertScreenCharsInRectEqual(t, d, NewRect(40, 12, 40, 12), []string{"E"})
}

// Test_DECALN_MovesCursorHome tests that DECALN moves the cursor to home position.
func Test_DECALN_MovesCursorHome(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(5, 5))
	DECALN(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 1))
}

// Test_DECALN_ClearsMargins tests that DECALN clears (resets) margins.
func Test_DECALN_ClearsMargins(t *testing.T) {
	d := NewDriver(80, 24)

	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 3)
	DECSTBM(d, 4, 5)
	DECALN(d)

	// Verify we can pass the top margin
	CUP(d, NewPoint(2, 4))
	CUU(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 3))

	// Verify we can pass the bottom margin
	CUP(d, NewPoint(2, 5))
	CUD(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 6))

	// Verify we can pass the left margin
	CUP(d, NewPoint(2, 4))
	CUB(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 4))

	// Verify we can pass the right margin
	CUP(d, NewPoint(3, 4))
	CUF(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(4, 4))
}
