// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	_ "github.com/framegrace/texelation/apps/texelterm/parser/sparse"
)

// feed pushes every rune of s through the parser.
func feed(p *parser.Parser, s string) {
	for _, r := range s {
		p.Parse(r)
	}
}

// gridTagAtRow returns the leading non-blank text in grid[row].
func gridTagAtRow(grid [][]parser.Cell, row int) string {
	if row < 0 || row >= len(grid) {
		return ""
	}
	var b strings.Builder
	for _, c := range grid[row] {
		if c.Rune == 0 || c.Rune == ' ' {
			if b.Len() == 0 {
				continue
			}
			break
		}
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// TestVTerm_LiveFollowLongOutput simulates an `ls -lR`-style flood: many
// short lines written consecutively. After the flood, the grid should show
// the most recent lines and PhysicalCursor should agree — i.e. the cursor's
// y position should land on a row that is actually in the rendered grid at
// the cursor's writeTop-relative offset.
func TestVTerm_LiveFollowLongOutput(t *testing.T) {
	const w, h = 80, 24
	v := parser.NewVTerm(w, h)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	// Shell prompt.
	feed(p, "$ ls -lR\r\n")

	// Flood output — 200 identifiable lines, each one short enough not to wrap.
	for i := range 200 {
		feed(p, fmt.Sprintf("L%03d: /path/to/file_%03d.txt\r\n", i, i))
	}

	grid := v.Grid()
	if len(grid) != h {
		t.Fatalf("grid rows = %d, want %d", len(grid), h)
	}

	// The parser emits CR+LF as "CR then LF". After the final CRLF, the
	// cursor is on a fresh blank row. The last non-blank row should contain
	// L199.
	lastNonBlank := ""
	for y := h - 1; y >= 0; y-- {
		if tag := gridTagAtRow(grid, y); tag != "" {
			lastNonBlank = tag
			break
		}
	}
	if !strings.HasPrefix(lastNonBlank, "L199") {
		t.Errorf("live-follow regression: last visible tag = %q, want L199*", lastNonBlank)
		for y, row := range grid {
			if tag := gridTagAtRow(grid, y); tag != "" {
				t.Logf("  grid[%02d]: %s", y, strings.TrimRight(cellsToStringTest(row), " \x00"))
			}
		}
	}

	// PhysicalCursor should also be at the bottom (cursor just newlined).
	px, py := v.PhysicalCursor()
	if py < h-2 || py > h-1 {
		t.Errorf("PhysicalCursor y = %d, want ~%d (last row)", py, h-1)
	}
	if px != 0 {
		t.Errorf("PhysicalCursor x = %d, want 0 (fresh row after CRLF)", px)
	}
}

// TestVTerm_LiveFollowWithResize reproduces the exact sequence the user hit:
// shell prompt, then bulk output on a terminal that just got resized. The
// real texelterm issues a Resize during pane init, then the shell floods
// output. If resize leaves viewAnchor in a bad state, the viewport will be
// stuck on history.
func TestVTerm_LiveFollowWithResize(t *testing.T) {
	const w, h = 80, 24
	v := parser.NewVTerm(w, h)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	feed(p, "$ ")
	// Resize (real texelterm triggers this during pane init).
	v.Resize(w, h)

	feed(p, "ls -lR\r\n")
	for i := range 100 {
		feed(p, fmt.Sprintf("L%03d: hello\r\n", i))
	}

	grid := v.Grid()
	lastNonBlank := ""
	for y := h - 1; y >= 0; y-- {
		if tag := gridTagAtRow(grid, y); tag != "" {
			lastNonBlank = tag
			break
		}
	}
	if !strings.HasPrefix(lastNonBlank, "L099") {
		t.Errorf("live-follow after resize: last tag = %q, want L099*", lastNonBlank)
	}
}

// TestVTerm_LiveFollowWithColors feeds colored output (CSI SGR) to check that
// SGR does not break the live-anchor path.
func TestVTerm_LiveFollowWithColors(t *testing.T) {
	const w, h = 80, 24
	v := parser.NewVTerm(w, h)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	for i := range 100 {
		// Simulate `ls --color=auto` output: bold blue, reset.
		feed(p, fmt.Sprintf("\x1b[01;34mL%03d\x1b[0m: file\r\n", i))
	}

	grid := v.Grid()
	lastNonBlank := ""
	for y := h - 1; y >= 0; y-- {
		if tag := gridTagAtRow(grid, y); tag != "" {
			lastNonBlank = tag
			break
		}
	}
	if !strings.HasPrefix(lastNonBlank, "L099") {
		t.Errorf("live-follow with SGR: last tag = %q, want L099*", lastNonBlank)
	}
}

// TestVTerm_PhysicalCursorMatchesGrid verifies the cursor's reported physical
// position is a row that actually exists in Grid() — specifically, that the
// PhysicalCursor fallback doesn't lie when CursorToView fails. The user's
// "cursor visible at the bottom of the panel, but can't see output" symptom
// is exactly this class of bug: cursor and grid disagree about where the
// live edge is.
func TestVTerm_PhysicalCursorMatchesGrid(t *testing.T) {
	const w, h = 80, 10
	v := parser.NewVTerm(w, h)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	// Write 30 lines so scrolling happens.
	for i := range 30 {
		feed(p, fmt.Sprintf("row%02d\r\n", i))
	}
	// Write a final marker with no trailing newline.
	feed(p, "MARK")

	grid := v.Grid()
	px, py := v.PhysicalCursor()

	if py < 0 || py >= h {
		t.Fatalf("PhysicalCursor y=%d out of range [0,%d)", py, h)
	}
	cellAtCursor := ""
	if len(grid[py]) > 0 {
		cellAtCursor = gridTagAtRow(grid, py)
	}
	if !strings.HasPrefix(cellAtCursor, "MARK") {
		t.Errorf("cursor/grid disagree: PhysicalCursor=(%d,%d) points to row %q, expected MARK*",
			px, py, cellAtCursor)
		for y, row := range grid {
			if tag := gridTagAtRow(grid, y); tag != "" {
				t.Logf("  grid[%02d]: %s", y, strings.TrimRight(cellsToStringTest(row), " \x00"))
			}
		}
	}
}

// TestVTerm_RestoreScrollOffsetThenNewOutput reproduces Regression A.
// Scenario: user closes texelterm while scrolled back; saved state carries
// ScrollOffset=N>0. On restart, applyRestoredStateLocked calls SetScrollOffset(N)
// BEFORE any render pass. ScrollUp(N) runs while viewAnchor is still 0,
// decrementing it clamps back to 0, and autoFollow is now false. Subsequent
// shell output can't pull the viewport back — the view is pinned to oldest
// history while the cursor is clamped to the bottom row.
func TestVTerm_RestoreScrollOffsetThenNewOutput(t *testing.T) {
	const w, h = 80, 10
	v := parser.NewVTerm(w, h)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	// Populate some history (~30 lines on a 10-row terminal, so plenty of
	// scrollback).
	for i := range 30 {
		feed(p, fmt.Sprintf("H%03d\r\n", i))
	}

	// Simulate applyRestoredStateLocked with a saved ScrollOffset of 5.
	// This is the exact call site in term.go:522.
	v.SetScrollOffset(5)

	// Shell resumes output.
	for i := range 20 {
		feed(p, fmt.Sprintf("N%03d\r\n", i))
	}

	// User hits End / scrolls back to live edge.
	v.ScrollToLiveEdge()

	grid := v.Grid()
	lastNonBlank := ""
	for y := h - 1; y >= 0; y-- {
		if tag := gridTagAtRow(grid, y); tag != "" {
			lastNonBlank = tag
			break
		}
	}
	if !strings.HasPrefix(lastNonBlank, "N019") {
		t.Errorf("after restore scroll-offset + new output + ScrollToLiveEdge: last tag = %q, want N019*", lastNonBlank)
		for y, row := range grid {
			if tag := gridTagAtRow(grid, y); tag != "" {
				t.Logf("  grid[%02d]: %s", y, strings.TrimRight(cellsToStringTest(row), " \x00"))
			}
		}
	}
}

// TestVTerm_SetScrollOffsetFromLiveEdge reproduces the "fresh restart with
// prior scroll offset" scenario more tightly. User never rendered since
// startup; ScrollUp should still land on a sensible anchor rather than
// collapsing viewAnchor to 0.
func TestVTerm_SetScrollOffsetFromLiveEdge(t *testing.T) {
	const w, h = 80, 10
	v := parser.NewVTerm(w, h)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	for i := range 30 {
		feed(p, fmt.Sprintf("H%03d\r\n", i))
	}

	// SetScrollOffset(3) without any intervening render — simulates the exact
	// path at startup where applyRestoredStateLocked runs before the first
	// Render pass.
	v.SetScrollOffset(3)

	grid := v.Grid()
	// Expect the view to be scrolled back 3 rows from the live edge. Last
	// written line before the trailing CRLF was "H029". Live edge had H25..H29
	// + blank cursor row on 10-row terminal. 3 rows back means the bottom
	// should now show H26 or thereabouts, NOT H00.
	lastNonBlank := ""
	for y := h - 1; y >= 0; y-- {
		if tag := gridTagAtRow(grid, y); tag != "" {
			lastNonBlank = tag
			break
		}
	}
	if strings.HasPrefix(lastNonBlank, "H00") {
		t.Errorf("SetScrollOffset(3) collapsed viewAnchor to 0: last tag = %q, want H2x", lastNonBlank)
		for y, row := range grid {
			if tag := gridTagAtRow(grid, y); tag != "" {
				t.Logf("  grid[%02d]: %s", y, strings.TrimRight(cellsToStringTest(row), " \x00"))
			}
		}
	}
}

// cellsToStringTest is a local helper to avoid depending on unexported helpers.
func cellsToStringTest(row []parser.Cell) string {
	var b strings.Builder
	for _, c := range row {
		if c.Rune == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}
