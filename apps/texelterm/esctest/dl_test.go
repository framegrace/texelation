// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the DL (Delete Line) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/dl.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import (
	"fmt"
	"testing"
)

// prepare fills the screen with 4-char line numbers (0001, 0002, ...) and places cursor at (1,2).
func prepare(d *Driver) {
	height := d.GetScreenSize().Height
	for i := 0; i < height; i++ {
		y := i + 1
		CUP(d, NewPoint(1, y))
		d.Write(fmt.Sprintf("%04d", y))
	}
	CUP(d, NewPoint(1, 2))
}

// prepareForRegion sets up a 5x5 screen with specific content.
func prepareForRegion(d *Driver) {
	lines := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	for i, line := range lines {
		y := i + 1
		CUP(d, NewPoint(1, y))
		d.Write(line)
	}
	CUP(d, NewPoint(3, 2))
}

// Test_DL_DefaultParam tests that DL with no parameter deletes a single line.
func Test_DL_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	prepare(d)
	DL(d)

	height := d.GetScreenSize().Height
	expected := []string{}
	for y := 1; y <= height; y++ {
		if y != 2 {
			expected = append(expected, fmt.Sprintf("%04d", y))
		}
	}
	expected = append(expected, "    ") // Last line blank

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 4, height), expected)
}

// Test_DL_ExplicitParam tests that DL deletes the given number of lines.
func Test_DL_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	prepare(d)
	DL(d, 2)

	height := d.GetScreenSize().Height
	expected := []string{}
	for y := 1; y <= height; y++ {
		if y < 2 || y > 3 {
			expected = append(expected, fmt.Sprintf("%04d", y))
		}
	}
	expected = append(expected, "    ", "    ") // Last two lines blank

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 4, height), expected)
}

// Test_DL_DeleteMoreThanVisible tests passing a too-big parameter to DL.
func Test_DL_DeleteMoreThanVisible(t *testing.T) {
	d := NewDriver(80, 24)
	prepare(d)

	height := d.GetScreenSize().Height
	DL(d, height*2)

	expected := []string{"0001"}
	for i := 1; i < height; i++ {
		expected = append(expected, "    ")
	}

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 4, height), expected)
}

// Test_DL_InScrollRegion tests that DL works correctly inside scroll region.
func Test_DL_InScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareForRegion(d)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	DL(d)
	DECSTBM(d, 0, 0)

	expected := []string{"abcde", "klmno", "pqrst", "     ", "uvwxy"}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5), expected)
}

// Test_DL_OutsideScrollRegion tests that DL does nothing outside scroll region.
func Test_DL_OutsideScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareForRegion(d)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 1))
	DL(d)
	DECSTBM(d, 0, 0)

	expected := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5), expected)
}

// Test_DL_InLeftRightScrollRegion tests that DL respects left-right margins.
func Test_DL_InLeftRightScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareForRegion(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	DL(d)
	DECRESET(d, DECLRMM)

	expected := []string{"abcde", "flmnj", "kqrso", "pvwxt", "u   y"}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5), expected)
}

// Test_DL_OutsideLeftRightScrollRegion tests that DL does nothing outside left-right margins.
func Test_DL_OutsideLeftRightScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareForRegion(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	CUP(d, NewPoint(1, 2))
	DL(d)
	DECRESET(d, DECLRMM)

	expected := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5), expected)
}

// Test_DL_InLeftRightAndTopBottomScrollRegion tests that DL respects both margin types.
func Test_DL_InLeftRightAndTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareForRegion(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	DL(d)
	DECRESET(d, DECLRMM)
	DECSTBM(d, 0, 0)

	expected := []string{"abcde", "flmnj", "kqrso", "p   t", "uvwxy"}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5), expected)
}

// Test_DL_ClearOutLeftRightAndTopBottomScrollRegion tests erasing whole scroll region.
func Test_DL_ClearOutLeftRightAndTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareForRegion(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	DL(d, 99)
	DECRESET(d, DECLRMM)
	DECSTBM(d, 0, 0)

	expected := []string{"abcde", "f   j", "k   o", "p   t", "uvwxy"}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5), expected)
}

// Test_DL_OutsideLeftRightAndTopBottomScrollRegion tests DL does nothing when outside both margin types.
func Test_DL_OutsideLeftRightAndTopBottomScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	prepareForRegion(d)
	DECSET(d, DECLRMM)
	DECSLRM(d, 2, 4)
	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(1, 1))
	DL(d)
	DECRESET(d, DECLRMM)
	DECSTBM(d, 0, 0)

	expected := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5), expected)
}
