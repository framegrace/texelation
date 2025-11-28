package parser

import (
	"fmt"
	"testing"
)

func TestSimpleScroll(t *testing.T) {
	h := NewTestHarness(10, 5)

	// Switch to alternate screen
	h.SendSeq("\x1b[?1049h")

	// Fill screen with a pattern
	for i := 0; i < 5; i++ {
		h.SendText(fmt.Sprintf("Line %d", i))
		h.SendSeq("\r\n")
	}

	// Set scrolling region from line 2 to 4 (0-indexed: 1 to 3)
	h.SendSeq("\x1b[2;4r")

	// Move cursor into region
	h.SendSeq("\x1b[4;1H")

	// Scroll up once (SU)
	h.SendSeq("\x1b[S")

	// --- Assertions ---

	// Note: Writing 5 lines with \r\n in a 5-line terminal causes "Line 0" to scroll off.
	// The initial screen state before scrolling is:
	//   Line 0: "Line 1"
	//   Line 1: "Line 2"
	//   Line 2: "Line 3"
	//   Line 3: "Line 4"
	//   Line 4: blank

	// Line 0 should be untouched (outside scrolling region)
	h.AssertText(t, 0, 0, "Line 1")

	// Line 1 (scrolled up - now contains what was line 2)
	h.AssertText(t, 0, 1, "Line 3")

	// Line 2 (scrolled up - now contains what was line 3)
	h.AssertText(t, 0, 2, "Line 4")

	// Line 3 (should be blank - was bottom of region)
	h.AssertLineBlank(t, 3)

	// Line 4 should be untouched (outside scrolling region) - was already blank
	h.AssertLineBlank(t, 4)
}

