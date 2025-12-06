// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests DECIC (Insert Column).
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/decic.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// DECIC (CSI Pn ' }) inserts blank columns at cursor position, shifting content right.
// This is a VT420 feature (vtLevel 4).
package esctest

import (
	"testing"
)

// Test_DECIC_DefaultParam tests DECIC with default parameter (1 column).
func Test_DECIC_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg\r\nABCDEFG")

	CUP(d, NewPoint(2, 1))
	DECIC(d, 0) // Default = 1

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 8, 2), []string{
		"a bcdefg",
		"A BCDEFG",
	})
}

// Test_DECIC_ExplicitParam tests DECIC with explicit parameter (2 columns).
func Test_DECIC_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg\r\nABCDEFG\r\nzyxwvut")

	CUP(d, NewPoint(2, 2))
	DECIC(d, 2)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 9, 3), []string{
		"a  bcdefg",
		"A  BCDEFG",
		"z  yxwvut",
	})
}

// Test_DECIC_CursorWithinTopBottom tests that DECIC only affects rows inside margins.
func Test_DECIC_CursorWithinTopBottom(t *testing.T) {
	d := NewDriver(80, 24)

	// Reset margins and set left/right margins
	DECSTBM(d, 0, 0)
	DECSET(d, DECLRMM)
	DECSLRM(d, 1, 20)

	// Write four lines
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg\r\nABCDEFG\r\nzyxwvut\r\nZYXWVUT")

	// Define scroll region (rows 2-3), insert 2 columns at position (2,2)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(2, 2))
	DECIC(d, 2)

	// Remove scroll region and verify
	DECSTBM(d, 0, 0)
	DECRESET(d, DECLRMM)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 9, 4), []string{
		"abcdefg  ", // Line 1: outside region, unchanged
		"A  BCDEFG", // Line 2: inside region, columns inserted
		"z  yxwvut", // Line 3: inside region, columns inserted
		"ZYXWVUT  ", // Line 4: outside region, unchanged
	})
}

// Test_DECIC_IsNoOpWhenCursorBeginsOutsideScrollRegion tests that DECIC is ignored outside margins.
func Test_DECIC_IsNoOpWhenCursorBeginsOutsideScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	d.WriteRaw("abcdefg\r\nABCDEFG")

	// Set left/right margins (2-5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)

	// Position cursor outside margins (column 1)
	CUP(d, NewPoint(1, 1))

	// Try to insert blanks (should be ignored)
	DECIC(d, 10)

	// Verify nothing changed
	DECRESET(d, DECLRMM)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 2), []string{
		"abcdefg",
		"ABCDEFG",
	})
}

// Test_DECIC_ScrollOffRightEdge tests DECIC truncation at right edge.
func Test_DECIC_ScrollOffRightEdge(t *testing.T) {
	d := NewDriver(80, 24)

	// Write at right edge
	startX := 80 - 7 + 1 // 74
	CUP(d, NewPoint(startX, 1))
	d.WriteRaw("abcdefg")
	CUP(d, NewPoint(startX, 2))
	d.WriteRaw("ABCDEFG")

	// Insert 1 column at position startX+1
	CUP(d, NewPoint(startX+1, 1))
	DECIC(d, 0) // Default = 1

	AssertScreenCharsInRectEqual(t, d, NewRect(startX, 1, 80, 2), []string{
		"a bcdef",
		"A BCDEF",
	})

	// Ensure no wrap-around
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3), []string{" ", " "})
}

// Test_DECIC_ScrollEntirelyOffRightEdge tests DECIC with width parameter.
func Test_DECIC_ScrollEntirelyOffRightEdge(t *testing.T) {
	d := NewDriver(80, 24)

	// Fill two lines with 'x'
	CUP(d, NewPoint(1, 1))
	for i := 0; i < 80; i++ {
		d.WriteRaw("x")
	}
	CUP(d, NewPoint(1, 2))
	for i := 0; i < 80; i++ {
		d.WriteRaw("x")
	}

	// Insert 80 columns at position 1
	CUP(d, NewPoint(1, 1))
	DECIC(d, 80)

	// All content should be shifted off, leaving blanks
	expectedLine := ""
	for i := 0; i < 80; i++ {
		expectedLine += " "
	}

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 80, 2), []string{
		expectedLine,
		expectedLine,
	})

	// Ensure no wrap-around
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3), []string{" ", " "})
}

// Test_DECIC_ScrollOffRightMarginInScrollRegion tests DECIC within left/right margins.
func Test_DECIC_ScrollOffRightMarginInScrollRegion(t *testing.T) {
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

	// Insert 1 column
	DECIC(d, 0) // Default = 1

	// 'e' at column 5 gets truncated at right margin
	DECRESET(d, DECLRMM)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 7, 2), []string{
		"ab cdfg",
		"AB CDFG",
	})
}
