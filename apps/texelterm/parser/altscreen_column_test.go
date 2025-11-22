package parser

import (
	"testing"
)

// TestAltScreenColumnZero tests column 0 behavior in alternate screen
func TestAltScreenColumnZero(t *testing.T) {
	t.Run("Alt screen: erase and rewrite column 0", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Write some content in main screen
		h.SendText("main screen content")

		// Switch to alternate screen
		h.SendSeq("\x1b[?1049h")

		// Write a prompt-like pattern
		h.SendText("❯ old menu item 1\n")
		h.SendText("> old menu item 2\n")
		h.SendText("  old menu item 3")

		// Now simulate app redrawing: go to home, erase screen, redraw
		h.SendSeq("\x1b[H")   // Home
		h.SendSeq("\x1b[2J")  // Clear screen

		// Redraw menu (app might start from column 0 or column 1)
		h.SendSeq("\x1b[1;1H") // Row 1, col 1
		h.SendText("1. New item 1\n")
		h.SendSeq("\x1b[2;1H") // Row 2, col 1
		h.SendText("2. New item 2\n")
		h.SendSeq("\x1b[3;1H") // Row 3, col 1
		h.SendText("3. New item 3")

		// Check that column 0 of each line has the new content, not old
		cell0_line0 := h.GetCell(0, 0)
		if cell0_line0.Rune != '1' {
			t.Errorf("Line 0, col 0 should be '1', got %q", cell0_line0.Rune)
			t.Logf("Buffer dump:")
			h.Dump()
		}

		cell0_line1 := h.GetCell(0, 1)
		if cell0_line1.Rune != '2' {
			t.Errorf("Line 1, col 0 should be '2', got %q", cell0_line1.Rune)
		}

		cell0_line2 := h.GetCell(0, 2)
		if cell0_line2.Rune != '3' {
			t.Errorf("Line 2, col 0 should be '3', got %q", cell0_line2.Rune)
		}
	})

	t.Run("Alt screen: CR + EL 2 + write from col 0", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Switch to alt screen
		h.SendSeq("\x1b[?1049h")

		// Write old content with color
		h.SendSeq("\x1b[34m") // Blue
		h.SendText("❯ ")
		h.SendSeq("\x1b[0m") // Reset
		h.SendText("old content")

		// Redraw: CR, erase line, write new
		h.SendSeq("\r")       // CR
		h.SendSeq("\x1b[2K")  // EL 2 - erase entire line
		h.SendText("new content")

		// Column 0 should be 'n', not '❯'
		cell0 := h.GetCell(0, 0)
		if cell0.Rune == '❯' {
			t.Errorf("Column 0 still has old prompt '❯', should be 'n'")
			t.Logf("Cell: Rune=%q, FG=%+v", cell0.Rune, cell0.FG)
		}
		if cell0.Rune != 'n' {
			t.Errorf("Column 0 should be 'n', got %q", cell0.Rune)
		}
	})

	t.Run("Alt screen: Erase line from cursor to end", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Switch to alt screen
		h.SendSeq("\x1b[?1049h")

		// Write content
		h.SendText("❯ old text here")

		// Go to column 0
		h.SendSeq("\x1b[1G")

		// Erase from cursor to end of line (should erase entire line since we're at col 0)
		h.SendSeq("\x1b[0K") // EL 0

		// Column 0 should be blank
		cell0 := h.GetCell(0, 0)
		if cell0.Rune != ' ' && cell0.Rune != 0 {
			t.Errorf("After EL 0 from column 0, column 0 should be blank, got %q", cell0.Rune)
		}

		// Write new content
		h.SendText("new")

		// Column 0 should be 'n'
		h.AssertRune(t, 0, 0, 'n')
	})
}
