// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/clearline_truncate_test.go
// Summary: Tests for ClearLine truncation of lines longer than terminal width.
// Usage: Run with `go test` to verify ClearLine properly truncates long lines.
// Notes: Reproduces the codex/htop bug where old content persisted after clearing.

package parser

import (
	"testing"
)

// TestClearLineTruncatesLongLines verifies that ClearLine removes content
// beyond the terminal width, fixing the bug where old characters persisted
// in column 0 when running apps like codex or htop.
func TestClearLineTruncatesLongLines(t *testing.T) {
	t.Run("EL 2 clears entire line and ensures proper width", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Move to line 1
		h.SendSeq("\n")

		// Position cursor to column 50 and write text
		h.SendSeq("\x1b[2;51H") // Row 2, col 51 (0-indexed: 1,50)
		longText := ""
		for i := 0; i < 70; i++ {
			longText += "x"
		}
		h.SendText(longText)

		// With wrapping, text wraps to next line.
		// Line 1 should have 'x' from column 50-79 (30 chars).
		// Remaining 40 chars wrap to line 2.

		// Verify we have 'x' at column 50 of line 1
		grid := h.vterm.Grid()
		if grid[1][50].Rune != 'x' {
			t.Fatal("Setup: expected 'x' at position (1,50)")
		}

		// Now clear the entire line with EL 2
		h.SendSeq("\x1b[2;1H")  // Go to start of line 1
		h.SendSeq("\x1b[2K")    // Erase entire line (EL 2)

		// Check that the line is properly cleared
		topLine := h.vterm.getTopHistoryLine()
		line := h.vterm.getHistoryLine(topLine + 1)

		// Line should be at most terminal width
		if len(line) > 80 {
			t.Errorf("After EL 2, line should be <= 80 chars, got %d", len(line))
			t.Error("This was the bug that caused old prompts to persist in codex/htop")
		}

		// Verify all visible characters are blank
		grid = h.vterm.Grid()
		for x := 0; x < 80; x++ {
			if grid[1][x].Rune != ' ' && grid[1][x].Rune != 0 {
				t.Errorf("After EL 2, grid[1][%d] should be blank, got %q", x, grid[1][x].Rune)
				break
			}
		}
	})

	t.Run("EL 0 truncates from cursor to end", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Write a long line
		for i := 0; i < 100; i++ {
			h.SendText("a")
		}

		// Position cursor at column 50
		h.SendSeq("\r")
		h.SendSeq("\x1b[C") // Move right, but let's use absolute positioning
		h.SendSeq("\x1b[1;51H") // Position to row 1, col 51 (0-indexed: 0,50)

		// Erase from cursor to end
		h.SendSeq("\x1b[0K") // EL 0

		// Check line was truncated at position 80 (terminal width)
		topLine := h.vterm.getTopHistoryLine()
		line := h.vterm.getHistoryLine(topLine)
		if len(line) > 80 {
			t.Errorf("After EL 0, line should be <= 80 chars, got %d", len(line))
		}
	})

	t.Run("Reproduces codex bug - old prompt persists", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Simulate shell prompt before codex runs
		h.SendText("❯ codex")
		h.SendSeq("\n")

		// User types something long that makes the line grow
		longText := ""
		for i := 0; i < 100; i++ {
			longText += "x"
		}
		h.SendText(longText)
		h.SendSeq("\n")

		// Now codex tries to draw its menu by clearing line and writing new content
		// Go to line 1 (where the long text was)
		h.SendSeq("\x1b[2;1H") // Row 2, col 1 (0-indexed: 1,0)

		// Clear the line
		h.SendSeq("\x1b[2K") // EL 2

		// Write new content
		h.SendText("Menu item 1")

		// Check that Grid shows the new content, not old content
		grid := h.vterm.Grid()
		if grid[1][0].Rune == 'x' {
			t.Error("BUG: Old 'x' character still visible after EL 2!")
			t.Error("This is the codex/htop bug where old content persists")
		}
		if grid[1][0].Rune != 'M' {
			t.Errorf("Grid[1][0] should be 'M', got %q", grid[1][0].Rune)
		}
	})

	t.Run("EL 1 does not truncate (clears beginning to cursor)", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Write a long line
		for i := 0; i < 100; i++ {
			h.SendText("b")
		}

		// Position cursor at column 50
		h.SendSeq("\r")
		h.SendSeq("\x1b[1;51H") // Row 1, col 51

		// Erase from beginning to cursor (EL 1)
		h.SendSeq("\x1b[1K")

		// The line can still be long because EL 1 doesn't erase to end
		// Line might still be > 80 because we only cleared beginning to cursor
		// This is correct behavior - EL 1 shouldn't truncate

		// But first 51 characters should be blank
		grid := h.vterm.Grid()
		for x := 0; x <= 50; x++ {
			if grid[0][x].Rune != ' ' && grid[0][x].Rune != 0 {
				t.Errorf("After EL 1, grid[0][%d] should be blank, got %q", x, grid[0][x].Rune)
				break
			}
		}

		// Remaining chars should still be 'b'
		for x := 51; x < 80; x++ {
			if grid[0][x].Rune != 'b' {
				t.Errorf("After EL 1, grid[0][%d] should still be 'b', got %q", x, grid[0][x].Rune)
				break
			}
		}
	})
}
