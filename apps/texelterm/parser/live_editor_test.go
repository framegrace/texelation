// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"testing"
)

// Helper to create a LiveEditor with initial content
func newEditorWithContent(content string) *LiveEditor {
	e := NewLiveEditor()
	for _, r := range content {
		e.WriteChar(r, DefaultFG, DefaultBG, 0, false)
	}
	return e
}

// Helper to get line content as string
func editorContent(e *LiveEditor) string {
	var result string
	for _, cell := range e.line.Cells {
		if cell.Rune != 0 {
			result += string(cell.Rune)
		}
	}
	return result
}

// === Basic Editing Tests ===

func TestLiveEditor_NewEditor(t *testing.T) {
	e := NewLiveEditor()

	if e.Len() != 0 {
		t.Errorf("Expected empty line, got length %d", e.Len())
	}
	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected cursor at 0, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_WriteChar(t *testing.T) {
	e := NewLiveEditor()

	e.WriteChar('H', DefaultFG, DefaultBG, 0, false)
	e.WriteChar('i', DefaultFG, DefaultBG, 0, false)

	if editorContent(e) != "Hi" {
		t.Errorf("Expected 'Hi', got '%s'", editorContent(e))
	}
	if e.GetCursorOffset() != 2 {
		t.Errorf("Expected cursor at 2, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_WriteChar_InsertMode(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(2) // Position after "He"

	e.WriteChar('X', DefaultFG, DefaultBG, 0, true) // Insert 'X'

	if editorContent(e) != "HeXllo" {
		t.Errorf("Expected 'HeXllo', got '%s'", editorContent(e))
	}
	if e.GetCursorOffset() != 3 {
		t.Errorf("Expected cursor at 3, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_WriteChar_Overwrite(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(2) // Position at 'l'

	e.WriteChar('X', DefaultFG, DefaultBG, 0, false) // Overwrite

	if editorContent(e) != "HeXlo" {
		t.Errorf("Expected 'HeXlo', got '%s'", editorContent(e))
	}
}

func TestLiveEditor_DeleteChars(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(1) // Position at 'e'

	e.DeleteChars(2) // Delete 'el'

	if editorContent(e) != "Hlo" {
		t.Errorf("Expected 'Hlo', got '%s'", editorContent(e))
	}
	if e.GetCursorOffset() != 1 {
		t.Errorf("Expected cursor still at 1, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_DeleteChars_AtEnd(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(5) // At end

	e.DeleteChars(1) // Nothing to delete

	if editorContent(e) != "Hello" {
		t.Errorf("Expected 'Hello', got '%s'", editorContent(e))
	}
}

func TestLiveEditor_DeleteChars_MoreThanAvailable(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(3) // At 'l'

	e.DeleteChars(10) // Delete more than available

	if editorContent(e) != "Hel" {
		t.Errorf("Expected 'Hel', got '%s'", editorContent(e))
	}
}

func TestLiveEditor_EraseToEnd(t *testing.T) {
	e := newEditorWithContent("Hello World")
	e.SetCursorOffset(5) // After "Hello"

	e.EraseToEnd()

	if editorContent(e) != "Hello" {
		t.Errorf("Expected 'Hello', got '%s'", editorContent(e))
	}
	if e.GetCursorOffset() != 5 {
		t.Errorf("Expected cursor still at 5, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_EraseFromStart(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(2) // At 'l'

	e.EraseFromStart(DefaultFG, DefaultBG)

	// First 3 chars (0,1,2) should be spaces
	content := editorContent(e)
	if content != "   lo" {
		t.Errorf("Expected '   lo', got '%s'", content)
	}
}

func TestLiveEditor_EraseLine(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(3)

	e.EraseLine()

	if e.Len() != 0 {
		t.Errorf("Expected empty line, got length %d", e.Len())
	}
	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected cursor at 0, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_EraseChars(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(1)

	e.EraseChars(2, DefaultFG, DefaultBG) // Erase 'el' with spaces

	if editorContent(e) != "H  lo" {
		t.Errorf("Expected 'H  lo', got '%s'", editorContent(e))
	}
}

func TestLiveEditor_InsertChars(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(2)

	e.InsertChars(3, DefaultFG, DefaultBG)

	if editorContent(e) != "He   llo" {
		t.Errorf("Expected 'He   llo', got '%s'", editorContent(e))
	}
}

// === Cursor Movement Tests ===

func TestLiveEditor_SetCursorOffset(t *testing.T) {
	e := newEditorWithContent("Hello")

	e.SetCursorOffset(3)
	if e.GetCursorOffset() != 3 {
		t.Errorf("Expected cursor at 3, got %d", e.GetCursorOffset())
	}

	// Negative clamps to 0
	e.SetCursorOffset(-5)
	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected cursor at 0 after negative, got %d", e.GetCursorOffset())
	}

	// Beyond line length is allowed (void space)
	e.SetCursorOffset(100)
	if e.GetCursorOffset() != 100 {
		t.Errorf("Expected cursor at 100, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_MoveCursor(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(2)

	e.MoveCursor(2)
	if e.GetCursorOffset() != 4 {
		t.Errorf("Expected cursor at 4, got %d", e.GetCursorOffset())
	}

	e.MoveCursor(-3)
	if e.GetCursorOffset() != 1 {
		t.Errorf("Expected cursor at 1, got %d", e.GetCursorOffset())
	}

	// Moving past start clamps to 0
	e.MoveCursor(-10)
	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected cursor at 0, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_CarriageReturn(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(3)

	e.CarriageReturn()

	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected cursor at 0, got %d", e.GetCursorOffset())
	}
	// Content unchanged
	if editorContent(e) != "Hello" {
		t.Errorf("Expected content unchanged, got '%s'", editorContent(e))
	}
}

func TestLiveEditor_Backspace(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(3)

	moved := e.Backspace()

	if !moved {
		t.Error("Expected Backspace to return true")
	}
	if e.GetCursorOffset() != 2 {
		t.Errorf("Expected cursor at 2, got %d", e.GetCursorOffset())
	}
	// Content unchanged (Backspace only moves cursor)
	if editorContent(e) != "Hello" {
		t.Errorf("Expected content unchanged, got '%s'", editorContent(e))
	}
}

func TestLiveEditor_Backspace_AtStart(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(0)

	moved := e.Backspace()

	if moved {
		t.Error("Expected Backspace at start to return false")
	}
	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected cursor still at 0, got %d", e.GetCursorOffset())
	}
}

// === Physical Cursor Derivation Tests ===

func TestLiveEditor_GetPhysicalCursor_EmptyLine(t *testing.T) {
	e := NewLiveEditor()

	row, col := e.GetPhysicalCursor(80)

	if row != 0 || col != 0 {
		t.Errorf("Expected (0,0), got (%d,%d)", row, col)
	}
}

func TestLiveEditor_GetPhysicalCursor_SingleLine(t *testing.T) {
	e := newEditorWithContent("Hello")

	tests := []struct {
		offset      int
		expectedRow int
		expectedCol int
	}{
		{0, 0, 0},
		{1, 0, 1},
		{3, 0, 3},
		{5, 0, 5}, // At end (append position)
	}

	for _, tt := range tests {
		e.SetCursorOffset(tt.offset)
		row, col := e.GetPhysicalCursor(80)
		if row != tt.expectedRow || col != tt.expectedCol {
			t.Errorf("Offset %d: expected (%d,%d), got (%d,%d)",
				tt.offset, tt.expectedRow, tt.expectedCol, row, col)
		}
	}
}

func TestLiveEditor_GetPhysicalCursor_WrappedLine(t *testing.T) {
	// Create a line that wraps at width=10
	e := newEditorWithContent("Hello World!") // 12 chars

	tests := []struct {
		offset      int
		width       int
		expectedRow int
		expectedCol int
	}{
		{0, 10, 0, 0},   // Start of first row
		{5, 10, 0, 5},   // Middle of first row
		{9, 10, 0, 9},   // End of first row
		{10, 10, 1, 0},  // Start of second row (wrap boundary!)
		{11, 10, 1, 1},  // Middle of second row
		{12, 10, 1, 2},  // End (append position)
		{15, 10, 1, 5},  // Beyond content (void space)
		{20, 10, 2, 0},  // Third row (void space)
		{25, 10, 2, 5},  // Middle of third row (void space)
	}

	for _, tt := range tests {
		e.SetCursorOffset(tt.offset)
		row, col := e.GetPhysicalCursor(tt.width)
		if row != tt.expectedRow || col != tt.expectedCol {
			t.Errorf("Offset %d, width %d: expected (%d,%d), got (%d,%d)",
				tt.offset, tt.width, tt.expectedRow, tt.expectedCol, row, col)
		}
	}
}

func TestLiveEditor_GetPhysicalCursor_ExactWrapBoundary(t *testing.T) {
	// Line exactly fills first row
	e := newEditorWithContent("0123456789") // 10 chars at width=10

	tests := []struct {
		offset      int
		expectedRow int
		expectedCol int
	}{
		{9, 0, 9},   // Last char of first row
		{10, 1, 0},  // Wrap to second row (append position)
	}

	for _, tt := range tests {
		e.SetCursorOffset(tt.offset)
		row, col := e.GetPhysicalCursor(10)
		if row != tt.expectedRow || col != tt.expectedCol {
			t.Errorf("Offset %d: expected (%d,%d), got (%d,%d)",
				tt.offset, tt.expectedRow, tt.expectedCol, row, col)
		}
	}
}

func TestLiveEditor_SetCursorFromPhysical(t *testing.T) {
	e := newEditorWithContent("Hello World!") // 12 chars

	tests := []struct {
		physRow        int
		physCol        int
		width          int
		expectedOffset int
	}{
		{0, 0, 10, 0},
		{0, 5, 10, 5},
		{0, 9, 10, 9},
		{1, 0, 10, 10},
		{1, 1, 10, 11},
		{2, 5, 10, 25}, // Void space
	}

	for _, tt := range tests {
		e.SetCursorFromPhysical(tt.physRow, tt.physCol, tt.width)
		if e.GetCursorOffset() != tt.expectedOffset {
			t.Errorf("Physical (%d,%d) width %d: expected offset %d, got %d",
				tt.physRow, tt.physCol, tt.width, tt.expectedOffset, e.GetCursorOffset())
		}
	}
}

func TestLiveEditor_SetCursorFromPhysical_Clamping(t *testing.T) {
	e := NewLiveEditor()

	// Negative row clamps to 0
	e.SetCursorFromPhysical(-1, 5, 10)
	if e.GetCursorOffset() != 5 {
		t.Errorf("Expected offset 5, got %d", e.GetCursorOffset())
	}

	// Negative col clamps to 0
	e.SetCursorFromPhysical(0, -1, 10)
	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected offset 0, got %d", e.GetCursorOffset())
	}

	// Col >= width clamps to width-1
	e.SetCursorFromPhysical(0, 15, 10)
	if e.GetCursorOffset() != 9 {
		t.Errorf("Expected offset 9, got %d", e.GetCursorOffset())
	}
}

// === Round-Trip Tests (Logical → Physical → Logical) ===

func TestLiveEditor_CursorRoundTrip(t *testing.T) {
	e := newEditorWithContent("Hello World! This is a test.") // 28 chars
	width := 10

	// Test round-trip for various offsets
	for offset := 0; offset <= 30; offset++ {
		e.SetCursorOffset(offset)
		row, col := e.GetPhysicalCursor(width)
		e.SetCursorFromPhysical(row, col, width)

		if e.GetCursorOffset() != offset {
			t.Errorf("Round-trip failed for offset %d: got %d (via row=%d, col=%d)",
				offset, e.GetCursorOffset(), row, col)
		}
	}
}

// === Lifecycle Tests ===

func TestLiveEditor_Commit(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(3)

	committed := e.Commit()

	// Check committed line
	if committed.Len() != 5 {
		t.Errorf("Expected committed line length 5, got %d", committed.Len())
	}

	// Check editor is reset
	if e.Len() != 0 {
		t.Errorf("Expected empty line after commit, got length %d", e.Len())
	}
	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected cursor at 0 after commit, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_Clear(t *testing.T) {
	e := newEditorWithContent("Hello")
	e.SetCursorOffset(3)

	e.Clear()

	if e.Len() != 0 {
		t.Errorf("Expected empty line, got length %d", e.Len())
	}
	if e.GetCursorOffset() != 0 {
		t.Errorf("Expected cursor at 0, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_GetPhysicalLines(t *testing.T) {
	e := newEditorWithContent("Hello World!") // 12 chars

	physical := e.GetPhysicalLines(10)

	if len(physical) != 2 {
		t.Errorf("Expected 2 physical lines, got %d", len(physical))
	}

	// First line: 10 chars
	if len(physical[0].Cells) != 10 {
		t.Errorf("Expected first line with 10 cells, got %d", len(physical[0].Cells))
	}
	if physical[0].LogicalIndex != -1 {
		t.Errorf("Expected LogicalIndex -1, got %d", physical[0].LogicalIndex)
	}

	// Second line: 2 chars
	if len(physical[1].Cells) != 2 {
		t.Errorf("Expected second line with 2 cells, got %d", len(physical[1].Cells))
	}
}

// === Backspace Across Wrap Boundary Test ===

func TestLiveEditor_BackspaceAcrossWrap(t *testing.T) {
	// This tests the key bug scenario:
	// User types a long line that wraps, then presses backspace from the second row

	e := newEditorWithContent("Hello World!") // 12 chars, wraps at width 10
	width := 10

	// Cursor at start of second physical row (offset 10)
	e.SetCursorOffset(10)
	row, col := e.GetPhysicalCursor(width)
	if row != 1 || col != 0 {
		t.Errorf("Expected cursor at (1,0), got (%d,%d)", row, col)
	}

	// Backspace should move to offset 9 (end of first row)
	e.Backspace()
	row, col = e.GetPhysicalCursor(width)
	if row != 0 || col != 9 {
		t.Errorf("After backspace: expected cursor at (0,9), got (%d,%d)", row, col)
	}
	if e.GetCursorOffset() != 9 {
		t.Errorf("Expected offset 9, got %d", e.GetCursorOffset())
	}
}

// === Erase + Backspace Pattern (Bash readline behavior) ===

func TestLiveEditor_BashBackspacePattern(t *testing.T) {
	// Bash sends BS (backspace) followed by EL 0 (erase to end)
	// This should delete the character before the cursor

	e := newEditorWithContent("Hello")
	e.SetCursorOffset(5) // At end

	// Simulate: BS + EL 0
	e.Backspace()  // Move to offset 4
	e.EraseToEnd() // Truncate at offset 4

	if editorContent(e) != "Hell" {
		t.Errorf("Expected 'Hell', got '%s'", editorContent(e))
	}
	if e.GetCursorOffset() != 4 {
		t.Errorf("Expected cursor at 4, got %d", e.GetCursorOffset())
	}
}

func TestLiveEditor_BashBackspaceAcrossWrap(t *testing.T) {
	// Same bash pattern but crossing a wrap boundary
	e := newEditorWithContent("Hello World!") // 12 chars
	e.SetCursorOffset(10)                     // Start of second row at width=10

	// BS + EL 0 (delete char at offset 9)
	e.Backspace()  // Move to offset 9
	e.EraseToEnd() // Truncate at offset 9

	if editorContent(e) != "Hello Wor" {
		t.Errorf("Expected 'Hello Wor', got '%s'", editorContent(e))
	}

	// Verify physical position
	row, col := e.GetPhysicalCursor(10)
	if row != 0 || col != 9 {
		t.Errorf("Expected cursor at (0,9), got (%d,%d)", row, col)
	}
}

// === Tab Navigation Tests ===

func TestLiveEditor_Tab(t *testing.T) {
	e := newEditorWithContent("Hi")
	e.SetCursorOffset(1)

	// Tab stops at every 8 columns
	tabStops := make(map[int]bool)
	for i := 0; i < 80; i += 8 {
		tabStops[i] = true
	}

	e.Tab(tabStops, 80)

	// From column 1, next tab stop is at column 8
	// Delta = 8 - 1 = 7, new offset = 1 + 7 = 8
	if e.GetCursorOffset() != 8 {
		t.Errorf("Expected cursor at 8, got %d", e.GetCursorOffset())
	}
}

// === Extend to Offset Test ===

func TestLiveEditor_ExtendToOffset(t *testing.T) {
	e := newEditorWithContent("Hi") // 2 chars

	e.ExtendToOffset(5, DefaultFG, DefaultBG)

	if e.Len() != 6 { // 0-5 inclusive = 6 chars
		t.Errorf("Expected length 6, got %d", e.Len())
	}

	// Original content preserved
	if e.line.Cells[0].Rune != 'H' || e.line.Cells[1].Rune != 'i' {
		t.Error("Original content not preserved")
	}

	// New cells are spaces
	for i := 2; i < 6; i++ {
		if e.line.Cells[i].Rune != ' ' {
			t.Errorf("Expected space at position %d", i)
		}
	}
}
