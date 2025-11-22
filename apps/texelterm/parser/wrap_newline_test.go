package parser

import (
	"strings"
	"testing"
)

// TestWrapNextWithNewline tests the critical case where:
// 1. Text fills exactly to the right edge (sets wrapNext flag)
// 2. Then a newline is sent
// This should NOT create an extra blank line
func TestWrapNextWithNewline(t *testing.T) {
	t.Run("Text to edge then newline - no extra blank line", func(t *testing.T) {
		h := NewTestHarness(10, 5) // Small width to make testing easier

		// Write exactly 10 characters (fills the line)
		h.SendText("1234567890")

		// Cursor should be at wrapNext (column 10, which is past the edge)
		// Now send a newline (must use SendSeq for control characters)
		h.SendSeq("\n")

		// Expected: cursor at start of line 2
		// NOT at start of line 3
		x, y := h.GetCursor()
		if y != 1 {
			t.Errorf("After text-to-edge + newline, cursor should be at Y=1, got Y=%d", y)
			t.Logf("Harness dump:")
			h.Dump()
		}
		if x != 0 {
			t.Errorf("After newline, cursor should be at X=0, got X=%d", x)
		}

		// Verify there's no blank line between line 0 and line 1
		line0 := h.GetLine(0)
		if len(line0) != 10 {
			t.Errorf("Line 0 should have 10 chars, got %d", len(line0))
		}

		// Write more text to verify we're on line 1, not line 2
		h.SendText("Next")
		curX, curY := h.GetCursor()
		t.Logf("After writing 'Next', cursor at (%d, %d)", curX, curY)
		t.Logf("Buffer dump:")
		h.Dump()
		h.AssertText(t, 0, 1, "Next")
	})

	t.Run("Two-line prompt with wrapping", func(t *testing.T) {
		h := NewTestHarness(40, 10)

		// Simulate a prompt that fills line 1, wraps, then adds content on line 2
		// This is similar to power prompts like starship or powerlevel10k

		// Line 1: Fill exactly to the edge
		prompt1 := strings.Repeat("=", 40) // Exactly 40 chars
		h.SendText(prompt1)

		// Now send newline to go to next line
		h.SendSeq("\n")

		// Line 2: The actual prompt
		promptText := "❯ "
		h.SendText(promptText)

		// Check cursor position - should be on line 1 (0-indexed), not line 2
		x, y := h.GetCursor()
		if y != 1 {
			t.Errorf("After 2-line prompt, cursor should be at Y=1, got Y=%d", y)
			t.Logf("Cursor at (%d, %d)", x, y)
			t.Logf("Dump:")
			h.Dump()
		}

		// Verify line 0 has the border
		h.AssertText(t, 0, 0, strings.Repeat("=", 40))
		// Verify line 1 has the prompt (check first 2 chars only)
		if len(promptText) >= 1 {
			h.AssertRune(t, 0, 1, '❯')
		}
		if len(promptText) >= 2 {
			h.AssertRune(t, 1, 1, ' ')
		}

		// Verify line 2 is blank (no extra line)
		line2 := h.GetLine(2)
		if len(line2) > 0 {
			t.Errorf("Line 2 should be blank, but has %d chars", len(line2))
		}
	})

	t.Run("Alternating full lines and newlines", func(t *testing.T) {
		h := NewTestHarness(10, 10)

		// This simulates repeated prompts or output
		for i := 0; i < 3; i++ {
			h.SendText("1234567890") // Exactly 10 chars
			h.SendSeq("\n")          // Newline
		}

		// Should be at line 3 (0, 1, 2)
		_, y := h.GetCursor()
		if y != 3 {
			t.Errorf("After 3 full lines with newlines, should be at Y=3, got Y=%d", y)
			h.Dump()
		}
	})

	t.Run("Almost full line then newline", func(t *testing.T) {
		h := NewTestHarness(10, 10)

		// Write 9 characters (one short of edge)
		h.SendText("123456789")
		h.SendSeq("\n")

		// Should be at line 1
		_, y := h.GetCursor()
		if y != 1 {
			t.Errorf("After 9 chars + newline, should be at Y=1, got Y=%d", y)
		}
	})

	t.Run("Overfull line forces wrap then newline", func(t *testing.T) {
		h := NewTestHarness(10, 10)

		// Write 11 characters - should wrap to 2 lines
		h.SendText("12345678901")

		// Should have wrapped: line 0 has "1234567890", line 1 has "1"
		h.AssertText(t, 0, 0, "1234567890")
		h.AssertText(t, 0, 1, "1")

		// Now send newline
		h.SendSeq("\n")

		// Should be at line 2 (not line 3)
		_, y := h.GetCursor()
		if y != 2 {
			t.Errorf("After wrap + newline, should be at Y=2, got Y=%d", y)
			h.Dump()
		}
	})

	t.Run("CR LF handling at edge", func(t *testing.T) {
		h := NewTestHarness(10, 10)

		// Write exactly 10 chars
		h.SendText("1234567890")

		// Send CR+LF (Windows-style newline)
		h.SendSeq("\r\n")

		// Should be at line 1, column 0
		x, y := h.GetCursor()
		if y != 1 {
			t.Errorf("After text-to-edge + CRLF, should be at Y=1, got Y=%d", y)
		}
		if x != 0 {
			t.Errorf("After CRLF, should be at X=0, got X=%d", x)
		}
	})
}

// TestWrapNextWithCarriageReturn tests wrapNext with carriage return
func TestWrapNextWithCarriageReturn(t *testing.T) {
	t.Run("Text to edge then CR", func(t *testing.T) {
		h := NewTestHarness(10, 10)

		// Write exactly 10 characters
		h.SendText("1234567890")

		// Send carriage return (should go to start of SAME line)
		h.SendSeq("\r")

		// Should still be on line 0
		x, y := h.GetCursor()
		if y != 0 {
			t.Errorf("After text-to-edge + CR, should still be at Y=0, got Y=%d", y)
		}
		if x != 0 {
			t.Errorf("After CR, should be at X=0, got X=%d", x)
		}
	})

	t.Run("Text to edge, CR, more text", func(t *testing.T) {
		h := NewTestHarness(10, 10)

		// Write exactly 10 characters
		h.SendText("1234567890")

		// CR and overwrite
		h.SendSeq("\r")
		h.SendText("ABCD")

		// Should have "ABCD567890" on line 0
		h.AssertText(t, 0, 0, "ABCD567890")

		// Should still be on line 0
		_, y := h.GetCursor()
		if y != 0 {
			t.Errorf("Should still be on Y=0, got Y=%d", y)
		}
	})
}
