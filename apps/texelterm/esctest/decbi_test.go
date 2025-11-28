// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests DECBI (Back Index) - horizontal backward scrolling.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/decbi.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// DECBI (ESC 6) moves cursor back one column, or scrolls content right when at left margin.
// This is a VT420 feature (vtLevel 4).
package esctest

import (
	"testing"
)

// Test_DECBI_Basic tests basic backward movement.
func Test_DECBI_Basic(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(5, 6))
	DECBI(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(4, 6))
}

// Test_DECBI_NoWrapOnLeftEdge tests that DECBI doesn't wrap at left edge.
func Test_DECBI_NoWrapOnLeftEdge(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 2))
	DECBI(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(1, 2))
}

// Test_DECBI_Scrolls tests horizontal scrolling when at left margin.
func Test_DECBI_Scrolls(t *testing.T) {
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

	// Position at left margin and back-index (should scroll content right)
	CUP(d, NewPoint(3, 5))
	DECBI(d)

	// Content within margins should have scrolled right, inserting blank column at left margin
	AssertScreenCharsInRectEqual(t, d, NewRect(2, 3, 6, 7), []string{
		"abcde",
		"f ghj", // Scrolled right within columns 3-5
		"k lmo",
		"p qrt",
		"uvwxy",
	})
}

// Test_DECBI_LeftOfMargin tests DECBI when cursor is left of the left margin.
func Test_DECBI_LeftOfMargin(t *testing.T) {
	d := NewDriver(80, 24)

	// Set left/right margins (3-5)
	DECSET(d, DECLRMM)
	DECSLRM(d, 3, 5)

	// Position at column 2 (outside margins, to the left)
	CUP(d, NewPoint(2, 1))
	DECBI(d)

	// Should move cursor left to column 1 (no scrolling)
	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(1, 1))
}

// Test_DECBI_WholeScreenScrolls tests DECBI at left edge with no margins.
func Test_DECBI_WholeScreenScrolls(t *testing.T) {
	d := NewDriver(80, 24)

	// Write "x" at (1,1)
	d.WriteRaw("x")

	// Move back to (1,1) and back-index
	CUP(d, NewPoint(1, 1))
	DECBI(d)

	// Content should have scrolled right (whole screen is the scrolling region)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 2, 1), []string{" x"})
}
