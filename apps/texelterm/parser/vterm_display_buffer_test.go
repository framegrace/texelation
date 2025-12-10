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

	// Scroll up
	v.Scroll(2)

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
