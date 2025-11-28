package parser

import (
	"fmt"
	"testing"
)

// TestScrollingRegionColumnZeroBug tests the interaction between scrolling regions
// and clearing/writing to column 0, which is suspected to be the cause of the
// codex rendering bug.
func TestScrollingRegionColumnZeroBug(t *testing.T) {
	t.Run("Clear and write in scrolling region", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// 1. Write initial content to the screen.
		h.SendText("Line 0: BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
		h.SendSeq("\r\n")
		h.SendText("Line 1: CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")
		h.SendSeq("\r\n")
		h.SendText("Line 2: DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD")
		h.SendSeq("\r\n")

		// 2. Set a scrolling region from line 2 to 10.
		h.SendSeq("\x1b[2;10r")

		// 3. Move cursor inside the scrolling region to line 2, col 0.
		h.SendSeq("\x1b[2;1H")

		// 4. Erase the line (EL 2)
		h.SendSeq("\x1b[2K")

		// 5. Write a box character to column 0.
		h.SendText("╭")

		// 6. Assert that the box character is present and the old content is gone.
		cell := h.GetCell(0, 1) // Line 2 is at y=1
		if cell.Rune != '╭' {
			t.Errorf("Expected '╭' at column 0 of line 1, but got %q", cell.Rune)
			h.Dump()
		}

		// Also check that the rest of the line is clear
		for i := 1; i < h.vterm.width; i++ {
			cell = h.GetCell(i, 1)
			if cell.Rune != ' ' && cell.Rune != 0 {
				t.Errorf("Expected blank at column %d of line 1, but got %q", i, cell.Rune)
				h.Dump()
				break
			}
		}

		// Check line length
		line := h.GetLine(1)
		if len(line) != h.vterm.width {
			t.Errorf("Expected line length to be %d, but got %d", h.vterm.width, len(line))
			h.Dump()
		}
	})

	t.Run("Scroll up within region and check for artifacts", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Fill screen with content (using sequences to position cursor)
		for i := 0; i < 10; i++ {
			h.SendSeq(fmt.Sprintf("\x1b[%d;1H", i+1)) // Position to line i+1, column 1 (1-indexed)
			h.SendText(fmt.Sprintf("Line %d ----------------------------------", i))
		}

		// Set scrolling region from line 3 to 7
		h.SendSeq("\x1b[3;7r")

		// Move cursor into region
		h.SendSeq("\x1b[7;1H")

		// Scroll up once (Index)
		h.SendSeq("\x1b[S")

		// Note: Writing to lines 1-10 in a 10-line terminal causes "Line 0" to scroll off.
		// Initial state after filling:
		//   Line 0: "Line 1 ---------------------------------"
		//   Line 1: "Line 2 ---------------------------------"
		//   ...
		//   Line 8: "Line 9 ---------------------------------"
		//
		// Scrolling region is lines 3-7 (1-indexed), which is 0-indexed lines 2-6.
		// After scrolling up once within that region:
		//   Lines 0-1: untouched (outside region)
		//   Lines 2-5: scrolled up (line 2 gets line 3's content, etc.)
		//   Line 6: blank (was bottom of region)
		//   Lines 7-9: untouched (outside region)

		// Line 0 should be untouched (outside region)
		h.AssertText(t, 0, 0, "Line 1 ---------------------------------")

		// Line 1 should be untouched (outside region)
		h.AssertText(t, 0, 1, "Line 2 ---------------------------------")

		// Line 2 (scrolled up - now has Line 4's content)
		h.AssertText(t, 0, 2, "Line 4 ---------------------------------")

		// The new line at the bottom of the region should be blank
		h.AssertLineBlank(t, 6)

		// The line below the region should be untouched
		h.AssertText(t, 0, 7, "Line 8 ---------------------------------")
	})
}
