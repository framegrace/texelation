package parser

import (
	"testing"
)

// TestNodeJSCLIPatterns tests sequences commonly used by Node.js CLI apps
func TestNodeJSCLIPatterns(t *testing.T) {
	t.Run("Node.js menu redraw pattern", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Simulate user prompt before app launches
		h.SendSeq("\x1b[34m") // Blue color
		h.SendText("❯ ")
		h.SendSeq("\x1b[0m")
		h.SendText("codex\n")

		// App switches to alt screen
		h.SendSeq("\x1b[?1049h")

		// Common Node.js pattern: save cursor, home, clear screen
		h.SendSeq("\x1b7")    // Save cursor (DECSC)
		h.SendSeq("\x1b[H")   // Home
		h.SendSeq("\x1b[2J")  // Clear entire screen

		// Draw initial menu
		h.SendSeq("\x1b[1;1H")
		h.SendText("1. Update now")
		h.SendSeq("\x1b[2;1H")
		h.SendText("2. Skip")

		// Verify line 0, column 0 has '1'
		cell := h.GetCell(0, 0)
		if cell.Rune != '1' {
			t.Errorf("Line 0, col 0 should be '1', got %q", cell.Rune)
			h.Dump()
		}

		// Now simulate user pressing down arrow - menu redraws
		// Node.js often does: go to line, clear line, write new content
		h.SendSeq("\x1b[1;1H") // Go to line 1
		h.SendSeq("\x1b[2K")   // Erase entire line
		h.SendText("  1. Update now") // Add indent to show selection changed

		h.SendSeq("\x1b[2;1H") // Go to line 2
		h.SendSeq("\x1b[2K")   // Erase entire line
		h.SendText("> 2. Skip") // Show selected item

		// Check line 0 - should start with spaces now
		cell0 := h.GetCell(0, 0)
		if cell0.Rune != ' ' {
			t.Errorf("After redraw, line 0 col 0 should be ' ' (space), got %q (0x%04x)", cell0.Rune, cell0.Rune)
			if cell0.Rune == '1' {
				t.Error("BUG: Old '1' character still visible!")
			}
		}

		// Check line 1 - should start with '>'
		cell1 := h.GetCell(0, 1)
		if cell1.Rune != '>' {
			t.Errorf("Line 1 col 0 should be '>', got %q", cell1.Rune)
		}
	})

	t.Run("Node.js partial line update", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		h.SendSeq("\x1b[?1049h")

		// Write initial content
		h.SendSeq("\x1b[1;1H")
		h.SendText("Old content here")

		// Node.js pattern: go to start of line, erase to end, write new
		h.SendSeq("\x1b[1;1H") // Go to line 1, col 1
		h.SendSeq("\x1b[0K")   // Erase from cursor to end (EL 0)
		h.SendText("New")

		// Column 0 should be 'N', not 'O'
		cell := h.GetCell(0, 0)
		if cell.Rune == 'O' {
			t.Error("BUG: Old 'O' character still visible after EL 0!")
		}
		if cell.Rune != 'N' {
			t.Errorf("Col 0 should be 'N', got %q", cell.Rune)
		}
	})

	t.Run("Node.js cursor save/restore with erase", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		h.SendSeq("\x1b[?1049h")

		// Write content
		h.SendText("Some text")

		// Save cursor, go home, erase, write, restore
		h.SendSeq("\x1b7")    // Save cursor
		h.SendSeq("\x1b[H")   // Home
		h.SendSeq("\x1b[2K")  // Erase line
		h.SendText("Status")
		h.SendSeq("\x1b8")    // Restore cursor

		// Line 0 should have "Status", not "Some text"
		cell := h.GetCell(0, 0)
		if cell.Rune == 'S' && h.GetCell(1, 0).Rune == 'o' {
			// Could be either "Some" or "Status" - check which
			if h.GetCell(2, 0).Rune == 'm' {
				t.Error("BUG: Old 'Some text' still visible after erase!")
			}
		}
	})

	t.Run("Node.js screen clear then position", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		h.SendSeq("\x1b[?1049h")

		// Write old content on multiple lines
		for i := 0; i < 5; i++ {
			h.SendText("Old line\n")
		}

		// Clear screen
		h.SendSeq("\x1b[2J")

		// Position and write new content
		h.SendSeq("\x1b[1;1H")
		h.SendText("New content")

		// All cells on line 0 except the new content should be blank
		for x := 11; x < 20; x++ {
			cell := h.GetCell(x, 0)
			if cell.Rune != ' ' && cell.Rune != 0 {
				t.Errorf("After clear screen, col %d should be blank, got %q", x, cell.Rune)
			}
		}

		// Check that old content is really gone
		cell := h.GetCell(0, 1)
		if cell.Rune == 'O' {
			t.Error("BUG: Old content still visible on line 1 after clear screen!")
		}
	})
}
