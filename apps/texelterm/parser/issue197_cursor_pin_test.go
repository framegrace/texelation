// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Issue #197: cursor row drifts up on widen instead of staying pinned at
// the viewport bottom. When content above the cursor reflows into fewer
// physical rows (because chains unwrap on widen), RecomputeLiveAnchor
// clamps the backward walk at writeTop and falls through to anchoring
// at writeTop with offset 0. Render fills `accumulated` rows and pads
// the bottom with blanks — the cursor row ends up mid-viewport.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// TestIssue197_CursorPinnedAtBottomOnWiden writes content that wraps at
// the initial width, then widens and verifies the cursor row stays at the
// bottom of the viewport.
func TestIssue197_CursorPinnedAtBottomOnWiden(t *testing.T) {
	const initialWidth = 40
	const height = 20
	const widerWidth = 120

	v := NewVTerm(initialWidth, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write 30 lines of ~80-char content. At width 40 each line autowraps
	// into 2 physical rows; at width 120 each line fits on 1 row. So
	// widening should reduce content from ~60 physical rows to ~30, leaving
	// scrollback rows that need to be pulled in to refill the viewport top.
	for i := 0; i < 30; i++ {
		tag := fmt.Sprintf("L%05d ", i)
		body := strings.Repeat("xy", (80-len(tag))/2)
		parseString(p, tag+body+"\r\n")
	}

	v.Resize(widerWidth, height)
	grid := v.Grid()

	lastNonBlank := -1
	for i, row := range grid {
		if hasVisibleContent(row) {
			lastNonBlank = i
		}
	}

	// After widen, the cursor sits at the row after the last written line
	// (where the next prompt would render). The expected behavior is that
	// the cursor row — the last visible content row plus zero or one blank
	// row — stays at the bottom of the viewport. If RecomputeLiveAnchor
	// failed to pull in scrollback to refill the top, the cursor row
	// drifts up and the bottom of the viewport is blank.
	cursorY := getCursorY(v)
	t.Logf("after widen: cursorY=%d, lastNonBlank=%d, height=%d", cursorY, lastNonBlank, height)
	if cursorY < height-1 {
		// Acceptable iff there's no scrollback to pull in. Here we wrote
		// 30 lines × 2 rows = 60 physical rows of history, far more than
		// height=20 — so there's plenty to pull from.
		t.Errorf("cursor at row %d after widen (want %d, the bottom of the viewport); blank rows below cursor indicate the live anchor failed to pull scrollback in to refill the top", cursorY, height-1)
		dumpGrid(t, grid)
	}
}

// getCursorY returns the cursor's viewport row. Tries CursorToView first;
// falls back to "row after the last visible content row" since the cursor
// sits on the row immediately after the last written line.
func getCursorY(v *VTerm) int {
	if row, _, ok := v.mainScreen.CursorToView(); ok {
		return row
	}
	grid := v.Grid()
	last := -1
	for i, row := range grid {
		if hasVisibleContent(row) {
			last = i
		}
	}
	return last + 1
}
