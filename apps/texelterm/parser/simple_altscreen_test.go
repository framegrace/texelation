package parser

import (
	"testing"
)

// TestSimpleAltScreen tests basic alternate screen operations
func TestSimpleAltScreen(t *testing.T) {
	t.Run("Simple alt screen write to column 0", func(t *testing.T) {
		h := NewTestHarness(10, 5)

		// Switch to alt screen
		h.SendSeq("\x1b[?1049h")

		// Write a single character
		h.SendText("X")

		// Check column 0
		cell := h.GetCell(0, 0)
		if cell.Rune != 'X' {
			t.Errorf("Expected 'X' at column 0, got %q", cell.Rune)
		}
	})

	t.Run("Alt screen: move to 1,1 and write", func(t *testing.T) {
		h := NewTestHarness(10, 5)

		// Switch to alt screen
		h.SendSeq("\x1b[?1049h")

		// Move to row 1, column 1 (both 1-indexed, so 0,0 in 0-indexed)
		h.SendSeq("\x1b[1;1H")

		// Check cursor position
		x, y := h.GetCursor()
		if x != 0 || y != 0 {
			t.Errorf("After ESC[1;1H, cursor should be at (0,0), got (%d,%d)", x, y)
		}

		// Write a character
		h.SendText("A")

		// Check column 0
		cell := h.GetCell(0, 0)
		if cell.Rune != 'A' {
			curX, curY := h.GetCursor()
			t.Errorf("Expected 'A' at column 0, got %q (0x%02x)", cell.Rune, cell.Rune)
			t.Logf("Cursor now at: (%d,%d)", curX, curY)
		}
	})

	t.Run("Alt screen: clear screen then write", func(t *testing.T) {
		h := NewTestHarness(10, 5)

		// Switch to alt screen
		h.SendSeq("\x1b[?1049h")

		// Write something first
		h.SendText("old")

		// Clear screen
		h.SendSeq("\x1b[2J")

		// Go home
		h.SendSeq("\x1b[H")

		// Write new content
		h.SendText("new")

		// Column 0 should be 'n'
		cell := h.GetCell(0, 0)
		if cell.Rune != 'n' {
			t.Errorf("Expected 'n' at column 0, got %q", cell.Rune)
		}
	})
}
