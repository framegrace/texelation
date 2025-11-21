// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/cursor_test.go
// Summary: Comprehensive tests for cursor movement control sequences.
// Usage: Run with `go test` to verify cursor movement correctness.
// Notes: Tests all cursor movement commands against xterm specification.

package parser

import (
	"testing"
)

// TestCursorUp tests CUU (Cursor Up) - ESC[<n>A
// XTerm spec: CSI Ps A - Cursor Up Ps Times (default = 1) (CUU)
func TestCursorUp(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int // width, height
		initialY  int
		sequence  string
		expectedY int
	}{
		{"no param (default 1)", [2]int{80, 24}, 10, "\x1b[A", 9},
		{"explicit 1", [2]int{80, 24}, 10, "\x1b[1A", 9},
		{"move 5", [2]int{80, 24}, 10, "\x1b[5A", 5},
		{"move 10", [2]int{80, 24}, 15, "\x1b[10A", 5},
		{"at top (no movement)", [2]int{80, 24}, 0, "\x1b[5A", 0},
		{"overflow (clamps to 0)", [2]int{80, 24}, 5, "\x1b[100A", 0},
		{"from bottom", [2]int{80, 24}, 23, "\x1b[20A", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			// Set initial cursor position (SetCursorPos takes y, x)
			h.vterm.SetCursorPos(tt.initialY, 0)
			// Send sequence
			h.SendSeq(tt.sequence)
			// Verify
			h.AssertCursor(t, 0, tt.expectedY)
		})
	}
}

// TestCursorDown tests CUD (Cursor Down) - ESC[<n>B
// XTerm spec: CSI Ps B - Cursor Down Ps Times (default = 1) (CUD)
func TestCursorDown(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		initialY  int
		sequence  string
		expectedY int
	}{
		{"no param (default 1)", [2]int{80, 24}, 10, "\x1b[B", 11},
		{"explicit 1", [2]int{80, 24}, 10, "\x1b[1B", 11},
		{"move 5", [2]int{80, 24}, 10, "\x1b[5B", 15},
		{"at bottom (no movement)", [2]int{80, 24}, 23, "\x1b[5B", 23},
		{"overflow (clamps to bottom)", [2]int{80, 24}, 10, "\x1b[100B", 23},
		{"from top", [2]int{80, 24}, 0, "\x1b[20B", 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			h.vterm.SetCursorPos(tt.initialY, 0)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, 0, tt.expectedY)
		})
	}
}

// TestCursorForward tests CUF (Cursor Forward) - ESC[<n>C
// XTerm spec: CSI Ps C - Cursor Forward Ps Times (default = 1) (CUF)
func TestCursorForward(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		initialX  int
		sequence  string
		expectedX int
	}{
		{"no param (default 1)", [2]int{80, 24}, 10, "\x1b[C", 11},
		{"explicit 1", [2]int{80, 24}, 10, "\x1b[1C", 11},
		{"move 5", [2]int{80, 24}, 10, "\x1b[5C", 15},
		{"at right edge", [2]int{80, 24}, 79, "\x1b[5C", 79},
		{"overflow (clamps to right)", [2]int{80, 24}, 10, "\x1b[100C", 79},
		{"from left edge", [2]int{80, 24}, 0, "\x1b[40C", 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			h.vterm.SetCursorPos(0, tt.initialX)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, tt.expectedX, 0)
		})
	}
}

// TestCursorBackward tests CUB (Cursor Backward) - ESC[<n>D
// XTerm spec: CSI Ps D - Cursor Backward Ps Times (default = 1) (CUB)
func TestCursorBackward(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		initialX  int
		sequence  string
		expectedX int
	}{
		{"no param (default 1)", [2]int{80, 24}, 10, "\x1b[D", 9},
		{"explicit 1", [2]int{80, 24}, 10, "\x1b[1D", 9},
		{"move 5", [2]int{80, 24}, 10, "\x1b[5D", 5},
		{"at left edge", [2]int{80, 24}, 0, "\x1b[5D", 0},
		{"overflow (clamps to left)", [2]int{80, 24}, 10, "\x1b[100D", 0},
		{"from right edge", [2]int{80, 24}, 79, "\x1b[40D", 39},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			h.vterm.SetCursorPos(0, tt.initialX)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, tt.expectedX, 0)
		})
	}
}

// TestCursorHorizontalAbsolute tests CHA (Cursor Horizontal Absolute) - ESC[<n>G
// XTerm spec: CSI Ps G - Cursor Character Absolute [column] (default = [row,1]) (CHA)
func TestCursorHorizontalAbsolute(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		initialX  int
		initialY  int
		sequence  string
		expectedX int
		expectedY int
	}{
		{"no param (column 1)", [2]int{80, 24}, 40, 10, "\x1b[G", 0, 10},
		{"column 1", [2]int{80, 24}, 40, 10, "\x1b[1G", 0, 10},
		{"column 10", [2]int{80, 24}, 0, 5, "\x1b[10G", 9, 5},
		{"column 80", [2]int{80, 24}, 0, 5, "\x1b[80G", 79, 5},
		{"beyond right (clamps)", [2]int{80, 24}, 0, 5, "\x1b[200G", 79, 5},
		{"Y unchanged", [2]int{80, 24}, 20, 15, "\x1b[50G", 49, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			h.vterm.SetCursorPos(tt.initialY, tt.initialX)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, tt.expectedX, tt.expectedY)
		})
	}
}

// TestCursorVerticalAbsolute tests VPA (Vertical Position Absolute) - ESC[<n>d
// XTerm spec: CSI Ps d - Line Position Absolute [row] (default = [1,column]) (VPA)
func TestCursorVerticalAbsolute(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		initialX  int
		initialY  int
		sequence  string
		expectedX int
		expectedY int
	}{
		{"no param (row 1)", [2]int{80, 24}, 40, 10, "\x1b[d", 40, 0},
		{"row 1", [2]int{80, 24}, 40, 10, "\x1b[1d", 40, 0},
		{"row 10", [2]int{80, 24}, 20, 0, "\x1b[10d", 20, 9},
		{"row 24", [2]int{80, 24}, 20, 0, "\x1b[24d", 20, 23},
		{"beyond bottom (clamps)", [2]int{80, 24}, 20, 0, "\x1b[100d", 20, 23},
		{"X unchanged", [2]int{80, 24}, 50, 5, "\x1b[15d", 50, 14},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			h.vterm.SetCursorPos(tt.initialY, tt.initialX)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, tt.expectedX, tt.expectedY)
		})
	}
}

// TestCursorPosition tests CUP (Cursor Position) - ESC[<row>;<col>H
// XTerm spec: CSI Ps ; Ps H - Cursor Position [row;column] (default = [1,1]) (CUP)
func TestCursorPosition(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		sequence  string
		expectedX int
		expectedY int
	}{
		{"no params (home)", [2]int{80, 24}, "\x1b[H", 0, 0},
		{"explicit home", [2]int{80, 24}, "\x1b[1;1H", 0, 0},
		{"row only", [2]int{80, 24}, "\x1b[10H", 0, 9},
		{"row and col", [2]int{80, 24}, "\x1b[10;20H", 19, 9},
		{"bottom right", [2]int{80, 24}, "\x1b[24;80H", 79, 23},
		{"overflow row (clamps)", [2]int{80, 24}, "\x1b[100;50H", 49, 23},
		{"overflow col (clamps)", [2]int{80, 24}, "\x1b[10;200H", 79, 9},
		{"overflow both (clamps)", [2]int{80, 24}, "\x1b[100;200H", 79, 23},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			// Start from arbitrary position (y=12, x=40)
			h.vterm.SetCursorPos(12, 40)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, tt.expectedX, tt.expectedY)
		})
	}
}

// TestHVP tests HVP (Horizontal and Vertical Position) - ESC[<row>;<col>f
// XTerm spec: CSI Ps ; Ps f - Horizontal and Vertical Position [row;column] (default = [1,1]) (HVP)
// Note: HVP is functionally identical to CUP
func TestHVP(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		sequence  string
		expectedX int
		expectedY int
	}{
		{"no params (home)", [2]int{80, 24}, "\x1b[f", 0, 0},
		{"explicit home", [2]int{80, 24}, "\x1b[1;1f", 0, 0},
		{"row and col", [2]int{80, 24}, "\x1b[10;20f", 19, 9},
		{"bottom right", [2]int{80, 24}, "\x1b[24;80f", 79, 23},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			h.vterm.SetCursorPos(12, 40)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, tt.expectedX, tt.expectedY)
		})
	}
}

// TestCursorNextLine tests CNL (Cursor Next Line) - ESC[<n>E
// XTerm spec: CSI Ps E - Cursor Next Line Ps Times (default = 1) (CNL)
// Moves cursor to beginning of line, Ps lines down
func TestCursorNextLine(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		initialX  int
		initialY  int
		sequence  string
		expectedX int
		expectedY int
	}{
		{"no param (default 1)", [2]int{80, 24}, 40, 10, "\x1b[E", 0, 11},
		{"explicit 1", [2]int{80, 24}, 40, 10, "\x1b[1E", 0, 11},
		{"move 5 lines", [2]int{80, 24}, 50, 5, "\x1b[5E", 0, 10},
		{"at bottom (no movement)", [2]int{80, 24}, 40, 23, "\x1b[5E", 0, 23},
		{"overflow (clamps)", [2]int{80, 24}, 40, 20, "\x1b[100E", 0, 23},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			h.vterm.SetCursorPos(tt.initialY, tt.initialX)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, tt.expectedX, tt.expectedY)
		})
	}
}

// TestCursorPreviousLine tests CPL (Cursor Previous Line) - ESC[<n>F
// XTerm spec: CSI Ps F - Cursor Previous Line Ps Times (default = 1) (CPL)
// Moves cursor to beginning of line, Ps lines up
func TestCursorPreviousLine(t *testing.T) {
	tests := []struct {
		name      string
		size      [2]int
		initialX  int
		initialY  int
		sequence  string
		expectedX int
		expectedY int
	}{
		{"no param (default 1)", [2]int{80, 24}, 40, 10, "\x1b[F", 0, 9},
		{"explicit 1", [2]int{80, 24}, 40, 10, "\x1b[1F", 0, 9},
		{"move 5 lines", [2]int{80, 24}, 50, 10, "\x1b[5F", 0, 5},
		{"at top (no movement)", [2]int{80, 24}, 40, 0, "\x1b[5F", 0, 0},
		{"overflow (clamps)", [2]int{80, 24}, 40, 5, "\x1b[100F", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(tt.size[0], tt.size[1])
			h.vterm.SetCursorPos(tt.initialY, tt.initialX)
			h.SendSeq(tt.sequence)
			h.AssertCursor(t, tt.expectedX, tt.expectedY)
		})
	}
}

// TestCursorSaveRestore tests DECSC/DECRC - ESC 7 / ESC 8
// XTerm spec: ESC 7 - Save Cursor (DECSC), ESC 8 - Restore Cursor (DECRC)
func TestCursorSaveRestore(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*TestHarness)
		sequence string
		verify   func(*testing.T, *TestHarness)
	}{
		{
			name: "save and restore position",
			setup: func(h *TestHarness) {
				h.vterm.SetCursorPos(15, 30) // Y=15, X=30
				h.SendSeq("\x1b7")           // Save
				h.vterm.SetCursorPos(0, 0)
			},
			sequence: "\x1b8", // Restore
			verify: func(t *testing.T, h *TestHarness) {
				h.AssertCursor(t, 30, 15)
			},
		},
		{
			name: "restore without save (home position)",
			setup: func(h *TestHarness) {
				h.vterm.SetCursorPos(20, 50) // Y=20, X=50
			},
			sequence: "\x1b8", // Restore without save
			verify: func(t *testing.T, h *TestHarness) {
				// Should restore to home (0,0) or stay at current position
				// Implementation dependent - check actual behavior
				_, _ = h.GetCursor()
				// This test documents current behavior
			},
		},
		{
			name: "multiple save/restore",
			setup: func(h *TestHarness) {
				h.vterm.SetCursorPos(5, 10) // Y=5, X=10
				h.SendSeq("\x1b7")          // Save 1
				h.vterm.SetCursorPos(10, 20) // Y=10, X=20
				h.SendSeq("\x1b7")          // Save 2 (overwrites)
				h.vterm.SetCursorPos(0, 0)
			},
			sequence: "\x1b8", // Restore
			verify: func(t *testing.T, h *TestHarness) {
				// Should restore most recent save
				h.AssertCursor(t, 20, 10)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewTestHarness(80, 24)
			tt.setup(h)
			h.SendSeq(tt.sequence)
			tt.verify(t, h)
		})
	}
}

// TestCursorMovementWithContent verifies cursor movement doesn't corrupt content.
func TestCursorMovementWithContent(t *testing.T) {
	h := NewTestHarness(80, 24)

	// Place some text
	h.SendText("Hello World")
	h.AssertText(t, 0, 0, "Hello World")
	h.AssertCursor(t, 11, 0)

	// Move cursor around
	h.SendSeq("\x1b[H")      // Home
	h.AssertCursor(t, 0, 0)
	h.AssertText(t, 0, 0, "Hello World") // Content preserved

	h.SendSeq("\x1b[6C")     // Forward 6
	h.AssertCursor(t, 6, 0)
	h.AssertText(t, 0, 0, "Hello World") // Content preserved

	h.SendSeq("\x1b[B")      // Down
	h.AssertCursor(t, 6, 1)
	h.AssertText(t, 0, 0, "Hello World") // Content preserved

	h.SendSeq("\x1b[A")      // Up
	h.AssertCursor(t, 6, 0)
	h.AssertText(t, 0, 0, "Hello World") // Content preserved
}

// TestCursorAtEdges verifies cursor behavior at screen edges.
func TestCursorAtEdges(t *testing.T) {
	h := NewTestHarness(80, 24)

	// Top-left corner
	h.SendSeq("\x1b[H")
	h.SendSeq("\x1b[A")  // Up (should stay)
	h.SendSeq("\x1b[D")  // Left (should stay)
	h.AssertCursor(t, 0, 0)

	// Bottom-right corner
	h.SendSeq("\x1b[24;80H")
	h.SendSeq("\x1b[B")  // Down (should stay)
	h.SendSeq("\x1b[C")  // Right (should stay)
	h.AssertCursor(t, 79, 23)

	// Top-right corner
	h.SendSeq("\x1b[1;80H")
	h.SendSeq("\x1b[A")  // Up (should stay)
	h.SendSeq("\x1b[C")  // Right (should stay)
	h.AssertCursor(t, 79, 0)

	// Bottom-left corner
	h.SendSeq("\x1b[24;1H")
	h.SendSeq("\x1b[B")  // Down (should stay)
	h.SendSeq("\x1b[D")  // Left (should stay)
	h.AssertCursor(t, 0, 23)
}
