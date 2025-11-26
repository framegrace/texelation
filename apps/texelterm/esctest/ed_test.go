// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the ED (Erase in Display) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/ed.py
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

// prepareED sets up display as: a / (blank) / bcd / (blank) / e
// With cursor on the 'c' (column 2, row 3)
func prepareED(d *Driver) {
	CUP(d, NewPoint(1, 1))
	d.Write("a")
	CUP(d, NewPoint(1, 3))
	d.Write("bcd")
	CUP(d, NewPoint(1, 5))
	d.Write("e")
	CUP(d, NewPoint(2, 3))
}

// prepareWideED sets up: abcde / fghij / klmno
// With cursor on the 'h' (column 3, row 2)
func prepareWideED(d *Driver) {
	CUP(d, NewPoint(1, 1))
	d.Write("abcde")
	CUP(d, NewPoint(1, 2))
	d.Write("fghij")
	CUP(d, NewPoint(1, 3))
	d.Write("klmno")
	CUP(d, NewPoint(3, 2))
}

// Test_ED_Default tests that ED with no parameter is same as ED(0).
func Test_ED_Default(t *testing.T) {
	d := NewDriver(80, 24)
	prepareED(d)
	ED(d)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 5),
		[]string{"a  ", "   ", "b  ", "   ", "   "})
}

// Test_ED_0 tests erase after cursor.
func Test_ED_0(t *testing.T) {
	d := NewDriver(80, 24)
	prepareED(d)
	ED(d, 0)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 5),
		[]string{"a  ", "   ", "b  ", "   ", "   "})
}

// Test_ED_1 tests erase before cursor.
func Test_ED_1(t *testing.T) {
	d := NewDriver(80, 24)
	prepareED(d)
	ED(d, 1)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 5),
		[]string{"   ", "   ", "  d", "   ", "e  "})
}

// Test_ED_2 tests erase whole screen.
func Test_ED_2(t *testing.T) {
	d := NewDriver(80, 24)
	prepareED(d)
	ED(d, 2)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 5),
		[]string{"   ", "   ", "   ", "   ", "   "})
}

// Test_ED_3 tests that ED(3) doesn't touch screen (only clears scrollback).
func Test_ED_3(t *testing.T) {
	d := NewDriver(80, 24)
	prepareED(d)
	ED(d, 3)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 3, 5),
		[]string{"a  ", "   ", "bcd", "   ", "e  "})
}

// Test_ED_0_WithScrollRegion tests that ED ignores scroll region.
func Test_ED_0_WithScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareWideED(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(3, 2))
	ED(d, 0)
	DECRESET(d, DECLRMM)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 3),
		[]string{"abcde", "fg   ", "     "})
}

// Test_ED_1_WithScrollRegion tests that ED ignores scroll region.
func Test_ED_1_WithScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareWideED(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(3, 2))
	ED(d, 1)
	DECRESET(d, DECLRMM)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 3),
		[]string{"     ", "   ij", "klmno"})
}

// Test_ED_2_WithScrollRegion tests that ED ignores scroll region.
func Test_ED_2_WithScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareWideED(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(3, 2))
	ED(d, 2)
	DECRESET(d, DECLRMM)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 3),
		[]string{"     ", "     ", "     "})
}
