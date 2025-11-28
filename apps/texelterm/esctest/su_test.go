// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the SU (Scroll Up) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/su.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// prepareSU sets up the screen with a 5x5 grid of letters for SU/SD tests.
func prepareSU(d *Driver) {
	lines := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	for i, line := range lines {
		CUP(d, NewPoint(1, i+1))
		d.Write(line)
	}
	CUP(d, NewPoint(3, 2)) // Cursor on 'h'
}

// Test_SU_DefaultParam tests that SU with no parameter scrolls up one line.
func Test_SU_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSU(d)
	SU(d)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"fghij", "klmno", "pqrst", "uvwxy", "     "})
}

// Test_SU_ExplicitParam tests that SU scrolls up by the given parameter.
func Test_SU_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSU(d)
	SU(d, 2)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"klmno", "pqrst", "uvwxy", "     ", "     "})
}

// Test_SU_CanClearScreen tests that scrolling by the screen height clears it.
func Test_SU_CanClearScreen(t *testing.T) {
	d := NewDriver(80, 24)

	// Fill screen with line numbers
	for i := 0; i < 24; i++ {
		CUP(d, NewPoint(1, i+1))
		d.Write(string(rune('0'+i/10)) + string(rune('0'+((i+1)%10))))
	}

	// Scroll by full height
	SU(d, 24)

	// Check first 4 columns are empty
	expected := make([]string, 24)
	for i := range expected {
		expected[i] = "  "
	}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 2, 24), expected)
}

// Test_SU_RespectsTopBottomScrollRegion tests SU respects scroll region.
func Test_SU_RespectsTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSU(d)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	SU(d, 2)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "pqrst", "     ", "     ", "uvwxy"})
}

// Test_SU_OutsideTopBottomScrollRegion tests SU works even when cursor is outside region.
func Test_SU_OutsideTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSU(d)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(1, 1)) // Outside region
	SU(d, 2)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "pqrst", "     ", "     ", "uvwxy"})
}

// Test_SU_RespectsLeftRightScrollRegion tests SU respects left/right margins.
func Test_SU_RespectsLeftRightScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSU(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	SU(d, 2)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"almne", "fqrsj", "kvwxo", "p   t", "u   y"})
}

// Test_SU_OutsideLeftRightScrollRegion tests SU works when cursor is outside left/right region.
func Test_SU_OutsideLeftRightScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSU(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(1, 2)) // Outside region (left of it)
	SU(d, 2)
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"almne", "fqrsj", "kvwxo", "p   t", "u   y"})
}

// Test_SU_LeftRightAndTopBottomScrollRegion tests SU with both margin types.
func Test_SU_LeftRightAndTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSU(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(1, 2))
	SU(d, 2)
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "fqrsj", "k   o", "p   t", "uvwxy"})
}

// Test_SU_BigScrollLeftRightAndTopBottomScrollRegion tests large scroll in region.
func Test_SU_BigScrollLeftRightAndTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSU(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(1, 2))
	SU(d, 99) // Scroll more than region height
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "f   j", "k   o", "p   t", "uvwxy"})
}
