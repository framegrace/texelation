// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the IL (Insert Line) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/il.py
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

// prepareWide sets up a screen for IL tests.
// Sets up: abcde / fghij / klmno
// With cursor on the 'l' (column 2, row 3)
func prepareWide(d *Driver) {
	CUP(d, NewPoint(1, 1))
	d.Write("abcde")
	CUP(d, NewPoint(1, 2))
	d.Write("fghij")
	CUP(d, NewPoint(1, 3))
	d.Write("klmno")
	CUP(d, NewPoint(2, 3))
}

// Test_IL_DefaultParam tests that IL inserts a single line at cursor.
func Test_IL_DefaultParam(t *testing.T) {
	d := NewDriver(80, 24)
	prepareWide(d)
	IL(d)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 4),
		[]string{"abcde", "fghij", "     ", "klmno"})
}

// Test_IL_ExplicitParam tests that IL inserts the given number of lines.
func Test_IL_ExplicitParam(t *testing.T) {
	d := NewDriver(80, 24)
	prepareWide(d)
	IL(d, 2)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "fghij", "     ", "     ", "klmno"})
}

// Test_IL_ScrollsOffBottom tests that lines scroll off the bottom.
func Test_IL_ScrollsOffBottom(t *testing.T) {
	d := NewDriver(80, 24)
	height := d.GetScreenSize().Height

	for i := 0; i < height; i++ {
		CUP(d, NewPoint(1, i+1))
		d.Write(fmt.Sprintf("%04d", i+1))
	}
	CUP(d, NewPoint(1, 2))
	IL(d)

	expected := 1
	for i := 0; i < height; i++ {
		y := i + 1
		if y == 2 {
			AssertScreenCharsInRectEqual(t, d, NewRect(1, y, 4, y), []string{"    "})
		} else {
			AssertScreenCharsInRectEqual(t, d, NewRect(1, y, 4, y), []string{fmt.Sprintf("%04d", expected)})
			expected++
		}
	}
}

// Test_IL_RespectsScrollRegion tests that IL respects scroll region.
func Test_IL_RespectsScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	lines := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	for i, line := range lines {
		CUP(d, NewPoint(1, i+1))
		d.Write(line)
	}

	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	IL(d)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "     ", "fghij", "klmno", "uvwxy"})
}

// Test_IL_RespectsScrollRegion_Over tests IL with count larger than region.
func Test_IL_RespectsScrollRegion_Over(t *testing.T) {
	d := NewDriver(80, 24)
	lines := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	for i, line := range lines {
		CUP(d, NewPoint(1, i+1))
		d.Write(line)
	}

	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 2))
	IL(d, 99)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "     ", "     ", "     ", "uvwxy"})
}

// Test_IL_AboveScrollRegion tests that IL does nothing above scroll region.
func Test_IL_AboveScrollRegion(t *testing.T) {
	d := NewDriver(80, 24)
	lines := []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"}
	for i, line := range lines {
		CUP(d, NewPoint(1, i+1))
		d.Write(line)
	}

	DECSTBM(d, 2, 4)
	CUP(d, NewPoint(3, 1))
	IL(d)
	DECSTBM(d, 0, 0)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 5, 5),
		[]string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"})
}
