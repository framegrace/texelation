package parser

import (
	"testing"
)

func TestScrollbackHistory_NewAndLen(t *testing.T) {
	h := NewScrollbackHistory(1000)
	if h.Len() != 0 {
		t.Errorf("expected empty history, got len %d", h.Len())
	}
	if h.MaxLines() != 1000 {
		t.Errorf("expected max 1000, got %d", h.MaxLines())
	}
}

func TestScrollbackHistory_Append(t *testing.T) {
	h := NewScrollbackHistory(1000)

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
	h := NewScrollbackHistory(1000)
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
	h := NewScrollbackHistory(5)

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
	h := NewScrollbackHistory(1000)
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
	h := NewScrollbackHistory(1000)
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
	h := NewScrollbackHistory(1000)
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
	h := NewScrollbackHistory(1000)
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
	h := NewScrollbackHistory(1000)
	h.AppendCells(makeCells("ABCDEFGHIJ")) // 10 chars -> 2 lines at width 5
	h.AppendCells(makeCells("XY"))         // 2 chars -> 1 line at width 5
	h.AppendCells(makeCells(""))           // empty -> 1 line

	count := h.PhysicalLineCount(5)
	if count != 4 { // 2 + 1 + 1
		t.Errorf("expected 4 physical lines, got %d", count)
	}
}

func TestScrollbackHistory_FindLogicalIndexForPhysicalLine(t *testing.T) {
	h := NewScrollbackHistory(1000)
	h.AppendCells(makeCells("ABCDEFGHIJ")) // logical 0 -> physical 0,1 (at width 5)
	h.AppendCells(makeCells("XY"))         // logical 1 -> physical 2
	h.AppendCells(makeCells("12345"))      // logical 2 -> physical 3

	tests := []struct {
		physical      int
		expectedLog   int
		expectedOff   int
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
	h := NewScrollbackHistory(1000)

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
