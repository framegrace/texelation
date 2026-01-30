// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_content_reader_test.go
// Summary: Tests for MemoryBufferReader with PageStore fallback.

package parser

import (
	"fmt"
	"testing"
)

// --- Test Helpers ---

// newTestPageStoreWithContent creates a temporary PageStore with test content.
func newTestPageStoreWithContent(t *testing.T, lines []string) *PageStore {
	t.Helper()
	tmpDir := t.TempDir()

	config := PageStoreConfig{
		BaseDir:        tmpDir,
		TerminalID:     "test-terminal",
		TargetPageSize: 64 * 1024,
	}

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("failed to create PageStore: %v", err)
	}

	for _, line := range lines {
		ll := NewLogicalLineFromCells(vwMakeCells(line))
		if err := ps.AppendLine(ll); err != nil {
			t.Fatalf("failed to append line: %v", err)
		}
	}

	return ps
}

// --- Backward Compatibility Tests ---

func TestMemoryBufferReader_WithoutPageStore(t *testing.T) {
	// Ensure existing behavior is unchanged when no PageStore is provided
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	// Write some content
	mb.Write('A', DefaultFG, DefaultBG, 0)
	mb.NewLine()
	mb.CarriageReturn()
	mb.Write('B', DefaultFG, DefaultBG, 0)

	reader := NewMemoryBufferReader(mb)

	// All standard operations should work
	if reader.GlobalOffset() != 0 {
		t.Errorf("expected GlobalOffset 0, got %d", reader.GlobalOffset())
	}
	if reader.GlobalEnd() != 2 {
		t.Errorf("expected GlobalEnd 2, got %d", reader.GlobalEnd())
	}
	if reader.TotalLines() != 2 {
		t.Errorf("expected TotalLines 2, got %d", reader.TotalLines())
	}

	line := reader.GetLine(0)
	if line == nil || len(line.Cells) != 1 || line.Cells[0].Rune != 'A' {
		t.Errorf("expected line 0 to be 'A', got %v", line)
	}

	// Out of bounds should return nil
	if reader.GetLine(100) != nil {
		t.Error("expected nil for out of bounds line")
	}
}

// --- PageStore Fallback Tests ---

func TestMemoryBufferReader_WithPageStore_LineInMemory(t *testing.T) {
	// When line is in memory, should return from MemoryBuffer (not PageStore)
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	// Write content to memory buffer
	mb.Write('M', DefaultFG, DefaultBG, 0) // 'M' for Memory
	mb.NewLine()
	mb.CarriageReturn()
	mb.Write('E', DefaultFG, DefaultBG, 0)
	mb.NewLine()
	mb.CarriageReturn()
	mb.Write('M', DefaultFG, DefaultBG, 0)

	// Create PageStore with different content (to verify we're reading from memory)
	ps := newTestPageStoreWithContent(t, []string{"P", "A", "G"}) // 'P' for PageStore
	defer ps.Close()

	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	// Should read from memory buffer, not PageStore
	line := reader.GetLine(0)
	if line == nil {
		t.Fatal("expected line 0 to exist")
	}
	if line.Cells[0].Rune != 'M' {
		t.Errorf("expected 'M' from memory, got '%c'", line.Cells[0].Rune)
	}
}

func TestMemoryBufferReader_WithPageStore_EvictedLine(t *testing.T) {
	// When line is evicted from memory, should fall back to PageStore
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 5})
	mb.SetTermWidth(80)

	// Create PageStore and write initial content
	ps := newTestPageStoreWithContent(t, []string{})
	defer ps.Close()

	// Write 15 lines - first 5 will be evicted (MaxLines=10, EvictionBatch=5)
	for i := 0; i < 15; i++ {
		r := rune('A' + i)
		mb.Write(r, DefaultFG, DefaultBG, 0)

		// Also write to PageStore (simulating persistence)
		ll := NewLogicalLineFromCells([]Cell{{Rune: r, FG: DefaultFG, BG: DefaultBG}})
		ps.AppendLine(ll)

		if i < 14 {
			mb.NewLine()
			mb.CarriageReturn()
		}
	}

	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	// Lines 0-4 should be evicted from memory
	if mb.GlobalOffset() != 5 {
		t.Fatalf("expected GlobalOffset 5 after eviction, got %d", mb.GlobalOffset())
	}

	// But reader should still be able to get line 0 via PageStore fallback
	line := reader.GetLine(0)
	if line == nil {
		t.Fatal("expected line 0 to be readable via PageStore fallback")
	}
	if line.Cells[0].Rune != 'A' {
		t.Errorf("expected 'A' from PageStore, got '%c'", line.Cells[0].Rune)
	}

	// Line 10 should be in memory
	line = reader.GetLine(10)
	if line == nil {
		t.Fatal("expected line 10 to exist in memory")
	}
	if line.Cells[0].Rune != 'K' {
		t.Errorf("expected 'K', got '%c'", line.Cells[0].Rune)
	}
}

func TestMemoryBufferReader_WithPageStore_GetLineRange_Split(t *testing.T) {
	// Test GetLineRange when range spans memory and disk
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 5})
	mb.SetTermWidth(80)

	ps := newTestPageStoreWithContent(t, []string{})
	defer ps.Close()

	// Write 15 lines
	for i := 0; i < 15; i++ {
		r := rune('A' + i)
		mb.Write(r, DefaultFG, DefaultBG, 0)

		ll := NewLogicalLineFromCells([]Cell{{Rune: r, FG: DefaultFG, BG: DefaultBG}})
		ps.AppendLine(ll)

		if i < 14 {
			mb.NewLine()
			mb.CarriageReturn()
		}
	}

	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	// Request range 3-8: lines 3,4 on disk (evicted), lines 5,6,7 in memory
	lines := reader.GetLineRange(3, 8)

	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}

	expected := []rune{'D', 'E', 'F', 'G', 'H'}
	for i, line := range lines {
		if line.Cells[0].Rune != expected[i] {
			t.Errorf("line %d: expected '%c', got '%c'", i, expected[i], line.Cells[0].Rune)
		}
	}
}

func TestMemoryBufferReader_WithPageStore_GetLineRange_AllOnDisk(t *testing.T) {
	// Test GetLineRange when entire range is on disk
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 5})
	mb.SetTermWidth(80)

	ps := newTestPageStoreWithContent(t, []string{})
	defer ps.Close()

	// Write 15 lines
	for i := 0; i < 15; i++ {
		r := rune('A' + i)
		mb.Write(r, DefaultFG, DefaultBG, 0)

		ll := NewLogicalLineFromCells([]Cell{{Rune: r, FG: DefaultFG, BG: DefaultBG}})
		ps.AppendLine(ll)

		if i < 14 {
			mb.NewLine()
			mb.CarriageReturn()
		}
	}

	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	// Request range 0-4: all on disk
	lines := reader.GetLineRange(0, 4)

	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	expected := []rune{'A', 'B', 'C', 'D'}
	for i, line := range lines {
		if line.Cells[0].Rune != expected[i] {
			t.Errorf("line %d: expected '%c', got '%c'", i, expected[i], line.Cells[0].Rune)
		}
	}
}

func TestMemoryBufferReader_WithPageStore_GlobalOffset(t *testing.T) {
	// GlobalOffset returns 0 when PageStore is available (can scroll to full history)
	// MemoryBufferOffset returns memory buffer's actual offset (for performance calculations)
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 5})
	mb.SetTermWidth(80)

	ps := newTestPageStoreWithContent(t, []string{"A", "B", "C"})
	defer ps.Close()

	// Write enough to trigger eviction
	for i := 0; i < 15; i++ {
		mb.Write(rune('A'+i), DefaultFG, DefaultBG, 0)
		if i < 14 {
			mb.NewLine()
			mb.CarriageReturn()
		}
	}

	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	// GlobalOffset should be 0 with PageStore (can scroll to beginning)
	if reader.GlobalOffset() != 0 {
		t.Errorf("expected GlobalOffset 0 with PageStore, got %d", reader.GlobalOffset())
	}

	// MemoryBufferOffset should match memory buffer's actual offset
	if reader.MemoryBufferOffset() != mb.GlobalOffset() {
		t.Errorf("expected MemoryBufferOffset %d, got %d", mb.GlobalOffset(), reader.MemoryBufferOffset())
	}

	// Can read lines before MemoryBufferOffset via PageStore fallback
	line := reader.GetLine(0)
	if line == nil {
		t.Error("expected to read evicted line 0 via PageStore fallback")
	}
}

func TestMemoryBufferReader_WithPageStore_TotalLines(t *testing.T) {
	// TotalLines returns memory buffer count (for efficient scroll calculations)
	// even when PageStore has more lines
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 5})
	mb.SetTermWidth(80)

	ps := newTestPageStoreWithContent(t, []string{})
	defer ps.Close()

	// Write 15 lines (5 will be evicted from memory)
	for i := 0; i < 15; i++ {
		mb.Write(rune('A'+i), DefaultFG, DefaultBG, 0)

		ll := NewLogicalLineFromCells([]Cell{{Rune: rune('A' + i), FG: DefaultFG, BG: DefaultBG}})
		ps.AppendLine(ll)

		if i < 14 {
			mb.NewLine()
			mb.CarriageReturn()
		}
	}

	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	// TotalLines should match memory buffer (10), not PageStore (15)
	// This keeps scroll calculations O(memory) instead of O(disk)
	if reader.TotalLines() != mb.TotalLines() {
		t.Errorf("expected TotalLines %d, got %d", mb.TotalLines(), reader.TotalLines())
	}

	// PageStore has 15 lines
	if ps.LineCount() != 15 {
		t.Errorf("expected PageStore LineCount 15, got %d", ps.LineCount())
	}
}

func TestMemoryBufferReader_WithPageStore_ContentVersion(t *testing.T) {
	// ContentVersion should still track memory buffer changes
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	ps := newTestPageStoreWithContent(t, []string{"A"})
	defer ps.Close()

	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	v1 := reader.ContentVersion()
	mb.Write('X', DefaultFG, DefaultBG, 0)
	v2 := reader.ContentVersion()

	if v2 <= v1 {
		t.Errorf("ContentVersion should increase on write, got v1=%d, v2=%d", v1, v2)
	}
}

// --- Edge Cases ---

func TestMemoryBufferReader_WithPageStore_EmptyPageStore(t *testing.T) {
	// Empty PageStore should not cause issues
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	mb.Write('A', DefaultFG, DefaultBG, 0)

	tmpDir := t.TempDir()
	config := PageStoreConfig{
		BaseDir:    tmpDir,
		TerminalID: "test",
	}
	ps, _ := CreatePageStore(config)
	defer ps.Close()

	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	// Should still work with empty PageStore
	line := reader.GetLine(0)
	if line == nil || line.Cells[0].Rune != 'A' {
		t.Error("expected to read from memory when PageStore is empty")
	}
}

func TestMemoryBufferReader_WithPageStore_NilPageStore(t *testing.T) {
	// Passing nil PageStore should behave like regular MemoryBufferReader
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	mb.Write('A', DefaultFG, DefaultBG, 0)

	reader := NewMemoryBufferReaderWithPageStore(mb, nil)

	line := reader.GetLine(0)
	if line == nil || line.Cells[0].Rune != 'A' {
		t.Error("expected to read from memory with nil PageStore")
	}

	// Should use memory buffer's GlobalOffset
	if reader.GlobalOffset() != mb.GlobalOffset() {
		t.Errorf("expected GlobalOffset %d, got %d", mb.GlobalOffset(), reader.GlobalOffset())
	}
}

// --- Integration Tests: ViewportWindow with PageStore ---

func TestViewportWindow_WithPageStore_ReadEvictedContent(t *testing.T) {
	// Integration test: MemoryBufferReader can read evicted content via PageStore,
	// even though normal scrolling is limited to in-memory range for performance.

	// Small buffer that will evict quickly
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 20, EvictionBatch: 10})
	mb.SetTermWidth(80)

	// Create PageStore
	ps := newTestPageStoreWithContent(t, []string{})
	defer ps.Close()

	// Write 30 lines (first 10 will be evicted)
	for i := 0; i < 30; i++ {
		lineText := fmt.Sprintf("Line %02d", i)
		for _, r := range lineText {
			mb.Write(r, DefaultFG, DefaultBG, 0)
		}

		// Also write to PageStore (simulating persistence)
		ll := NewLogicalLineFromCells(vwMakeCells(lineText))
		ps.AppendLine(ll)

		if i < 29 {
			mb.NewLine()
			mb.CarriageReturn()
		}
	}

	// Verify eviction occurred
	if mb.GlobalOffset() == 0 {
		t.Fatal("expected eviction to occur, but GlobalOffset is 0")
	}
	evictedCount := mb.GlobalOffset()
	t.Logf("Evicted %d lines, memory now holds lines %d-%d", evictedCount, mb.GlobalOffset(), mb.GlobalEnd()-1)

	// Create reader with PageStore
	reader := NewMemoryBufferReaderWithPageStore(mb, ps)

	// Direct read of evicted line should work via PageStore fallback
	line0 := reader.GetLine(0)
	if line0 == nil {
		t.Fatal("expected to read evicted line 0 via PageStore")
	}
	line0Text := vwCellsToString(line0.Cells)
	if line0Text != "Line 00" {
		t.Errorf("expected line 0 to be 'Line 00', got '%s'", line0Text)
	}

	// Range read spanning evicted and in-memory should work
	lines := reader.GetLineRange(5, 15) // 5-9 evicted, 10-14 in memory
	if len(lines) != 10 {
		t.Fatalf("expected 10 lines, got %d", len(lines))
	}

	// Verify content
	for i, line := range lines {
		expected := fmt.Sprintf("Line %02d", i+5)
		got := vwCellsToString(line.Cells)
		if got != expected {
			t.Errorf("line %d: expected '%s', got '%s'", i+5, expected, got)
		}
	}
}

func TestViewportWindow_WithPageStore_FullHistoryScrolling(t *testing.T) {
	// With PageStore, scrolling can access full history (including evicted content).
	// ScrollToTop goes to the oldest line on disk, not just in memory.

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 20, EvictionBatch: 10})
	mb.SetTermWidth(80)

	ps := newTestPageStoreWithContent(t, []string{})
	defer ps.Close()

	viewport := NewViewportWindow(mb, 80, 10)

	// Write 25 lines (first 5 will be evicted with MaxLines=20, EvictionBatch=10)
	for i := 0; i < 25; i++ {
		lineText := fmt.Sprintf("L%02d", i)
		for _, r := range lineText {
			mb.Write(r, DefaultFG, DefaultBG, 0)
		}

		ll := NewLogicalLineFromCells(vwMakeCells(lineText))
		ps.AppendLine(ll)

		if i < 24 {
			mb.NewLine()
			mb.CarriageReturn()
		}
	}

	viewport.SetPageStore(ps)

	// Verify eviction occurred
	if mb.GlobalOffset() == 0 {
		t.Fatal("expected eviction to occur")
	}
	t.Logf("Memory buffer offset: %d (lines 0-%d evicted)", mb.GlobalOffset(), mb.GlobalOffset()-1)

	// ScrollToTop should now go to line 0 (on disk via PageStore)
	viewport.ScrollToTop()
	grid := viewport.GetVisibleGrid()

	row0 := vwGridRowToString(grid[0])
	t.Logf("After ScrollToTop, row 0: '%s'", row0)

	// Should show L00 - the oldest line (from disk)
	if row0 != "L00" {
		t.Errorf("ScrollToTop should show oldest line 'L00', got '%s'", row0)
	}

	// ScrollToBottom should show the most recent lines
	viewport.ScrollToBottom()
	grid = viewport.GetVisibleGrid()

	// Last row with content should be L24
	lastRowIdx := len(grid) - 1
	for lastRowIdx >= 0 && vwGridRowToString(grid[lastRowIdx]) == "" {
		lastRowIdx--
	}
	if lastRowIdx >= 0 {
		lastRow := vwGridRowToString(grid[lastRowIdx])
		t.Logf("After ScrollToBottom, last row: '%s'", lastRow)
		if lastRow != "L24" {
			t.Errorf("expected last row 'L24', got '%s'", lastRow)
		}
	}
}
