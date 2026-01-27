// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/tui_viewport_manager_test.go

package parser

import (
	"testing"
)

func TestTUIViewportManager_EnterExitMode(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(80, 24, history)
	mgr := NewTUIViewportManager(viewport, history)

	if mgr.IsActive() {
		t.Error("Expected inactive initially")
	}

	mgr.EnterTUIMode()
	if !mgr.IsActive() {
		t.Error("Expected active after EnterTUIMode")
	}

	// Enter again should be no-op
	mgr.EnterTUIMode()
	if !mgr.IsActive() {
		t.Error("Expected still active")
	}

	mgr.ExitTUIMode()
	if mgr.IsActive() {
		t.Error("Expected inactive after ExitTUIMode")
	}
}

func TestTUIViewportManager_LiveViewportStart(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(80, 24, history)
	mgr := NewTUIViewportManager(viewport, history)

	// Add some history
	history.Append(NewLogicalLineFromCells([]Cell{{Rune: 'A'}}))
	history.Append(NewLogicalLineFromCells([]Cell{{Rune: 'B'}}))
	history.Append(NewLogicalLineFromCells([]Cell{{Rune: 'C'}}))

	if history.TotalLen() != 3 {
		t.Errorf("Expected 3 lines, got %d", history.TotalLen())
	}

	// Enter TUI mode - should record liveViewportStart
	mgr.EnterTUIMode()

	if mgr.LiveViewportStart() != 3 {
		t.Errorf("Expected liveViewportStart=3, got %d", mgr.LiveViewportStart())
	}
}

func TestTUIViewportManager_CommitLiveViewport(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(80, 24, history)
	mgr := NewTUIViewportManager(viewport, history)

	// Write some content to viewport
	viewport.Write('H', DefaultFG, DefaultBG, 0, false)
	viewport.Write('e', DefaultFG, DefaultBG, 0, false)
	viewport.Write('l', DefaultFG, DefaultBG, 0, false)
	viewport.Write('l', DefaultFG, DefaultBG, 0, false)
	viewport.Write('o', DefaultFG, DefaultBG, 0, false)

	mgr.EnterTUIMode()

	committed := mgr.CommitLiveViewport()
	if committed == 0 {
		t.Error("Expected at least 1 line committed")
	}

	// Check history has the content
	if history.TotalLen() == 0 {
		t.Error("Expected history to have lines after commit")
	}
}

func TestTUIViewportManager_FreezeScrolledLines(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(80, 24, history)
	mgr := NewTUIViewportManager(viewport, history)

	mgr.EnterTUIMode()

	// Create some lines to freeze
	line1 := NewLogicalLineFromCells([]Cell{{Rune: 'L'}, {Rune: '1'}})
	line2 := NewLogicalLineFromCells([]Cell{{Rune: 'L'}, {Rune: '2'}})

	mgr.FreezeScrolledLines([]*LogicalLine{line1, line2})

	if history.TotalLen() != 2 {
		t.Errorf("Expected 2 frozen lines, got %d", history.TotalLen())
	}

	if mgr.FrozenLineCount() != 2 {
		t.Errorf("Expected 2 frozen tracked, got %d", mgr.FrozenLineCount())
	}
}

func TestTUIViewportManager_TruncateTo(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(80, 24, history)
	mgr := NewTUIViewportManager(viewport, history)

	// Add 10 lines
	for i := 0; i < 10; i++ {
		history.Append(NewLogicalLineFromCells([]Cell{{Rune: rune('0' + i)}}))
	}

	if history.TotalLen() != 10 {
		t.Errorf("Expected 10 lines, got %d", history.TotalLen())
	}

	// Truncate to keep only 5
	mgr.TruncateTo(5)

	if history.TotalLen() != 5 {
		t.Errorf("Expected 5 lines after truncate, got %d", history.TotalLen())
	}
}

func TestTUIViewportManager_Reset(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(80, 24, history)
	mgr := NewTUIViewportManager(viewport, history)

	mgr.EnterTUIMode()
	mgr.FreezeScrolledLines([]*LogicalLine{NewLogicalLineFromCells([]Cell{{Rune: 'X'}})})

	if !mgr.IsActive() || mgr.FrozenLineCount() == 0 {
		t.Error("Expected active with frozen lines before reset")
	}

	mgr.Reset()

	if mgr.IsActive() {
		t.Error("Expected inactive after reset")
	}
	if mgr.FrozenLineCount() != 0 {
		t.Errorf("Expected 0 frozen count after reset, got %d", mgr.FrozenLineCount())
	}
	if mgr.LiveViewportStart() != 0 {
		t.Errorf("Expected liveViewportStart=0 after reset, got %d", mgr.LiveViewportStart())
	}
}

func TestTUIViewportManager_IsFrozen(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(80, 24, history)
	mgr := NewTUIViewportManager(viewport, history)

	mgr.EnterTUIMode()
	mgr.FreezeScrolledLines([]*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'A'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'B'}}),
	})

	if !mgr.IsFrozen(0) {
		t.Error("Expected line 0 to be frozen")
	}
	if !mgr.IsFrozen(1) {
		t.Error("Expected line 1 to be frozen")
	}
	if mgr.IsFrozen(2) {
		t.Error("Expected line 2 to NOT be frozen")
	}
}

func TestTUIViewportManager_NotActiveSkipsFreeze(t *testing.T) {
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(80, 24, history)
	mgr := NewTUIViewportManager(viewport, history)

	// Don't enter TUI mode
	mgr.FreezeScrolledLines([]*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'X'}}),
	})

	// Should be ignored since not active
	if history.TotalLen() != 0 {
		t.Errorf("Expected 0 lines (freeze skipped when not active), got %d", history.TotalLen())
	}
}

func TestTUIViewportManager_NoDuplicatesOnMultipleCommits(t *testing.T) {
	// This test verifies the truncate+append pattern works correctly:
	// Multiple commits should REPLACE content, not append duplicates.
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(20, 5, history)
	mgr := NewTUIViewportManager(viewport, history)

	// Write content to viewport row 0
	viewport.SetCursor(0, 0)
	for _, r := range "TUI Content" {
		viewport.Write(r, DefaultFG, DefaultBG, 0, false)
	}

	// Enter TUI mode
	mgr.EnterTUIMode()

	// First commit
	committed1 := mgr.CommitLiveViewport()
	historyLen1 := history.TotalLen()

	if committed1 == 0 {
		t.Fatal("First commit should have committed lines")
	}

	t.Logf("After first commit: %d lines committed, history has %d lines", committed1, historyLen1)

	// Clear viewport committed state so we can commit again
	// (simulates viewport being redrawn by TUI app)
	for y := 0; y < viewport.Height(); y++ {
		viewport.ClearRowCommitted(y)
	}

	// Second commit with same content - should REPLACE, not append
	committed2 := mgr.CommitLiveViewport()
	historyLen2 := history.TotalLen()

	t.Logf("After second commit: %d lines committed, history has %d lines", committed2, historyLen2)

	// History length should be the same (content replaced, not appended)
	if historyLen2 != historyLen1 {
		t.Errorf("History grew from %d to %d - duplicates were appended instead of replaced!",
			historyLen1, historyLen2)
	}

	// Third commit to triple-check
	for y := 0; y < viewport.Height(); y++ {
		viewport.ClearRowCommitted(y)
	}
	mgr.CommitLiveViewport()
	historyLen3 := history.TotalLen()

	if historyLen3 != historyLen1 {
		t.Errorf("History grew from %d to %d after third commit - duplicates detected!",
			historyLen1, historyLen3)
	}

	// 10 commits should still result in same history size
	for i := 0; i < 10; i++ {
		for y := 0; y < viewport.Height(); y++ {
			viewport.ClearRowCommitted(y)
		}
		mgr.CommitLiveViewport()
	}
	historyLen10 := history.TotalLen()

	if historyLen10 != historyLen1 {
		t.Errorf("After 10 commits, history grew from %d to %d - duplicates detected!",
			historyLen1, historyLen10)
	}
}

func TestTUIViewportManager_ContentUpdatesOnCommit(t *testing.T) {
	// Verify that when viewport content CHANGES, the new content replaces the old
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(20, 3, history)
	mgr := NewTUIViewportManager(viewport, history)

	// Enter TUI mode
	mgr.EnterTUIMode()

	// Write "Version 1" and commit
	viewport.SetCursor(0, 0)
	for _, r := range "Version 1" {
		viewport.Write(r, DefaultFG, DefaultBG, 0, false)
	}
	mgr.CommitLiveViewport()

	historyLen1 := history.TotalLen()
	t.Logf("After Version 1: history has %d lines", historyLen1)

	// Clear and write "Version 2"
	for y := 0; y < viewport.Height(); y++ {
		viewport.ClearRowCommitted(y)
		for x := 0; x < viewport.Width(); x++ {
			viewport.Grid()[y][x] = Cell{Rune: ' '}
		}
	}
	viewport.SetCursor(0, 0)
	for _, r := range "Version 2" {
		viewport.Write(r, DefaultFG, DefaultBG, 0, false)
	}
	mgr.CommitLiveViewport()

	historyLen2 := history.TotalLen()
	t.Logf("After Version 2: history has %d lines", historyLen2)

	// History should still have same number of lines (replaced)
	if historyLen2 != historyLen1 {
		t.Errorf("History grew from %d to %d - content should have been replaced!",
			historyLen1, historyLen2)
	}

	// Verify content is now "Version 2"
	line := history.GetGlobal(0)
	content := ""
	for _, c := range line.Cells {
		if c.Rune != 0 && c.Rune != ' ' {
			content += string(c.Rune)
		}
	}
	if content != "Version2" {
		t.Errorf("Expected 'Version2', got '%s' - content was not replaced!", content)
	}
}

func TestTUIViewportManager_ExitPreservesContent(t *testing.T) {
	// Verify that ExitTUIMode preserves committed content
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	viewport := NewViewportState(20, 3, history)
	mgr := NewTUIViewportManager(viewport, history)

	// Enter TUI mode and commit content
	mgr.EnterTUIMode()

	viewport.SetCursor(0, 0)
	for _, r := range "Preserved" {
		viewport.Write(r, DefaultFG, DefaultBG, 0, false)
	}
	mgr.CommitLiveViewport()

	historyLenBefore := history.TotalLen()
	if historyLenBefore == 0 {
		t.Fatal("Expected content in history after commit")
	}

	// Exit TUI mode - content should be preserved
	mgr.ExitTUIMode()

	historyLenAfter := history.TotalLen()
	if historyLenAfter != historyLenBefore {
		t.Errorf("ExitTUIMode changed history length from %d to %d - content was not preserved!",
			historyLenBefore, historyLenAfter)
	}

	// Verify content is still there
	line := history.GetGlobal(0)
	content := ""
	for _, c := range line.Cells {
		if c.Rune != 0 && c.Rune != ' ' {
			content += string(c.Rune)
		}
	}
	if content != "Preserved" {
		t.Errorf("Expected 'Preserved', got '%s'", content)
	}
}
