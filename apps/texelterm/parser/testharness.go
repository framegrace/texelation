// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/testharness.go
// Summary: Test harness for VTerm control sequence testing.
// Usage: Used by test files to send sequences and verify buffer state.
// Notes: Provides helpers for systematic testing of all control sequences.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// TestHarness provides utilities for testing VTerm control sequences.
type TestHarness struct {
	vterm  *VTerm
	parser *Parser
}

// NewTestHarness creates a new test harness with specified terminal size.
func NewTestHarness(width, height int, opts ...Option) *TestHarness {
	vterm := NewVTerm(width, height, opts...)
	parser := NewParser(vterm)
	return &TestHarness{
		vterm:  vterm,
		parser: parser,
	}
}

// SendSeq sends a control sequence string to the parser.
// Example: h.SendSeq("\x1b[5A") sends "cursor up 5"
func (h *TestHarness) SendSeq(seq string) {
	for _, r := range seq {
		h.parser.Parse(r)
	}
}

// SendText sends printable text (no control sequences).
func (h *TestHarness) SendText(text string) {
	for _, r := range text {
		h.vterm.placeChar(r)
	}
}

// GetCell returns the cell at the specified position.
// Position is relative to current viewport (0-based).
func (h *TestHarness) GetCell(x, y int) Cell {
	topLine := h.vterm.getTopHistoryLine()
	historyLine := topLine + y

	if historyLine < 0 || historyLine >= h.vterm.HistoryLength() {
		return Cell{} // out of bounds
	}

	line := h.vterm.HistoryLineCopy(historyLine)
	if x < 0 || x >= len(line) {
		return Cell{} // out of bounds
	}

	return line[x]
}

// GetCursor returns the current cursor position (0-based).
func (h *TestHarness) GetCursor() (x, y int) {
	return h.vterm.GetCursorX(), h.vterm.GetCursorY()
}

// GetCurrentAttr returns the current text attributes.
func (h *TestHarness) GetCurrentAttr() Attribute {
	return h.vterm.currentAttr
}

// GetScrollRegion returns the current scrolling region (0-based, inclusive).
func (h *TestHarness) GetScrollRegion() (top, bottom int) {
	return h.vterm.marginTop, h.vterm.marginBottom
}

// GetSize returns the terminal size.
func (h *TestHarness) GetSize() (width, height int) {
	return h.vterm.width, h.vterm.height
}

// GetHistoryLength returns the number of lines in history buffer.
func (h *TestHarness) GetHistoryLength() int {
	return h.vterm.HistoryLength()
}

// AssertCell verifies that a cell matches the expected value.
func (h *TestHarness) AssertCell(t *testing.T, x, y int, expected Cell) {
	t.Helper()
	actual := h.GetCell(x, y)

	if actual.Rune != expected.Rune {
		t.Errorf("Cell[%d,%d] rune: expected %q, got %q", x, y, expected.Rune, actual.Rune)
	}

	// Only check style fields if expected has them set
	if expected.FG.Mode != ColorModeDefault && actual.FG != expected.FG {
		t.Errorf("Cell[%d,%d] FG: expected %+v, got %+v", x, y, expected.FG, actual.FG)
	}
	if expected.BG.Mode != ColorModeDefault && actual.BG != expected.BG {
		t.Errorf("Cell[%d,%d] BG: expected %+v, got %+v", x, y, expected.BG, actual.BG)
	}
	if expected.Attr&AttrBold != 0 && actual.Attr&AttrBold == 0 {
		t.Errorf("Cell[%d,%d] should be bold", x, y)
	}
	if expected.Attr&AttrUnderline != 0 && actual.Attr&AttrUnderline == 0 {
		t.Errorf("Cell[%d,%d] should be underlined", x, y)
	}
	if expected.Attr&AttrReverse != 0 && actual.Attr&AttrReverse == 0 {
		t.Errorf("Cell[%d,%d] should be reverse", x, y)
	}
}

// AssertRune verifies that a cell contains the expected rune (ignores style).
func (h *TestHarness) AssertRune(t *testing.T, x, y int, expectedRune rune) {
	t.Helper()
	actual := h.GetCell(x, y)
	if actual.Rune != expectedRune {
		t.Errorf("Cell[%d,%d] rune: expected %q, got %q", x, y, expectedRune, actual.Rune)
	}
}

// AssertText verifies a sequence of cells matches expected text.
func (h *TestHarness) AssertText(t *testing.T, x, y int, expectedText string) {
	t.Helper()
	for i, expectedRune := range expectedText {
		h.AssertRune(t, x+i, y, expectedRune)
	}
}

// AssertCursor verifies the cursor is at the expected position.
func (h *TestHarness) AssertCursor(t *testing.T, expectedX, expectedY int) {
	t.Helper()
	actualX, actualY := h.GetCursor()
	if actualX != expectedX || actualY != expectedY {
		t.Errorf("Cursor position: expected (%d,%d), got (%d,%d)",
			expectedX, expectedY, actualX, actualY)
	}
}

// AssertScrollRegion verifies the scrolling region matches expected values.
func (h *TestHarness) AssertScrollRegion(t *testing.T, expectedTop, expectedBottom int) {
	t.Helper()
	actualTop, actualBottom := h.GetScrollRegion()
	if actualTop != expectedTop || actualBottom != expectedBottom {
		t.Errorf("Scroll region: expected [%d,%d], got [%d,%d]",
			expectedTop, expectedBottom, actualTop, actualBottom)
	}
}

// AssertBlank verifies that a cell is blank (space or null rune).
func (h *TestHarness) AssertBlank(t *testing.T, x, y int) {
	t.Helper()
	actual := h.GetCell(x, y)
	if actual.Rune != 0 && actual.Rune != ' ' {
		t.Errorf("Cell[%d,%d] should be blank, got %q", x, y, actual.Rune)
	}
}

// AssertLineBlank verifies an entire line is blank.
func (h *TestHarness) AssertLineBlank(t *testing.T, y int) {
	t.Helper()
	width, _ := h.GetSize()
	for x := 0; x < width; x++ {
		h.AssertBlank(t, x, y)
	}
}

// Dump returns a visual representation of the terminal buffer for debugging.
// Shows all visible lines with cursor position marked.
func (h *TestHarness) Dump() string {
	width, height := h.GetSize()
	cursorX, cursorY := h.GetCursor()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Terminal %dx%d (cursor at %d,%d)\n", width, height, cursorX, cursorY))
	sb.WriteString(strings.Repeat("=", width) + "\n")

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := h.GetCell(x, y)
			if x == cursorX && y == cursorY {
				sb.WriteString("[") // Mark cursor position
			} else if cell.Rune == 0 {
				sb.WriteString(" ")
			} else {
				sb.WriteRune(cell.Rune)
			}
		}
		sb.WriteString(fmt.Sprintf(" |%d\n", y))
	}

	sb.WriteString(strings.Repeat("=", width) + "\n")
	return sb.String()
}

// DumpWithHistory dumps terminal buffer including scrollback history.
func (h *TestHarness) DumpWithHistory() string {
	width, _ := h.GetSize()
	histLen := h.GetHistoryLength()
	cursorX, cursorY := h.GetCursor()
	topLine := h.vterm.getTopHistoryLine()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Terminal with history (total %d lines, cursor at %d,%d, top line=%d)\n",
		histLen, cursorX, cursorY, topLine))
	sb.WriteString(strings.Repeat("=", width) + "\n")

	for i := 0; i < histLen; i++ {
		line := h.vterm.HistoryLineCopy(i)
		for x := 0; x < width && x < len(line); x++ {
			// Mark cursor position if this is the cursor line
			if i == topLine+cursorY && x == cursorX {
				sb.WriteString("[")
			} else if line[x].Rune == 0 {
				sb.WriteString(" ")
			} else {
				sb.WriteRune(line[x].Rune)
			}
		}

		// Indicate if line is wrapped
		wrapped := ""
		if len(line) > 0 && line[len(line)-1].Wrapped {
			wrapped = " (wrapped)"
		}

		// Mark viewport range
		marker := " "
		if i >= topLine && i < topLine+h.vterm.height {
			marker = "*" // In viewport
		}

		sb.WriteString(fmt.Sprintf(" |%d%s%s\n", i, marker, wrapped))
	}

	sb.WriteString(strings.Repeat("=", width) + "\n")
	return sb.String()
}

// Clear clears the terminal by resetting it completely.
func (h *TestHarness) Clear() {
	// For tests, we want a true reset of the terminal state
	// Use RIS (Reset to Initial State) instead of just ED 2
	h.Reset()
}

// Reset resets the terminal to initial state (ESC c).
func (h *TestHarness) Reset() {
	h.SendSeq("\x1bc")
}

// FillWithPattern fills the terminal with a test pattern.
// Useful for setting up known initial state.
func (h *TestHarness) FillWithPattern(pattern string) {
	width, height := h.GetSize()
	h.Clear()

	// Fill each line completely - cursor will auto-wrap at width
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) % len(pattern)
			h.vterm.placeChar(rune(pattern[idx]))
		}
	}

	// Reset cursor to home
	h.SendSeq("\x1b[H")
}
