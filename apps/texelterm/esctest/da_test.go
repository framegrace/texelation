// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests DA (Device Attributes) - primary device attributes query.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/da.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// DA (CSI c) queries the terminal for its capabilities.
// Response format: CSI ? Ps ; Ps ; ... c
// Where Ps values indicate supported features:
//   62 = VT220, 63 = VT320, 64 = VT420, 65 = VT520
//   1 = 132 columns, 2 = printer, 4 = sixel, 6 = selective erase,
//   21 = horizontal scrolling, 22 = color, 28 = rectangular editing
package esctest

import (
	"strings"
	"testing"
)

// Test_DA_NoParameter tests DA with no parameter.
func Test_DA_NoParameter(t *testing.T) {
	d := NewDriver(80, 24)

	// Send DA query
	d.WriteRaw("\x1b[c")

	// Check that a response was sent
	response := d.ReadPtyResponse()
	if response == "" {
		t.Fatal("No response to DA query")
	}

	// Response should be CSI ? ... c
	if !strings.HasPrefix(response, "\x1b[?") {
		t.Errorf("DA response should start with CSI ?, got: %q", response)
	}
	if !strings.HasSuffix(response, "c") {
		t.Errorf("DA response should end with 'c', got: %q", response)
	}

	// Response should include VT level (62 for VT220)
	if !strings.Contains(response, "62") {
		t.Errorf("DA response should include VT level 62 (VT220), got: %q", response)
	}
}

// Test_DA_0 tests DA with explicit parameter 0.
func Test_DA_0(t *testing.T) {
	d := NewDriver(80, 24)

	// Send DA query with parameter 0
	d.WriteRaw("\x1b[0c")

	// Check that a response was sent
	response := d.ReadPtyResponse()
	if response == "" {
		t.Fatal("No response to DA query")
	}

	// Response should be CSI ? ... c
	if !strings.HasPrefix(response, "\x1b[?") {
		t.Errorf("DA response should start with CSI ?, got: %q", response)
	}
	if !strings.HasSuffix(response, "c") {
		t.Errorf("DA response should end with 'c', got: %q", response)
	}

	// Response should include VT level (62 for VT220)
	if !strings.Contains(response, "62") {
		t.Errorf("DA response should include VT level 62 (VT220), got: %q", response)
	}
}
