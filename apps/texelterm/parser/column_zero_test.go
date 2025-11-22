package parser

import (
	"testing"
)

// TestColumnZeroOverwrite tests that column 0 can be properly overwritten
func TestColumnZeroOverwrite(t *testing.T) {
	t.Run("Write to column 0 after CR", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Write a prompt character at column 0
		h.SendText("> ")

		// Move back to start of line
		h.SendSeq("\r")

		// Write new text that should overwrite the >
		h.SendText("X")

		// Column 0 should now be 'X', not '>'
		h.AssertRune(t, 0, 0, 'X')
	})

	t.Run("Erase line then write from column 1", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Write a prompt at column 0
		h.SendText("❯ old text")

		// Move to start and erase entire line
		h.SendSeq("\r")      // CR
		h.SendSeq("\x1b[2K") // EL 2 - erase entire line

		// Now move to column 1 and write new text
		h.SendSeq("\x1b[1G") // CHA - move to column 1 (which is column 0 in 0-indexed)
		h.SendText("new text")

		// Column 0 should have 'n', not '❯'
		got := h.GetCell(0, 0)
		if got.Rune == '❯' {
			t.Errorf("Column 0 still has prompt character '❯', expected it to be erased")
		}
		if got.Rune != 'n' {
			t.Errorf("Column 0 should be 'n', got %q", got.Rune)
		}
	})

	t.Run("Move to column 2 leaves column 0 and 1 unchanged", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Write a prompt
		h.SendText("❯ ")

		// CR and erase line
		h.SendSeq("\r\x1b[2K")

		// Move to column 2 (0-indexed column 1) and write
		h.SendSeq("\x1b[2G")
		h.SendText("text")

		// Column 0 should be blank (erased by EL 2)
		cell0 := h.GetCell(0, 0)
		if cell0.Rune != ' ' && cell0.Rune != 0 {
			t.Errorf("Column 0 should be blank, got %q", cell0.Rune)
		}

		// Column 1 should have 't'
		h.AssertRune(t, 1, 0, 't')
	})

	t.Run("CHA to column 1 should work", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Start at some position
		h.SendText("test")

		// Use CHA to move to column 1 (0-indexed column 0)
		h.SendSeq("\x1b[1G")

		x, _ := h.GetCursor()
		if x != 0 {
			t.Errorf("After ESC[1G, cursor should be at X=0, got X=%d", x)
		}

		// Write should overwrite column 0
		h.SendText("X")
		h.AssertRune(t, 0, 0, 'X')
	})

	t.Run("CHA to column 0 should go to column 0", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Start at some position
		h.SendText("test")

		// ESC[0G should be treated as ESC[1G (column 1 = 0-indexed 0)
		h.SendSeq("\x1b[0G")

		x, _ := h.GetCursor()
		if x != 0 {
			t.Errorf("After ESC[0G, cursor should be at X=0, got X=%d", x)
		}
	})
}
