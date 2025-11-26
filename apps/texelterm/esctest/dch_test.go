// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the DCH (Delete Character) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/dch.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_DCH_DefaultParam tests that DCH with no parameter deletes one character at cursor.
func Test_DCH_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abcd")
	CUP(d, NewPoint(2, 1))
	DCH(d)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 4, 1),
		[]string{"acd "})
}

// Test_DCH_ExplicitParam tests that DCH deletes the specified number of characters.
func Test_DCH_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abcd")
	CUP(d, NewPoint(2, 1))
	DCH(d, 2)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 4, 1),
		[]string{"ad  "})
}

// Test_DCH_RespectsMargins tests that DCH respects left-right margins.
func Test_DCH_RespectsMargins(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abcde")
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(3, 1))
	DCH(d)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 1),
		[]string{"abd e"})
}

// Test_DCH_DeleteAllWithMargins tests deleting all characters up to right margin.
func Test_DCH_DeleteAllWithMargins(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abcde")
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(3, 1))
	DCH(d, 99)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 1),
		[]string{"ab  e"})
}

// Test_DCH_DoesNothingOutsideLeftRightMargin tests that DCH does nothing outside margins.
func Test_DCH_DoesNothingOutsideLeftRightMargin(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abcde")
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(1, 1))
	DCH(d, 99)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 1),
		[]string{"abcde"})
}

// Test_DCH_WorksOutsideTopBottomMargin tests that DCH works outside scrolling margin.
// Per Thomas Dickey, DCH should work outside scrolling margin (see xterm changelog for patch 316).
func Test_DCH_WorksOutsideTopBottomMargin(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("abcde")
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(1, 1))
	DCH(d, 99)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 1),
		[]string{"     "})
}
