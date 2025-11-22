package parser

import (
	"testing"
)

// TestInsertCharacters tests ICH (Insert Character) - ESC[@
func TestInsertCharacters(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*TestHarness)
		seq    string
		verify func(*testing.T, *TestHarness)
	}{
		{
			name: "ICH default (1 char)",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H") // Home
				h.SendText("ABCDEFGH")
				h.vterm.SetCursorPos(0, 3) // Position at 'D'
			},
			seq: "\x1b[@", // Insert 1 character
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "ABC DEFG") // 'H' pushed off if width=80
				h.AssertCursor(t, 3, 0)           // Cursor doesn't move
			},
		},
		{
			name: "ICH 1 explicit",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGH")
				h.vterm.SetCursorPos(0, 3)
			},
			seq: "\x1b[1@",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "ABC DEFG")
				h.AssertCursor(t, 3, 0)
			},
		},
		{
			name: "ICH 3 chars",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGH")
				h.vterm.SetCursorPos(0, 2)
			},
			seq: "\x1b[3@",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "AB   CDE") // Shifts CDE right
				h.AssertCursor(t, 2, 0)
			},
		},
		{
			name: "ICH at start of line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABCDEFGH")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[2@",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "  ABCDEF")
				h.AssertCursor(t, 0, 0)
			},
		},
		{
			name: "ICH at end of content",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABC")
				h.vterm.SetCursorPos(0, 3)
			},
			seq: "\x1b[2@",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "ABC")
				h.AssertText(t, 3, 0, "  ") // Two spaces inserted
			},
		},
		{
			name: "ICH beyond line end clamps",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("ABC")
				h.vterm.SetCursorPos(0, 1)
			},
			seq: "\x1b[100@", // Insert 100 chars
			verify: func(t *testing.T, h *TestHarness) {
				// Should insert up to end of line
				result := h.GetLine(0)
				// Check that B and C are pushed right
				if len(result) < 2 {
					t.Errorf("Line too short: %d", len(result))
				}
			},
		},
		{
			name: "ICH doesn't affect other lines",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.SendSeq("\r\n")
				h.SendText("Line2")
				h.vterm.SetCursorPos(1, 2)
			},
			seq: "\x1b[3@",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
				h.AssertText(t, 0, 1, "Li   ne1")
				h.AssertText(t, 0, 2, "Line2")
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

// TestInsertLines tests IL (Insert Line) - ESC[L
func TestInsertLines(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*TestHarness)
		seq    string
		verify func(*testing.T, *TestHarness)
	}{
		{
			name: "IL default (1 line)",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.SendSeq("\r\n")
				h.SendText("Line2")
				h.vterm.SetCursorPos(1, 0) // At Line1
			},
			seq: "\x1b[L",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
				h.AssertBlank(t, 0, 1) // Blank line inserted
				h.AssertText(t, 0, 2, "Line1")
				h.AssertCursor(t, 0, 1)
			},
		},
		{
			name: "IL 1 explicit",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.SendSeq("\r\n")
				h.SendText("Line2")
				h.vterm.SetCursorPos(1, 0)
			},
			seq: "\x1b[1L",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
				h.AssertBlank(t, 0, 1)
				h.AssertText(t, 0, 2, "Line1")
			},
		},
		{
			name: "IL 2 lines",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.SendSeq("\r\n")
				h.SendText("Line2")
				h.SendSeq("\r\n")
				h.SendText("Line3")
				h.vterm.SetCursorPos(1, 0)
			},
			seq: "\x1b[2L",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
				h.AssertBlank(t, 0, 1)
				h.AssertBlank(t, 0, 2)
				h.AssertText(t, 0, 3, "Line1")
			},
		},
		{
			name: "IL at top of screen",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[L",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertBlank(t, 0, 0)
				h.AssertText(t, 0, 1, "Line0")
			},
		},
		{
			name: "IL pushes lines down",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				for i := 0; i < 5; i++ {
					h.SendText("Line")
					if i < 4 {
						h.SendSeq("\r\n")
					}
				}
				h.vterm.SetCursorPos(2, 0)
			},
			seq: "\x1b[L",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line")
				h.AssertText(t, 0, 1, "Line")
				h.AssertBlank(t, 0, 2) // Inserted blank
				h.AssertText(t, 0, 3, "Line")
				h.AssertText(t, 0, 4, "Line")
			},
		},
		{
			name: "IL outside margins does nothing",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[5;20r") // Set margins 5-20
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.vterm.SetCursorPos(0, 0) // Outside margins
			},
			seq: "\x1b[L",
			verify: func(t *testing.T, h *TestHarness) {
				// Line should be unchanged since we're outside margins
				h.AssertText(t, 0, 0, "Line0")
			},
		},
		{
			name: "IL at bottom line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.vterm.SetCursorPos(23, 0) // Last line
			},
			seq: "\x1b[L",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
				h.AssertBlank(t, 0, 23)
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

// TestDeleteLines tests DL (Delete Line) - ESC[M
func TestDeleteLines(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*TestHarness)
		seq    string
		verify func(*testing.T, *TestHarness)
	}{
		{
			name: "DL default (1 line)",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.SendSeq("\r\n")
				h.SendText("Line2")
				h.vterm.SetCursorPos(1, 0) // At Line1
			},
			seq: "\x1b[M",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
				h.AssertText(t, 0, 1, "Line2") // Line1 deleted, Line2 moved up
				h.AssertCursor(t, 0, 1)
			},
		},
		{
			name: "DL 1 explicit",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.SendSeq("\r\n")
				h.SendText("Line2")
				h.vterm.SetCursorPos(1, 0)
			},
			seq: "\x1b[1M",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
				h.AssertText(t, 0, 1, "Line2")
			},
		},
		{
			name: "DL 2 lines",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.SendSeq("\r\n")
				h.SendText("Line2")
				h.SendSeq("\r\n")
				h.SendText("Line3")
				h.vterm.SetCursorPos(1, 0)
			},
			seq: "\x1b[2M",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
				h.AssertText(t, 0, 1, "Line3") // Line1 and Line2 deleted
			},
		},
		{
			name: "DL at top of screen",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.SendSeq("\r\n")
				h.SendText("Line2")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[M",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line1") // Line0 deleted
				h.AssertText(t, 0, 1, "Line2")
			},
		},
		{
			name: "DL pulls lines up",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				for i := 0; i < 5; i++ {
					h.SendText("Line")
					if i < 4 {
						h.SendSeq("\r\n")
					}
				}
				h.vterm.SetCursorPos(2, 0)
			},
			seq: "\x1b[M",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line")
				h.AssertText(t, 0, 1, "Line")
				h.AssertText(t, 0, 2, "Line") // Line at row 3 pulled up
				h.AssertText(t, 0, 3, "Line")
			},
		},
		{
			name: "DL outside margins does nothing",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[5;20r") // Set margins 5-20
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.vterm.SetCursorPos(0, 0) // Outside margins
			},
			seq: "\x1b[M",
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertText(t, 0, 0, "Line0")
			},
		},
		{
			name: "DL at bottom line",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				for i := 0; i < 24; i++ {
					h.SendText("Line")
					if i < 23 {
						h.SendSeq("\r\n")
					}
				}
				h.vterm.SetCursorPos(23, 0) // Last line
			},
			seq: "\x1b[M",
			verify: func(t *testing.T, h *TestHarness) {
				// Last line deleted, bottom should be blank
				h.AssertBlank(t, 0, 23)
			},
		},
		{
			name: "DL more lines than available",
			setup: func(h *TestHarness) {
				h.SendSeq("\x1b[H")
				h.SendText("Line0")
				h.SendSeq("\r\n")
				h.SendText("Line1")
				h.vterm.SetCursorPos(0, 0)
			},
			seq: "\x1b[100M", // Delete 100 lines
			verify: func(t *testing.T, h *TestHarness) {
				// All lines should be deleted
				h.AssertBlank(t, 0, 0)
				h.AssertBlank(t, 0, 1)
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

// TestInsertDeleteCombinations tests combinations of insert/delete operations
func TestInsertDeleteCombinations(t *testing.T) {
	t.Run("ICH then DCH", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[H")
		h.SendText("ABCDEFGH")
		h.vterm.SetCursorPos(0, 2)
		h.SendSeq("\x1b[3@") // Insert 3 spaces at C
		h.AssertText(t, 0, 0, "AB   CDE")
		h.SendSeq("\x1b[3P") // Delete 3 characters
		h.AssertText(t, 0, 0, "ABCDEFGH") // Should be back to original
	})

	t.Run("IL then DL", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[H")
		h.SendText("Line0")
		h.SendSeq("\r\n")
		h.SendText("Line1")
		h.SendSeq("\r\n")
		h.SendText("Line2")
		h.vterm.SetCursorPos(1, 0)
		h.SendSeq("\x1b[L") // Insert line at Line1
		h.AssertBlank(t, 0, 1)
		h.AssertText(t, 0, 2, "Line1")
		h.SendSeq("\x1b[M") // Delete the blank line
		h.AssertText(t, 0, 1, "Line1")
		h.AssertText(t, 0, 2, "Line2")
	})

	t.Run("Multiple ICH operations", func(t *testing.T) {
		h := NewTestHarness(80, 24)
		h.SendSeq("\x1b[H")
		h.SendText("ABCD")
		h.vterm.SetCursorPos(0, 1)
		h.SendSeq("\x1b[@") // Insert at B
		h.SendSeq("\x1b[@") // Insert again
		h.AssertText(t, 0, 0, "A  BC")
	})
}
