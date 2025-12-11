// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"testing"
)

func TestVTerm_DisplayBufferInit(t *testing.T) {
	v := NewVTerm(80, 24)

	// Initially disabled
	if v.IsDisplayBufferEnabled() {
		t.Error("display buffer should be disabled by default")
	}

	// Enable
	v.EnableDisplayBuffer()
	if !v.IsDisplayBufferEnabled() {
		t.Error("display buffer should be enabled after EnableDisplayBuffer()")
	}

	// Disable
	v.DisableDisplayBuffer()
	if v.IsDisplayBufferEnabled() {
		t.Error("display buffer should be disabled after DisableDisplayBuffer()")
	}
}

func TestVTerm_WithDisplayBufferOption(t *testing.T) {
	// Test enabling via option
	v := NewVTerm(80, 24, WithDisplayBuffer(true))
	if !v.IsDisplayBufferEnabled() {
		t.Error("display buffer should be enabled when WithDisplayBuffer(true) is passed")
	}

	// Test disabling via option (default behavior)
	v2 := NewVTerm(80, 24, WithDisplayBuffer(false))
	if v2.IsDisplayBufferEnabled() {
		t.Error("display buffer should be disabled when WithDisplayBuffer(false) is passed")
	}
}

func TestVTerm_DisplayBufferPlaceChar(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write "Hello"
	for _, r := range "Hello" {
		v.placeChar(r)
	}

	// Check the current logical line has the content
	line := v.displayBufferGetCurrentLine()
	if line == nil {
		t.Fatal("current line should not be nil")
	}
	if line.Len() != 5 {
		t.Errorf("expected line length 5, got %d", line.Len())
	}

	got := cellsToString(line.Cells)
	if got != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", got)
	}
}

func TestVTerm_DisplayBufferLineFeed(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write "Line1" then newline
	for _, r := range "Line1" {
		v.placeChar(r)
	}
	v.LineFeed()

	// History should have one committed line
	histLen := v.displayBufferHistoryLen()
	if histLen != 1 {
		t.Errorf("expected 1 line in history, got %d", histLen)
	}

	// Current line should be empty (new line)
	line := v.displayBufferGetCurrentLine()
	if line.Len() != 0 {
		t.Errorf("expected empty current line after LF, got len %d", line.Len())
	}

	// Write "Line2"
	for _, r := range "Line2" {
		v.placeChar(r)
	}
	v.LineFeed()

	// Now should have 2 lines in history
	histLen = v.displayBufferHistoryLen()
	if histLen != 2 {
		t.Errorf("expected 2 lines in history, got %d", histLen)
	}
}

func TestVTerm_DisplayBufferGrid(t *testing.T) {
	v := NewVTerm(10, 3)
	v.EnableDisplayBuffer()

	// Write a line
	for _, r := range "ABC" {
		v.placeChar(r)
	}

	// Get grid
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil")
	}
	if len(grid) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(grid))
	}
	if len(grid[0]) != 10 {
		t.Fatalf("expected 10 cols, got %d", len(grid[0]))
	}

	// Find the ABC somewhere in the grid
	found := false
	for _, row := range grid {
		if row[0].Rune == 'A' && row[1].Rune == 'B' && row[2].Rune == 'C' {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'ABC' in grid")
	}
}

func TestVTerm_DisplayBufferResize(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write a long line that will wrap when narrower
	text := "ABCDEFGHIJ" // 10 chars
	for _, r := range text {
		v.placeChar(r)
	}
	v.LineFeed()

	// At width 10, this is 1 physical line
	// Resize to width 5 - should wrap to 2 physical lines
	v.Resize(5, 5)

	// The history still has 1 logical line
	if v.displayBufferHistoryLen() != 1 {
		t.Errorf("expected 1 logical line in history, got %d", v.displayBufferHistoryLen())
	}

	// Grid should now show wrapped content
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil after resize")
	}

	// Grid should be 5 wide now
	if len(grid[0]) != 5 {
		t.Errorf("expected width 5 after resize, got %d", len(grid[0]))
	}
}

func TestVTerm_DisplayBufferScroll(t *testing.T) {
	v := NewVTerm(10, 3)
	v.EnableDisplayBuffer()

	// Add 5 lines to create scrollable content
	for i := 0; i < 5; i++ {
		for _, r := range "Line" {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	// Should be at live edge initially
	if !v.displayBufferAtLiveEdge() {
		t.Error("should be at live edge initially")
	}

	// Scroll up (negative delta = view older content)
	v.Scroll(-2)

	// Should no longer be at live edge
	if v.displayBufferAtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Scroll back to bottom
	v.displayBufferScrollToBottom()

	if !v.displayBufferAtLiveEdge() {
		t.Error("should be at live edge after ScrollToBottom")
	}
}

func TestVTerm_DisplayBufferCarriageReturn(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write "Hello"
	for _, r := range "Hello" {
		v.placeChar(r)
	}

	// Carriage return
	v.displayBufferCarriageReturn()

	// Write "XX" - should overwrite
	for _, r := range "XX" {
		v.placeChar(r)
	}

	line := v.displayBufferGetCurrentLine()
	got := cellsToString(line.Cells)
	if got != "XXllo" {
		t.Errorf("expected 'XXllo' after CR overwrite, got '%s'", got)
	}
}

func TestVTerm_DisplayBufferLoadHistory(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Create some logical lines to load
	lines := []*LogicalLine{
		NewLogicalLineFromCells(makeCells("History line 1")),
		NewLogicalLineFromCells(makeCells("History line 2")),
		NewLogicalLineFromCells(makeCells("History line 3")),
	}

	v.displayBufferLoadHistory(lines)

	// History should have 3 lines
	if v.displayBufferHistoryLen() != 3 {
		t.Errorf("expected 3 lines in history, got %d", v.displayBufferHistoryLen())
	}

	// Grid should show the loaded content
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil")
	}

	// Should be at live edge
	if !v.displayBufferAtLiveEdge() {
		t.Error("should be at live edge after loading history")
	}
}

func TestVTerm_DisplayBufferLoadFromPhysical(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Create physical lines with wrapping
	line1 := makeCells("Hello")
	line1[len(line1)-1].Wrapped = true

	line2 := makeCells("World")
	// Not wrapped - ends logical line

	physical := [][]Cell{line1, line2}

	v.displayBufferLoadFromPhysical(physical)

	// Should have 1 logical line (wrapped physical lines joined)
	if v.displayBufferHistoryLen() != 1 {
		t.Errorf("expected 1 logical line, got %d", v.displayBufferHistoryLen())
	}

	// Verify the content
	history := v.DisplayBufferGetHistory()
	if history == nil {
		t.Fatal("history is nil")
	}

	line := history.Get(0)
	got := cellsToString(line.Cells)
	if got != "HelloWorld" {
		t.Errorf("expected 'HelloWorld', got %q", got)
	}
}

func TestVTerm_DisplayBufferBackspace(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write "Hello"
	for _, r := range "Hello" {
		v.placeChar(r)
	}

	// Backspace twice
	v.Backspace()
	v.Backspace()

	// Write "XY" - should produce "HelXY"
	for _, r := range "XY" {
		v.placeChar(r)
	}

	line := v.displayBufferGetCurrentLine()
	got := cellsToString(line.Cells)
	if got != "HelXY" {
		t.Errorf("expected 'HelXY' after backspace+type, got '%s'", got)
	}
}

func TestVTerm_DisplayBufferReflowAfterLoad(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Load a long logical line
	longText := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lines := []*LogicalLine{
		NewLogicalLineFromCells(makeCells(longText)),
	}
	v.displayBufferLoadHistory(lines)

	// At width 20, this is 2 physical lines (20 + 6)
	// Resize to width 10 - should reflow to 3 physical lines (10 + 10 + 6)
	v.Resize(10, 5)

	// History should still have 1 logical line
	if v.displayBufferHistoryLen() != 1 {
		t.Errorf("expected 1 logical line after resize, got %d", v.displayBufferHistoryLen())
	}

	// Grid should show reflowed content
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil after resize")
	}
	if len(grid[0]) != 10 {
		t.Errorf("expected width 10 after resize, got %d", len(grid[0]))
	}
}

func TestVTerm_DisplayBufferEraseToEndOfLine(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write "Hello World"
	for _, r := range "Hello World" {
		v.placeChar(r)
	}

	// Move cursor back to position 5 (after "Hello")
	v.displayBuf.currentLogicalX = 5
	v.cursorX = 5

	// Erase from cursor to end (EL 0)
	v.ClearLine(0)

	line := v.displayBufferGetCurrentLine()
	got := cellsToString(line.Cells)
	if got != "Hello" {
		t.Errorf("expected 'Hello' after erase to end, got '%s'", got)
	}
}

func TestVTerm_DisplayBufferEraseEntireLine(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write "Hello World"
	for _, r := range "Hello World" {
		v.placeChar(r)
	}

	// Erase entire line (EL 2)
	v.ClearLine(2)

	line := v.displayBufferGetCurrentLine()
	if line.Len() != 0 {
		t.Errorf("expected empty line after erase entire, got len %d", line.Len())
	}
}

func TestVTerm_DisplayBufferEraseCharacters(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write "Hello World"
	for _, r := range "Hello World" {
		v.placeChar(r)
	}

	// Move cursor to position 0
	v.displayBuf.currentLogicalX = 0
	v.cursorX = 0

	// Erase 5 characters (ECH 5)
	v.EraseCharacters(5)

	line := v.displayBufferGetCurrentLine()
	got := cellsToString(line.Cells)
	if got != "      World" {
		t.Errorf("expected '      World' after erase chars, got '%s'", got)
	}
}

func TestVTerm_DisplayBufferResizeReflowContent(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write a line that exactly fills 10 columns
	for _, r := range "ABCDEFGHIJ" {
		v.placeChar(r)
	}
	v.LineFeed()

	// Write another line (don't LineFeed at end - keep it as current line)
	for _, r := range "1234567890" {
		v.placeChar(r)
	}

	// At width 10, we have 1 committed line + current line
	if v.displayBufferHistoryLen() != 1 {
		t.Fatalf("expected 1 committed line in history, got %d", v.displayBufferHistoryLen())
	}

	// Resize to width 5 - each line should wrap to 2 physical lines
	v.Resize(5, 5)

	// Grid should now show wrapped content correctly
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil after resize")
	}

	// Verify the grid is 5 columns wide
	if len(grid[0]) != 5 {
		t.Errorf("expected width 5, got %d", len(grid[0]))
	}

	// History should still have 1 committed line (unchanged)
	if v.displayBufferHistoryLen() != 1 {
		t.Errorf("expected 1 logical line after resize, got %d", v.displayBufferHistoryLen())
	}
}

func TestVTerm_DisplayBufferResizeWiderUnwraps(t *testing.T) {
	v := NewVTerm(5, 5)
	v.EnableDisplayBuffer()

	// Write content that wraps at width 5 (keep as current line, no LineFeed)
	for _, r := range "ABCDEFGHIJ" { // 10 chars = 2 physical lines at width 5
		v.placeChar(r)
	}

	// Current line is not committed yet, history is empty
	if v.displayBufferHistoryLen() != 0 {
		t.Errorf("expected 0 committed lines, got %d", v.displayBufferHistoryLen())
	}

	// Now resize wider - should unwrap to single physical line
	v.Resize(20, 5)

	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil after resize")
	}

	// Verify the grid is 20 columns wide
	if len(grid[0]) != 20 {
		t.Errorf("expected width 20, got %d", len(grid[0]))
	}

	// Still 0 committed lines (current line not committed)
	if v.displayBufferHistoryLen() != 0 {
		t.Errorf("expected 0 logical lines, got %d", v.displayBufferHistoryLen())
	}
}

func TestVTerm_DisplayBufferMultipleResizes(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write a long line
	longText := "The quick brown fox jumps over the lazy dog"
	for _, r := range longText {
		v.placeChar(r)
	}
	v.LineFeed()

	initialHistLen := v.displayBufferHistoryLen()

	// Resize multiple times - history should remain unchanged
	v.Resize(10, 5)
	v.Resize(40, 5)
	v.Resize(15, 5)
	v.Resize(20, 5)

	// History length should be unchanged through all resizes
	if v.displayBufferHistoryLen() != initialHistLen {
		t.Errorf("history length changed after resizes: expected %d, got %d",
			initialHistLen, v.displayBufferHistoryLen())
	}

	// Grid should be correct dimensions
	grid := v.Grid()
	if len(grid[0]) != 20 {
		t.Errorf("expected width 20, got %d", len(grid[0]))
	}
}

func TestVTerm_DisplayBufferLongLineWrap(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write a line longer than width (should auto-wrap but stay as one logical line)
	for _, r := range "ABCDEFGHIJKLMNOPQRST" { // 20 chars = 2 wraps at width 10
		v.placeChar(r)
	}

	// Should still be 0 committed lines (current line not committed)
	if v.displayBufferHistoryLen() != 0 {
		t.Errorf("expected 0 committed lines during typing, got %d", v.displayBufferHistoryLen())
	}

	// Current line should have all 20 characters
	line := v.displayBufferGetCurrentLine()
	if line.Len() != 20 {
		t.Errorf("expected current line length 20, got %d", line.Len())
	}

	// Now commit with LineFeed
	v.LineFeed()

	// Should have 1 committed line
	if v.displayBufferHistoryLen() != 1 {
		t.Errorf("expected 1 committed line after LF, got %d", v.displayBufferHistoryLen())
	}

	// That line should have 20 characters
	history := v.DisplayBufferGetHistory()
	committedLine := history.Get(0)
	if committedLine.Len() != 20 {
		t.Errorf("expected committed line length 20, got %d", committedLine.Len())
	}
}

func TestVTerm_DisplayBufferCursorAfterResize(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write some content
	for _, r := range "Hello World" {
		v.placeChar(r)
	}

	// Cursor should be at position 11
	if v.displayBuf.currentLogicalX != 11 {
		t.Errorf("expected logicalX 11, got %d", v.displayBuf.currentLogicalX)
	}

	// Resize narrower
	v.Resize(10, 5)

	// Logical X should be unchanged (still position 11 in logical line)
	if v.displayBuf.currentLogicalX != 11 {
		t.Errorf("expected logicalX 11 after resize, got %d", v.displayBuf.currentLogicalX)
	}
}

func TestVTerm_DisplayBufferScrollPreservesContent(t *testing.T) {
	v := NewVTerm(10, 3)
	v.EnableDisplayBuffer()

	// Write multiple lines
	lines := []string{"Line1", "Line2", "Line3", "Line4", "Line5"}
	for _, line := range lines {
		for _, r := range line {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	// Should have 5 committed lines
	if v.displayBufferHistoryLen() != 5 {
		t.Errorf("expected 5 lines in history, got %d", v.displayBufferHistoryLen())
	}

	// Scroll up (view older content)
	v.Scroll(-2)

	// History should still have 5 lines
	if v.displayBufferHistoryLen() != 5 {
		t.Errorf("expected 5 lines after scroll, got %d", v.displayBufferHistoryLen())
	}

	// Should not be at live edge
	if v.displayBufferAtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Scroll back down
	v.Scroll(2)

	// Should be at live edge again
	if !v.displayBufferAtLiveEdge() {
		t.Error("should be at live edge after scrolling back down")
	}
}

func TestVTerm_DisplayBufferScrollRegion(t *testing.T) {
	// Scroll regions (DECSTBM) are display-only operations
	// They should NOT affect the logical line history
	v := NewVTerm(20, 10)
	v.EnableDisplayBuffer()

	// Write some lines first
	for i := 0; i < 5; i++ {
		for _, r := range "Line" {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	initialHistLen := v.displayBufferHistoryLen()

	// Set scroll region using DECSTBM (CSI 2;8 r)
	// SetMargins takes 1-indexed values like the terminal protocol
	v.SetMargins(2, 8)

	// History should be unchanged by setting scroll region
	if v.displayBufferHistoryLen() != initialHistLen {
		t.Errorf("scroll region setup changed history: expected %d, got %d",
			initialHistLen, v.displayBufferHistoryLen())
	}

	// Reset scroll region
	v.SetMargins(1, 10)
}

func TestVTerm_DisplayBufferEmptyLines(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write a line, then empty line, then another line
	for _, r := range "First" {
		v.placeChar(r)
	}
	v.LineFeed()

	// Empty line (just LF)
	v.LineFeed()

	for _, r := range "Third" {
		v.placeChar(r)
	}
	v.LineFeed()

	// Should have 3 committed lines (including empty one)
	if v.displayBufferHistoryLen() != 3 {
		t.Errorf("expected 3 lines in history, got %d", v.displayBufferHistoryLen())
	}

	// Verify content
	history := v.DisplayBufferGetHistory()

	line0 := history.Get(0)
	if cellsToString(line0.Cells) != "First" {
		t.Errorf("expected 'First', got '%s'", cellsToString(line0.Cells))
	}

	line1 := history.Get(1)
	if line1.Len() != 0 {
		t.Errorf("expected empty line, got length %d", line1.Len())
	}

	line2 := history.Get(2)
	if cellsToString(line2.Cells) != "Third" {
		t.Errorf("expected 'Third', got '%s'", cellsToString(line2.Cells))
	}
}

func TestVTerm_DisplayBufferProgressBar(t *testing.T) {
	// Simulates a progress bar that uses CR to overwrite
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write "Progress: 0%"
	for _, r := range "Progress: 0%" {
		v.placeChar(r)
	}

	// CR and overwrite
	v.CarriageReturn()
	for _, r := range "Progress: 50%" {
		v.placeChar(r)
	}

	// CR and overwrite again
	v.CarriageReturn()
	for _, r := range "Progress: 100%" {
		v.placeChar(r)
	}

	// Should still be on current line (not committed)
	if v.displayBufferHistoryLen() != 0 {
		t.Errorf("expected 0 committed lines during progress, got %d", v.displayBufferHistoryLen())
	}

	// Current line should have final content
	line := v.displayBufferGetCurrentLine()
	got := cellsToString(line.Cells)
	if got != "Progress: 100%" {
		t.Errorf("expected 'Progress: 100%%', got '%s'", got)
	}

	// Now commit
	v.LineFeed()

	if v.displayBufferHistoryLen() != 1 {
		t.Errorf("expected 1 committed line, got %d", v.displayBufferHistoryLen())
	}
}

func TestVTerm_DisplayBufferLargeHistory(t *testing.T) {
	// Test performance with large history
	v := NewVTerm(80, 24)
	v.EnableDisplayBuffer()

	// Write 10000 lines
	for i := 0; i < 10000; i++ {
		text := "This is line number " + string(rune('0'+i%10))
		for _, r := range text {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	// Should have 10000 committed lines
	if v.displayBufferHistoryLen() != 10000 {
		t.Errorf("expected 10000 lines in history, got %d", v.displayBufferHistoryLen())
	}

	// Grid should still work
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil with large history")
	}
	if len(grid) != 24 {
		t.Errorf("expected 24 rows, got %d", len(grid))
	}

	// Resize should work (this is the key test - O(viewport) not O(history))
	v.Resize(40, 24)

	grid = v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil after resize")
	}
	if len(grid[0]) != 40 {
		t.Errorf("expected width 40, got %d", len(grid[0]))
	}

	// Scroll up into history
	v.Scroll(-100)

	// Should still have 10000 lines
	if v.displayBufferHistoryLen() != 10000 {
		t.Errorf("expected 10000 lines after scroll, got %d", v.displayBufferHistoryLen())
	}
}

func BenchmarkDisplayBuffer_PlaceChar(b *testing.B) {
	v := NewVTerm(80, 24)
	v.EnableDisplayBuffer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.placeChar('A')
		if i%80 == 79 {
			v.LineFeed()
		}
	}
}

func BenchmarkDisplayBuffer_Resize(b *testing.B) {
	v := NewVTerm(80, 24)
	v.EnableDisplayBuffer()

	// Write some content first
	for i := 0; i < 1000; i++ {
		for _, r := range "Test line content here" {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			v.Resize(40, 24)
		} else {
			v.Resize(80, 24)
		}
	}
}

func BenchmarkDisplayBuffer_Scroll(b *testing.B) {
	v := NewVTerm(80, 24)
	v.EnableDisplayBuffer()

	// Write content to create scrollable history
	for i := 0; i < 1000; i++ {
		for _, r := range "Test line" {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Scroll(-10) // Scroll up
		v.Scroll(10)  // Scroll back down
	}
}
