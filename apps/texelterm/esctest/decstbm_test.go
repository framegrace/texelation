// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the DECSTBM (Set Top and Bottom Margins) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/decstbm.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import "testing"

// Test_DECSTBM_ScrollsOnNewline tests that newlines scroll within the margin.
func Test_DECSTBM_ScrollsOnNewline(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(1, 2))
	d.Write("1\r\n")
	d.Write("2")
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3),
		[]string{"1", "2"})
	d.Write("\r\n")
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3),
		[]string{"2", " "})
	AssertEQ(t, d.GetCursorPosition().Y, 3)
}

// Test_DECSTBM_NewlineBelowRegion tests that newlines outside the region don't affect it.
func Test_DECSTBM_NewlineBelowRegion(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(1, 2))
	d.Write("1\r\n")
	d.Write("2")
	CUP(d, NewPoint(1, 4))
	d.Write("\r\n")
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3),
		[]string{"1", "2"})
}

// Test_DECSTBM_MovsCursorToOrigin tests that DECSTBM moves cursor to 1,1.
func Test_DECSTBM_MovsCursorToOrigin(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(3, 2))
	DECSTBM(d, 2, 3)
	AssertEQ(t, d.GetCursorPosition().X, 1)
	AssertEQ(t, d.GetCursorPosition().Y, 1)
}

// Test_DECSTBM_TopBelowBottom tests that invalid margins (top == bottom) are rejected.
func Test_DECSTBM_TopBelowBottom(t *testing.T) {
	d := NewDriver(80, 24)
	// Set invalid margin (top == bottom)
	DECSTBM(d, 3, 3)

	// Fill screen with line numbers
	for i := 0; i < 24; i++ {
		d.Write("0000")
		if i != 23 {
			d.Write("\r\n")
		}
	}

	// Verify all lines remain intact
	for i := 0; i < 24; i++ {
		y := i + 1
		AssertScreenCharsInRectEqual(t, d, NewRect(1, y, 4, y),
			[]string{"0000"})
	}

	// Try to scroll at bottom - should scroll entire screen since margins invalid
	CUP(d, NewPoint(1, 24))
	d.Write("\n")

	// Line 1 should have scrolled off
	for i := 0; i < 23; i++ {
		y := i + 1
		AssertScreenCharsInRectEqual(t, d, NewRect(1, y, 4, y),
			[]string{"0000"})
	}

	// Last line should be blank
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 24, 4, 24),
		[]string{"    "})
}

// Test_DECSTBM_DefaultRestores tests that DECSTBM() with no args restores full screen.
func Test_DECSTBM_DefaultRestores(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(1, 2))
	d.Write("1\r\n")
	d.Write("2")
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3),
		[]string{"1", "2"})

	position := NewPoint(d.GetCursorPosition().X, d.GetCursorPosition().Y)
	DECSTBM(d, 0, 0) // Reset margins
	CUP(d, position)
	d.Write("\r\n")

	// Content should remain since margins are now full screen
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3),
		[]string{"1", "2"})
	AssertEQ(t, d.GetCursorPosition().Y, 4)
}

// Test_DECSTBM_CursorBelowRegionAtBottomTriesToScroll tests scrolling outside margins.
func Test_DECSTBM_CursorBelowRegionAtBottomTriesToScroll(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 2, 3)
	CUP(d, NewPoint(1, 2))
	d.Write("1\r\n")
	d.Write("2")

	// Move cursor to last line of screen (outside region)
	CUP(d, NewPoint(1, 24))
	d.Write("3\r\n")

	// Region content should be unchanged
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3),
		[]string{"1", "2"})

	// "3" should be on line 24
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 24, 1, 24),
		[]string{"3"})

	// Cursor should still be at bottom
	AssertEQ(t, d.GetCursorPosition().Y, 24)
}

// Test_DECSTBM_MaxSizeOfRegionIsPageSize tests that margins beyond screen are clamped.
func Test_DECSTBM_MaxSizeOfRegionIsPageSize(t *testing.T) {
	d := NewDriver(80, 24)

	// Write "x" at line 2
	CUP(d, NewPoint(1, 2))
	d.Write("x")

	// Set scroll bottom to beyond screen (should clamp to 24)
	DECSTBM(d, 1, 34)

	// Move to last line and scroll
	CUP(d, NewPoint(1, 24))
	d.Write("\r\n")

	// Line 2 should have scrolled to line 1
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 2),
		[]string{"x", " "})

	// Cursor should be at last line
	AssertEQ(t, d.GetCursorPosition().Y, 24)
}

// Test_DECSTBM_TopOfZeroIsTopOfScreen tests that top=0 means screen top.
func Test_DECSTBM_TopOfZeroIsTopOfScreen(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 0, 3)
	CUP(d, NewPoint(1, 2))
	d.Write("1\r\n")
	d.Write("2\r\n")
	d.Write("3\r\n")
	d.Write("4")

	// Lines 1-3 should have "2", "3", "4" (line 1 scrolled off)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 3),
		[]string{"2", "3", "4"})
}

// Test_DECSTBM_BottomOfZeroIsBottomOfScreen tests that bottom=0 means screen bottom.
func Test_DECSTBM_BottomOfZeroIsBottomOfScreen(t *testing.T) {
	d := NewDriver(80, 24)

	// Write "x" at line 3
	CUP(d, NewPoint(1, 3))
	d.Write("x")

	// Set scroll region from line 2 to bottom of screen
	DECSTBM(d, 2, 0)

	// Move to last line and scroll
	CUP(d, NewPoint(1, 24))
	d.Write("\r\n")

	// Line 3 should have scrolled to line 2
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 2, 1, 3),
		[]string{"x", " "})

	// Cursor should be at last line
	AssertEQ(t, d.GetCursorPosition().Y, 24)
}
