// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests SM (Set Mode) and RM (Reset Mode) - ANSI mode management.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - Files: esctest/tests/sm.py, esctest/tests/rm.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests focus on IRM (Insert/Replace Mode - mode 4).
// Other modes like LNM (Line Feed/New Line Mode) are not implemented in texelterm.
package esctest

import (
	"strings"
	"testing"
)

// Test_SM_IRM tests turning on insert mode.
func Test_SM_IRM(t *testing.T) {
	d := NewDriver(80, 24)

	// Write "abc" at (1,1)
	d.WriteRaw("abc")

	// Move back to (1,1) and turn on insert mode
	CUP(d, NewPoint(1, 1))
	SM(d, IRM)

	// Write "X" - should insert before 'a', not replace
	d.WriteRaw("X")

	// Result should be "Xabc"
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 4, 1), []string{"Xabc"})
}

// Test_SM_IRM_DoesNotWrapUnlessCursorAtMargin tests that insert mode doesn't cause wrapping.
func Test_SM_IRM_DoesNotWrapUnlessCursorAtMargin(t *testing.T) {
	d := NewDriver(80, 24)

	// Fill line 1 with 79 'a's and one 'b' (at column 80)
	d.WriteRaw(strings.Repeat("a", 79))
	d.WriteRaw("b")

	// Line 2 should be empty
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 2), []string{" "})

	// Turn on insert mode and insert at column 1
	CUP(d, NewPoint(1, 1))
	SM(d, IRM)
	d.WriteRaw("X")

	// Line 2 should still be empty (no wrap occurred)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 2), []string{" "})

	// But inserting at column 80 (right margin) should cause wrap
	CUP(d, NewPoint(80, 1))
	d.WriteRaw("YZ")

	// "Z" should have wrapped to line 2
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 2), []string{"Z"})
}

// Test_SM_IRM_TruncatesAtRightMargin tests insert truncation at right margin.
func Test_SM_IRM_TruncatesAtRightMargin(t *testing.T) {
	d := NewDriver(80, 24)

	// Write "abcdef" starting at column 5
	CUP(d, NewPoint(5, 1))
	d.WriteRaw("abcdef")

	// Set left/right margins (5-10)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)

	// Insert "X" at column 7 with insert mode on
	CUP(d, NewPoint(7, 1))
	SM(d, IRM)
	d.WriteRaw("X")
	DECRESET(d, DECLRMM)

	// Result: "abXcde" in columns 5-10, 'f' truncated at margin, column 11 empty
	AssertScreenCharsInRectEqual(t, d, NewRect(5, 1, 11, 1), []string{"abXcde "})
}

// Test_RM_IRM tests turning off insert mode (back to replace mode).
func Test_RM_IRM(t *testing.T) {
	d := NewDriver(80, 24)

	// Turn on insert mode and write some text
	SM(d, IRM)
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("X")
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("W")

	// Now turn on replace mode
	CUP(d, NewPoint(1, 1))
	RM(d, IRM)

	// Write "YZ" - should replace, not insert
	d.WriteRaw("YZ")

	// Result should be "YZ" (replaced "WX")
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 2, 1), []string{"YZ"})
}
