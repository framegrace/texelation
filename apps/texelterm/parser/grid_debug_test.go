package parser

import (
	"testing"
)

// TestGridBasic tests that Grid() shows the correct content
func TestGridBasic(t *testing.T) {
	t.Run("Simple write then read via Grid", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Write some text
		h.SendText("Hello")

		// Get the grid
		grid := h.vterm.Grid()

		// Check that we can see what we wrote
		if grid[0][0].Rune != 'H' {
			t.Errorf("Grid[0][0] should be 'H', got %q", grid[0][0].Rune)
		}
		if grid[0][1].Rune != 'e' {
			t.Errorf("Grid[0][1] should be 'e', got %q", grid[0][1].Rune)
		}
	})

	t.Run("Overwrite column 0 via Grid", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Write initial text
		h.SendText("Old")

		// Check initial state
		grid := h.vterm.Grid()
		if grid[0][0].Rune != 'O' {
			t.Fatalf("Setup: Grid[0][0] should be 'O', got %q", grid[0][0].Rune)
		}

		// Go back to start and overwrite
		h.SendSeq("\r")
		h.SendText("New")

		// Check via Grid
		grid = h.vterm.Grid()
		if grid[0][0].Rune == 'O' {
			t.Error("BUG: Grid still shows 'O' at column 0 after overwrite!")
		}
		if grid[0][0].Rune != 'N' {
			t.Errorf("Grid[0][0] should be 'N', got %q", grid[0][0].Rune)
		}
	})

	t.Run("Long line via Grid", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Write a very long line (more than 80 chars)
		longText := ""
		for i := 0; i < 100; i++ {
			longText += "x"
		}
		h.SendText(longText)

		// Grid should show the first 80 chars on row 0
		// and remaining chars on row 1
		grid := h.vterm.Grid()

		// Row 0 should be full of 'x'
		for x := 0; x < 80; x++ {
			if grid[0][x].Rune != 'x' {
				t.Errorf("Grid[0][%d] should be 'x', got %q", x, grid[0][x].Rune)
				break
			}
		}

		// Row 1 should have the remaining 20 'x' characters
		for x := 0; x < 20; x++ {
			if grid[1][x].Rune != 'x' {
				t.Errorf("Grid[1][%d] should be 'x', got %q", x, grid[1][x].Rune)
				break
			}
		}
	})
}
