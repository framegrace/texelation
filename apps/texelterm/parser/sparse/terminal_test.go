// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestTerminal_NewInitialState(t *testing.T) {
	tm := NewTerminal(80, 24)
	if got := tm.Width(); got != 80 {
		t.Errorf("Width = %d, want 80", got)
	}
	if got := tm.Height(); got != 24 {
		t.Errorf("Height = %d, want 24", got)
	}
	if !tm.IsFollowing() {
		t.Error("new Terminal should follow writeBottom")
	}
	if got := tm.ContentEnd(); got != -1 {
		t.Errorf("fresh ContentEnd = %d, want -1", got)
	}
}

func TestTerminal_WriteCellAdvancesFollowingView(t *testing.T) {
	tm := NewTerminal(10, 5)
	tm.WriteCell(parser.Cell{Rune: 'h'})
	tm.Newline()
	// Cursor should be on row 1 now.
	gi, col := tm.Cursor()
	if gi != 1 || col != 0 {
		t.Errorf("after Newline, Cursor = (%d,%d), want (1,0)", gi, col)
	}
	// Because we're following, viewBottom should track writeBottom.
	_, vbottom := tm.VisibleRange()
	if vbottom != 4 {
		t.Errorf("viewBottom = %d, want 4 (writeBottom)", vbottom)
	}
}

func TestTerminal_NewlineAtBottomScrollsAndViewFollows(t *testing.T) {
	tm := NewTerminal(10, 3)
	tm.SetCursor(2, 0)
	tm.Newline()
	// writeTop advanced, writeBottom = 3, following view snaps.
	_, vbottom := tm.VisibleRange()
	if vbottom != 3 {
		t.Errorf("viewBottom = %d, want 3", vbottom)
	}
}

func TestTerminal_ResizeShrinkShellCase(t *testing.T) {
	tm := NewTerminal(80, 40)
	// Fill 40 rows.
	for i := 0; i < 40; i++ {
		tm.WriteCell(parser.Cell{Rune: 'X'})
		tm.Newline()
	}
	// cursor is now at row 40 of a scrolled window.
	tm.SetCursor(39, 0)
	tm.Resize(80, 20)

	_, vbottom := tm.VisibleRange()
	_, writeBottom := tm.WriteTop(), tm.WriteBottom()
	if vbottom != writeBottom {
		t.Errorf("following view: viewBottom = %d, writeBottom = %d", vbottom, writeBottom)
	}
	if got := tm.Height(); got != 20 {
		t.Errorf("Height = %d, want 20", got)
	}
}

func TestTerminal_ResizeFrozenViewStaysPut(t *testing.T) {
	tm := NewTerminal(80, 40)
	for i := 0; i < 80; i++ {
		tm.WriteCell(parser.Cell{Rune: 'X'})
		tm.Newline()
	}
	// Scroll back 20 rows.
	tm.ScrollUp(20)
	_, beforeBottom := tm.VisibleRange()

	tm.Resize(80, 30) // shrink; frozen view must not move (Rule 6)

	_, afterBottom := tm.VisibleRange()
	if afterBottom != beforeBottom {
		t.Errorf("frozen view moved: %d -> %d", beforeBottom, afterBottom)
	}
}

func TestTerminal_GridReturnsHeightXWidth(t *testing.T) {
	tm := NewTerminal(10, 5)
	tm.WriteCell(parser.Cell{Rune: 'A'})
	tm.WriteCell(parser.Cell{Rune: 'B'})

	grid := tm.Grid()
	if len(grid) != 5 {
		t.Fatalf("grid rows = %d, want 5", len(grid))
	}
	for y, row := range grid {
		if len(row) != 10 {
			t.Errorf("row %d width = %d, want 10", y, len(row))
		}
	}
	if grid[0][0].Rune != 'A' {
		t.Errorf("grid[0][0] = %q, want A", grid[0][0].Rune)
	}
	if grid[0][1].Rune != 'B' {
		t.Errorf("grid[0][1] = %q, want B", grid[0][1].Rune)
	}
	// Unwritten cells are blank.
	if grid[0][5].Rune != 0 {
		t.Errorf("grid[0][5] = %q, want blank", grid[0][5].Rune)
	}
	if grid[4][0].Rune != 0 {
		t.Errorf("grid[4][0] = %q, want blank (unwritten row)", grid[4][0].Rune)
	}
}

func TestTerminal_GridReflectsScrollback(t *testing.T) {
	tm := NewTerminal(10, 3)
	// Fill rows 0,1,2 then scroll down — writeTop=1, writeBottom=3.
	tm.WriteCell(parser.Cell{Rune: 'A'})
	tm.Newline()
	tm.WriteCell(parser.Cell{Rune: 'B'})
	tm.Newline()
	tm.WriteCell(parser.Cell{Rune: 'C'})
	tm.Newline() // scrolls
	// Following view: viewBottom = 3, view covers [1,2,3]
	grid := tm.Grid()
	if grid[0][0].Rune != 'B' {
		t.Errorf("grid[0][0] = %q, want B (row 1)", grid[0][0].Rune)
	}
	if grid[1][0].Rune != 'C' {
		t.Errorf("grid[1][0] = %q, want C (row 2)", grid[1][0].Rune)
	}
	if grid[2][0].Rune != 0 {
		t.Errorf("grid[2][0] = %q, want blank (row 3, unwritten)", grid[2][0].Rune)
	}
}

// TestTerminal_ShrinkDragDoesNotDuplicateTextContent simulates claude-like
// behavior: each shrink step, "redraw" the UI at the new size with a text
// marker on row 1. Afterward, verify the text marker appears exactly once in
// the store.
func TestTerminal_ShrinkDragDoesNotDuplicateTextContent(t *testing.T) {
	tm := NewTerminal(80, 40)
	marker := "Claude Code"

	// Initial draw: border on row 0, marker on row 1, cursor parked at
	// row 37 (input box bottom).
	drawUI := func(h int) {
		// Border row 0.
		tm.SetCursor(0, 0)
		for _, r := range "┌──────────────┐" {
			tm.WriteCell(parser.Cell{Rune: r})
		}
		// Text row 1.
		tm.SetCursor(1, 0)
		for _, r := range marker {
			tm.WriteCell(parser.Cell{Rune: r})
		}
		// Cursor parked at last-row (input box).
		tm.SetCursor(h-2, 5)
	}

	drawUI(40)

	// Shrink-drag from 40 -> 20.
	for h := 39; h >= 20; h-- {
		tm.Resize(80, h)
		// Clear the old window and redraw at new size.
		// (In real life, the TUI does this via ESC[2J or scroll region.)
		tm.EraseDisplay()
		drawUI(h)
	}

	// Count occurrences of the marker across the entire store, from globalIdx 0
	// up to ContentEnd.
	count := 0
	end := tm.ContentEnd()
	for gi := int64(0); gi <= end; gi++ {
		line := tm.ReadLine(gi)
		if containsRunes(line, []rune(marker)) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("marker %q appears %d times in store; want 1", marker, count)
	}
}

// getStore is a test-only accessor for the internal Store.
func getStore(t *Terminal) *Store { return t.store }

// containsRunes reports whether row contains the full sequence needle as a
// contiguous run of Rune fields.
func containsRunes(row []parser.Cell, needle []rune) bool {
	if len(needle) == 0 || len(row) < len(needle) {
		return false
	}
	for start := 0; start+len(needle) <= len(row); start++ {
		match := true
		for j, r := range needle {
			if row[start+j].Rune != r {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
