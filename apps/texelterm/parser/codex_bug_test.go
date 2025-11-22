package parser

import (
	"testing"
)

// TestCodexColumnZeroBug reproduces the exact codex rendering bug
func TestCodexColumnZereBug(t *testing.T) {
	t.Run("Reproduce codex menu rendering", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// User has a shell prompt with blue color
		h.SendSeq("\x1b[34m") // Blue
		h.SendText("❯ ")
		h.SendSeq("\x1b[0m") // Reset
		h.SendText("codex")
		h.SendSeq("\n")

		// Codex launches and switches to alternate screen
		h.SendSeq("\x1b[?1049h")

		// Codex draws its menu - simulating what we see in wrong.png
		// It probably does: home, clear screen, then draw menu items

		h.SendSeq("\x1b[H")   // Home
		h.SendSeq("\x1b[2J")  // Clear screen

		// Draw menu items starting from top
		h.SendSeq("\x1b[1;1H") // Row 1, col 1
		h.SendText("1. Update now (runs `npm install -g @openai/codex`)")
		h.SendSeq("\x1b[2;1H") // Row 2, col 1
		h.SendText("2. Skip")

		// Check that column 0 does NOT have the old prompt character
		cell0_line0 := h.GetCell(0, 0)
		cell0_line1 := h.GetCell(0, 1)

		if cell0_line0.Rune == '❯' {
			t.Errorf("BUG REPRODUCED: Column 0 line 0 still has prompt '❯'")
			t.Logf("Expected '1', got %q", cell0_line0.Rune)
			h.Dump()
		}

		if cell0_line0.Rune != '1' {
			t.Errorf("Column 0 line 0 should be '1', got %q", cell0_line0.Rune)
		}

		if cell0_line1.Rune != '2' {
			t.Errorf("Column 0 line 1 should be '2', got %q", cell0_line1.Rune)
		}
	})

	t.Run("Alt screen: CR + EL 0 from column 0", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Switch to alt screen
		h.SendSeq("\x1b[?1049h")

		// Simulate: write old content with color at column 0
		h.SendSeq("\x1b[34m") // Blue
		h.SendText("❯")
		h.SendSeq("\x1b[0m") // Reset
		h.SendText(" old text")

		// App redraws: CR to column 0
		h.SendSeq("\r")

		// Erase from cursor to end of line (cursor is at column 0, so should erase entire line)
		h.SendSeq("\x1b[0K") // EL 0

		// Write new text
		h.SendText("new")

		// Column 0 should be 'n', not '❯'
		cell0 := h.GetCell(0, 0)
		if cell0.Rune == '❯' {
			t.Errorf("BUG: Column 0 still has '❯' after CR + EL 0 + write")
			t.Logf("Expected 'n', got %q", cell0.Rune)
		}
		if cell0.Rune != 'n' {
			t.Errorf("Column 0 should be 'n', got %q", cell0.Rune)
		}
	})

	t.Run("Alt screen: Direct write to column 0", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Switch to alt screen
		h.SendSeq("\x1b[?1049h")

		// Write something at column 0
		h.SendText("OLD")

		// Position to column 0
		h.SendSeq("\x1b[1G")

		// Overwrite column 0
		h.SendText("N")

		// Column 0 should be 'N'
		cell0 := h.GetCell(0, 0)
		if cell0.Rune == 'O' {
			t.Errorf("BUG: Column 0 still has 'O', couldn't overwrite")
		}
		if cell0.Rune != 'N' {
			t.Errorf("Column 0 should be 'N', got %q", cell0.Rune)
		}
	})
}
