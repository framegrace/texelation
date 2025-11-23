package parser

import (
	"testing"
)

// TestColumnZeroEraseIssue reproduces the bug where column 0 isn't erased
func TestColumnZeroEraseIssue(t *testing.T) {
	t.Run("Column 0 not erased with CR + EL 2", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Switch to alt screen
		h.SendSeq("\x1b[?1049h")

		// Simulate what exists before codex starts (e.g., prompt)
		h.SendSeq("\x1b[34m") // Blue color
		h.SendText("❯ ")
		h.SendSeq("\x1b[0m")
		h.SendText("codex\n")

		// Now codex tries to clear and redraw
		// It goes to position 1,1 (which should be 0,0 internally)
		h.SendSeq("\x1b[1;1H")

		// Erase entire line
		h.SendSeq("\x1b[2K")

		// Write new content
		h.SendText("Menu item 1")

		// Column 0 should be 'M', not '❯'
		cell := h.GetCell(0, 0)
		if cell.Rune == '❯' {
			t.Errorf("BUG: Column 0 still has '❯' after EL 2, should have 'M'")
			t.Logf("Column 0: %q (0x%04x)", cell.Rune, cell.Rune)
		}
		if cell.Rune != 'M' {
			t.Errorf("Column 0 should be 'M', got %q", cell.Rune)
		}
	})

	t.Run("CSI H positioning to 1,1", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[?1049h")

		// Write something at top-left
		h.SendText("Old")

		// Position to 1,1 (top-left in terminal coordinates)
		h.SendSeq("\x1b[1;1H")

		// Check cursor is at 0,0 internally
		if h.vterm.cursorX != 0 {
			t.Errorf("After CSI 1;1H, cursorX should be 0, got %d", h.vterm.cursorX)
		}
		if h.vterm.cursorY != 0 {
			t.Errorf("After CSI 1;1H, cursorY should be 0, got %d", h.vterm.cursorY)
		}

		// Write should overwrite
		h.SendText("New")

		cell := h.GetCell(0, 0)
		if cell.Rune != 'N' {
			t.Errorf("Column 0 should be 'N', got %q", cell.Rune)
		}
	})

	t.Run("Carriage return goes to column 0", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[?1049h")

		// Write some text
		h.SendText("Hello")

		// Cursor should be at column 5
		if h.vterm.cursorX != 5 {
			t.Errorf("After 'Hello', cursorX should be 5, got %d", h.vterm.cursorX)
		}

		// Carriage return
		h.SendSeq("\r")

		// Cursor should be at column 0
		if h.vterm.cursorX != 0 {
			t.Errorf("After CR, cursorX should be 0, got %d", h.vterm.cursorX)
		}

		// Write 'X' - should overwrite 'H'
		h.SendText("X")

		cell := h.GetCell(0, 0)
		if cell.Rune != 'X' {
			t.Errorf("Column 0 should be 'X', got %q", cell.Rune)
		}
	})

	t.Run("EL 2 from column 0 erases entire line", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[?1049h")

		// Write a full line
		h.SendText("This is old content that should be erased")

		// Go back to start
		h.SendSeq("\r")

		// Cursor should be at column 0
		if h.vterm.cursorX != 0 {
			t.Errorf("After CR, cursorX should be 0, got %d", h.vterm.cursorX)
		}

		// Erase entire line (EL 2)
		h.SendSeq("\x1b[2K")

		// All cells should be blank
		for x := 0; x < 42; x++ {
			cell := h.GetCell(x, 0)
			if cell.Rune != ' ' && cell.Rune != 0 {
				t.Errorf("After EL 2, column %d should be blank, got %q", x, cell.Rune)
				break
			}
		}

		// Specifically check column 0
		cell0 := h.GetCell(0, 0)
		if cell0.Rune == 'T' {
			t.Error("BUG: Column 0 still has 'T' after EL 2!")
		}
	})
}
