// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"testing"
)

func TestExtractText_Empty(t *testing.T) {
	text := ExtractText(nil)
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}

	text = ExtractText([]Cell{})
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}
}

func TestExtractText_SimpleText(t *testing.T) {
	cells := textExtractorMakeCells("hello world")
	text := ExtractText(cells)
	if text != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", text)
	}
}

func TestExtractText_WithAttributes(t *testing.T) {
	// Text with bold and color attributes should still extract plain text
	cells := []Cell{
		{Rune: 'h', FG: Color{Mode: ColorModeStandard, Value: 1}, Attr: AttrBold},
		{Rune: 'e', FG: Color{Mode: ColorModeStandard, Value: 2}},
		{Rune: 'l', BG: Color{Mode: ColorMode256, Value: 100}},
		{Rune: 'l', Attr: AttrUnderline},
		{Rune: 'o'},
	}
	text := ExtractText(cells)
	if text != "hello" {
		t.Errorf("expected %q, got %q", "hello", text)
	}
}

func TestExtractText_TrailingSpaces(t *testing.T) {
	cells := textExtractorMakeCells("hello   ")
	text := ExtractText(cells)
	if text != "hello" {
		t.Errorf("expected %q (trimmed), got %q", "hello", text)
	}
}

func TestExtractText_TrailingTabs(t *testing.T) {
	cells := textExtractorMakeCells("hello\t\t")
	text := ExtractText(cells)
	if text != "hello" {
		t.Errorf("expected %q (trimmed), got %q", "hello", text)
	}
}

func TestExtractText_InternalSpaces(t *testing.T) {
	// Internal spaces should be preserved
	cells := textExtractorMakeCells("hello   world")
	text := ExtractText(cells)
	if text != "hello   world" {
		t.Errorf("expected %q, got %q", "hello   world", text)
	}
}

func TestExtractText_NullCells(t *testing.T) {
	// Null runes (empty cells) should be skipped
	cells := []Cell{
		{Rune: 'h'},
		{Rune: 0}, // null
		{Rune: 'i'},
	}
	text := ExtractText(cells)
	if text != "hi" {
		t.Errorf("expected %q, got %q", "hi", text)
	}
}

func TestExtractText_ControlCharacters(t *testing.T) {
	// Control characters should be skipped (except space/tab)
	cells := []Cell{
		{Rune: 'h'},
		{Rune: '\x07'}, // BEL
		{Rune: 'i'},
		{Rune: '\x1b'}, // ESC
		{Rune: '!'},
	}
	text := ExtractText(cells)
	if text != "hi!" {
		t.Errorf("expected %q, got %q", "hi!", text)
	}
}

func TestExtractText_SpaceAndTab(t *testing.T) {
	// Space and tab should be preserved (they're control chars but useful)
	cells := textExtractorMakeCells("hello\tworld test")
	text := ExtractText(cells)
	if text != "hello\tworld test" {
		t.Errorf("expected %q, got %q", "hello\tworld test", text)
	}
}

func TestExtractText_Unicode(t *testing.T) {
	// Unicode characters should be preserved
	cells := textExtractorMakeCells("hello 世界 🚀")
	text := ExtractText(cells)
	if text != "hello 世界 🚀" {
		t.Errorf("expected %q, got %q", "hello 世界 🚀", text)
	}
}

func TestExtractText_WideCharacter(t *testing.T) {
	// Wide characters like CJK occupy 2 cells
	// First cell has the rune, second cell typically has rune=0
	cells := []Cell{
		{Rune: '你', Wide: true},
		{Rune: 0, Wide: false}, // continuation
		{Rune: '好', Wide: true},
		{Rune: 0, Wide: false}, // continuation
	}
	text := ExtractText(cells)
	if text != "你好" {
		t.Errorf("expected %q, got %q", "你好", text)
	}
}

func TestExtractTextFromLine(t *testing.T) {
	line := &LogicalLine{
		Cells: textExtractorMakeCells("test line"),
	}
	text := ExtractTextFromLine(line)
	if text != "test line" {
		t.Errorf("expected %q, got %q", "test line", text)
	}
}

func TestExtractTextFromLine_Nil(t *testing.T) {
	text := ExtractTextFromLine(nil)
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}
}

func TestExtractText_CommandLine(t *testing.T) {
	// Typical command with prompt: "$ docker run nginx"
	cells := textExtractorMakeCells("$ docker run nginx")
	text := ExtractText(cells)
	if text != "$ docker run nginx" {
		t.Errorf("expected %q, got %q", "$ docker run nginx", text)
	}
}

func TestExtractText_OutputLine(t *testing.T) {
	// Typical output: colored and formatted
	cells := []Cell{
		{Rune: 'E', FG: Color{Mode: ColorModeStandard, Value: 1}, Attr: AttrBold}, // red bold
		{Rune: 'R'},
		{Rune: 'R'},
		{Rune: 'O'},
		{Rune: 'R'},
		{Rune: ':'},
		{Rune: ' '},
		{Rune: 'f'},
		{Rune: 'a'},
		{Rune: 'i'},
		{Rune: 'l'},
		{Rune: 'e'},
		{Rune: 'd'},
	}
	text := ExtractText(cells)
	if text != "ERROR: failed" {
		t.Errorf("expected %q, got %q", "ERROR: failed", text)
	}
}

// Helper to create cells from a string
func textExtractorMakeCells(s string) []Cell {
	cells := make([]Cell, 0, len(s))
	for _, r := range s {
		cells = append(cells, Cell{Rune: r, FG: DefaultFG, BG: DefaultBG})
	}
	return cells
}
