// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This package is derived from esctest2 by George Nachman and Thomas E. Dickey.
// Original project: https://github.com/ThomasDickey/esctest2
// License: GPL v2
//
// The tests have been converted from Python to Go to enable offline, deterministic
// testing of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import (
	"fmt"
	"strings"
	"testing"
)

// ESC is the escape character.
const ESC = "\x1b"

// --- Assertion Functions ---

// AssertEQ asserts that two values are equal.
func AssertEQ(t *testing.T, actual, expected interface{}) {
	t.Helper()
	if actual != expected {
		t.Errorf("Expected %v, got %v", expected, actual)
	}
}

// AssertTrue asserts that a value is true.
func AssertTrue(t *testing.T, value bool, message string) {
	t.Helper()
	if !value {
		if message != "" {
			t.Errorf("Assertion failed: %s", message)
		} else {
			t.Error("Assertion failed")
		}
	}
}

// AssertGE asserts that actual is greater than or equal to minimum.
func AssertGE(t *testing.T, actual, minimum int) {
	t.Helper()
	if actual < minimum {
		t.Errorf("Expected %d >= %d", actual, minimum)
	}
}

// AssertScreenCharsInRectEqual asserts that the characters in a rectangle match expected strings.
func AssertScreenCharsInRectEqual(t *testing.T, d *Driver, rect Rect, expected []string) {
	t.Helper()
	actual := d.GetScreenCharsInRect(rect)

	if len(actual) != len(expected) {
		t.Errorf("Line count mismatch: expected %d lines, got %d lines", len(expected), len(actual))
		return
	}

	for i, expectedLine := range expected {
		if i >= len(actual) {
			t.Errorf("Line %d: missing (expected %q)", i+1, expectedLine)
			continue
		}
		if actual[i] != expectedLine {
			t.Errorf("Line %d: expected %q, got %q", i+1, expectedLine, actual[i])
		}
	}
}

// --- Escape Sequence Commands ---

// CUP (Cursor Position) - Move cursor to specified position.
func CUP(d *Driver, p Point) {
	d.WriteRaw(fmt.Sprintf("%s[%d;%dH", ESC, p.Y, p.X))
}

// CUU (Cursor Up) - Move cursor up by n lines.
func CUU(d *Driver, n ...int) {
	count := 1
	if len(n) > 0 {
		count = n[0]
	}
	d.WriteRaw(fmt.Sprintf("%s[%dA", ESC, count))
}

// CUD (Cursor Down) - Move cursor down by n lines.
func CUD(d *Driver, n ...int) {
	count := 1
	if len(n) > 0 {
		count = n[0]
	}
	d.WriteRaw(fmt.Sprintf("%s[%dB", ESC, count))
}

// CUF (Cursor Forward) - Move cursor forward by n columns.
func CUF(d *Driver, n ...int) {
	count := 1
	if len(n) > 0 {
		count = n[0]
	}
	d.WriteRaw(fmt.Sprintf("%s[%dC", ESC, count))
}

// CUB (Cursor Back) - Move cursor backward by n columns.
func CUB(d *Driver, n ...int) {
	count := 1
	if len(n) > 0 {
		count = n[0]
	}
	d.WriteRaw(fmt.Sprintf("%s[%dD", ESC, count))
}

// CHA (Cursor Horizontal Absolute) - Move cursor to column n on current line.
func CHA(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[G", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dG", ESC, n[0]))
	}
}

// ICH (Insert Character) - Insert n blank characters at cursor position.
func ICH(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[@", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%d@", ESC, n[0]))
	}
}

// DCH (Delete Character) - Delete n characters at cursor position.
func DCH(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[P", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dP", ESC, n[0]))
	}
}

// ECH (Erase Character) - Erase n characters at cursor position.
func ECH(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[X", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dX", ESC, n[0]))
	}
}

// DECSTBM (Set Top and Bottom Margins) - Set scrolling region.
func DECSTBM(d *Driver, top, bottom int) {
	d.WriteRaw(fmt.Sprintf("%s[%d;%dr", ESC, top, bottom))
}

// DECSLRM (Set Left and Right Margins) - Set left/right margins.
func DECSLRM(d *Driver, left, right int) {
	d.WriteRaw(fmt.Sprintf("%s[%d;%ds", ESC, left, right))
}

// DECSET - Set DEC Private Mode.
func DECSET(d *Driver, mode int) {
	d.WriteRaw(fmt.Sprintf("%s[?%dh", ESC, mode))
}

// DECRESET - Reset DEC Private Mode.
func DECRESET(d *Driver, mode int) {
	d.WriteRaw(fmt.Sprintf("%s[?%dl", ESC, mode))
}

// DEC Private Mode constants
const (
	DECLRMM           = 69   // Left/right margin mode
	DECOM             = 6    // Origin mode
	DECAWM            = 7    // Auto-wrap mode
	ReverseWrapInline = 45   // Reverse-wraparound mode
	ReverseWrapExtend = 1045 // Extended Reverse-wraparound mode
)

// Blank returns a space character (used for blank cells).
// In xterm, blank cells are represented as spaces.
func Blank() string {
	return " "
}

// Empty returns a NUL character (used for empty/erased cells).
// In some terminals, erased cells are distinguished from written blanks.
func Empty() string {
	return " " // For simplicity, we treat empty cells as spaces
}

// Repeat returns a string repeated n times.
func Repeat(s string, n int) string {
	return strings.Repeat(s, n)
}
