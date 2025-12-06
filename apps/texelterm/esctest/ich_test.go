// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the ICH (Insert Character) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/ich.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_ICH_DefaultParam tests ICH with default parameter (should insert 1 blank).
func Test_ICH_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 1))
	AssertEQ(t, d.GetCursorPosition().X, 1)
	d.Write("abcdefg")
	CUP(d, NewPoint(2, 1))
	AssertEQ(t, d.GetCursorPosition().X, 2)
	ICH(d)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 8, 1),
		[]string{"a" + Blank() + "bcdefg"})
}

// Test_ICH_ExplicitParam tests ICH with explicit parameter.
func Test_ICH_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 1))
	AssertEQ(t, d.GetCursorPosition().X, 1)
	d.Write("abcdefg")
	CUP(d, NewPoint(2, 1))
	AssertEQ(t, d.GetCursorPosition().X, 2)
	ICH(d, 2)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 9, 1),
		[]string{"a" + Blank() + Blank() + "bcdefg"})
}

// Test_ICH_IsNoOpWhenCursorBeginsOutsideScrollRegion ensures ICH does nothing when
// the cursor starts outside the scroll region.
func Test_ICH_IsNoOpWhenCursorBeginsOutsideScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 1))
	s := "abcdefg"
	d.Write(s)

	// Set margin: from columns 2 to 5
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)

	// Position cursor outside margins
	CUP(d, NewPoint(1, 1))

	// Insert blanks
	ICH(d, 10)

	// Ensure nothing happened.
	DECRESET(d, DECLRMM)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, len(s), 1),
		[]string{s})
}

// Test_ICH_ScrollOffRightEdge tests ICH behavior when pushing text off the right edge.
func Test_ICH_ScrollOffRightEdge(t *testing.T) {
	d := NewDriver(80, 24)
	width := d.GetScreenSize().Width
	s := "abcdefg"
	startX := width - len(s) + 1
	CUP(d, NewPoint(startX, 1))
	d.Write(s)
	CUP(d, NewPoint(startX+1, 1))
	ICH(d)

	AssertScreenCharsInRectEqual(t, d, NewRect(startX, 1, width, 1),
		[]string{"a" + Blank() + "bcdef"})
	// Ensure there is no wrap-around.
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 2), []string{Empty()})
}

// Test_ICH_ScrollEntirelyOffRightEdge tests ICH behavior when pushing all text off the right edge.
func Test_ICH_ScrollEntirelyOffRightEdge(t *testing.T) {
	d := NewDriver(80, 24)
	width := d.GetScreenSize().Width
	CUP(d, NewPoint(1, 1))
	d.Write(Repeat("x", width))
	CUP(d, NewPoint(1, 1))
	ICH(d, width)

	expectedLine := Repeat(Blank(), width)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, width, 1),
		[]string{expectedLine})
	// Ensure there is no wrap-around.
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 2), []string{Empty()})
}

// Test_ICH_ScrollOffRightMarginInScrollRegion tests ICH when cursor is within the scroll region.
func Test_ICH_ScrollOffRightMarginInScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(1, 1))
	s := "abcdefg"
	d.Write(s)

	// Set margin: from columns 2 to 5
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 5)

	// Position cursor inside margins
	CUP(d, NewPoint(3, 1))

	// Insert blank
	ICH(d)

	// Ensure the 'e' gets dropped.
	DECRESET(d, DECLRMM)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, len(s), 1),
		[]string{"ab" + Blank() + "cdfg"})
}
