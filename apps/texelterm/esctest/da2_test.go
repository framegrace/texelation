// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests DA2 (Secondary Device Attributes) - terminal identification.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/da2.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// DA2 (CSI > c) queries the terminal for its type and version.
// Response format: CSI > Ps ; Pv ; Pc c
// Where:
//   Ps = Terminal type (0=VT100, 1=VT220, 24=VT320, 41=VT420, 64=VT520)
//   Pv = Firmware version number
//   Pc = Keyboard type (usually 0)
package esctest

import (
	"strings"
	"testing"
)

// Test_DA2_NoParameter tests DA2 with no parameter.
func Test_DA2_NoParameter(t *testing.T) {
	d := NewDriver(80, 24)

	// Send DA2 query
	d.WriteRaw("\x1b[>c")

	// Check that a response was sent
	response := d.ReadPtyResponse()
	if response == "" {
		t.Fatal("No response to DA2 query")
	}

	// Response should be CSI > ... c
	if !strings.HasPrefix(response, "\x1b[>") {
		t.Errorf("DA2 response should start with CSI >, got: %q", response)
	}
	if !strings.HasSuffix(response, "c") {
		t.Errorf("DA2 response should end with 'c', got: %q", response)
	}

	// Response should have three parameters (terminal type, version, keyboard)
	// Format: CSI > Ps ; Pv ; Pc c
	parts := strings.TrimPrefix(response, "\x1b[>")
	parts = strings.TrimSuffix(parts, "c")
	params := strings.Split(parts, ";")
	if len(params) != 3 {
		t.Errorf("DA2 response should have 3 parameters, got %d: %q", len(params), response)
	}
}

// Test_DA2_0 tests DA2 with explicit parameter 0.
func Test_DA2_0(t *testing.T) {
	d := NewDriver(80, 24)

	// Send DA2 query with parameter 0
	d.WriteRaw("\x1b[>0c")

	// Check that a response was sent
	response := d.ReadPtyResponse()
	if response == "" {
		t.Fatal("No response to DA2 query")
	}

	// Response should be CSI > ... c
	if !strings.HasPrefix(response, "\x1b[>") {
		t.Errorf("DA2 response should start with CSI >, got: %q", response)
	}
	if !strings.HasSuffix(response, "c") {
		t.Errorf("DA2 response should end with 'c', got: %q", response)
	}

	// Response should have three parameters (terminal type, version, keyboard)
	parts := strings.TrimPrefix(response, "\x1b[>")
	parts = strings.TrimSuffix(parts, "c")
	params := strings.Split(parts, ";")
	if len(params) != 3 {
		t.Errorf("DA2 response should have 3 parameters, got %d: %q", len(params), response)
	}
}
