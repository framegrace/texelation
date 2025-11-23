package parser

import (
	"testing"
)

// TestLongLineColumnZeroOverwrite reproduces the exact bug from codex logs
// where a 113-character line with '❯' at column 0 doesn't get overwritten by SPACE
func TestLongLineColumnZeroOverwrite(t *testing.T) {
	t.Run("Overwrite column 0 on very long line (113 chars)", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Create a very long line starting with '❯' (like the logs show)
		// The logs showed: "line length=113" when trying to write SPACE
		h.SendText("❯ ")

		// Write a bunch of text to make the line very long (>80 chars, wrapping)
		longText := "This is a very long line that will wrap beyond the terminal width "
		longText += "and continue wrapping to create a line that is 113 characters total"
		h.SendText(longText)

		// Verify we have the '❯' at column 0
		cell := h.GetCell(0, 0)
		if cell.Rune != '❯' {
			t.Fatalf("Setup: column 0 should have '❯', got %q", cell.Rune)
		}

		// Now go back to column 0 and write SPACE (exactly what codex does)
		h.SendSeq("\r") // Carriage return to column 0
		h.SendText(" ") // Write SPACE

		// Verify column 0 now has SPACE
		cell = h.GetCell(0, 0)
		if cell.Rune == '❯' {
			t.Error("BUG REPRODUCED: Column 0 still has '❯' after SPACE write on long line!")
			t.Logf("This is the exact bug from the logs")
		}
		if cell.Rune != ' ' {
			t.Errorf("Column 0 should be ' ', got %q", cell.Rune)
		}
	})

	t.Run("Overwrite column 0 on 113-char line directly", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Build exactly 113 characters starting with '❯'
		text := "❯ "
		for len(text) < 113 {
			text += "x"
		}

		h.SendText(text[:113])

		// Verify setup
		if h.GetCell(0, 0).Rune != '❯' {
			t.Fatal("Setup failed")
		}

		// Go to column 0 and write SPACE
		h.SendSeq("\r")
		h.SendText(" ")

		// Check result
		cell := h.GetCell(0, 0)
		if cell.Rune == '❯' {
			t.Error("BUG: Column 0 not overwritten on 113-char line!")
		}
		if cell.Rune != ' ' {
			t.Errorf("Column 0 should be ' ', got %q", cell.Rune)
		}
	})

	t.Run("Write to column 0 after line wraps", func(t *testing.T) {
		h := NewTestHarness(40, 10) // Smaller width to test wrapping

		// Write more than one line's worth
		h.SendText("❯ ")
		for i := 0; i < 50; i++ {
			h.SendText("x")
		}

		// Go back to start of first line
		h.SendSeq("\r")
		h.SendText("Z")

		// Column 0 should be 'Z', not '❯'
		cell := h.GetCell(0, 0)
		if cell.Rune == '❯' {
			t.Error("BUG: Column 0 not overwritten after wrap!")
		}
		if cell.Rune != 'Z' {
			t.Errorf("Column 0 should be 'Z', got %q", cell.Rune)
		}
	})
}
