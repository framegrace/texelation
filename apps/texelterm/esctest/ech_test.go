// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the ECH (Erase Character) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/ech.py
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

// Test_ECH_DefaultParam tests that ECH erases the character under the cursor.
func Test_ECH_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abc")
	CUP(d, NewPoint(1, 1))
	ECH(d)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 1),
		[]string{" bc"})
}

// Test_ECH_ExplicitParam tests that ECH erases N characters starting at cursor.
func Test_ECH_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abc")
	CUP(d, NewPoint(1, 1))
	ECH(d, 2)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 1),
		[]string{"  c"})
}

// Test_ECH_IgnoresScrollRegion tests that ECH ignores scroll region when cursor is inside it.
func Test_ECH_IgnoresScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abcdefg")
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(3, 1))
	ECH(d, 4)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 1),
		[]string{"ab    g"})
}

// Test_ECH_OutsideScrollRegion tests that ECH ignores scroll region when cursor is outside it.
func Test_ECH_OutsideScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abcdefg")
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(1, 1))
	ECH(d, 4)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 1),
		[]string{"    efg"})
}
