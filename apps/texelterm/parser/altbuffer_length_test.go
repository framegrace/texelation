package parser

import (
	"testing"
)

// TestAltBufferLinesFullWidth ensures all altBuffer lines are always full width
func TestAltBufferLinesFullWidth(t *testing.T) {
	h := NewTestHarness(40, 10)

	// Switch to alt screen
	h.SendSeq("\x1b[?1049h")

	// Write something on line 0
	h.SendText("test")

	// Erase the line
	h.SendSeq("\r\x1b[2K")

	// Now write something new
	h.SendText("new")

	// Get the grid
	grid := h.vterm.Grid()

	// Check that ALL lines are full width
	for y := 0; y < len(grid); y++ {
		if len(grid[y]) != 40 {
			t.Errorf("Line %d has length %d, expected 40", y, len(grid[y]))
			t.Logf("This will cause renderer to access out of bounds or see truncated lines")
		}
	}

	// Specifically check line 0
	if len(grid[0]) < 40 {
		t.Errorf("Line 0 only has %d cells, expected 40", len(grid[0]))
	}

	// Check that column 0 has 'n'
	if grid[0][0].Rune != 'n' {
		t.Errorf("Column 0 should have 'n', got %q", grid[0][0].Rune)
	}
}

// TestAltBufferMultipleErasesKeepsWidth tests that multiple erases don't shrink lines
func TestAltBufferMultipleErasesKeepsWidth(t *testing.T) {
	h := NewTestHarness(40, 10)

	h.SendSeq("\x1b[?1049h")

	// Write and erase multiple times
	for i := 0; i < 5; i++ {
		h.SendSeq("\x1b[H")  // Home
		h.SendSeq("\x1b[2K") // Erase line
		h.SendText("test")
	}

	grid := h.vterm.Grid()
	for y := 0; y < len(grid); y++ {
		if len(grid[y]) != 40 {
			t.Errorf("After %d erases, line %d has length %d, expected 40", 5, y, len(grid[y]))
		}
	}
}
