// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the REP (Repeat) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/rep.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: REP is adapted from ECMA-48 by xterm to follow DEC standard margins.
package esctest

import "testing"

// Test_REP_DefaultParam tests that REP repeats the last character once with no parameter.
func Test_REP_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("a")
	REP(d)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 1),
		[]string{"aa "})
}

// Test_REP_ExplicitParam tests that REP repeats the last character N times.
func Test_REP_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	d.Write("a")
	REP(d, 2)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 4, 1),
		[]string{"aaa "})
}

// Test_REP_RespectsLeftRightMargins tests that REP respects left/right margins.
func Test_REP_RespectsLeftRightMargins(t *testing.T) {
	d := NewDriver(80, 24)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(2, 1))
	d.Write("a")
	REP(d, 3)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 2),
		[]string{" aaa ",
			" a   "})
}

// Test_REP_RespectsTopBottomMargins tests that REP respects top/bottom margins.
func Test_REP_RespectsTopBottomMargins(t *testing.T) {
	d := NewDriver(80, 24)
	width := d.GetScreenSize().Width
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(width-2, 4))
	d.Write("a")
	REP(d, 3)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 3, width, 4),
		[]string{Repeat(" ", width-3) + "aaa",
			"a" + Repeat(" ", width-1)})
}
