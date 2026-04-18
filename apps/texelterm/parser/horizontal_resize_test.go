// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Issue #48 — duplicate lines on horizontal (width-only) resize. Post-PR #184
// (view-side reflow), widening reflows wrapped chains into fewer physical
// rows. The hypothesis under test: the chain re-join pulls already-visible
// content into the viewport a second time, producing visible duplicates.
//
// These tests feed long wrapping content through the VTerm parser (so the
// autowrap `Wrapped` flags are set), resize wider, and inspect the rendered
// grid for repeated line identifiers. Each line carries a unique "L00NNN"
// token; a unique token should never appear on more than one visual row.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// TestHorizontalResize_NoDuplicatesOnWiden is the minimal repro: write 40
// lines of ~200-char content through the parser at width 80 (so each line
// autowraps into 3 physical rows, each 80-col row has Wrapped=true on its
// last cell), widen to 120, and check the rendered grid for duplicates.
//
// At 80-wide, a 200-char logical line takes 3 rows (80+80+40). At 120-wide,
// the same content reflows to 2 rows (120+80). If the reflow walk pulls in
// already-rendered rows a second time, the same "L00NNN" token appears in
// multiple visual rows — which is what the user reports.
func TestHorizontalResize_NoDuplicatesOnWiden(t *testing.T) {
	const initialWidth = 80
	const height = 24
	const widerWidth = 120
	const numLines = 40
	const logicalLen = 200 // forces wrap at 80: 3 rows; at 120: 2 rows

	v := NewVTerm(initialWidth, height)
	p := NewParser(v)

	for i := 0; i < numLines; i++ {
		tag := fmt.Sprintf("L%05d ", i)
		body := strings.Repeat("abcdefghij", (logicalLen-len(tag))/10)
		parseString(p, tag+body+"\r\n")
	}

	v.Resize(widerWidth, height)

	grid := v.Grid()
	counts := tokenCounts(grid)
	for tok, c := range counts {
		if c > 1 {
			t.Errorf("token %q appears on %d visual rows after widen (want ≤1)", tok, c)
		}
	}
	t.Logf("grid after widen %d→%d (height=%d):", initialWidth, widerWidth, height)
	dumpGrid(t, grid)
}

// TestHorizontalResize_RepeatedWidensNoDuplicates simulates a window-manager
// drag: a burst of Resize() calls at monotonically increasing widths. Each
// resize is a fresh SIGWINCH to the TUI. If the bug compounds per-resize,
// this test should accumulate more duplicates than a single resize.
func TestHorizontalResize_RepeatedWidensNoDuplicates(t *testing.T) {
	const height = 24
	const numLines = 30
	const logicalLen = 200

	v := NewVTerm(80, height)
	p := NewParser(v)
	for i := 0; i < numLines; i++ {
		tag := fmt.Sprintf("L%05d ", i)
		body := strings.Repeat("abcdefghij", (logicalLen-len(tag))/10)
		parseString(p, tag+body+"\r\n")
	}

	for w := 82; w <= 160; w += 4 {
		v.Resize(w, height)
	}

	grid := v.Grid()
	counts := tokenCounts(grid)
	for tok, c := range counts {
		if c > 1 {
			t.Errorf("token %q appears on %d visual rows after drag-resize (want ≤1)", tok, c)
		}
	}
	if t.Failed() {
		dumpGrid(t, grid)
	}
}

// TestHorizontalResize_ClearRedrawNoDuplicates simulates what a well-behaved
// TUI does on SIGWINCH: emit ESC[H ESC[2J and re-emit its screen. If the
// pre-clear viewport content leaked into scrollback (as widened-reflow rows
// above the live window), the post-clear redraw would stack on top, yielding
// both the reflowed scrollback copy and the fresh redraw as "duplicates".
func TestHorizontalResize_ClearRedrawNoDuplicates(t *testing.T) {
	const height = 24
	const numLines = 20
	const logicalLen = 200

	v := NewVTerm(80, height)
	p := NewParser(v)
	for i := 0; i < numLines; i++ {
		tag := fmt.Sprintf("L%05d ", i)
		body := strings.Repeat("abcdefghij", (logicalLen-len(tag))/10)
		parseString(p, tag+body+"\r\n")
	}

	v.Resize(120, height)

	// TUI's SIGWINCH handler: home + clear display, then redraw screen.
	parseString(p, "\x1b[H\x1b[2J")
	for i := 0; i < height-2; i++ {
		tag := fmt.Sprintf("R%05d ", i)
		parseString(p, tag+"redraw\r\n")
	}

	grid := v.Grid()
	counts := tokenCounts(grid)
	for tok, c := range counts {
		if c > 1 {
			t.Errorf("token %q appears on %d visual rows after clear+redraw (want ≤1)", tok, c)
		}
	}
	if t.Failed() {
		dumpGrid(t, grid)
	}
}

// TestHorizontalResize_ClaudeBannerRepaintStyle models the Claude TUI's
// observed resize-repaint behaviour (#48). After a resize, Claude:
//   - positions cursor to (1,1) with ESC[H
//   - re-emits its 9-row banner
//   - re-emits the prompt + any response content so far
//
// Critically, it does NOT send ESC[2J or ESC[J first — it overwrites cells
// in-place. If the banner's rows were 1:1 at the OLD width, widening the
// window keeps them 1:1 (no reflow, they were never wrapped). Then the
// overwrite lands on the same cells. Good.
//
// But rows that WERE wrapped at the old width (e.g. long response lines)
// reflow to fewer physical rows at the new width. Claude still emits the
// SAME number of rows as at old width — so what Claude thinks is row 10
// ends up at a different globalIdx than the pre-resize row 10. Result:
// leftover content from rows Claude didn't overwrite stays on-screen.
//
// If the observed duplicates show this shape (banner intact but interleaved
// with old content), this is the mechanism.
func TestHorizontalResize_ClaudeBannerRepaintStyle(t *testing.T) {
	const height = 24
	v := NewVTerm(80, height)
	p := NewParser(v)

	// Fake "Claude banner" — 5 short rows (not wrapped at 80).
	banner := []string{
		" BANNER-A line 1",
		" BANNER-B line 2",
		" BANNER-C line 3",
		" BANNER-D line 4",
		"❯ print long lines",
	}
	for _, row := range banner {
		parseString(p, row+"\r\n")
	}

	// Response content: long lines that WILL wrap at 80 (180 chars each).
	for i := 0; i < 8; i++ {
		tag := fmt.Sprintf("L%05d ", i)
		body := strings.Repeat("abcdefghij", 17)
		parseString(p, tag+body+"\r\n")
	}

	// User drags wider. No explicit clear — just SIGWINCH.
	v.Resize(120, height)

	// Claude's SIGWINCH repaint: home + re-emit banner + re-emit prompt.
	// NO ESC[2J.
	parseString(p, "\x1b[H")
	for _, row := range banner {
		parseString(p, row+"\r\n")
	}

	grid := v.Grid()
	counts := tokenCounts(grid)
	bannerSeen := map[string]int{}
	for _, row := range grid {
		text := cellsToString(row)
		for _, b := range banner {
			if strings.Contains(text, strings.TrimSpace(b)[:8]) {
				bannerSeen[strings.TrimSpace(b)[:8]]++
			}
		}
	}
	for tag, c := range bannerSeen {
		if c > 1 {
			t.Errorf("banner row %q visible on %d rows after Claude-style repaint", tag, c)
		}
	}
	for tok, c := range counts {
		if c > 1 {
			t.Errorf("L-token %q appears on %d visual rows", tok, c)
		}
	}
	t.Logf("grid after Claude-style repaint at width 120:")
	dumpGrid(t, grid)
}

// tokenCounts extracts each row's unique identifier (the first 6 characters
// matching L00NNN or R00NNN) and counts occurrences across the grid. Empty
// rows and rows without a token are ignored.
func tokenCounts(grid [][]Cell) map[string]int {
	counts := make(map[string]int)
	for _, row := range grid {
		text := cellsToString(row)
		tok := firstToken(text)
		if tok != "" {
			counts[tok]++
		}
	}
	return counts
}

// firstToken scans a rendered row for the leading L/R-prefixed tag
// "L00NNN"/"R00NNN" used by these tests. Returns "" if none is found.
func firstToken(s string) string {
	for i := 0; i+6 <= len(s); i++ {
		c := s[i]
		if c != 'L' && c != 'R' {
			continue
		}
		allDigits := true
		for j := i + 1; j < i+6; j++ {
			if s[j] < '0' || s[j] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return s[i : i+6]
		}
	}
	return ""
}

func dumpGrid(t *testing.T, grid [][]Cell) {
	t.Helper()
	for i, row := range grid {
		t.Logf("[%02d] %s", i, trimRight(cellsToString(row)))
	}
}
