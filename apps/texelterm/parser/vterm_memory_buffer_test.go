// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_memory_buffer_test.go
// Summary: Integration tests for VTerm with the new MemoryBuffer/ViewportWindow system.

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// parseString is a helper that writes a string to VTerm via the parser.
func parseString(p *Parser, s string) {
	for _, r := range s {
		p.Parse(r)
	}
}

// TestVTerm_MemoryBufferBasicWrite tests that characters are written to MemoryBuffer.
func TestVTerm_MemoryBufferBasicWrite(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Verify the system is enabled
	if !v.IsMemoryBufferEnabled() {
		t.Fatal("MemoryBuffer should be enabled")
	}

	// Write some characters
	parseString(p, "Hello, World!")

	// Get the grid and verify content
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid should not be nil")
	}

	if len(grid) != 24 {
		t.Fatalf("Expected 24 rows, got %d", len(grid))
	}

	// Check the first row contains "Hello, World!"
	expected := "Hello, World!"
	actual := gridRowToString(grid[0][:len(expected)])
	if actual != expected {
		t.Errorf("Expected %q, got %q", expected, actual)
	}
}

// TestVTerm_MemoryBufferLineFeed tests line feed behavior with MemoryBuffer.
func TestVTerm_MemoryBufferLineFeed(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write text on first line
	parseString(p, "Line 1")
	p.Parse('\n')
	p.Parse('\r')
	parseString(p, "Line 2")

	grid := v.Grid()

	// Verify both lines are present
	row0 := gridRowToString(grid[0][:6])
	row1 := gridRowToString(grid[1][:6])

	if row0 != "Line 1" {
		t.Errorf("Row 0: expected 'Line 1', got %q", row0)
	}
	if row1 != "Line 2" {
		t.Errorf("Row 1: expected 'Line 2', got %q", row1)
	}
}

// TestVTerm_MemoryBufferScrollRegion tests scroll region operations.
func TestVTerm_MemoryBufferScrollRegion(t *testing.T) {
	v := NewVTerm(80, 10, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Fill visible area with lines
	// After 10 iterations: A on line 0, B on line 1, ... J on line 9
	// Each LF+CR moves cursor down and back to column 0
	for i := 0; i < 10; i++ {
		p.Parse('A' + rune(i))
		p.Parse('\n')
		p.Parse('\r')
	}

	// Now cursor is at line 10, which triggers scrolling
	// Write one more line to trigger scroll
	parseString(p, "Last")

	grid := v.Grid()

	// After scrolling, the first visible character should be from the history
	// The exact character depends on how many scrolls occurred
	firstChar := grid[0][0].Rune

	// Check that scrolling happened: first char should NOT be 'A' anymore
	if firstChar == 'A' {
		t.Error("First char is still 'A' - scrolling did not occur")
	}

	// Verify the last row has "Last"
	lastRow := gridRowToString(grid[9][:4])
	if lastRow != "Last" {
		t.Errorf("Last row should have 'Last', got %q", lastRow)
	}
}

// TestVTerm_MemoryBufferEraseLine tests line erase operations.
func TestVTerm_MemoryBufferEraseLine(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write content
	parseString(p, "ABCDEFGHIJ")

	// Move cursor to middle and erase to end of line using CSI K
	parseString(p, "\x1b[6G") // Move to column 6
	parseString(p, "\x1b[K")  // Erase to end of line

	grid := v.Grid()
	row := gridRowToString(grid[0][:10])

	// First 5 chars should remain, rest should be spaces
	if row != "ABCDE     " {
		t.Errorf("After erase to end: expected 'ABCDE     ', got %q", row)
	}
}

// TestVTerm_MemoryBufferEraseScreen tests screen erase operations.
func TestVTerm_MemoryBufferEraseScreen(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write content on multiple lines
	parseString(p, "Line 0\r\nLine 1\r\nLine 2")

	// Erase entire screen using CSI 2J
	parseString(p, "\x1b[2J")

	grid := v.Grid()

	// All rows should be empty
	for y := 0; y < 3; y++ {
		for x := 0; x < 6; x++ {
			if grid[y][x].Rune != 0 && grid[y][x].Rune != ' ' {
				t.Errorf("Row %d col %d should be empty after screen erase, got '%c'",
					y, x, grid[y][x].Rune)
			}
		}
	}
}

// TestVTerm_MemoryBufferUserScroll tests user-initiated scrollback navigation.
func TestVTerm_MemoryBufferUserScroll(t *testing.T) {
	v := NewVTerm(80, 10, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write more lines than the viewport can hold to build history
	for i := 0; i < 20; i++ {
		p.Parse('A' + rune(i%26))
		p.Parse('\n')
		p.Parse('\r')
	}

	// Should be at live edge initially
	if !v.memoryBufferAtLiveEdge() {
		t.Error("Should be at live edge after writing")
	}

	// Scroll up into history
	v.Scroll(-5)

	if v.memoryBufferAtLiveEdge() {
		t.Error("Should not be at live edge after scrolling up")
	}

	// Scroll back to bottom
	v.memoryBufferScrollToBottom()

	if !v.memoryBufferAtLiveEdge() {
		t.Error("Should be at live edge after scroll to bottom")
	}
}

// TestVTerm_MemoryBufferResize tests terminal resize with MemoryBuffer.
func TestVTerm_MemoryBufferResize(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write some content
	parseString(p, "Hello, World!\r\nLine 2")

	// Resize smaller
	v.Resize(40, 12)

	grid := v.Grid()

	// Check dimensions
	if len(grid) != 12 {
		t.Errorf("Expected 12 rows after resize, got %d", len(grid))
	}
	if len(grid[0]) != 40 {
		t.Errorf("Expected 40 cols after resize, got %d", len(grid[0]))
	}

	// Content should still be visible (first 13 chars of row 0)
	row0 := gridRowToString(grid[0][:13])
	if row0 != "Hello, World!" {
		t.Errorf("Content lost after resize: expected 'Hello, World!', got %q", row0)
	}
}

// TestVTerm_MemoryBufferCursorTracking tests cursor position tracking.
func TestVTerm_MemoryBufferCursorTracking(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Initially at 0,0
	x, y := v.Cursor()
	if x != 0 || y != 0 {
		t.Errorf("Initial cursor: expected (0,0), got (%d,%d)", x, y)
	}

	// Write and cursor should advance
	parseString(p, "Hello")
	x, y = v.Cursor()
	if x != 5 || y != 0 {
		t.Errorf("After write: expected (5,0), got (%d,%d)", x, y)
	}

	// Line feed
	p.Parse('\n')
	p.Parse('\r')
	x, y = v.Cursor()
	if x != 0 || y != 1 {
		t.Errorf("After LF+CR: expected (0,1), got (%d,%d)", x, y)
	}
}

// TestVTerm_MemoryBufferWideCharacter tests wide character handling.
func TestVTerm_MemoryBufferWideCharacter(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write a wide character (emoji)
	parseString(p, "Hello 👋 World")

	x, y := v.Cursor()
	// The emoji takes 2 cells, so cursor should advance accordingly
	if y != 0 {
		t.Errorf("Cursor Y should be 0, got %d", y)
	}
	// "Hello " (6) + emoji (2 cells) + " World" (6) = 14 visual columns
	if x < 14 {
		t.Errorf("Cursor X should be at least 14 after wide char, got %d", x)
	}
}

// TestVTerm_MemoryBufferWithDisk tests disk persistence integration.
func TestVTerm_MemoryBufferWithDisk(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "test_history.hist3")

	v := NewVTerm(80, 24)

	opts := MemoryBufferOptions{
		MaxLines:      1000,
		EvictionBatch: 100,
		DiskPath:      diskPath,
	}
	err := v.EnableMemoryBufferWithDisk(diskPath, opts)
	if err != nil {
		t.Fatalf("Failed to enable memory buffer with disk: %v", err)
	}

	if !v.IsMemoryBufferEnabled() {
		t.Fatal("MemoryBuffer should be enabled")
	}

	p := NewParser(v)

	// Write some content
	parseString(p, "Test content for disk persistence\r\nLine 2")

	// Close the memory buffer to flush to disk
	err = v.CloseMemoryBuffer()
	if err != nil {
		t.Errorf("Failed to close memory buffer: %v", err)
	}

	// Verify the history file was created
	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		t.Error("History file was not created on disk")
	}
}

// TestVTerm_MemoryBufferEnabledByDefault verifies MemoryBuffer is the default.
// Note: MemoryBuffer is now the only system (DisplayBuffer was removed).
func TestVTerm_MemoryBufferEnabledByDefault(t *testing.T) {
	v := NewVTerm(80, 24)

	// MemoryBuffer should always be enabled by default
	if !v.IsMemoryBufferEnabled() {
		t.Error("MemoryBuffer should be enabled by default")
	}
}

// TestVTerm_MemoryBufferGridDimensions verifies grid has correct dimensions.
func TestVTerm_MemoryBufferGridDimensions(t *testing.T) {
	testCases := []struct {
		width, height int
	}{
		{80, 24},
		{120, 40},
		{40, 10},
		{132, 43}, // VT100 wide mode
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			v := NewVTerm(tc.width, tc.height, WithMemoryBuffer())
			v.EnableMemoryBuffer()

			grid := v.Grid()

			if len(grid) != tc.height {
				t.Errorf("Height: expected %d, got %d", tc.height, len(grid))
			}
			if len(grid) > 0 && len(grid[0]) != tc.width {
				t.Errorf("Width: expected %d, got %d", tc.width, len(grid[0]))
			}
		})
	}
}

// TestVTerm_MemoryBufferTotalLines tests line counting.
func TestVTerm_MemoryBufferTotalLines(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Initially 1 line (the current line)
	total := v.memoryBufferTotalLines()
	if total < 1 {
		t.Errorf("Should have at least 1 line initially, got %d", total)
	}

	// Add more lines
	for i := 0; i < 50; i++ {
		p.Parse('X')
		p.Parse('\n')
		p.Parse('\r')
	}

	total = v.memoryBufferTotalLines()
	if total < 25 {
		t.Errorf("Should have many lines, got %d", total)
	}
}

// gridRowToString converts a slice of cells to a string for testing.
func gridRowToString(cells []Cell) string {
	runes := make([]rune, len(cells))
	for i, c := range cells {
		if c.Rune == 0 {
			runes[i] = ' '
		} else {
			runes[i] = c.Rune
		}
	}
	return string(runes)
}
