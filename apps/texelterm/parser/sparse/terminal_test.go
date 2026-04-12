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
	_ = parser.Cell{}
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

	tm.Resize(80, 30) // grow

	_, afterBottom := tm.VisibleRange()
	if afterBottom != beforeBottom {
		t.Errorf("frozen view moved: %d -> %d", beforeBottom, afterBottom)
	}
}
