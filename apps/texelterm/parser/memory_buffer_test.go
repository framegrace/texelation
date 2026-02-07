// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/memory_buffer_test.go
// Summary: Tests for MemoryBuffer and DirtyTracker.

package parser

import (
	"sync"
	"testing"
)

// --- DirtyTracker Tests ---

func TestDirtyTracker_MarkAndClear(t *testing.T) {
	dt := NewDirtyTracker()

	// Initially empty
	if dt.DirtyCount() != 0 {
		t.Errorf("expected 0 dirty, got %d", dt.DirtyCount())
	}

	// Mark some lines
	dt.MarkDirty(5)
	dt.MarkDirty(10)
	dt.MarkDirty(15)

	if dt.DirtyCount() != 3 {
		t.Errorf("expected 3 dirty, got %d", dt.DirtyCount())
	}

	if !dt.IsDirty(5) || !dt.IsDirty(10) || !dt.IsDirty(15) {
		t.Error("expected lines 5, 10, 15 to be dirty")
	}

	if dt.IsDirty(7) {
		t.Error("line 7 should not be dirty")
	}

	// Clear one
	dt.ClearDirty(10)
	if dt.IsDirty(10) {
		t.Error("line 10 should not be dirty after clear")
	}
	if dt.DirtyCount() != 2 {
		t.Errorf("expected 2 dirty, got %d", dt.DirtyCount())
	}
}

func TestDirtyTracker_GetDirty(t *testing.T) {
	dt := NewDirtyTracker()

	dt.MarkDirty(100)
	dt.MarkDirty(50)
	dt.MarkDirty(75)

	dirty := dt.GetDirty()
	if len(dirty) != 3 {
		t.Fatalf("expected 3 dirty lines, got %d", len(dirty))
	}

	// Should be sorted
	if dirty[0] != 50 || dirty[1] != 75 || dirty[2] != 100 {
		t.Errorf("expected sorted [50, 75, 100], got %v", dirty)
	}
}

func TestDirtyTracker_ClearAll(t *testing.T) {
	dt := NewDirtyTracker()

	dt.MarkDirty(1)
	dt.MarkDirty(2)
	dt.MarkDirty(3)

	dt.ClearAll()

	if dt.DirtyCount() != 0 {
		t.Errorf("expected 0 dirty after ClearAll, got %d", dt.DirtyCount())
	}
}

func TestDirtyTracker_RemoveBelow(t *testing.T) {
	dt := NewDirtyTracker()

	dt.MarkDirty(5)
	dt.MarkDirty(10)
	dt.MarkDirty(15)
	dt.MarkDirty(20)

	dt.RemoveBelow(12)

	if dt.IsDirty(5) || dt.IsDirty(10) {
		t.Error("lines below 12 should be removed")
	}
	if !dt.IsDirty(15) || !dt.IsDirty(20) {
		t.Error("lines >= 12 should remain")
	}
	if dt.DirtyCount() != 2 {
		t.Errorf("expected 2 dirty, got %d", dt.DirtyCount())
	}
}

// --- MemoryBuffer Tests ---

func TestMemoryBuffer_DefaultConfig(t *testing.T) {
	config := DefaultMemoryBufferConfig()

	if config.MaxLines != 50000 {
		t.Errorf("expected MaxLines 50000, got %d", config.MaxLines)
	}
	if config.EvictionBatch != 1000 {
		t.Errorf("expected EvictionBatch 1000, got %d", config.EvictionBatch)
	}
}

func TestMemoryBuffer_WriteAndRead(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Write some characters
	mb.Write('H', DefaultFG, DefaultBG, 0)
	mb.Write('e', DefaultFG, DefaultBG, 0)
	mb.Write('l', DefaultFG, DefaultBG, 0)
	mb.Write('l', DefaultFG, DefaultBG, 0)
	mb.Write('o', DefaultFG, DefaultBG, 0)

	// Read back
	line := mb.GetLine(0)
	if line == nil {
		t.Fatal("expected line 0 to exist")
	}
	if len(line.Cells) != 5 {
		t.Errorf("expected 5 cells, got %d", len(line.Cells))
	}

	expected := "Hello"
	for i, r := range expected {
		if line.Cells[i].Rune != r {
			t.Errorf("cell %d: expected '%c', got '%c'", i, r, line.Cells[i].Rune)
		}
	}
}

func TestMemoryBuffer_WriteWide(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	// Write a wide character
	ok := mb.WriteWide('😀', DefaultFG, DefaultBG, 0, true)
	if !ok {
		t.Error("WriteWide should succeed")
	}

	// Cursor should advance by 2
	if mb.CursorCol() != 2 {
		t.Errorf("expected cursor col 2, got %d", mb.CursorCol())
	}

	// Read back
	line := mb.GetLine(0)
	if line == nil {
		t.Fatal("expected line 0 to exist")
	}
	if len(line.Cells) != 2 {
		t.Errorf("expected 2 cells, got %d", len(line.Cells))
	}
	if !line.Cells[0].Wide {
		t.Error("first cell should be wide")
	}
	if line.Cells[1].Rune != 0 {
		t.Error("second cell should be placeholder (rune 0)")
	}
}

func TestMemoryBuffer_WriteWideAtEdge(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	// Position cursor at column 79 (last column)
	mb.SetCursor(0, 79)

	// Try to write wide char - should fail (no room)
	ok := mb.WriteWide('😀', DefaultFG, DefaultBG, 0, true)
	if ok {
		t.Error("WriteWide should fail at edge")
	}

	// Cursor should not have moved
	if mb.CursorCol() != 79 {
		t.Errorf("cursor should still be at 79, got %d", mb.CursorCol())
	}
}

func TestMemoryBuffer_CursorMovement(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Initial position
	line, col := mb.GetCursor()
	if line != 0 || col != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", line, col)
	}

	// Set cursor
	mb.SetCursor(5, 10)
	line, col = mb.GetCursor()
	if line != 5 || col != 10 {
		t.Errorf("expected (5,10), got (%d,%d)", line, col)
	}

	// NewLine
	mb.NewLine()
	if mb.CursorLine() != 6 {
		t.Errorf("expected line 6, got %d", mb.CursorLine())
	}
	if mb.CursorCol() != 10 {
		t.Errorf("NewLine should not change column, got %d", mb.CursorCol())
	}

	// CarriageReturn
	mb.CarriageReturn()
	if mb.CursorCol() != 0 {
		t.Errorf("expected col 0, got %d", mb.CursorCol())
	}
}

func TestMemoryBuffer_Eviction(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 5})

	// Fill buffer beyond capacity
	for i := 0; i < 15; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('A'+i), DefaultFG, DefaultBG, 0)
	}

	// Should have evicted to make room
	if mb.GlobalOffset() == 0 {
		t.Error("expected eviction to have occurred")
	}

	// Old lines should be gone
	if mb.GetLine(0) != nil {
		t.Error("line 0 should have been evicted")
	}

	// Recent lines should exist
	line := mb.GetLine(mb.GlobalOffset())
	if line == nil {
		t.Error("line at globalOffset should exist")
	}
}

func TestMemoryBuffer_DirtyTracking(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Write creates dirty entry
	mb.Write('X', DefaultFG, DefaultBG, 0)

	if !mb.IsDirty(0) {
		t.Error("line 0 should be dirty after write")
	}

	dirty := mb.GetDirtyLines()
	if len(dirty) != 1 || dirty[0] != 0 {
		t.Errorf("expected [0], got %v", dirty)
	}

	// Clear dirty
	mb.ClearDirty(0)
	if mb.IsDirty(0) {
		t.Error("line 0 should not be dirty after clear")
	}

	// Manual mark
	mb.MarkDirty(5)
	if !mb.IsDirty(5) {
		t.Error("line 5 should be dirty after MarkDirty")
	}

	// Clear all
	mb.ClearAllDirty()
	if len(mb.GetDirtyLines()) != 0 {
		t.Error("expected no dirty lines after ClearAllDirty")
	}
}

func TestMemoryBuffer_FixedWidthFlags(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Ensure line exists
	mb.EnsureLine(5)

	// Initially not fixed
	if mb.IsLineFixed(5) {
		t.Error("line should not be fixed initially")
	}

	// Set fixed
	mb.SetLineFixed(5, 80)
	if !mb.IsLineFixed(5) {
		t.Error("line should be fixed after SetLineFixed")
	}

	// Verify dirty was marked
	if !mb.IsDirty(5) {
		t.Error("line should be dirty after SetLineFixed")
	}

	// Invalid width should be rejected
	mb.ClearAllDirty()
	mb.SetLineFixed(6, 0)
	if mb.IsLineFixed(6) {
		t.Error("invalid width 0 should be rejected")
	}

	mb.SetLineFixed(7, -5)
	if mb.IsLineFixed(7) {
		t.Error("negative width should be rejected")
	}

	mb.SetLineFixed(8, 15000)
	if mb.IsLineFixed(8) {
		t.Error("width > 10000 should be rejected")
	}
}

func TestMemoryBuffer_GlobalIndexing(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 3})

	// Create 10 lines
	for i := 0; i < 10; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('0'+i), DefaultFG, DefaultBG, 0)
	}

	initialOffset := mb.GlobalOffset()
	initialEnd := mb.GlobalEnd()

	// Force eviction by adding more
	for i := 10; i < 15; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('A'+i-10), DefaultFG, DefaultBG, 0)
	}

	// GlobalOffset should have increased
	if mb.GlobalOffset() <= initialOffset {
		t.Error("GlobalOffset should have increased after eviction")
	}

	// GlobalEnd should have increased
	if mb.GlobalEnd() <= initialEnd {
		t.Error("GlobalEnd should have increased")
	}

	// TotalLines should be <= MaxLines
	if mb.TotalLines() > int64(mb.config.MaxLines) {
		t.Errorf("TotalLines %d exceeds MaxLines %d", mb.TotalLines(), mb.config.MaxLines)
	}

	// Lines before GlobalOffset should be nil
	if mb.GetLine(mb.GlobalOffset()-1) != nil {
		t.Error("line before GlobalOffset should return nil")
	}

	// Lines at GlobalOffset should exist
	if mb.GetLine(mb.GlobalOffset()) == nil {
		t.Error("line at GlobalOffset should exist")
	}
}

func TestMemoryBuffer_LineOperations(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Create some lines with content
	for i := 0; i < 5; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('A'+i), DefaultFG, DefaultBG, 0)
	}

	// Verify content
	for i := 0; i < 5; i++ {
		line := mb.GetLine(int64(i))
		if line == nil || len(line.Cells) == 0 || line.Cells[0].Rune != rune('A'+i) {
			t.Errorf("line %d has wrong content", i)
		}
	}

	// Insert line at position 2
	mb.InsertLine(2)

	// Line 2 should now be empty
	line2 := mb.GetLine(2)
	if line2 == nil {
		t.Fatal("line 2 should exist")
	}
	if len(line2.Cells) != 0 {
		t.Errorf("inserted line should be empty, got %d cells", len(line2.Cells))
	}

	// Original line 2 ('C') should now be at line 3
	line3 := mb.GetLine(3)
	if line3 == nil || len(line3.Cells) == 0 || line3.Cells[0].Rune != 'C' {
		t.Error("line 3 should have content 'C'")
	}

	// Delete line 2
	mb.DeleteLine(2)

	// Line 2 should now have 'C' again
	line2 = mb.GetLine(2)
	if line2 == nil || len(line2.Cells) == 0 || line2.Cells[0].Rune != 'C' {
		t.Error("after delete, line 2 should have 'C'")
	}
}

func TestMemoryBuffer_EraseOperations(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Write a line
	mb.SetCursor(0, 0)
	for _, r := range "Hello World" {
		mb.Write(r, DefaultFG, DefaultBG, 0)
	}

	// Erase to end of line from col 6
	mb.ClearAllDirty()
	mb.EraseToEndOfLine(0, 6, DefaultFG, DefaultBG)

	line := mb.GetLine(0)
	if line == nil {
		t.Fatal("line should exist")
	}
	if len(line.Cells) != 6 {
		t.Errorf("expected 6 cells after erase, got %d", len(line.Cells))
	}
	if !mb.IsDirty(0) {
		t.Error("line should be dirty after erase")
	}

	// Erase from start of line to col 2
	mb.ClearAllDirty()
	mb.EraseFromStartOfLine(0, 2, DefaultFG, DefaultBG)

	line = mb.GetLine(0)
	// First 3 cells should be spaces
	for i := 0; i <= 2; i++ {
		if line.Cells[i].Rune != ' ' {
			t.Errorf("cell %d should be space, got %c", i, line.Cells[i].Rune)
		}
	}

	// Create another line and erase it completely
	mb.SetCursor(1, 0)
	mb.Write('X', DefaultFG, DefaultBG, 0)
	mb.Write('Y', DefaultFG, DefaultBG, 0)

	mb.EraseLine(1, DefaultFG, DefaultBG)
	line = mb.GetLine(1)
	if line == nil {
		t.Fatal("line 1 should exist")
	}
	if len(line.Cells) != 0 {
		t.Errorf("erased line should be empty, got %d cells", len(line.Cells))
	}
}

func TestMemoryBuffer_GetLineRange(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Create 10 lines
	for i := 0; i < 10; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('0'+i), DefaultFG, DefaultBG, 0)
	}

	// Get range [3, 7)
	lines := mb.GetLineRange(3, 7)
	if len(lines) != 4 {
		t.Errorf("expected 4 lines, got %d", len(lines))
	}

	for i, line := range lines {
		expected := rune('3' + i)
		if line == nil || len(line.Cells) == 0 || line.Cells[0].Rune != expected {
			t.Errorf("line %d has wrong content", i)
		}
	}

	// Out of bounds should return partial
	lines = mb.GetLineRange(8, 15)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines (8,9), got %d", len(lines))
	}

	// Completely out of bounds
	lines = mb.GetLineRange(100, 110)
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestMemoryBuffer_ContentVersion(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	v0 := mb.ContentVersion()

	mb.Write('A', DefaultFG, DefaultBG, 0)
	v1 := mb.ContentVersion()

	if v1 <= v0 {
		t.Error("content version should increase after write")
	}

	mb.SetLineFixed(0, 80)
	v2 := mb.ContentVersion()

	if v2 <= v1 {
		t.Error("content version should increase after SetLineFixed")
	}

	mb.EraseLine(0, DefaultFG, DefaultBG)
	v3 := mb.ContentVersion()

	if v3 <= v2 {
		t.Error("content version should increase after erase")
	}
}

func TestMemoryBuffer_RingBufferWrap(t *testing.T) {
	// Small buffer to test ring wrapping
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 5, EvictionBatch: 2})

	// Fill exactly to capacity
	for i := 0; i < 5; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('A'+i), DefaultFG, DefaultBG, 0)
	}

	// All 5 should exist
	for i := 0; i < 5; i++ {
		line := mb.GetLine(int64(i))
		if line == nil {
			t.Errorf("line %d should exist", i)
		}
	}

	// Add more to trigger eviction and wrap
	for i := 5; i < 10; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('0'+i-5), DefaultFG, DefaultBG, 0)
	}

	// Old lines should be gone
	for i := 0; i < int(mb.GlobalOffset()); i++ {
		if mb.GetLine(int64(i)) != nil {
			t.Errorf("line %d should have been evicted", i)
		}
	}

	// New lines should exist
	for i := mb.GlobalOffset(); i < mb.GlobalEnd(); i++ {
		if mb.GetLine(i) == nil {
			t.Errorf("line %d should exist", i)
		}
	}
}

func TestMemoryBuffer_Concurrency(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 1000, EvictionBatch: 100})

	var wg sync.WaitGroup
	numWriters := 10
	writesPerWriter := 100

	// Concurrent writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				line := int64(writerID*writesPerWriter + i)
				mb.SetCursor(line, 0)
				mb.Write(rune('A'+writerID), DefaultFG, DefaultBG, 0)
			}
		}(w)
	}

	// Concurrent readers
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = mb.GetLine(int64(i))
				_ = mb.GetDirtyLines()
				_ = mb.ContentVersion()
			}
		}()
	}

	wg.Wait()

	// If we get here without deadlock or panic, concurrency is OK
	// Verify some state
	if mb.TotalLines() == 0 {
		t.Error("expected some lines to be stored")
	}
}

func TestMemoryBuffer_EnsureLine_GapFill(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Ensure line 5 (skipping 0-4)
	line := mb.EnsureLine(5)
	if line == nil {
		t.Fatal("EnsureLine should return a line")
	}

	// Lines 0-4 should also exist now (gap fill)
	for i := int64(0); i <= 5; i++ {
		if mb.GetLine(i) == nil {
			t.Errorf("line %d should exist after gap fill", i)
		}
	}
}

func TestMemoryBuffer_SetCell(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	cell := Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG, Attr: AttrBold}
	mb.SetCell(3, 5, cell)

	// Line 3 should exist with cell at column 5
	line := mb.GetLine(3)
	if line == nil {
		t.Fatal("line 3 should exist")
	}
	if len(line.Cells) < 6 {
		t.Fatalf("line should have at least 6 cells, got %d", len(line.Cells))
	}
	if line.Cells[5].Rune != 'X' || line.Cells[5].Attr != AttrBold {
		t.Error("cell not set correctly")
	}
}

func TestMemoryBuffer_EvictWithDirty(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 3})

	// Create lines and mark dirty
	for i := 0; i < 10; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('A'+i), DefaultFG, DefaultBG, 0)
	}

	// All should be dirty
	dirty := mb.GetDirtyLines()
	if len(dirty) != 10 {
		t.Errorf("expected 10 dirty lines, got %d", len(dirty))
	}

	// Force eviction
	for i := 10; i < 15; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write(rune('0'+i-10), DefaultFG, DefaultBG, 0)
	}

	// Evicted lines should no longer be in dirty list
	dirty = mb.GetDirtyLines()
	for _, idx := range dirty {
		if idx < mb.GlobalOffset() {
			t.Errorf("dirty line %d is below GlobalOffset %d", idx, mb.GlobalOffset())
		}
	}
}

func TestMemoryBuffer_TermWidth(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	// Default width
	if mb.TermWidth() != DefaultWidth {
		t.Errorf("expected default width %d, got %d", DefaultWidth, mb.TermWidth())
	}

	// Set width
	mb.SetTermWidth(120)
	if mb.TermWidth() != 120 {
		t.Errorf("expected width 120, got %d", mb.TermWidth())
	}

	// Invalid width should use default
	mb.SetTermWidth(0)
	if mb.TermWidth() != DefaultWidth {
		t.Errorf("expected default width for 0, got %d", mb.TermWidth())
	}

	mb.SetTermWidth(-10)
	if mb.TermWidth() != DefaultWidth {
		t.Errorf("expected default width for negative, got %d", mb.TermWidth())
	}
}
