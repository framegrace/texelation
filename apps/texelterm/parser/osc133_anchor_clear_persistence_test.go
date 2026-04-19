// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/osc133_anchor_clear_persistence_test.go
// Summary: End-to-end regression test for the phantom-Claude-text bug.
// Simulates a full shell session: OSC 133 anchors set, command output written,
// ED 2 fired (TUI reset), session closed and reopened. Verifies that the
// tombstoned rows are absent after reload — i.e. the phantom text does not
// bleed into the new session's scrollback.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// TestOSC133_ED2_TombstonesPersistToDisk is the direct regression for the
// phantom-Claude-text bug: a non-alt-screen TUI (like Claude Code) writes
// output below the prompt, then issues ED 2 to home its first frame. The
// rows between CommandStart and WriteBottomHWM should be tombstoned on disk
// so that after a server restart the phantom text does not appear in
// scrollback.
//
// Sequence under test:
//  1. OSC 133;A (prompt start) + "$ " prompt text
//  2. OSC 133;B (prompt end) + "ls" user input + CRLF
//  3. OSC 133;C (command start)
//  4. 10 lines of "phantom output line X"
//  5. ED 2 (ESC[2J) — TUI resets with anchor-rewind + tombstone
//  6. CloseMemoryBuffer — flushes all ops to disk
//  7. Reopen from disk
//  8. Scan every loaded row: none may contain "phantom output"
func TestOSC133_ED2_TombstonesPersistToDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	// Use a small viewport so that phantom output overflows it and forces
	// writeTop to advance past the CommandStart anchor. The rewind + tombstone
	// path is only exercised when writeTop > anchor.
	const cols, rows = 40, 5

	// --- Session 1: simulate a Claude-style shell interaction ---
	v := NewVTerm(cols, rows)
	if err := v.EnableMemoryBufferWithDisk(dir, MemoryBufferOptions{
		TerminalID: "osc133-tomb-test",
	}); err != nil {
		t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
	}
	if v.mainScreen == nil {
		t.Skip("MainScreenFactory not registered; sparse package not imported")
	}

	p := NewParser(v)

	// OSC 133;A — prompt start (shell emits before drawing prompt)
	parseString(p, "\x1b]133;A\x07")
	// Prompt text: "$ "
	parseString(p, "$ ")
	// OSC 133;B — prompt end / input start
	parseString(p, "\x1b]133;B\x07")
	// User types "ls" then presses Enter
	parseString(p, "ls\r\n")
	// OSC 133;C — command start
	parseString(p, "\x1b]133;C\x07")

	cmdAnchor := v.CommandStartGlobalLine
	if cmdAnchor < 0 {
		t.Fatalf("OSC 133;C did not set CommandStartGlobalLine (got %d)", cmdAnchor)
	}

	// Write 10 lines of phantom output — more than the viewport height (5)
	// so writeTop advances past cmdAnchor, setting up the rewind condition.
	for i := 0; i < 10; i++ {
		parseString(p, fmt.Sprintf("phantom output line %d\r\n", i))
	}

	hwmBefore := v.mainScreen.WriteBottomHWM()

	// Verify the setup condition: writeTop must have advanced past cmdAnchor
	// for the rewind + tombstone path to fire.
	writeTopBeforeED2 := v.mainScreen.WriteTop()
	if writeTopBeforeED2 <= cmdAnchor {
		t.Fatalf("setup: writeTop=%d did not advance past cmdAnchor=%d (need viewport overflow)",
			writeTopBeforeED2, cmdAnchor)
	}

	// ED 2 — TUI initialises its first frame, clearing the screen.
	// The anchor-rewind path in mainScreenEraseScreen should:
	//   1. Rewind writeTop to cmdAnchor
	//   2. ClearRangePersistent([cmdAnchor, HWM]) — tombstone the phantom rows
	//   3. EraseDisplay — blank the viewport
	parseString(p, "\x1b[2J")

	// Quick sanity check: writeTop should have rewound to cmdAnchor.
	if got := v.mainScreen.WriteTop(); got != cmdAnchor {
		t.Errorf("writeTop after ED 2: got %d, want %d (CommandStart anchor)", got, cmdAnchor)
	}

	// Close the session, flushing all write + delete ops to disk.
	if err := v.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// --- Session 2: reload from disk and verify no phantom text ---
	v2 := NewVTerm(cols, rows)
	if err := v2.EnableMemoryBufferWithDisk(dir, MemoryBufferOptions{
		TerminalID: "osc133-tomb-test",
	}); err != nil {
		t.Fatalf("re-EnableMemoryBufferWithDisk: %v", err)
	}
	defer v2.CloseMemoryBuffer()

	if v2.mainScreen == nil {
		t.Fatal("v2.mainScreen is nil after reload")
	}

	// Scan every row from globalIdx 0 through hwmBefore (the highest row
	// any phantom text could have been written to). None may contain
	// "phantom output".
	for gi := int64(0); gi <= hwmBefore; gi++ {
		cells := v2.mainScreen.ReadLine(gi)
		if cells == nil {
			continue
		}
		var sb strings.Builder
		for _, c := range cells {
			if c.Rune != 0 {
				sb.WriteRune(c.Rune)
			}
		}
		if strings.Contains(sb.String(), "phantom output") {
			t.Errorf("stale phantom text survived reload at gi=%d: %q", gi, sb.String())
		}
	}
}
