// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the TBC (Tab Clear) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/tbc.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_TBC_Default tests that TBC with no param clears tab stop at cursor.
func Test_TBC_Default(t *testing.T) {
	d := NewDriver(80, 24)

	// Tab from column 1 should go to 9
	d.Write("\t")
	AssertEQ(t, d.GetCursorPosition().X, 9)

	// Clear tab stop at column 9
	TBC(d)

	// Move back to column 1 and tab - should skip 9 and go to 17
	CUP(d, NewPoint(1, 1))
	d.Write("\t")
	AssertEQ(t, d.GetCursorPosition().X, 17)
}

// Test_TBC_0 tests that TBC(0) clears tab stop at cursor.
func Test_TBC_0(t *testing.T) {
	d := NewDriver(80, 24)

	// Tab from column 1 should go to 9
	d.Write("\t")
	AssertEQ(t, d.GetCursorPosition().X, 9)

	// Clear tab stop at column 9
	TBC(d, 0)

	// Move back to column 1 and tab - should skip 9 and go to 17
	CUP(d, NewPoint(1, 1))
	d.Write("\t")
	AssertEQ(t, d.GetCursorPosition().X, 17)
}

// Test_TBC_3 tests that TBC(3) clears all tab stops.
func Test_TBC_3(t *testing.T) {
	d := NewDriver(80, 24)

	// Remove all tab stops
	TBC(d, 3)

	// Set a tab stop at column 30
	CUP(d, NewPoint(30, 1))
	HTS(d)

	// Move back to column 1 and tab - should go to 30
	CUP(d, NewPoint(1, 1))
	d.Write("\t")
	AssertEQ(t, d.GetCursorPosition().X, 30)
}

// Test_TBC_NoOp tests that clearing nonexistent tab stop does nothing.
func Test_TBC_NoOp(t *testing.T) {
	d := NewDriver(80, 24)

	// Move to column 10 (no tab stop there) and clear
	CUP(d, NewPoint(10, 1))
	TBC(d, 0)

	// Tab stops at 9 and 17 should still work
	CUP(d, NewPoint(1, 1))
	d.Write("\t")
	AssertEQ(t, d.GetCursorPosition().X, 9)
	d.Write("\t")
	AssertEQ(t, d.GetCursorPosition().X, 17)
}
