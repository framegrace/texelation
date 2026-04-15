// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/reverse_search_test.go
// Summary: Sparse-native port of TestReverseSearch_RealReadlineSequences
// from vterm_memory_buffer_test.go. Replays the exact escape sequences
// emitted by bash readline during reverse-i-search, including ICH (\e[1@),
// DCH (\e[nP), and reverse video for match highlighting. Exercises the
// dirty-tracking render simulation from CLAUDE.md's "Testing Visual Bugs"
// section to ensure the cursor's rendered row matches Grid() after every
// edit step.

package parser

import (
	"strings"
	"testing"
)

// TestReverseSearch_RealReadlineSequences replays the actual escape
// sequences captured from bash readline (reverse search → accept → edit
// with ICH character insertion). Verifies that the dirty-tracking render
// simulation produces the same content as Grid() at the cursor's row.
func TestReverseSearch_RealReadlineSequences(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	width, height := 80, 24
	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "bash-5.3$ ")

	renderBuf := make([][]Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, width)
	}
	simulateRender := func() {
		dirtyLines, allDirty := v.DirtyLines()
		grid := v.Grid()
		if allDirty {
			for y := 0; y < height && y < len(grid); y++ {
				copy(renderBuf[y], grid[y])
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < height && y < len(grid) {
					copy(renderBuf[y], grid[y])
				}
			}
		}
		v.ClearDirty()
	}
	simulateRender()

	// Type "echo hello world" and press Enter (adds to bash history).
	parseString(p, "echo hello world\r\n")
	parseString(p, "hello world\r\n")
	parseString(p, "bash-5.3$ ")
	simulateRender()

	// Ctrl+R: bash enters reverse search mode.
	parseString(p, "\r(reverse-i-search)`': ")
	simulateRender()

	// Type 'e' in search; a match is found (with reverse video highlight).
	parseString(p, "\b\b\be': echo h\x1b[7me\x1b[27mllo world")
	parseString(p, strings.Repeat("\b", 19))
	simulateRender()

	// Right arrow to accept (DCH to remove search prefix, then redraw).
	parseString(p, "\r\x1b[16Pbash-5.3$ echo hello world")
	parseString(p, strings.Repeat("\b", 12))
	simulateRender()

	// Ctrl+A: move to beginning of editable area.
	parseString(p, strings.Repeat("\b", v.cursorX-10))
	simulateRender()

	// Type 'X' using ICH (\e[1@ inserts blank, then X overwrites).
	parseString(p, "\x1b[1@X")
	simulateRender()

	// renderBuf and Grid must agree on the cursor's row.
	grid := v.Grid()
	gridRow := strings.TrimRight(gridRowToString(grid[v.cursorY]), " ")
	renderRow := strings.TrimRight(gridRowToString(renderBuf[v.cursorY]), " ")
	if gridRow != renderRow {
		t.Errorf("render/grid mismatch at row %d: render=%q grid=%q", v.cursorY, renderRow, gridRow)
	}
	if !strings.Contains(gridRow, "Xecho hello world") {
		t.Errorf("cursor row: got %q, want substring %q", gridRow, "Xecho hello world")
	}
}
