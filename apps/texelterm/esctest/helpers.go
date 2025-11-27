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

// VPA (Vertical Position Absolute) - Move cursor to row n on current column.
func VPA(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[d", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dd", ESC, n[0]))
	}
}

// HVP (Horizontal and Vertical Position) - Same as CUP but uses 'f' instead of 'H'.
func HVP(d *Driver, p ...Point) {
	if len(p) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[f", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%d;%df", ESC, p[0].Y, p[0].X))
	}
}

// CNL (Cursor Next Line) - Move cursor down n lines to column 1.
func CNL(d *Driver, n ...int) {
	count := 1
	if len(n) > 0 {
		count = n[0]
	}
	d.WriteRaw(fmt.Sprintf("%s[%dE", ESC, count))
}

// CPL (Cursor Previous Line) - Move cursor up n lines to column 1.
func CPL(d *Driver, n ...int) {
	count := 1
	if len(n) > 0 {
		count = n[0]
	}
	d.WriteRaw(fmt.Sprintf("%s[%dF", ESC, count))
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

// REP (Repeat) - Repeat the previous graphic character n times.
func REP(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[b", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%db", ESC, n[0]))
	}
}

// DL (Delete Line) - Delete n lines at cursor position.
func DL(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[M", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dM", ESC, n[0]))
	}
}

// IL (Insert Line) - Insert n blank lines at cursor position.
func IL(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[L", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dL", ESC, n[0]))
	}
}

// ED (Erase in Display) - Erase parts of the display.
func ED(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[J", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dJ", ESC, n[0]))
	}
}

// EL (Erase in Line) - Erase parts of the line.
func EL(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[K", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dK", ESC, n[0]))
	}
}

// IND (Index) - Move cursor down one line, scroll if at bottom.
func IND(d *Driver) {
	d.WriteRaw(fmt.Sprintf("%sD", ESC))
}

// RI (Reverse Index) - Move cursor up one line, scroll if at top.
func RI(d *Driver) {
	d.WriteRaw(fmt.Sprintf("%sM", ESC))
}

// SU (Scroll Up) - Scroll up by n lines (default 1).
func SU(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[S", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dS", ESC, n[0]))
	}
}

// SD (Scroll Down) - Scroll down by n lines (default 1).
func SD(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[T", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dT", ESC, n[0]))
	}
}

// HTS (Horizontal Tab Set) - Set a tab stop at current column (ESC H).
func HTS(d *Driver) {
	d.WriteRaw(fmt.Sprintf("%sH", ESC))
}

// TBC (Tab Clear) - Clear tab stops.
// No parameter or 0: clear tab at cursor
// 3: clear all tabs
func TBC(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[g", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dg", ESC, n[0]))
	}
}

// HPA (Horizontal Position Absolute) - Move cursor to absolute column.
func HPA(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[`", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%d`", ESC, n[0]))
	}
}

// HPR (Horizontal Position Relative) - Move cursor right by n columns.
func HPR(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[a", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%da", ESC, n[0]))
	}
}

// VPR (Vertical Position Relative) - Move cursor down by n rows.
func VPR(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[e", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%de", ESC, n[0]))
	}
}

// CBT (Cursor Backward Tab) - Move cursor backward n tab stops.
func CBT(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[Z", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dZ", ESC, n[0]))
	}
}

// CHT (Cursor Horizontal Tab) - Move cursor forward n tab stops.
func CHT(d *Driver, n ...int) {
	if len(n) == 0 {
		d.WriteRaw(fmt.Sprintf("%s[I", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%dI", ESC, n[0]))
	}
}

// CR (Carriage Return) - Move cursor to column 1 (or left margin).
func CR(d *Driver) {
	d.WriteRaw("\r")
}

// LF (Line Feed) - Move cursor down one line.
func LF(d *Driver) {
	d.WriteRaw("\n")
}

// NEL (Next Line) - Move cursor to next line and column 1.
func NEL(d *Driver) {
	d.WriteRaw(fmt.Sprintf("%sE", ESC))
}

// DECALN (Screen Alignment Test) - Fill screen with E's, reset margins, move cursor to home.
func DECALN(d *Driver) {
	d.WriteRaw(fmt.Sprintf("%s#8", ESC))
}

// VT (Vertical Tab) - Move cursor down one line (same as IND).
func VT(d *Driver) {
	d.WriteRaw("\v")
}

// FF (Form Feed) - Move cursor down one line (same as IND).
func FF(d *Driver) {
	d.WriteRaw("\f")
}

// DECSTBM (Set Top and Bottom Margins) - Set scrolling region.
func DECSTBM(d *Driver, top, bottom int) {
	if top == 0 && bottom == 0 {
		// Reset margins
		d.WriteRaw(fmt.Sprintf("%s[r", ESC))
	} else {
		d.WriteRaw(fmt.Sprintf("%s[%d;%dr", ESC, top, bottom))
	}
}

// DECSC (Save Cursor) - Save cursor position and attributes (ESC 7).
func DECSC(d *Driver) {
	d.WriteRaw(fmt.Sprintf("%s7", ESC))
}

// DECRC (Restore Cursor) - Restore cursor position and attributes (ESC 8).
func DECRC(d *Driver) {
	d.WriteRaw(fmt.Sprintf("%s8", ESC))
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
