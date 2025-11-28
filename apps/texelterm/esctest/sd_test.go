// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the SD (Scroll Down) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/sd.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: SD is referred to as "PAN UP" in DEC STD 070. The cursor position
// does not move along with the scrolled data.
package esctest

import "testing"

// prepareSD sets up the screen with a 5x5 grid of letters for SD tests.
func prepareSD(d *Driver) {
	lines := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	for i, line := range lines {
		CUP(d, NewPoint(1, i+1))
		d.Write(line)
	}
	CUP(d, NewPoint(3, 2)) // Cursor on 'h'
}

// Test_SD_DefaultParam tests that SD with no parameter scrolls down one line.
func Test_SD_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSD(d)
	SD(d)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"     ", "abcde", "fghij", "klmno", "pqrst"})
}

// Test_SD_ExplicitParam tests that SD scrolls down by the given parameter.
func Test_SD_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSD(d)
	SD(d, 2)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"     ", "     ", "abcde", "fghij", "klmno"})
}

// Test_SD_CanClearScreen tests that scrolling by the screen height clears it.
func Test_SD_CanClearScreen(t *testing.T) {
	d := NewDriver(80, 24)

	// Fill screen with line numbers
	for i := 0; i < 24; i++ {
		CUP(d, NewPoint(1, i+1))
		d.Write(string(rune('0'+i/10)) + string(rune('0'+((i+1)%10))))
	}

	// Scroll by full height
	SD(d, 24)

	// Check first 4 columns are empty
	expected := make([]string, 24)
	for i := range expected {
		expected[i] = "  "
	}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 2, 24), expected)
}

// Test_SD_RespectsTopBottomScrollRegion tests SD respects scroll region.
func Test_SD_RespectsTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSD(d)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	SD(d, 2)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "     ", "     ", "fghij", "uvwxy"})
}

// Test_SD_OutsideTopBottomScrollRegion tests SD works even when cursor is outside region.
func Test_SD_OutsideTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSD(d)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(1, 1)) // Outside region
	SD(d, 2)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "     ", "     ", "fghij", "uvwxy"})
}

// Test_SD_RespectsLeftRightScrollRegion tests SD respects left/right margins.
func Test_SD_RespectsLeftRightScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSD(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	SD(d, 2)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"a   e", "f   j", "kbcdo", "pghit", "ulmny"})
}

// Test_SD_OutsideLeftRightScrollRegion tests SD works when cursor is outside left/right region.
func Test_SD_OutsideLeftRightScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSD(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(1, 2)) // Outside region (left of it)
	SD(d, 2)
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 7),
		[]string{"a   e", "f   j", "kbcdo", "pghit", "ulmny", " qrs ", " vwx "})
}

// Test_SD_LeftRightAndTopBottomScrollRegion tests SD with both margin types.
func Test_SD_LeftRightAndTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSD(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(1, 2))
	SD(d, 2)
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "f   j", "k   o", "pghit", "uvwxy"})
}

// Test_SD_BigScrollLeftRightAndTopBottomScrollRegion tests large scroll in region.
func Test_SD_BigScrollLeftRightAndTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareSD(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(1, 2))
	SD(d, 99) // Scroll more than region height
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "f   j", "k   o", "p   t", "uvwxy"})
}
