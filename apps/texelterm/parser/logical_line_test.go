package parser

import (
	"testing"
)

func TestLogicalLine_NewAndLen(t *testing.T) {
	line := NewLogicalLine()
	if line.Len() != 0 {
		t.Errorf("expected new line to have length 0, got %d", line.Len())
	}
}

func TestLogicalLine_NewFromCells(t *testing.T) {
	cells := []Cell{
		{Rune: 'H', FG: DefaultFG, BG: DefaultBG},
		{Rune: 'i', FG: DefaultFG, BG: DefaultBG},
	}
	line := NewLogicalLineFromCells(cells)

	if line.Len() != 2 {
		t.Errorf("expected length 2, got %d", line.Len())
	}

	// Verify it's a copy, not aliased
	cells[0].Rune = 'X'
	if line.Cells[0].Rune != 'H' {
		t.Error("cells should be copied, not aliased")
	}
}

func TestLogicalLine_SetCell(t *testing.T) {
	line := NewLogicalLine()

	// Set beyond current length - should extend
	line.SetCell(5, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})

	if line.Len() != 6 {
		t.Errorf("expected length 6 after SetCell(5, ...), got %d", line.Len())
	}

	if line.Cells[5].Rune != 'X' {
		t.Errorf("expected 'X' at position 5, got '%c'", line.Cells[5].Rune)
	}

	// Positions 0-4 should be spaces
	for i := 0; i < 5; i++ {
		if line.Cells[i].Rune != ' ' {
			t.Errorf("expected space at position %d, got '%c'", i, line.Cells[i].Rune)
		}
	}
}

func TestLogicalLine_Append(t *testing.T) {
	line := NewLogicalLine()
	line.Append(Cell{Rune: 'A', FG: DefaultFG, BG: DefaultBG})
	line.Append(Cell{Rune: 'B', FG: DefaultFG, BG: DefaultBG})

	if line.Len() != 2 {
		t.Errorf("expected length 2, got %d", line.Len())
	}
	if line.Cells[0].Rune != 'A' || line.Cells[1].Rune != 'B' {
		t.Error("cells not appended correctly")
	}
}

func TestLogicalLine_Truncate(t *testing.T) {
	cells := []Cell{
		{Rune: 'A'}, {Rune: 'B'}, {Rune: 'C'}, {Rune: 'D'}, {Rune: 'E'},
	}
	line := NewLogicalLineFromCells(cells)

	line.Truncate(3)
	if line.Len() != 3 {
		t.Errorf("expected length 3 after truncate, got %d", line.Len())
	}

	// Truncate beyond length should be no-op
	line.Truncate(10)
	if line.Len() != 3 {
		t.Errorf("truncate beyond length should be no-op, got length %d", line.Len())
	}
}

func TestLogicalLine_Clear(t *testing.T) {
	cells := []Cell{{Rune: 'A'}, {Rune: 'B'}}
	line := NewLogicalLineFromCells(cells)

	line.Clear()
	if line.Len() != 0 {
		t.Errorf("expected length 0 after clear, got %d", line.Len())
	}
}

func TestLogicalLine_Clone(t *testing.T) {
	line := NewLogicalLine()
	line.Append(Cell{Rune: 'X'})

	clone := line.Clone()

	// Modify original
	line.Cells[0].Rune = 'Y'

	if clone.Cells[0].Rune != 'X' {
		t.Error("clone should be independent of original")
	}
}

func TestLogicalLine_WrapToWidth(t *testing.T) {
	tests := []struct {
		name          string
		cells         []Cell
		width         int
		expectedLines int
	}{
		{
			name:          "empty line",
			cells:         nil,
			width:         80,
			expectedLines: 1, // Empty produces one empty physical line
		},
		{
			name:          "fits in one line",
			cells:         makeCells("Hello"),
			width:         80,
			expectedLines: 1,
		},
		{
			name:          "exactly one line",
			cells:         makeCells("12345"),
			width:         5,
			expectedLines: 1,
		},
		{
			name:          "wraps to two lines",
			cells:         makeCells("1234567890"),
			width:         5,
			expectedLines: 2,
		},
		{
			name:          "wraps to three lines",
			cells:         makeCells("123456789012"),
			width:         5,
			expectedLines: 3,
		},
		{
			name:          "handles width=1",
			cells:         makeCells("ABC"),
			width:         1,
			expectedLines: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := NewLogicalLineFromCells(tc.cells)
			physical := line.WrapToWidth(tc.width)

			if len(physical) != tc.expectedLines {
				t.Errorf("expected %d physical lines, got %d", tc.expectedLines, len(physical))
			}

			// Verify offsets are correct
			for i, p := range physical {
				expectedOffset := i * tc.width
				if p.Offset != expectedOffset {
					t.Errorf("line %d: expected offset %d, got %d", i, expectedOffset, p.Offset)
				}
			}
		})
	}
}

func TestLogicalLine_WrapToWidth_Content(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("ABCDEFGHIJ"))
	physical := line.WrapToWidth(4)

	// Should produce: "ABCD", "EFGH", "IJ"
	if len(physical) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(physical))
	}

	expected := []string{"ABCD", "EFGH", "IJ"}
	for i, p := range physical {
		got := cellsToString(p.Cells)
		if got != expected[i] {
			t.Errorf("line %d: expected %q, got %q", i, expected[i], got)
		}
	}
}

func TestLogicalLine_TrimTrailingSpaces(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("Hello   "))
	line.TrimTrailingSpaces()

	if line.Len() != 5 {
		t.Errorf("expected length 5 after trimming, got %d", line.Len())
	}

	// Line with styled trailing space should keep it
	line2 := NewLogicalLine()
	line2.Append(Cell{Rune: 'A', FG: DefaultFG, BG: DefaultBG})
	line2.Append(Cell{Rune: ' ', FG: Color{Mode: ColorMode256, Value: 1}, BG: DefaultBG}) // Colored space

	line2.TrimTrailingSpaces()
	if line2.Len() != 2 {
		t.Errorf("styled trailing space should not be trimmed, got length %d", line2.Len())
	}
}

func TestLogicalLine_InsertCell(t *testing.T) {
	// Test inserting into existing line
	line := NewLogicalLineFromCells(makeCells("ABC"))

	line.InsertCell(1, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})

	if line.Len() != 4 {
		t.Errorf("expected length 4 after insert, got %d", line.Len())
	}
	if cellsToString(line.Cells) != "AXBC" {
		t.Errorf("expected 'AXBC', got '%s'", cellsToString(line.Cells))
	}
}

func TestLogicalLine_InsertCell_AtStart(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("ABC"))

	line.InsertCell(0, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})

	if cellsToString(line.Cells) != "XABC" {
		t.Errorf("expected 'XABC', got '%s'", cellsToString(line.Cells))
	}
}

func TestLogicalLine_InsertCell_AtEnd(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("ABC"))

	line.InsertCell(3, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})

	if cellsToString(line.Cells) != "ABCX" {
		t.Errorf("expected 'ABCX', got '%s'", cellsToString(line.Cells))
	}
}

func TestLogicalLine_InsertCell_BeyondLength(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("AB"))

	// Insert at position 5 - should extend line with spaces up to position 5, then place X
	line.InsertCell(5, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})

	if line.Len() != 6 { // AB + 3 spaces + X
		t.Errorf("expected length 6, got %d", line.Len())
	}
	// After insert at position 5, content should be "AB" + 3 spaces + "X"
	expected := "AB   X"
	if cellsToString(line.Cells) != expected {
		t.Errorf("expected %q, got %q", expected, cellsToString(line.Cells))
	}
}

func TestLogicalLine_InsertCell_MultipleInserts(t *testing.T) {
	// Simulate insert mode: typing "XY" at position 1 in "ABC"
	line := NewLogicalLineFromCells(makeCells("ABC"))

	line.InsertCell(1, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})
	line.InsertCell(2, Cell{Rune: 'Y', FG: DefaultFG, BG: DefaultBG})

	if cellsToString(line.Cells) != "AXYBC" {
		t.Errorf("expected 'AXYBC', got '%s'", cellsToString(line.Cells))
	}
}

// --- FixedWidth Tests ---

func TestLogicalLine_FixedWidth_DefaultZero(t *testing.T) {
	line := NewLogicalLine()
	if line.FixedWidth != 0 {
		t.Errorf("new line should have FixedWidth=0, got %d", line.FixedWidth)
	}

	line2 := NewLogicalLineFromCells(makeCells("Hello"))
	if line2.FixedWidth != 0 {
		t.Errorf("line from cells should have FixedWidth=0, got %d", line2.FixedWidth)
	}
}

func TestLogicalLine_ClipOrPadToWidth_Clipping(t *testing.T) {
	// Line is 10 chars, viewport is 5 - should clip
	line := &LogicalLine{
		Cells:      makeCells("1234567890"),
		FixedWidth: 10,
	}

	physical := line.ClipOrPadToWidth(5)

	if len(physical.Cells) != 5 {
		t.Errorf("expected 5 cells (clipped), got %d", len(physical.Cells))
	}
	if cellsToString(physical.Cells) != "12345" {
		t.Errorf("expected '12345', got '%s'", cellsToString(physical.Cells))
	}
	if physical.Offset != 0 {
		t.Errorf("offset should be 0, got %d", physical.Offset)
	}
}

func TestLogicalLine_ClipOrPadToWidth_Padding(t *testing.T) {
	// Line is 5 chars, viewport is 10 - should pad
	line := &LogicalLine{
		Cells:      makeCells("Hello"),
		FixedWidth: 5,
	}

	physical := line.ClipOrPadToWidth(10)

	if len(physical.Cells) != 10 {
		t.Errorf("expected 10 cells (padded), got %d", len(physical.Cells))
	}
	if cellsToString(physical.Cells) != "Hello     " {
		t.Errorf("expected 'Hello     ', got '%s'", cellsToString(physical.Cells))
	}
}

func TestLogicalLine_ClipOrPadToWidth_ExactMatch(t *testing.T) {
	// Line is 5 chars, viewport is 5 - no change
	line := &LogicalLine{
		Cells:      makeCells("Hello"),
		FixedWidth: 5,
	}

	physical := line.ClipOrPadToWidth(5)

	if len(physical.Cells) != 5 {
		t.Errorf("expected 5 cells, got %d", len(physical.Cells))
	}
	if cellsToString(physical.Cells) != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", cellsToString(physical.Cells))
	}
}

func TestLogicalLine_ClipOrPadToWidth_EmptyLine(t *testing.T) {
	line := &LogicalLine{
		Cells:      nil,
		FixedWidth: 10,
	}

	physical := line.ClipOrPadToWidth(5)

	if len(physical.Cells) != 5 {
		t.Errorf("expected 5 cells, got %d", len(physical.Cells))
	}
	// All spaces
	for i, c := range physical.Cells {
		if c.Rune != ' ' {
			t.Errorf("expected space at position %d, got '%c'", i, c.Rune)
		}
	}
}

func TestLogicalLine_WrapToWidth_RespectsFixedWidth(t *testing.T) {
	// When FixedWidth > 0, WrapToWidth should use ClipOrPadToWidth
	line := &LogicalLine{
		Cells:      makeCells("1234567890"),
		FixedWidth: 10, // Fixed at 10 columns
	}

	// Request wrap at 5 columns - should NOT wrap, should clip
	physical := line.WrapToWidth(5)

	if len(physical) != 1 {
		t.Errorf("fixed-width line should produce exactly 1 physical line, got %d", len(physical))
	}
	if len(physical[0].Cells) != 5 {
		t.Errorf("expected 5 cells (clipped to viewport), got %d", len(physical[0].Cells))
	}
	if cellsToString(physical[0].Cells) != "12345" {
		t.Errorf("expected '12345', got '%s'", cellsToString(physical[0].Cells))
	}
}

func TestLogicalLine_WrapToWidth_FixedWidthPadding(t *testing.T) {
	line := &LogicalLine{
		Cells:      makeCells("Hi"),
		FixedWidth: 5,
	}

	// Viewport is 10 - should pad to viewport width
	physical := line.WrapToWidth(10)

	if len(physical) != 1 {
		t.Errorf("fixed-width line should produce exactly 1 physical line, got %d", len(physical))
	}
	if len(physical[0].Cells) != 10 {
		t.Errorf("expected 10 cells (padded to viewport), got %d", len(physical[0].Cells))
	}
}

func TestLogicalLine_WrapToWidth_ZeroFixedWidth(t *testing.T) {
	// FixedWidth=0 should use normal reflow
	line := &LogicalLine{
		Cells:      makeCells("1234567890"),
		FixedWidth: 0, // Normal reflow
	}

	physical := line.WrapToWidth(5)

	if len(physical) != 2 {
		t.Errorf("normal line should wrap to 2 physical lines, got %d", len(physical))
	}
	if cellsToString(physical[0].Cells) != "12345" {
		t.Errorf("first line should be '12345', got '%s'", cellsToString(physical[0].Cells))
	}
	if cellsToString(physical[1].Cells) != "67890" {
		t.Errorf("second line should be '67890', got '%s'", cellsToString(physical[1].Cells))
	}
}

func TestLogicalLine_Clone_PreservesFixedWidth(t *testing.T) {
	line := &LogicalLine{
		Cells:      makeCells("Hello"),
		FixedWidth: 80,
	}

	clone := line.Clone()

	if clone.FixedWidth != 80 {
		t.Errorf("clone should preserve FixedWidth, expected 80, got %d", clone.FixedWidth)
	}

	// Modifying original shouldn't affect clone
	line.FixedWidth = 100
	if clone.FixedWidth != 80 {
		t.Error("clone's FixedWidth should be independent")
	}
}

// --- Overlay Tests ---

func TestLogicalLine_OverlayFields(t *testing.T) {
	line := NewLogicalLine()
	if line.Overlay != nil {
		t.Error("new line should have nil Overlay")
	}
	if line.OverlayWidth != 0 {
		t.Error("new line should have zero OverlayWidth")
	}
	if line.Synthetic {
		t.Error("new line should not be Synthetic")
	}
}

func TestLogicalLine_CloneWithOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("original"))
	line.Overlay = []Cell{
		{Rune: 'F', FG: DefaultFG, BG: DefaultBG},
		{Rune: 'M', FG: DefaultFG, BG: DefaultBG},
		{Rune: 'T', FG: DefaultFG, BG: DefaultBG},
	}
	line.OverlayWidth = 80
	line.Synthetic = true

	clone := line.Clone()

	if len(clone.Overlay) != 3 {
		t.Fatalf("expected overlay len 3, got %d", len(clone.Overlay))
	}
	if clone.OverlayWidth != 80 {
		t.Errorf("expected OverlayWidth 80, got %d", clone.OverlayWidth)
	}
	if !clone.Synthetic {
		t.Error("expected Synthetic=true on clone")
	}

	// Verify no aliasing
	clone.Overlay[0].Rune = 'X'
	if line.Overlay[0].Rune != 'F' {
		t.Error("overlay should be deep-copied, not aliased")
	}
}

func TestLogicalLine_CloneNilOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("no overlay"))
	clone := line.Clone()
	if clone.Overlay != nil {
		t.Error("clone of line without overlay should have nil Overlay")
	}
}

// Helper to create cells from a string
func makeCells(s string) []Cell {
	cells := make([]Cell, len(s))
	for i, r := range s {
		cells[i] = Cell{Rune: r, FG: DefaultFG, BG: DefaultBG}
	}
	return cells
}

// Helper to convert cells back to string
func cellsToString(cells []Cell) string {
	runes := make([]rune, len(cells))
	for i, c := range cells {
		runes[i] = c.Rune
	}
	return string(runes)
}
