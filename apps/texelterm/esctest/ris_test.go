// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests RIS (Reset to Initial State, ESC c) behavior.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/ris.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// The tests have been converted from Python to Go to enable offline, deterministic
// testing of the texelterm terminal emulator.
package esctest

import "testing"

// Test_RIS_ClearsScreen tests that RIS clears the screen.
func Test_RIS_ClearsScreen(t *testing.T) {
	d := NewDriver(80, 24)
	d.WriteRaw("x")

	RIS(d)

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 1), []string{Empty()})
}

// Test_RIS_CursorToOrigin tests that RIS moves cursor to origin.
func Test_RIS_CursorToOrigin(t *testing.T) {
	d := NewDriver(80, 24)
	CUP(d, NewPoint(5, 6))

	RIS(d)

	AssertEQ(t, d.GetCursorPosition(), NewPoint(1, 1))
}

// Test_RIS_ResetTabs tests that RIS resets tab stops to default (every 8 columns).
func Test_RIS_ResetTabs(t *testing.T) {
	d := NewDriver(80, 24)
	// Set some custom tab stops
	HTS(d)
	CUF(d, 1)
	HTS(d)
	CUF(d, 1)
	HTS(d)

	RIS(d)

	// After reset, tab should go to column 9 (default every 8)
	d.WriteRaw("\t")
	AssertEQ(t, d.GetCursorPosition(), NewPoint(9, 1))
}

// Test_RIS_ExitAltScreen tests that RIS exits alt screen and clears both buffers.
func Test_RIS_ExitAltScreen(t *testing.T) {
	d := NewDriver(80, 24)
	d.WriteRaw("m")
	DECSET(d, 1049) // Enter alt screen
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("a")

	RIS(d)

	// Should be back on main screen, which should be cleared
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 1), []string{Empty()})
	// Alt screen should also be cleared
	DECSET(d, 1049)
	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 1), []string{Empty()})
}

// Test_RIS_ResetDECOM tests that RIS resets origin mode.
func Test_RIS_ResetDECOM(t *testing.T) {
	d := NewDriver(80, 24)
	DECSTBM(d, 5, 7)
	DECSET(d, DECLRMM)
	DECSLRM(d, 5, 7)
	DECSET(d, DECOM)

	RIS(d)

	// Origin mode should be off, so CUP(1,1) should write to actual 1,1
	CUP(d, NewPoint(1, 1))
	d.WriteRaw("X")

	DECRESET(d, DECLRMM)
	DECSTBM(d, 0, 0) // Reset margins

	AssertScreenCharsInRectEqual(t, d, NewRect(1, 1, 1, 1), []string{"X"})
}

// Test_RIS_RemoveMargins tests that RIS removes all margins.
func Test_RIS_RemoveMargins(t *testing.T) {
	d := NewDriver(80, 24)
	DECSET(d, DECLRMM)
	DECSLRM(d, 3, 5)
	DECSTBM(d, 4, 6)

	RIS(d)

	// Margins should be gone, so cursor can move freely
	CUP(d, NewPoint(3, 4))
	CUB(d, 1)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 4))
	CUU(d, 1)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(2, 3))

	CUP(d, NewPoint(5, 6))
	CUF(d, 1)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 6))
	CUD(d, 1)
	AssertEQ(t, d.GetCursorPosition(), NewPoint(6, 7))
}
