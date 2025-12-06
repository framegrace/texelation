// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests DECFI (Forward Index) - horizontal forward scrolling.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/decfi.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// DECFI (ESC 9) moves cursor forward one column, or scrolls content left when at right margin.
// This is a VT420 feature (vtLevel 4).
//
// From DEC STD 070:
// "The DECFI control causes the active position to move forward one column.
// If the active position was already at the right margin, the contents of the
// Logical Display Page within the right, left, top and bottom margins shifts
// left one column. The column shifting beyond the left margin is deleted.
// A new column is inserted at the right margin with all attributes turned off."
package esctest

import (
	"testing"
)

// Test_DECFI_Basic tests basic forward movement.
func Test_DECFI_Basic(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(5, 6))
	DECFI(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(6, 6))
}

// Test_DECFI_NoWrapOnRightEdge tests that DECFI doesn't wrap at right edge.
func Test_DECFI_NoWrapOnRightEdge(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(80, 2))
	DECFI(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(80, 2))
}

// Test_DECFI_Scrolls tests horizontal scrolling when at right margin.
func Test_DECFI_Scrolls(t *testing.T) {
	d := NewDriver(80, 24)

	// Write test pattern
	strings := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	y := 3
	for _, s := range strings {
		CUP(d, NewPoint(2, y))
		d.WriteRaw(s)
		y++
	}

	// Set left/right and top/bottom margins
	DECSET(d, DECLRMM)
	DECSLRM(d, 3, 5)
	DECSTBM(d, 4, 6)

	// Position at right margin and forward-index (should scroll content left)
	CUP(d, NewPoint(5, 5))
	DECFI(d)

	// Content within margins should have scrolled left, inserting blank column at right margin
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 3, 6, 7), []string{
		"abcde",
		"fhi j", // Scrolled left within columns 3-5
		"kmn o",
		"prs t",
		"uvwxy",
	})
}

// Test_DECFI_RightOfMargin tests DECFI when cursor is right of the right margin.
func Test_DECFI_RightOfMargin(t *testing.T) {
	d := NewDriver(80, 24)

	// Set left/right margins (3-5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 3, 5)

	// Position at column 6 (outside margins, to the right)
	CUP(d, NewPoint(6, 1))
	DECFI(d)

	// Should move cursor right to column 7 (no scrolling)
	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(7, 1))
}

// Test_DECFI_WholeScreenScrolls tests DECFI at right edge with cursor at screen edge.
func Test_DECFI_WholeScreenScrolls(t *testing.T) {
	d := NewDriver(80, 24)

	// Move to right edge and write "x"
	CUP(d, NewPoint(80, 1))
	d.WriteRaw("x")

	// Forward-index (should be ignored - at right edge of screen)
	DECFI(d)

	// Content should not have scrolled (command ignored at screen edge)
	AssertScreenCharsInRectEqual(t, d, NewRect(79, 1, 80, 1), []string{"x "})
}
