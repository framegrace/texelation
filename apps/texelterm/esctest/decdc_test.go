// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests DECDC (Delete Column).
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/decdc.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// DECDC (CSI Pn ' ~) deletes columns at cursor position, shifting content left.
// This is a VT420 feature (vtLevel 4).
//
// From DEC STD 070:
// "The DECDC control causes Pn columns to be deleted at the active column position.
// The contents of the display are shifted to the left from the right margin to the
// active column. Columns containing blank characters with normal rendition are
// shifted into the display from the right margin."
package esctest

import (
	"testing"
)

// Test_DECDC_DefaultParam tests DECDC with default parameter (1 column).
func Test_DECDC_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg\r\nABCDEFG")

	CUP(d, NewPoint(2, 1))
	DECDC(d, 0) // Default = 1

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 2), []string{
		"acdefg ",
		"ACDEFG ",
	})
}

// Test_DECDC_ExplicitParam tests DECDC with explicit parameter (2 columns).
func Test_DECDC_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg\r\nABCDEFG\r\nzyxwvut")

	CUP(d, NewPoint(2, 2))
	DECDC(d, 2)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 3), []string{
		"adefg  ",
		"ADEFG  ",
		"zwvut  ",
	})
}

// Test_DECDC_CursorWithinTopBottom tests that DECDC only affects rows inside margins.
func Test_DECDC_CursorWithinTopBottom(t *testing.T) {
	d := NewDriver(80, 24)

	// Reset margins and set left/right margins
	DECSTBM(d, 0, 0)
	DECSET(d, DECLRMM)
	DECSLRM(d, 1, 20)

	// Write four lines
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg\r\nABCDEFG\r\nzyxwvut\r\nZYXWVUT")

	// Define scroll region (rows 2-3), delete 2 columns at position (2,2)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(2, 2))
	DECDC(d, 2)

	// Remove scroll region and verify
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 4), []string{
		"abcdefg", // Line 1: outside region, unchanged
		"ADEFG  ", // Line 2: inside region, columns deleted
		"zwvut  ", // Line 3: inside region, columns deleted
		"ZYXWVUT", // Line 4: outside region, unchanged
	})
}

// Test_DECDC_IsNoOpWhenCursorBeginsOutsideScrollRegion tests that DECDC is ignored outside margins.
func Test_DECDC_IsNoOpWhenCursorBeginsOutsideScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg\r\nABCDEFG")

	// Set left/right margins (2-5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)

	// Position cursor outside margins (column 1)
	CUP(d, NewPoint(1, 1))

	// Try to delete columns (should be ignored)
	DECDC(d, 10)

	// Verify nothing changed
	DECRESET(d, DECLRMM)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 2), []string{
		"abcdefg",
		"ABCDEFG",
	})
}

// Test_DECDC_DeleteAll tests DECDC with more columns than available.
func Test_DECDC_DeleteAll(t *testing.T) {
	d := NewDriver(80, 24)

	// Write at right edge
	startX := 80 - 7 + 1 // 74
	CUP(d, NewPoint(startX, 1))
	d.WriteRaw("abcdefg")
	CUP(d, NewPoint(startX, 2))
	d.WriteRaw("ABCDEFG")

	// Delete more columns than available
	CUP(d, NewPoint(startX+1, 1))
	DECDC(d, 80+10)

	// Only 'a' and 'A' should remain (at column startX)
	AssertScreenCharsInRectEqual(t, d, NewRect(startX, 1, 80, 2), []string{
		"a      ",
		"A      ",
	})

	// Ensure no wrap-around
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3), []string{" ", " "})
}

// Test_DECDC_DeleteWithLeftRightMargins tests DECDC within left/right margins.
func Test_DECDC_DeleteWithLeftRightMargins(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg")
	CUP(d, NewPoint(1, 2))
	d.WriteRaw("ABCDEFG")

	// Set left/right margins (2-5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)

	// Position cursor inside margins (column 3)
	CUP(d, NewPoint(3, 1))

	// Delete 1 column
	DECDC(d, 0) // Default = 1

	// Content from columns 4-5 shifts left, blank inserted at column 5
	DECRESET(d, DECLRMM)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 2), []string{
		"abde fg",
		"ABDE FG",
	})
}

// Test_DECDC_DeleteAllWithLeftRightMargins tests DECDC deleting all columns within margins.
func Test_DECDC_DeleteAllWithLeftRightMargins(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg")
	CUP(d, NewPoint(1, 2))
	d.WriteRaw("ABCDEFG")

	// Set left/right margins (2-5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)

	// Position cursor inside margins (column 3)
	CUP(d, NewPoint(3, 1))

	// Delete all columns within margins
	DECDC(d, 99)

	// Columns 3-5 should become blanks, content outside margins unchanged
	DECRESET(d, DECLRMM)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 2), []string{
		"ab   fg",
		"AB   FG",
	})
}
