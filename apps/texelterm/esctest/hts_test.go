// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file contains tests for the HTS (Horizontal Tab Set) escape sequence.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/hts.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// These tests have been converted from Python to Go for offline testing
// of the texelterm terminal emulator without requiring Python or PTY interaction.
//
// Note: 8-bit control test skipped as we don't support 8-bit controls.
package esctest

import "testing"

// Test_HTS_Basic tests that HTS sets a tab stop at the cursor position.
func Test_HTS_Basic(t *testing.T) {
	d := NewDriver(80, 24)

	// Remove all tabs
	TBC(d, 3)

	// Set a tabstop at column 20
	CUP(d, NewPoint(20, 1))
	HTS(d)

	// Move to column 1 and tab - should go to 20
	CUP(d, NewPoint(1, 1))
	d.Write("\t")

	pos := d.GetCursorPosition()
	AssertEQ(t, pos.X, 20)
}
