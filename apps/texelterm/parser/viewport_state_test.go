// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"testing"
)

func TestViewportState_CommitViewportAsFixedWidth_Basic(t *testing.T) {
	// Create history to receive commits
	history := NewScrollbackHistory(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
	})

	// Create viewport with content
	vs := NewViewportState(10, 3, history)

	// Write some content to each row
	vs.SetCursor(0, 0)
	for _, r := range "Row0Hello" {
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}
	vs.SetCursor(0, 1)
	for _, r := range "Row1World" {
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}
	vs.SetCursor(0, 2)
	for _, r := range "Row2Test!" {
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}

	// Commit viewport as fixed-width
	committed := vs.CommitViewportAsFixedWidth()

	if committed != 3 {
		t.Errorf("expected 3 lines committed, got %d", committed)
	}

	// Check history has 3 lines
	if history.TotalLen() != 3 {
		t.Errorf("expected history to have 3 lines, got %d", history.TotalLen())
	}

	// Check each line has FixedWidth set
	for i := int64(0); i < 3; i++ {
		line := history.GetGlobal(i)
		if line == nil {
			t.Fatalf("line %d is nil", i)
		}
		if line.FixedWidth != 10 {
			t.Errorf("line %d: expected FixedWidth=10, got %d", i, line.FixedWidth)
		}
	}
}

func TestViewportState_CommitViewportAsFixedWidth_SkipsCommitted(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
	})

	vs := NewViewportState(10, 3, history)

	// Mark row 1 as already committed
	vs.MarkRowAsCommitted(1)

	// Write content to all rows
	vs.SetCursor(0, 0)
	for _, r := range "Row0" {
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}
	vs.SetCursor(0, 2)
	for _, r := range "Row2" {
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}

	committed := vs.CommitViewportAsFixedWidth()

	// Should skip row 1 (already committed)
	if committed != 2 {
		t.Errorf("expected 2 lines committed (skipping already-committed), got %d", committed)
	}
}

func TestViewportState_CommitViewportAsFixedWidth_MarksAsCommitted(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
	})

	vs := NewViewportState(10, 3, history)

	// Commit once
	vs.CommitViewportAsFixedWidth()

	// Second commit should commit 0 (all already committed)
	committed := vs.CommitViewportAsFixedWidth()

	if committed != 0 {
		t.Errorf("expected 0 lines committed on second call, got %d", committed)
	}
}

func TestViewportState_CommitViewportAsFixedWidth_PreservesContent(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
	})

	vs := NewViewportState(10, 2, history)

	// Write specific content
	vs.SetCursor(0, 0)
	for _, r := range "ABCDEFGHIJ" { // Exactly 10 chars
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}
	vs.SetCursor(0, 1)
	for _, r := range "1234" { // 4 chars, rest will be spaces
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}

	vs.CommitViewportAsFixedWidth()

	// Check content is preserved
	line0 := history.GetGlobal(0)
	if cellsToString(line0.Cells) != "ABCDEFGHIJ" {
		t.Errorf("line 0: expected 'ABCDEFGHIJ', got '%s'", cellsToString(line0.Cells))
	}

	line1 := history.GetGlobal(1)
	content := cellsToString(line1.Cells)
	// Content should be "1234" followed by 6 spaces, but TrimTrailingSpaces may trim
	if len(content) < 4 || content[:4] != "1234" {
		t.Errorf("line 1: expected to start with '1234', got '%s'", content)
	}
}

func TestViewportState_CommitViewportAsFixedWidth_NilHistory(t *testing.T) {
	// Create viewport without history
	vs := NewViewportState(10, 3, nil)

	// Should not panic, should return 0
	committed := vs.CommitViewportAsFixedWidth()

	if committed != 0 {
		t.Errorf("expected 0 lines committed with nil history, got %d", committed)
	}
}

func TestViewportState_CommitViewportAsFixedWidth_FixedWidthNoReflow(t *testing.T) {
	// This tests that committed lines don't reflow on resize
	history := NewScrollbackHistory(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
	})

	vs := NewViewportState(20, 2, history)

	// Write 20-char content
	vs.SetCursor(0, 0)
	for _, r := range "12345678901234567890" {
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}

	vs.CommitViewportAsFixedWidth()

	// Get the line
	line := history.GetGlobal(0)

	// Wrap to 10 columns - should NOT wrap (FixedWidth=20)
	wrapped := line.WrapToWidth(10)

	if len(wrapped) != 1 {
		t.Errorf("fixed-width line should not wrap, got %d physical lines", len(wrapped))
	}

	// Content should be clipped to 10
	if len(wrapped[0].Cells) != 10 {
		t.Errorf("expected 10 cells (clipped), got %d", len(wrapped[0].Cells))
	}
}

func TestViewportState_CommitViewportAsFixedWidth_SkipsFromHistory(t *testing.T) {
	// This tests that rows populated from history are NOT re-committed.
	// This is critical to prevent the scroll duplication bug where
	// scrolling up and then triggering TUI commit would create duplicate entries.
	history := NewScrollbackHistory(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
	})

	// Pre-populate history with some content
	for i := 0; i < 5; i++ {
		line := NewLogicalLine()
		for _, r := range "HistoryLine" {
			line.Append(Cell{Rune: r, FG: DefaultFG, BG: DefaultBG})
		}
		history.Append(line)
	}

	// Record initial history count
	initialHistoryLen := history.TotalLen()
	if initialHistoryLen != 5 {
		t.Fatalf("expected 5 history lines, got %d", initialHistoryLen)
	}

	// Create viewport
	vs := NewViewportState(20, 3, history)

	// Simulate row 0 being populated from history (scroll up scenario)
	// This is what MarkRowAsCommitted does when populating from history
	vs.MarkRowAsCommitted(0)

	// Row 1 is fresh content (not from history)
	vs.SetCursor(0, 1)
	for _, r := range "FreshContent" {
		vs.Write(r, DefaultFG, DefaultBG, 0, false)
	}

	// Row 2 is also from history
	vs.MarkRowAsCommitted(2)

	// Commit viewport - should only commit row 1 (fresh content)
	committed := vs.CommitViewportAsFixedWidth()

	if committed != 1 {
		t.Errorf("expected 1 line committed (only fresh content), got %d", committed)
	}

	// History should only have 1 new line
	if history.TotalLen() != initialHistoryLen+1 {
		t.Errorf("expected history to have %d lines, got %d", initialHistoryLen+1, history.TotalLen())
	}

	// The new line should be the fresh content
	newLine := history.GetGlobal(initialHistoryLen)
	content := cellsToString(newLine.Cells)
	if content != "FreshContent" {
		t.Errorf("expected 'FreshContent', got '%s'", content)
	}
}

func TestViewportState_CommitViewportAsFixedWidth_FromHistoryStaysAfterDirty(t *testing.T) {
	// Even if a row from history is marked dirty, it should still not be
	// re-committed because FromHistory is set.
	history := NewScrollbackHistory(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
	})

	vs := NewViewportState(20, 3, history)

	// Mark row 0 as from history
	vs.MarkRowAsCommitted(0)

	// Verify the FromHistory flag is set
	if !vs.rowMeta[0].FromHistory {
		t.Fatal("FromHistory should be true after MarkRowAsCommitted")
	}

	// Write to that row - this marks it dirty but should NOT clear FromHistory
	vs.SetCursor(0, 0)
	vs.Write('X', DefaultFG, DefaultBG, 0, false)

	// Row should now be dirty
	if vs.rowMeta[0].State != LineStateDirty {
		t.Error("row should be marked dirty after write")
	}

	// BUT FromHistory should still be true
	if !vs.rowMeta[0].FromHistory {
		t.Error("FromHistory should remain true after write - this is the bug fix")
	}

	// CommitViewportAsFixedWidth should skip this row because FromHistory is true
	committed := vs.CommitViewportAsFixedWidth()

	// Only rows 1 and 2 (which are clean/empty but not from history) should commit
	// Actually rows 1 and 2 are LineStateClean, which means they haven't been touched
	// Let me check the logic - CommitViewportAsFixedWidth checks:
	// 1. State == LineStateCommitted -> skip
	// 2. FromHistory -> skip
	// Row 0: Dirty but FromHistory=true -> skip
	// Row 1: Clean, FromHistory=false -> will be committed (empty line)
	// Row 2: Clean, FromHistory=false -> will be committed (empty line)
	// So we expect 2 commits
	if committed != 2 {
		t.Errorf("expected 2 lines committed, got %d", committed)
	}

	// History should have 2 lines
	if history.TotalLen() != 2 {
		t.Errorf("expected 2 history lines, got %d", history.TotalLen())
	}
}
