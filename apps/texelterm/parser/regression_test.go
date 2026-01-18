// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/regression_test.go
// Summary: Regression tests for previously fixed bugs.
// These tests ensure that bugs found during debugging don't recur.

package parser

import (
	"strings"
	"testing"
)

// ============================================================================
// Wide Character (Emoji) Tests
// ============================================================================
// Wide characters like emoji should occupy 2 cells in the terminal.
// The first cell holds the character with Wide=true, the second cell is a
// placeholder (Rune=0) also with Wide=true.

// TestWideCharacter_BasicPlacement tests that wide characters occupy 2 cells.
func TestWideCharacter_BasicPlacement(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Write: A + wide emoji + B
	// 🎉 is U+1F389, UTF-8: F0 9F 8E 89
	p.Parse('A')
	for _, ch := range "🎉" {
		p.Parse(ch)
	}
	p.Parse('B')

	grid := v.Grid()

	// A should be at position 0
	if grid[0][0].Rune != 'A' {
		t.Errorf("Expected 'A' at (0,0), got %q", grid[0][0].Rune)
	}

	// Emoji should be at position 1
	if grid[0][1].Rune != '🎉' {
		t.Errorf("Expected '🎉' at (0,1), got %q (0x%X)", grid[0][1].Rune, grid[0][1].Rune)
	}

	// Check Wide flag
	if !grid[0][1].Wide {
		t.Error("Expected Wide=true for emoji cell at (0,1)")
	}

	// Position 2 should be the wide character continuation (placeholder)
	// It should have Rune=0 and Wide=true
	t.Logf("Position (0,2): rune=%q (0x%X), Wide=%v", grid[0][2].Rune, grid[0][2].Rune, grid[0][2].Wide)

	// B should be at position 3 (after the 2-cell wide character)
	if grid[0][3].Rune != 'B' {
		t.Errorf("Expected 'B' at (0,3), got %q - wide character may not be consuming 2 cells", grid[0][3].Rune)
	}

	// Cursor should be at x=4 (A=1 + emoji=2 + B=1)
	if v.cursorX != 4 {
		t.Errorf("Expected cursor at x=4, got x=%d", v.cursorX)
	}
}

// TestWideCharacter_MultipleEmoji tests multiple wide characters in sequence.
func TestWideCharacter_MultipleEmoji(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Write: 🎉🎊🎁 (3 emojis = 6 cells)
	for _, ch := range "🎉🎊🎁" {
		p.Parse(ch)
	}

	grid := v.Grid()

	// First emoji at 0
	if grid[0][0].Rune != '🎉' {
		t.Errorf("Expected '🎉' at (0,0), got %q", grid[0][0].Rune)
	}

	// Second emoji at 2
	if grid[0][2].Rune != '🎊' {
		t.Errorf("Expected '🎊' at (0,2), got %q", grid[0][2].Rune)
	}

	// Third emoji at 4
	if grid[0][4].Rune != '🎁' {
		t.Errorf("Expected '🎁' at (0,4), got %q", grid[0][4].Rune)
	}

	// Cursor should be at x=6
	if v.cursorX != 6 {
		t.Errorf("Expected cursor at x=6, got x=%d", v.cursorX)
	}
}

// TestWideCharacter_AtLineEnd tests wide character at end of line.
func TestWideCharacter_AtLineEnd(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Write 9 characters, then a wide character
	// The wide char needs 2 cells but only 1 remains on line
	for _, ch := range "123456789" {
		p.Parse(ch)
	}
	// Cursor at x=9, writing a 2-cell wide char should wrap
	for _, ch := range "🎉" {
		p.Parse(ch)
	}

	grid := v.Grid()
	t.Logf("Row 0: %s", cellsToStringTest(grid[0]))
	t.Logf("Row 1: %s", cellsToStringTest(grid[1]))

	// The emoji should either:
	// 1. Wrap to next line (starts at row 1, col 0)
	// 2. Or be placed with wrap handling
	// Check that cursor moved appropriately
	t.Logf("Cursor at (%d, %d)", v.cursorX, v.cursorY)
}

// ============================================================================
// Erase Line (EL) Tests
// ============================================================================
// EL 0 (ESC[K) should erase from cursor to end of the CURRENT line only.
// It should NOT affect content on other lines.

// TestEL0_OnlyErasesCurrentLine tests that EL 0 doesn't affect other lines.
func TestEL0_OnlyErasesCurrentLine(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Write 3 lines
	for _, ch := range "Line1-Content" {
		p.Parse(ch)
	}
	p.Parse('\r')
	p.Parse('\n')
	for _, ch := range "Line2-Content" {
		p.Parse(ch)
	}
	p.Parse('\r')
	p.Parse('\n')
	for _, ch := range "Line3-Content" {
		p.Parse(ch)
	}

	// Move cursor to row 2 (1-indexed), column 6
	// CSI 2;6H
	for _, ch := range "\x1b[2;6H" {
		p.Parse(ch)
	}

	// Verify cursor position
	if v.cursorY != 1 || v.cursorX != 5 {
		t.Logf("Cursor at (%d, %d), expected (5, 1)", v.cursorX, v.cursorY)
	}

	// Erase to end of line (EL 0)
	for _, ch := range "\x1b[K" {
		p.Parse(ch)
	}

	grid := v.Grid()

	// Line1 should be unchanged
	line1 := strings.TrimRight(cellsToStringTest(grid[0]), " ")
	if line1 != "Line1-Content" {
		t.Errorf("Line1 was modified: expected 'Line1-Content', got %q", line1)
	}

	// Line2 should have first 5 chars preserved, rest erased
	line2 := strings.TrimRight(cellsToStringTest(grid[1]), " ")
	if line2 != "Line2" {
		t.Errorf("Line2 incorrect: expected 'Line2', got %q", line2)
	}

	// Line3 should be unchanged
	line3 := strings.TrimRight(cellsToStringTest(grid[2]), " ")
	if line3 != "Line3-Content" {
		t.Errorf("Line3 was modified: expected 'Line3-Content', got %q", line3)
	}
}

// TestEL1_OnlyErasesCurrentLine tests that EL 1 doesn't affect other lines.
func TestEL1_OnlyErasesCurrentLine(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Write 3 lines
	for _, ch := range "Line1-Content" {
		p.Parse(ch)
	}
	p.Parse('\r')
	p.Parse('\n')
	for _, ch := range "Line2-Content" {
		p.Parse(ch)
	}
	p.Parse('\r')
	p.Parse('\n')
	for _, ch := range "Line3-Content" {
		p.Parse(ch)
	}

	// Move cursor to row 2 (1-indexed), column 8
	for _, ch := range "\x1b[2;8H" {
		p.Parse(ch)
	}

	// Erase from start of line to cursor (EL 1)
	for _, ch := range "\x1b[1K" {
		p.Parse(ch)
	}

	grid := v.Grid()

	// Line1 should be unchanged
	line1 := strings.TrimRight(cellsToStringTest(grid[0]), " ")
	if line1 != "Line1-Content" {
		t.Errorf("Line1 was modified: expected 'Line1-Content', got %q", line1)
	}

	// Line2: positions 0-7 erased (8 chars including cursor), rest preserved
	// "Line2-Content" (positions 0-12) -> positions 8-12 remain = "ntent"
	// CSI 2;8H moves to column 8 (1-indexed) = position 7 (0-indexed)
	// EL 1 erases from start to cursor inclusive (positions 0-7)
	line2 := cellsToStringTest(grid[1])
	t.Logf("Line2 after EL 1: %q", line2)

	// First 8 characters (positions 0-7) should be spaces
	for i := 0; i < 8; i++ {
		if grid[1][i].Rune != ' ' && grid[1][i].Rune != 0 {
			t.Errorf("Position %d should be erased, got %q", i, grid[1][i].Rune)
		}
	}

	// "ntent" should remain (positions 8-12)
	remaining := strings.TrimLeft(strings.TrimRight(line2, " "), " ")
	if remaining != "ntent" {
		t.Errorf("Expected 'ntent' to remain, got %q", remaining)
	}

	// Line3 should be unchanged
	line3 := strings.TrimRight(cellsToStringTest(grid[2]), " ")
	if line3 != "Line3-Content" {
		t.Errorf("Line3 was modified: expected 'Line3-Content', got %q", line3)
	}
}

// TestEL2_OnlyErasesCurrentLine tests that EL 2 doesn't affect other lines.
func TestEL2_OnlyErasesCurrentLine(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Write 3 lines
	for _, ch := range "Line1-Content" {
		p.Parse(ch)
	}
	p.Parse('\r')
	p.Parse('\n')
	for _, ch := range "Line2-Content" {
		p.Parse(ch)
	}
	p.Parse('\r')
	p.Parse('\n')
	for _, ch := range "Line3-Content" {
		p.Parse(ch)
	}

	// Move cursor to row 2
	for _, ch := range "\x1b[2;5H" {
		p.Parse(ch)
	}

	// Erase entire line (EL 2)
	for _, ch := range "\x1b[2K" {
		p.Parse(ch)
	}

	grid := v.Grid()

	// Line1 should be unchanged
	line1 := strings.TrimRight(cellsToStringTest(grid[0]), " ")
	if line1 != "Line1-Content" {
		t.Errorf("Line1 was modified: expected 'Line1-Content', got %q", line1)
	}

	// Line2 should be entirely blank
	line2 := strings.TrimRight(cellsToStringTest(grid[1]), " ")
	if line2 != "" {
		t.Errorf("Line2 should be blank, got %q", line2)
	}

	// Line3 should be unchanged
	line3 := strings.TrimRight(cellsToStringTest(grid[2]), " ")
	if line3 != "Line3-Content" {
		t.Errorf("Line3 was modified: expected 'Line3-Content', got %q", line3)
	}
}

// ============================================================================
// Erase Display (ED) Tests
// ============================================================================

// TestED0_EraseToEndOfScreen tests ED 0 erases from cursor to end of screen.
func TestED0_EraseToEndOfScreen(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Fill screen with X's
	for row := 0; row < 5; row++ {
		for col := 0; col < 10; col++ {
			p.Parse('X')
		}
		if row < 4 {
			p.Parse('\r')
			p.Parse('\n')
		}
	}

	// Move to row 3, col 5 (1-indexed)
	for _, ch := range "\x1b[3;5H" {
		p.Parse(ch)
	}

	// Erase from cursor to end of screen (ED 0)
	for _, ch := range "\x1b[J" {
		p.Parse(ch)
	}

	grid := v.Grid()

	// Rows 0-1 should be unchanged (all X's)
	for row := 0; row < 2; row++ {
		for col := 0; col < 10; col++ {
			if grid[row][col].Rune != 'X' {
				t.Errorf("Row %d col %d should be 'X', got %q", row, col, grid[row][col].Rune)
			}
		}
	}

	// Row 2: cols 0-3 should be X, cols 4+ should be space
	for col := 0; col < 4; col++ {
		if grid[2][col].Rune != 'X' {
			t.Errorf("Row 2 col %d should be 'X', got %q", col, grid[2][col].Rune)
		}
	}
	for col := 4; col < 10; col++ {
		if grid[2][col].Rune != ' ' && grid[2][col].Rune != 0 {
			t.Errorf("Row 2 col %d should be erased, got %q", col, grid[2][col].Rune)
		}
	}

	// Rows 3-4 should be entirely erased
	for row := 3; row < 5; row++ {
		for col := 0; col < 10; col++ {
			if grid[row][col].Rune != ' ' && grid[row][col].Rune != 0 {
				t.Errorf("Row %d col %d should be erased, got %q", row, col, grid[row][col].Rune)
			}
		}
	}
}

// TestED1_EraseFromStartOfScreen tests ED 1 erases from start of screen to cursor.
func TestED1_EraseFromStartOfScreen(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Fill screen with X's
	for row := 0; row < 5; row++ {
		for col := 0; col < 10; col++ {
			p.Parse('X')
		}
		if row < 4 {
			p.Parse('\r')
			p.Parse('\n')
		}
	}

	// Move to row 3, col 5 (1-indexed)
	for _, ch := range "\x1b[3;5H" {
		p.Parse(ch)
	}

	// Erase from start of screen to cursor (ED 1)
	for _, ch := range "\x1b[1J" {
		p.Parse(ch)
	}

	grid := v.Grid()

	// Rows 0-1 should be entirely erased
	for row := 0; row < 2; row++ {
		for col := 0; col < 10; col++ {
			if grid[row][col].Rune != ' ' && grid[row][col].Rune != 0 {
				t.Errorf("Row %d col %d should be erased, got %q", row, col, grid[row][col].Rune)
			}
		}
	}

	// Row 2: cols 0-4 should be erased, cols 5+ should be X
	for col := 0; col <= 4; col++ {
		if grid[2][col].Rune != ' ' && grid[2][col].Rune != 0 {
			t.Errorf("Row 2 col %d should be erased, got %q", col, grid[2][col].Rune)
		}
	}
	for col := 5; col < 10; col++ {
		if grid[2][col].Rune != 'X' {
			t.Errorf("Row 2 col %d should be 'X', got %q", col, grid[2][col].Rune)
		}
	}

	// Rows 3-4 should be unchanged (all X's)
	for row := 3; row < 5; row++ {
		for col := 0; col < 10; col++ {
			if grid[row][col].Rune != 'X' {
				t.Errorf("Row %d col %d should be 'X', got %q", row, col, grid[row][col].Rune)
			}
		}
	}
}

// TestED2_EraseEntireScreen tests ED 2 erases entire visible screen.
func TestED2_EraseEntireScreen(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Fill screen with X's
	for row := 0; row < 5; row++ {
		for col := 0; col < 10; col++ {
			p.Parse('X')
		}
		if row < 4 {
			p.Parse('\r')
			p.Parse('\n')
		}
	}

	// Move somewhere in the middle
	for _, ch := range "\x1b[3;5H" {
		p.Parse(ch)
	}

	// Erase entire screen (ED 2)
	for _, ch := range "\x1b[2J" {
		p.Parse(ch)
	}

	grid := v.Grid()

	// Entire screen should be erased
	for row := 0; row < 5; row++ {
		for col := 0; col < 10; col++ {
			if grid[row][col].Rune != ' ' && grid[row][col].Rune != 0 {
				t.Errorf("Row %d col %d should be erased, got %q", row, col, grid[row][col].Rune)
			}
		}
	}
}

// ============================================================================
// ECH (Erase Characters) and DCH (Delete Characters) Tests
// ============================================================================

// TestECH_EraseCharacters tests ECH replaces characters with spaces.
func TestECH_EraseCharacters(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Write ABCDEFGHIJ
	for _, ch := range "ABCDEFGHIJ" {
		p.Parse(ch)
	}

	// Move to column 4 (1-indexed) - at 'D'
	for _, ch := range "\x1b[1;4H" {
		p.Parse(ch)
	}

	// Erase 3 characters (ECH 3)
	for _, ch := range "\x1b[3X" {
		p.Parse(ch)
	}

	grid := v.Grid()
	line := cellsToStringTest(grid[0])
	t.Logf("After ECH 3: %q", line)

	// Expected: "ABC   GHIJ" (D,E,F replaced with spaces)
	if grid[0][3].Rune != ' ' {
		t.Errorf("Position 3 should be space, got %q", grid[0][3].Rune)
	}
	if grid[0][4].Rune != ' ' {
		t.Errorf("Position 4 should be space, got %q", grid[0][4].Rune)
	}
	if grid[0][5].Rune != ' ' {
		t.Errorf("Position 5 should be space, got %q", grid[0][5].Rune)
	}
	if grid[0][6].Rune != 'G' {
		t.Errorf("Position 6 should be 'G', got %q", grid[0][6].Rune)
	}
}

// TestDCH_DeleteCharacters tests DCH removes characters and shifts left.
func TestDCH_DeleteCharacters(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Write ABCDEFGHIJ
	for _, ch := range "ABCDEFGHIJ" {
		p.Parse(ch)
	}

	// Move to column 4 (1-indexed) - at 'D'
	for _, ch := range "\x1b[1;4H" {
		p.Parse(ch)
	}

	// Delete 3 characters (DCH 3)
	for _, ch := range "\x1b[3P" {
		p.Parse(ch)
	}

	grid := v.Grid()
	line := strings.TrimRight(cellsToStringTest(grid[0]), " ")
	t.Logf("After DCH 3: %q", line)

	// Expected: "ABCGHIJ" (DEF deleted, GHIJ shifted left)
	if line != "ABCGHIJ" {
		t.Errorf("Expected 'ABCGHIJ', got %q", line)
	}
}
