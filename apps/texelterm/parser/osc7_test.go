// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/osc7_test.go
// Summary: OSC 7 "current working directory" sequence parsing.
// Ported from the pre-sparse vterm_memory_buffer_test.go.

package parser

import "testing"

// TestOSC7_WorkingDirectory verifies that OSC 7 (current working directory)
// sequences in both `file://host/path` and `file:///path` forms populate
// CurrentWorkingDir, and that malformed sequences without the file:// prefix
// are ignored.
func TestOSC7_WorkingDirectory(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	p := NewParser(v)

	// OSC 7 with hostname: ESC ] 7 ; <uri> BEL
	parseString(p, "\x1b]7;file://myhost/home/user/projects\x07")
	if got, want := v.CurrentWorkingDir, "/home/user/projects"; got != want {
		t.Errorf("OSC 7 with host: got %q, want %q", got, want)
	}

	// OSC 7 with empty hostname (file:///path)
	parseString(p, "\x1b]7;file:///tmp/test\x07")
	if got, want := v.CurrentWorkingDir, "/tmp/test"; got != want {
		t.Errorf("OSC 7 empty host: got %q, want %q", got, want)
	}

	// OSC 7 with invalid URI (no file:// prefix) — must leave CurrentWorkingDir unchanged.
	v.CurrentWorkingDir = ""
	parseString(p, "\x1b]7;/tmp/bad\x07")
	if v.CurrentWorkingDir != "" {
		t.Errorf("OSC 7 invalid URI: got %q, want empty", v.CurrentWorkingDir)
	}
}
