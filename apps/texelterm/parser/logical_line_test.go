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
