// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests BS (Backspace) character behavior.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/bs.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// Note: Reverse wraparound tests (modes 45/1045) are not yet implemented in texelterm.
// These tests focus on basic backspace behavior and margin interaction.
package esctest

import (
	"testing"
)

// Test_BS_Basic tests that backspace moves cursor left one position.
func Test_BS_Basic(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(3, 3))
	BS(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(2, 3))
}

// Test_BS_NoWrapByDefault tests that backspace stops at left edge.
func Test_BS_NoWrapByDefault(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 3))
	BS(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(1, 3))
}

// Test_BS_StopsAtOrigin tests that backspace at (1,1) stays at (1,1).
func Test_BS_StopsAtOrigin(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 1))
	BS(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(1, 1))
}

// Test_BS_StopsAtLeftMargin tests that backspace stops at left margin.
func Test_BS_StopsAtLeftMargin(t *testing.T) {
	d := NewDriver(80, 24)

	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)
	CUP(d, NewPoint(5, 1))
	BS(d)
	DECRESET(d, DECLRMM)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(5, 1))
}

// Test_BS_MovesLeftWhenLeftOfLeftMargin tests that backspace still works outside margins.
func Test_BS_MovesLeftWhenLeftOfLeftMargin(t *testing.T) {
	d := NewDriver(80, 24)

	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 10)
	CUP(d, NewPoint(4, 1))
	BS(d)
	DECRESET(d, DECLRMM)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(3, 1))
}

// Test_BS_ClearsWrapNext tests that backspace from wrapNext position works correctly.
// When autowrap is enabled and cursor reaches right edge, a "wrapNext" flag is set.
// Backspace clears wrapNext and moves the cursor back one position.
func Test_BS_ClearsWrapNext(t *testing.T) {
	d := NewDriver(80, 24)

	// Move to column 79 and write two characters
	// 'a' goes to column 79, cursor moves to column 80
	// 'b' goes to column 80, cursor sets wrapNext flag (stays at column 80)
	CUP(d, NewPoint(79, 1))
	d.WriteRaw("ab")

	// Cursor is at column 80 with wrapNext=true
	// Backspace clears wrapNext and moves cursor to column 79
	BS(d)

	// Writing 'X' overwrites 'a' at column 79
	d.WriteRaw("X")

	// Result: column 79 has 'X', column 80 has 'b'
	AssertScreenCharsInRectEqual(t, d, NewRect(79, 1, 80, 1), []string{"Xb"})
}

// Test_BS_MultipleBackspaces tests multiple backspaces in a row.
func Test_BS_MultipleBackspaces(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(10, 5))
	BS(d)
	BS(d)
	BS(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos, NewPoint(7, 5))
}

// Test_BS_DoesNotChangeRow tests that backspace doesn't change row (no reverse wrap).
func Test_BS_DoesNotChangeRow(t *testing.T) {
	d := NewDriver(80, 24)

	CUP(d, NewPoint(1, 5))
	BS(d)

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.Y, 5) // Row should not change
	AssertEQ(t, pos.X, 1) // Should stay at column 1
}
