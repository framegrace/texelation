package parser

import (
	"testing"
)

// TestMainScreenColumnZero tests the exact codex scenario on main screen (no alt buffer)
func TestMainScreenColumnZero(t *testing.T) {
	t.Run("Codex pattern on main screen", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Simulate user prompt before codex starts (on main screen)
		h.SendSeq("\x1b[34m") // Blue color
		h.SendText("❯ ")
		h.SendSeq("\x1b[0m")
		h.SendText("codex")
		h.SendSeq("\n") // Use SendSeq for newline to properly trigger LineFeed

		// Now codex runs WITHOUT switching to alt screen
		// It writes at the current position (line 1, column 0)

		// Codex pattern: position to column 0, write SPACE, cursor auto-advances to 1
		// Then erase from 1 to end
		h.SendSeq("\r") // CR to column 0
		h.SendText(" ") // SPACE at column 0, cursor advances to 1
		h.SendSeq("\x1b[0K") // Erase from cursor to end (EL 0)
		h.SendText("Menu item 1") // Write new content

		// Check what's at line 1, column 0
		// Should be SPACE (from the SPACE write), not '❯'
		cell := h.GetCell(0, 1)
		if cell.Rune == '❯' {
			t.Errorf("BUG: Column 0 of line 1 still has '❯' after SPACE write, should have ' '")
			t.Logf("Column 0: %q (0x%04x)", cell.Rune, cell.Rune)
		}
		if cell.Rune != ' ' {
			t.Errorf("Column 0 of line 1 should be ' ', got %q", cell.Rune)
		}

		// Check that "Menu item 1" follows
		if h.GetCell(1, 1).Rune != 'M' {
			t.Errorf("Column 1 should be 'M', got %q", h.GetCell(1, 1).Rune)
		}
	})

	t.Run("Multiple overwrites on main screen", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Write initial content
		h.SendText("Old line")
		h.SendSeq("\n")

		// Go back to line 0
		h.SendSeq("\x1b[1;1H") // Position to 1,1 (line 0, col 0)

		// Erase entire line
		h.SendSeq("\x1b[2K")

		// Write new content
		h.SendText("New line")

		// Check column 0 is 'N', not 'O'
		cell := h.GetCell(0, 0)
		if cell.Rune == 'O' {
			t.Error("BUG: Column 0 still has 'O' after EL 2 on main screen!")
		}
		if cell.Rune != 'N' {
			t.Errorf("Column 0 should be 'N', got %q", cell.Rune)
		}
	})

	t.Run("SPACE write at column 0 main screen", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Write something at column 0
		h.SendText("X")

		// Go back to column 0
		h.SendSeq("\r")

		// Write SPACE - this should overwrite 'X'
		h.SendText(" ")

		// Check column 0
		cell := h.GetCell(0, 0)
		if cell.Rune == 'X' {
			t.Error("BUG: Column 0 still has 'X' after SPACE write!")
		}
		if cell.Rune != ' ' {
			t.Errorf("Column 0 should be ' ', got %q", cell.Rune)
		}
	})
}
