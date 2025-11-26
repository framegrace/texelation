// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the EL (Erase in Line) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/el.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: Protection-related tests (DECSCA, SPA/EPA) have been omitted as these
// are advanced features not yet implemented in texelterm.
package esctest

import "testing"

// prepareEL sets up screen to "abcdefghij" on first line with cursor on 'e' (column 5).
func prepareEL(d *Driver) {
	CUP(d, NewPoint(1, 1))
	d.Write("abcdefghij")
	CUP(d, NewPoint(5, 1))
}

// Test_EL_Default tests that EL with no parameter erases to right of cursor.
func Test_EL_Default(t *testing.T) {
	d := NewDriver(80, 24)
	prepareEL(d)
	EL(d)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 10, 1),
		[]string{"abcd      "})
}

// Test_EL_0 tests erase to right of cursor.
func Test_EL_0(t *testing.T) {
	d := NewDriver(80, 24)
	prepareEL(d)
	EL(d, 0)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 10, 1),
		[]string{"abcd      "})
}

// Test_EL_1 tests erase to left of cursor.
func Test_EL_1(t *testing.T) {
	d := NewDriver(80, 24)
	prepareEL(d)
	EL(d, 1)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 10, 1),
		[]string{"     fghij"})
}

// Test_EL_2 tests erase whole line.
func Test_EL_2(t *testing.T) {
	d := NewDriver(80, 24)
	prepareEL(d)
	EL(d, 2)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 10, 1),
		[]string{"          "})
}

// Test_EL_IgnoresScrollRegion tests that EL ignores scroll region.
func Test_EL_IgnoresScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareEL(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(5, 1))
	EL(d, 2)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 10, 1),
		[]string{"          "})
}
