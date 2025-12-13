package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScrollbackHistory_NewAndLen(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	if h.Len() != 0 {
		t.Errorf("expected empty history, got len %d", h.Len())
	}
	if h.MaxMemoryLines() != 1000 {
		t.Errorf("expected max 1000, got %d", h.MaxMemoryLines())
	}
}

func TestScrollbackHistory_Append(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})

	h.Append(NewLogicalLineFromCells(makeCells("Line 1")))
	h.Append(NewLogicalLineFromCells(makeCells("Line 2")))

	if h.Len() != 2 {
		t.Errorf("expected 2 lines, got %d", h.Len())
	}

	if !h.IsDirty() {
		t.Error("history should be dirty after append")
	}
}

func TestScrollbackHistory_Get(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	h.Append(NewLogicalLineFromCells(makeCells("Line 0")))
	h.Append(NewLogicalLineFromCells(makeCells("Line 1")))
	h.Append(NewLogicalLineFromCells(makeCells("Line 2")))

	line := h.Get(1)
	if line == nil {
		t.Fatal("expected line at index 1")
	}
	if cellsToString(line.Cells) != "Line 1" {
		t.Errorf("expected 'Line 1', got '%s'", cellsToString(line.Cells))
	}

	// Out of bounds
	if h.Get(-1) != nil {
		t.Error("Get(-1) should return nil")
	}
	if h.Get(100) != nil {
		t.Error("Get(100) should return nil")
	}
}

func TestScrollbackHistory_MaxLines(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 5})

	// Add 7 lines
	for i := 0; i < 7; i++ {
		h.AppendCells(makeCells("Line"))
	}

	// Should only have 5
	if h.Len() != 5 {
		t.Errorf("expected 5 lines (max), got %d", h.Len())
	}
}

func TestScrollbackHistory_Clear(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	h.Append(NewLogicalLineFromCells(makeCells("Line")))
	h.Append(NewLogicalLineFromCells(makeCells("Line")))

	h.Clear()

	if h.Len() != 0 {
		t.Errorf("expected 0 after clear, got %d", h.Len())
	}
	if !h.IsDirty() {
		t.Error("should be dirty after clear")
	}
}

func TestScrollbackHistory_GetRange(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	for i := 0; i < 5; i++ {
		h.AppendCells(makeCells("Line"))
	}

	// Normal range
	lines := h.GetRange(1, 3)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	// Clamped range
	lines = h.GetRange(-5, 100)
	if len(lines) != 5 {
		t.Errorf("expected 5 lines (clamped), got %d", len(lines))
	}

	// Empty range
	lines = h.GetRange(3, 2)
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for reversed range, got %d", len(lines))
	}
}

func TestScrollbackHistory_LastN(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	for i := 0; i < 10; i++ {
		h.AppendCells(makeCells("Line"))
	}

	lines := h.LastN(3)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}

	// More than available
	lines = h.LastN(100)
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}

	// Zero or negative
	if h.LastN(0) != nil {
		t.Error("LastN(0) should return nil")
	}
}

func TestScrollbackHistory_WrapToWidth(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	h.AppendCells(makeCells("ABCDEFGHIJ")) // 10 chars -> 2 lines at width 5
	h.AppendCells(makeCells("XY"))         // 2 chars -> 1 line at width 5

	physical := h.WrapToWidth(0, 2, 5)

	if len(physical) != 3 {
		t.Errorf("expected 3 physical lines, got %d", len(physical))
	}

	// Verify logical indices
	if physical[0].LogicalIndex != 0 || physical[1].LogicalIndex != 0 {
		t.Error("first two physical lines should point to logical index 0")
	}
	if physical[2].LogicalIndex != 1 {
		t.Error("third physical line should point to logical index 1")
	}
}

func TestScrollbackHistory_PhysicalLineCount(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	h.AppendCells(makeCells("ABCDEFGHIJ")) // 10 chars -> 2 lines at width 5
	h.AppendCells(makeCells("XY"))         // 2 chars -> 1 line at width 5
	h.AppendCells(makeCells(""))           // empty -> 1 line

	count := h.PhysicalLineCount(5)
	if count != 4 { // 2 + 1 + 1
		t.Errorf("expected 4 physical lines, got %d", count)
	}
}

func TestScrollbackHistory_FindLogicalIndexForPhysicalLine(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	h.AppendCells(makeCells("ABCDEFGHIJ")) // logical 0 -> physical 0,1 (at width 5)
	h.AppendCells(makeCells("XY"))         // logical 1 -> physical 2
	h.AppendCells(makeCells("12345"))      // logical 2 -> physical 3

	tests := []struct {
		physical    int
		expectedLog int
		expectedOff int
	}{
		{0, 0, 0},
		{1, 0, 5}, // Second row of first logical line, offset 5
		{2, 1, 0},
		{3, 2, 0},
		{4, -1, 0},  // Past end
		{-1, -1, 0}, // Negative
	}

	for _, tc := range tests {
		logIdx, offset := h.FindLogicalIndexForPhysicalLine(tc.physical, 5)
		if logIdx != tc.expectedLog || offset != tc.expectedOff {
			t.Errorf("physical %d: expected (%d, %d), got (%d, %d)",
				tc.physical, tc.expectedLog, tc.expectedOff, logIdx, offset)
		}
	}
}

func TestScrollbackHistory_DirtyFlag(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})

	if h.IsDirty() {
		t.Error("new history should not be dirty")
	}

	h.Append(NewLogicalLine())
	if !h.IsDirty() {
		t.Error("should be dirty after append")
	}

	h.MarkClean()
	if h.IsDirty() {
		t.Error("should not be dirty after MarkClean")
	}
}

// Test disk-backed history
func TestScrollbackHistory_WithDisk(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "history.hist")

	// Create history with disk
	h, err := NewScrollbackHistoryWithDisk(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
		DiskPath:       path,
	})
	if err != nil {
		t.Fatalf("Failed to create history: %v", err)
	}

	// Add some lines
	for i := 0; i < 50; i++ {
		h.AppendCells(makeCells("Line"))
	}

	if h.Len() != 50 {
		t.Errorf("expected 50 lines, got %d", h.Len())
	}
	if h.TotalLen() != 50 {
		t.Errorf("expected total 50, got %d", h.TotalLen())
	}
	if !h.HasDiskBacking() {
		t.Error("should have disk backing")
	}

	// Close
	if err := h.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("History file not created: %v", err)
	}

	// Reopen
	h2, err := NewScrollbackHistoryWithDisk(ScrollbackHistoryConfig{
		MaxMemoryLines: 100,
		DiskPath:       path,
	})
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer h2.Close()

	if h2.TotalLen() != 50 {
		t.Errorf("expected 50 lines after reopen, got %d", h2.TotalLen())
	}
}

func TestScrollbackHistory_DiskTrimsMemory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "history.hist")

	h, err := NewScrollbackHistoryWithDisk(ScrollbackHistoryConfig{
		MaxMemoryLines: 10,
		DiskPath:       path,
	})
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}
	defer h.Close()

	// Add 50 lines
	for i := 0; i < 50; i++ {
		h.AppendCells(makeCells("Line"))
	}

	// Memory should be trimmed to 10
	if h.Len() != 10 {
		t.Errorf("expected 10 in memory, got %d", h.Len())
	}

	// Total should be 50
	if h.TotalLen() != 50 {
		t.Errorf("expected 50 total, got %d", h.TotalLen())
	}

	// Window should start at 40
	if h.WindowStart() != 40 {
		t.Errorf("expected window start 40, got %d", h.WindowStart())
	}
}

func TestScrollbackHistory_LoadAbove(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "history.hist")

	h, err := NewScrollbackHistoryWithDisk(ScrollbackHistoryConfig{
		MaxMemoryLines: 10,
		DiskPath:       path,
	})
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}
	defer h.Close()

	// Add 50 lines
	for i := 0; i < 50; i++ {
		h.AppendCells(makeCells("Line"))
	}

	// Memory has lines 40-49, disk has 0-49

	if !h.CanLoadAbove() {
		t.Error("should be able to load above")
	}

	// Load 5 lines above
	loaded := h.LoadAbove(5)
	if loaded != 5 {
		t.Errorf("expected to load 5, loaded %d", loaded)
	}

	// Now memory should have lines 35-49 (15 lines, but trimmed to 10)
	// After loading 5 above, and trimming below...
	if h.Len() != 10 {
		t.Errorf("expected 10 in memory after trim, got %d", h.Len())
	}

	if h.WindowStart() != 35 {
		t.Errorf("expected window start 35, got %d", h.WindowStart())
	}
}

func TestScrollbackHistory_GetGlobal(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "history.hist")

	h, err := NewScrollbackHistoryWithDisk(ScrollbackHistoryConfig{
		MaxMemoryLines: 10,
		DiskPath:       path,
	})
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}
	defer h.Close()

	// Add lines with identifiable content
	for i := 0; i < 30; i++ {
		cells := []Cell{{Rune: rune('A' + (i % 26))}}
		h.Append(NewLogicalLineFromCells(cells))
	}

	// Memory has 20-29, disk has 0-29

	// Get from memory (line 25)
	line := h.GetGlobal(25)
	if line == nil {
		t.Fatal("expected line 25")
	}
	expectedRune := rune('A' + (25 % 26))
	if line.Cells[0].Rune != expectedRune {
		t.Errorf("line 25: expected %c, got %c", expectedRune, line.Cells[0].Rune)
	}

	// Get from disk (line 5)
	line = h.GetGlobal(5)
	if line == nil {
		t.Fatal("expected line 5 from disk")
	}
	expectedRune = rune('A' + (5 % 26))
	if line.Cells[0].Rune != expectedRune {
		t.Errorf("line 5: expected %c, got %c", expectedRune, line.Cells[0].Rune)
	}
}
