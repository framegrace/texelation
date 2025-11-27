// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the CR (Carriage Return) control character.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/cr.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_CR_Basic tests basic CR functionality - moves to column 1.
func Test_CR_Basic(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(3, 3))
	CR(d)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 3))
}

// Test_CR_MovesToLeftMarginWhenRightOfLeftMargin tests that CR moves to left margin when cursor is right of it.
func Test_CR_MovesToLeftMarginWhenRightOfLeftMargin(t *testing.T) {
	d := NewDriver(80, 24)

	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)
	CUP(d, NewPoint(6, 1))
	CR(d)
	DECRESET(d, DECLRMM)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(5, 1))
}

// Test_CR_MovesToLeftOfScreenWhenLeftOfLeftMargin tests that CR moves to column 1 when cursor starts left of margin.
func Test_CR_MovesToLeftOfScreenWhenLeftOfLeftMargin(t *testing.T) {
	d := NewDriver(80, 24)

	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)
	CUP(d, NewPoint(4, 1))
	CR(d)
	DECRESET(d, DECLRMM)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 1))
}

// Test_CR_StaysPutWhenAtLeftMargin tests that CR stays at left margin when already there.
func Test_CR_StaysPutWhenAtLeftMargin(t *testing.T) {
	d := NewDriver(80, 24)

	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)
	CUP(d, NewPoint(5, 1))
	CR(d)
	DECRESET(d, DECLRMM)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(5, 1))
}

// Test_CR_MovesToLeftMarginWhenLeftOfLeftMarginInOriginMode tests that in origin mode,
// CR always goes to the left margin, even if cursor starts left of it.
func Test_CR_MovesToLeftMarginWhenLeftOfLeftMarginInOriginMode(t *testing.T) {
	d := NewDriver(80, 24)

	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)
	DECSET(d, DECOM)
	CUP(d, NewPoint(4, 1))
	CR(d)
	DECRESET(d, DECLRMM)
	d.Write("x")
	DECRESET(d, DECOM)
	AssertScreenCharsInRectEqual(t, d, NewRect(5, 1, 5, 1), []string{"x"})
}
