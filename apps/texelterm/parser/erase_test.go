// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/erase_test.go
// Summary: Comprehensive tests for erase and delete control sequences.
// Usage: Run with `go test` to verify erase operation correctness.
// Notes: Tests all erase commands against xterm specification.

package parser

import (
	"testing"
)

// TestEraseInDisplay tests ED (Erase in Display) - ESC[<n>J
// XTerm spec: CSI Ps J - Erase in Display (ED)
//   Ps = 0 (default): Erase from cursor to end of display
//   Ps = 1: Erase from beginning to cursor
//   Ps = 2: Erase entire display
//   Ps = 3: Erase display and scrollback buffer
func TestEraseInDisplay(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*TestHarness)
		seq     string
		verify  func(*testing.T, *TestHarness)
	}{
		{
			name: "ED 0 - erase from cursor to end",
			setup: func(h *TestHarness) {
				h.FillWithPattern("ABCDEFGHIJ")
				h.vterm.SetCursorPos(2, 5) // Row 2, Col 5
			},
			seq: "\x1b[J", // Default is 0
			verify: func(t *testing.T, h *TestHarness) {
				// Row 0 and 1 should be unchanged
				h.AssertRune(t, 0, 0, 'A')
				h.AssertRune(t, 1, 0, 'B')
				h.AssertRune(t, 0, 1, 'A')

				// Row 2 col 0-4 should be unchanged
				h.AssertRune(t, 0, 2, 'A')
				h.AssertRune(t, 4, 2, 'E')

				// Row 2 col 5 onwards should be blank
				h.AssertBlank(t, 5, 2)
				h.AssertBlank(t, 10, 2)

				// All rows below should be blank
				h.AssertBlank(t, 0, 3)
				h.AssertBlank(t, 0, 4)
			},
		},
		{
			name: "ED 0 explicit - erase from cursor to end",
			setup: func(h *TestHarness) {
				h.FillWithPattern("ABCDEFGHIJ")
				h.vterm.SetCursorPos(2, 5)
			},
			seq: "\x1b[0J",
			verify: func(t *testing.T, h *TestHarness) {
				// Same as default
				h.AssertRune(t, 0, 0, 'A')
				h.AssertBlank(t, 5, 2)
			},
		},
		{
			name: "ED 1 - erase from beginning to cursor",
			setup: func(h *TestHarness) {
				h.FillWithPattern("ABCDEFGHIJ")
				h.vterm.SetCursorPos(2, 5)
			},
			seq: "\x1b[1J",
			verify: func(t *testing.T, h *TestHarness) {
				// All of rows 0 and 1 should be blank
				h.AssertBlank(t, 0, 0)
				h.AssertBlank(t, 79, 0)
				h.AssertBlank(t, 0, 1)
				h.AssertBlank(t, 79, 1)

				// Row 2 col 0-5 (inclusive) should be blank
				h.AssertBlank(t, 0, 2)
				h.AssertBlank(t, 5, 2)

				// Row 2 col 6+ should be unchanged
				h.AssertRune(t, 6, 2, 'G')

				// Rows below cursor should be unchanged
				h.AssertRune(t, 0, 3, 'A')
			},
		},
		{
			name: "ED 2 - erase entire display",
			setup: func(h *TestHarness) {
				h.FillWithPattern("ABCDEFGHIJ")
				h.vterm.SetCursorPos(10, 10)
			},
			seq: "\x1b[2J",
			verify: func(t *testing.T, h *TestHarness) {
				width, height := h.GetSize()
				// Entire display should be blank
				for y := 0; y < height; y++ {
					for x := 0; x < width; x++ {
						h.AssertBlank(t, x, y)
					}
				}
				// Cursor position should be unchanged
				h.AssertCursor(t, 10, 10)
			},
		},
		{
			name: "ED 2 from home position",
			setup: func(h *TestHarness) {
				h.FillWithPattern("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[2J",
			verify: func(t *testing.T, h *TestHarness) {
				// Entire display blank
				h.AssertBlank(t, 0, 0)
				h.AssertBlank(t, 79, 23)
				h.AssertCursor(t, 0, 0)
			},
		},
		{
			name: "ED 3 - erase display and scrollback",
			setup: func(h *TestHarness) {
				// Add some history
				for i := 0; i < 50; i++ {
					h.SendText("Line ")
					h.SendText(string(rune('A' + (i % 26))))
					h.SendSeq("\r\n")
				}
			},
			seq: "\x1b[3J",
			verify: func(t *testing.T, h *TestHarness) {
				// History should be cleared
				histLen := h.GetHistoryLength()
				// Should have minimal history (just current screen)
				if histLen > 30 {
					t.Errorf("History not cleared: has %d lines", histLen)
				}
			},
		},
		{
			name: "ED at top-left",
			setup: func(h *TestHarness) {
				h.FillWithPattern("XYZ")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[J",
			verify: func(t *testing.T, h *TestHarness) {
				// Everything should be blank
				h.AssertBlank(t, 0, 0)
				h.AssertBlank(t, 79, 23)
			},
		},
		{
			name: "ED at bottom-right",
			setup: func(h *TestHarness) {
				h.FillWithPattern("XYZ")
				h.vterm.SetCursorPos(23, 79)
			},
			seq: "\x1b[J",
			verify: func(t *testing.T, h *TestHarness) {
				// Only last cell should be erased
				// Pattern repeats every 3 chars, so (0*80+0)%3=0->'X'
				h.AssertRune(t, 0, 0, 'X')
				// (23*80+78)%3 = 1918%3 = 2 -> 'Z'
				h.AssertRune(t, 78, 23, 'Z')
				h.AssertBlank(t, 79, 23)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			tt.setup(h)
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// TestEraseInLine tests EL (Erase in Line) - ESC[<n>K
// XTerm spec: CSI Ps K - Erase in Line (EL)
//   Ps = 0 (default): Erase from cursor to end of line
//   Ps = 1: Erase from beginning to cursor
//   Ps = 2: Erase entire line
func TestEraseInLine(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*TestHarness)
		seq     string
		verify  func(*testing.T, *TestHarness)
	}{
		{
			name: "EL 0 - erase from cursor to end of line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H") // Home
				h.SendText("Hello World Test")
				h.vterm.SetCursorPos(0, 6) // After "Hello "
			},
			seq: "\x1b[K",
			verify: func(t *testing.T, h *TestHarness) {
				// "Hello " should remain
				h.AssertText(t, 0, 0, "Hello ")
				// Rest of line should be blank
				h.AssertBlank(t, 6, 0)
				h.AssertBlank(t, 10, 0)
				h.AssertBlank(t, 79, 0)
				// Other lines unaffected
				h.AssertBlank(t, 0, 1)
			},
		},
		{
			name: "EL 0 explicit",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 5)
			},
			seq: "\x1b[0K",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "ABCDE")
				h.AssertBlank(t, 5, 0)
			},
		},
		{
			name: "EL 1 - erase from beginning to cursor",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Hello World Test")
				h.vterm.SetCursorPos(0, 6)
			},
			seq: "\x1b[1K",
			verify: func(t *testing.T, h *TestHarness) {
				// Up to and including cursor should be blank
				h.AssertBlank(t, 0, 0)
				h.AssertBlank(t, 6, 0)
				// Rest should remain
				h.AssertRune(t, 7, 0, 'o')
				h.AssertRune(t, 8, 0, 'r')
			},
		},
		{
			name: "EL 2 - erase entire line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Hello World Test")
				h.vterm.SetCursorPos(0, 10)
			},
			seq: "\x1b[2K",
			verify: func(t *testing.T, h *TestHarness) {
				width, _ := h.GetSize()
				// Entire line should be blank
				for x := 0; x < width; x++ {
					h.AssertBlank(t, x, 0)
				}
				// Cursor unchanged
				h.AssertCursor(t, 10, 0)
			},
		},
		{
			name: "EL at start of line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Test Line")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[K",
			verify: func(t *testing.T, h *TestHarness) {
				// Entire line erased
				h.AssertBlank(t, 0, 0)
				h.AssertBlank(t, 8, 0)
			},
		},
		{
			name: "EL at end of line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Test")
				h.vterm.SetCursorPos(0, 4)
			},
			seq: "\x1b[K",
			verify: func(t *testing.T, h *TestHarness) {
				// "Test" unchanged
				h.AssertText(t, 0, 0, "Test")
				// Rest blank
				h.AssertBlank(t, 4, 0)
			},
		},
		{
			name: "EL on middle line doesn't affect others",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line 1")
				h.SendSeq("\r\n")
				h.SendText("Line 2")
				h.SendSeq("\r\n")
				h.SendText("Line 3")
				h.vterm.SetCursorPos(1, 3) // Middle of line 2
			},
			seq: "\x1b[2K",
			verify: func(t *testing.T, h *TestHarness) {
				// Line 1 unchanged
				h.AssertText(t, 0, 0, "Line 1")
				// Line 2 erased
				h.AssertBlank(t, 0, 1)
				// Line 3 unchanged
				h.AssertText(t, 0, 2, "Line 3")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			tt.setup(h)
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// TestEraseCharacter tests ECH (Erase Character) - ESC[<n>X
// XTerm spec: CSI Ps X - Erase Ps Character(s) (default = 1) (ECH)
// Erases characters at cursor position, leaving blanks
func TestEraseCharacter(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*TestHarness)
		seq     string
		verify  func(*testing.T, *TestHarness)
	}{
		{
			name: "ECH default (1 char)",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 3)
			},
			seq: "\x1b[X",
			verify: func(t *testing.T, h *TestHarness) {
				// ABC then blank then EFGHIJ
				h.AssertText(t, 0, 0, "ABC")
				h.AssertBlank(t, 3, 0)
				h.AssertText(t, 4, 0, "EFGHIJ")
			},
		},
		{
			name: "ECH 1 explicit",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 3)
			},
			seq: "\x1b[1X",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "ABC")
				h.AssertBlank(t, 3, 0)
				h.AssertText(t, 4, 0, "EFGHIJ")
			},
		},
		{
			name: "ECH 5 chars",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 2)
			},
			seq: "\x1b[5X",
			verify: func(t *testing.T, h *TestHarness) {
				// AB then 5 blanks then HIJ
				h.AssertText(t, 0, 0, "AB")
				h.AssertBlank(t, 2, 0)
				h.AssertBlank(t, 3, 0)
				h.AssertBlank(t, 4, 0)
				h.AssertBlank(t, 5, 0)
				h.AssertBlank(t, 6, 0)
				h.AssertText(t, 7, 0, "HIJ")
			},
		},
		{
			name: "ECH at start of line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[3X",
			verify: func(t *testing.T, h *TestHarness) {
				// 3 blanks then DEFGHIJ
				h.AssertBlank(t, 0, 0)
				h.AssertBlank(t, 1, 0)
				h.AssertBlank(t, 2, 0)
				h.AssertText(t, 3, 0, "DEFGHIJ")
			},
		},
		{
			name: "ECH beyond line end clamps",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABC")
				h.vterm.SetCursorPos(0, 1)
			},
			seq: "\x1b[100X", // Erase way more than exists
			verify: func(t *testing.T, h *TestHarness) {
				// A then rest blank
				h.AssertRune(t, 0, 0, 'A')
				h.AssertBlank(t, 1, 0)
				h.AssertBlank(t, 2, 0)
				h.AssertBlank(t, 10, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			tt.setup(h)
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// TestDeleteCharacter tests DCH (Delete Character) - ESC[<n>P
// XTerm spec: CSI Ps P - Delete Ps Character(s) (default = 1) (DCH)
// Deletes characters and shifts remaining characters left
func TestDeleteCharacter(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*TestHarness)
		seq     string
		verify  func(*testing.T, *TestHarness)
	}{
		{
			name: "DCH default (1 char)",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 3)
			},
			seq: "\x1b[P",
			verify: func(t *testing.T, h *TestHarness) {
				// ABC then EFGHIJ (D deleted, rest shifted left)
				h.AssertText(t, 0, 0, "ABCEFGHIJ")
				// End of line should be blank
				h.AssertBlank(t, 9, 0)
			},
		},
		{
			name: "DCH 1 explicit",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 3)
			},
			seq: "\x1b[1P",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "ABCEFGHIJ")
				h.AssertBlank(t, 9, 0)
			},
		},
		{
			name: "DCH 3 chars",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 2)
			},
			seq: "\x1b[3P",
			verify: func(t *testing.T, h *TestHarness) {
				// AB then FGHIJ (CDE deleted)
				h.AssertText(t, 0, 0, "ABFGHIJ")
				// Last 3 chars should be blank
				h.AssertBlank(t, 7, 0)
				h.AssertBlank(t, 8, 0)
				h.AssertBlank(t, 9, 0)
			},
		},
		{
			name: "DCH at start of line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGHIJ")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[2P",
			verify: func(t *testing.T, h *TestHarness) {
				// CDEFGHIJ (AB deleted)
				h.AssertText(t, 0, 0, "CDEFGHIJ")
				h.AssertBlank(t, 8, 0)
				h.AssertBlank(t, 9, 0)
			},
		},
		{
			name: "DCH delete all to end",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEF")
				h.vterm.SetCursorPos(0, 2)
			},
			seq: "\x1b[100P", // Delete more than exists
			verify: func(t *testing.T, h *TestHarness) {
				// AB then all blank
				h.AssertText(t, 0, 0, "AB")
				h.AssertBlank(t, 2, 0)
				h.AssertBlank(t, 5, 0)
			},
		},
		{
			name: "DCH at end of content",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABC")
				h.vterm.SetCursorPos(0, 3)
			},
			seq: "\x1b[P",
			verify: func(t *testing.T, h *TestHarness) {
				// ABC unchanged (nothing to delete)
				h.AssertText(t, 0, 0, "ABC")
				h.AssertBlank(t, 3, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			tt.setup(h)
			h.SendSeq(tt.seq)
			tt.verify(t, h)
		})
	}
}

// TestEraseWithColors tests that erase operations preserve background color
// XTerm spec: Erase operations fill with spaces using current background color
func TestEraseWithColors(t *testing.T) {
	t.Run("EL preserves current background", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Set red background
		h.SendSeq("\x1b[41m")

		// Write some text
		h.SendSeq("\x1b[H")
		h.SendText("Test")
		h.vterm.SetCursorPos(0, 2)

		// Erase to end of line
		h.SendSeq("\x1b[K")

		// Check that erased cells have red background
		cell := h.GetCell(2, 0)
		if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 1 {
			t.Errorf("Erased cell background: expected red (mode=Standard, value=1), got mode=%v, value=%v",
				cell.BG.Mode, cell.BG.Value)
		}
	})

	t.Run("ED preserves current background", func(t *testing.T) {
		h := NewTestHarness(80, 24)

		// Set blue background
		h.SendSeq("\x1b[44m")

		// Position and erase
		h.vterm.SetCursorPos(5, 5)
		h.SendSeq("\x1b[J")

		// Check erased cell has blue background
		cell := h.GetCell(10, 10)
		if cell.BG.Mode != ColorModeStandard || cell.BG.Value != 4 {
			t.Errorf("Erased cell background: expected blue (mode=Standard, value=4), got mode=%v, value=%v",
				cell.BG.Mode, cell.BG.Value)
		}
	})
}
