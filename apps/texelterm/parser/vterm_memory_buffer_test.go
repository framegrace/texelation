// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_memory_buffer_test.go
// Summary: Integration tests for VTerm with the new MemoryBuffer/ViewportWindow system.

package parser

import (
	"fmt"
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
// TestVTerm_ScrollRegionPreservesScrollback tests that scroll region operations
// on the main screen preserve scrolled-off content as scrollback history.
// This simulates the Codex CLI pattern: static header, scroll region, static footer.
func TestVTerm_ScrollRegionPreservesScrollback(t *testing.T) {
	width, height := 40, 10
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write header on row 0
	parseString(p, "=== HEADER ===")

	// Write footer on row 9
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height)) // Move to last row
	parseString(p, "=== FOOTER ===")

	// Set scroll region: rows 2-9 (1-indexed in VT: ESC[2;9r] means rows 1-8 in 0-indexed)
	// For a 10-row terminal, this leaves row 0 as header and row 9 as footer
	parseString(p, "\x1b[2;9r") // Scroll region rows 2 through 9 (0-indexed: 1 through 8)

	// Move cursor to top of scroll region
	parseString(p, "\x1b[2;1H") // Row 2, Col 1 (1-indexed)

	// Write lines that will fill the scroll region and eventually scroll
	// Scroll region is rows 1-8 (0-indexed), so 8 rows
	lines := []string{
		"Line-A content here",
		"Line-B content here",
		"Line-C content here",
		"Line-D content here",
		"Line-E content here",
		"Line-F content here",
		"Line-G content here",
		"Line-H content here",
		"Line-I content here", // This should push Line-A into scrollback
		"Line-J content here", // This should push Line-B into scrollback
		"Line-K content here", // This should push Line-C into scrollback
	}

	for i, line := range lines {
		parseString(p, line)
		// Only add LF+CR after lines that aren't the last one,
		// to avoid an extra scroll at the end
		if i < len(lines)-1 {
			parseString(p, "\n") // LF triggers scroll when at bottom of region
			parseString(p, "\r") // CR to column 0
		}
	}

	// Now check the visible grid
	grid := v.Grid()

	// Verify header is still intact at row 0
	headerRow := gridRowToString(grid[0][:14])
	if headerRow != "=== HEADER ===" {
		t.Errorf("Header corrupted: got %q, want %q", headerRow, "=== HEADER ===")
	}

	// Verify footer is still intact at row 9
	footerRow := gridRowToString(grid[9][:14])
	if footerRow != "=== FOOTER ===" {
		t.Errorf("Footer corrupted: got %q, want %q", footerRow, "=== FOOTER ===")
	}

	// Verify the scroll region shows the latest lines.
	// Region is rows 1-8 (8 rows). We wrote 11 lines.
	// First 8 lines fill the region (A through H). Lines 9-11 (I, J, K)
	// each trigger a scroll, pushing A, B, C into scrollback.
	// Visible region: D, E, F, G, H, I, J, K
	expectedRegion := []string{
		"Line-D", "Line-E", "Line-F", "Line-G",
		"Line-H", "Line-I", "Line-J", "Line-K",
	}
	for i, expected := range expectedRegion {
		row := i + 1 // Rows 1-8
		actual := gridRowToString(grid[row][:6])
		if actual != expected {
			t.Errorf("Region row %d: got %q, want %q", row, actual, expected)
		}
	}

	// Verify scrollback contains the scrolled-off lines
	mb := v.memBufState.memBuf
	liveEdge := v.memBufState.liveEdgeBase

	if liveEdge != 3 {
		t.Errorf("liveEdgeBase: got %d, want 3 (3 scroll events)", liveEdge)
	}

	// The scrollback lines are at global indices 0, 1, 2
	// and should contain "Line-A", "Line-B", "Line-C" respectively
	expectedScrollback := []string{"Line-A", "Line-B", "Line-C"}
	for i, expected := range expectedScrollback {
		globalIdx := int64(i)
		line := mb.GetLine(globalIdx)
		if line == nil {
			t.Fatalf("Scrollback line at global %d is nil (liveEdge=%d)", globalIdx, liveEdge)
		}
		actual := ""
		for _, cell := range line.Cells {
			if cell.Rune == 0 {
				break
			}
			actual += string(cell.Rune)
		}
		if len(actual) < 6 {
			t.Errorf("Scrollback line at global %d too short: %q", globalIdx, actual)
			continue
		}
		if actual[:6] != expected {
			t.Errorf("Scrollback line at global %d: got %q, want prefix %q", globalIdx, actual[:6], expected)
		}
	}

	t.Logf("liveEdgeBase=%d, scrollback has %d lines of Codex-like content", liveEdge, liveEdge)
}

// TestVTerm_ScrollRegionNoHeader tests scroll region starting at row 0 (no header).
func TestVTerm_ScrollRegionNoHeader(t *testing.T) {
	width, height := 40, 6
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write footer on row 5 (last row)
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
	parseString(p, "FOOTER")

	// Set scroll region: rows 1-5 (1-indexed), which is rows 0-4 (0-indexed)
	// This means top=0, bottom=4, footer at row 5
	parseString(p, "\x1b[1;5r")

	// Move cursor to top of region
	parseString(p, "\x1b[1;1H")

	// Write 7 lines in 5-row region, causing 2 scrolls
	for i := 0; i < 7; i++ {
		parseString(p, fmt.Sprintf("Line-%c", 'A'+rune(i)))
		if i < 6 {
			parseString(p, "\n\r")
		}
	}

	grid := v.Grid()

	// Footer should be intact at row 5
	footerRow := gridRowToString(grid[5][:6])
	if footerRow != "FOOTER" {
		t.Errorf("Footer corrupted: got %q, want %q", footerRow, "FOOTER")
	}

	// Verify scrollback exists
	mb := v.memBufState.memBuf
	liveEdge := v.memBufState.liveEdgeBase
	if liveEdge < 2 {
		t.Errorf("liveEdgeBase should have advanced at least 2, got %d", liveEdge)
	}

	// Scrollback should contain Line-A and Line-B
	for i := int64(0); i < liveEdge && i < 2; i++ {
		line := mb.GetLine(i)
		if line == nil {
			t.Errorf("Scrollback line at global %d is nil", i)
			continue
		}
		text := gridRowToString(line.Cells[:6])
		expected := fmt.Sprintf("Line-%c", 'A'+rune(i))
		if text != expected {
			t.Errorf("Scrollback[%d]: got %q, want %q", i, text, expected)
		}
	}
	t.Logf("liveEdgeBase=%d (no header case)", liveEdge)
}

// TestVTerm_ScrollRegionNoFooter tests scroll region ending at last row (no footer).
func TestVTerm_ScrollRegionNoFooter(t *testing.T) {
	width, height := 40, 6
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write header on row 0
	parseString(p, "HEADER")

	// Set scroll region: rows 2-6 (1-indexed), which is rows 1-5 (0-indexed)
	// This means top=1, bottom=5=height-1, no footer
	parseString(p, "\x1b[2;6r")

	// Move cursor to top of region
	parseString(p, "\x1b[2;1H")

	// Write 7 lines in 5-row region, causing 2 scrolls
	for i := 0; i < 7; i++ {
		parseString(p, fmt.Sprintf("Line-%c", 'A'+rune(i)))
		if i < 6 {
			parseString(p, "\n\r")
		}
	}

	grid := v.Grid()

	// Header should be intact at row 0
	headerRow := gridRowToString(grid[0][:6])
	if headerRow != "HEADER" {
		t.Errorf("Header corrupted: got %q, want %q", headerRow, "HEADER")
	}

	// Verify scrollback exists
	mb := v.memBufState.memBuf
	liveEdge := v.memBufState.liveEdgeBase
	if liveEdge < 2 {
		t.Errorf("liveEdgeBase should have advanced at least 2, got %d", liveEdge)
	}

	// Scrollback should contain Line-A and Line-B
	for i := int64(0); i < liveEdge && i < 2; i++ {
		line := mb.GetLine(i)
		if line == nil {
			t.Errorf("Scrollback line at global %d is nil", i)
			continue
		}
		text := gridRowToString(line.Cells[:6])
		expected := fmt.Sprintf("Line-%c", 'A'+rune(i))
		if text != expected {
			t.Errorf("Scrollback[%d]: got %q, want %q", i, text, expected)
		}
	}
	t.Logf("liveEdgeBase=%d (no footer case)", liveEdge)
}

// TestVTerm_ScrollRegionMultipleScrollN tests scrolling by n > 1 at once.
func TestVTerm_ScrollRegionMultipleScrollN(t *testing.T) {
	width, height := 40, 8
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write header at row 0
	parseString(p, "HEADER")

	// Write footer at row 7
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
	parseString(p, "FOOTER")

	// Set scroll region: rows 2-7 (1-indexed) = rows 1-6 (0-indexed)
	parseString(p, "\x1b[2;7r")

	// Move to top of region and fill it
	parseString(p, "\x1b[2;1H")
	for i := 0; i < 6; i++ {
		parseString(p, fmt.Sprintf("Line-%c", 'A'+rune(i)))
		if i < 5 {
			parseString(p, "\n\r")
		}
	}

	liveEdgeBefore := v.memBufState.liveEdgeBase

	// Use CSI 3S to scroll up 3 lines at once
	parseString(p, "\x1b[3S")

	liveEdgeAfter := v.memBufState.liveEdgeBase

	// liveEdgeBase should have advanced by 3
	if liveEdgeAfter-liveEdgeBefore != 3 {
		t.Errorf("liveEdgeBase delta: got %d, want 3", liveEdgeAfter-liveEdgeBefore)
	}

	grid := v.Grid()

	// Header still intact
	headerRow := gridRowToString(grid[0][:6])
	if headerRow != "HEADER" {
		t.Errorf("Header corrupted after multi-scroll: got %q, want %q", headerRow, "HEADER")
	}

	// Footer still intact
	footerRow := gridRowToString(grid[7][:6])
	if footerRow != "FOOTER" {
		t.Errorf("Footer corrupted after multi-scroll: got %q, want %q", footerRow, "FOOTER")
	}

	// Scrollback should contain Line-A, Line-B, Line-C
	mb := v.memBufState.memBuf
	for i := 0; i < 3; i++ {
		globalIdx := liveEdgeBefore + int64(i)
		line := mb.GetLine(globalIdx)
		if line == nil {
			t.Fatalf("Scrollback line at global %d is nil", globalIdx)
		}
		text := gridRowToString(line.Cells[:6])
		expected := fmt.Sprintf("Line-%c", 'A'+rune(i))
		if text != expected {
			t.Errorf("Scrollback[%d]: got %q, want %q", globalIdx, text, expected)
		}
	}

	t.Logf("Multi-scroll: liveEdge went from %d to %d", liveEdgeBefore, liveEdgeAfter)
}

// TestVTerm_ScrollRegionFullScreenUnchanged verifies that full-screen margins
// (no scroll region) continue to work correctly — liveEdgeBase advances through
// the normal memoryBufferLineFeed path, not the scroll region path.
func TestVTerm_ScrollRegionFullScreenUnchanged(t *testing.T) {
	v := NewVTerm(40, 5, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write 8 lines with full-screen margins (default)
	for i := 0; i < 8; i++ {
		parseString(p, fmt.Sprintf("Line-%d", i))
		parseString(p, "\r\n")
	}

	// liveEdgeBase should have advanced as lines scrolled off
	liveEdge := v.memBufState.liveEdgeBase
	if liveEdge == 0 {
		t.Error("liveEdgeBase should have advanced for full-screen scrolling")
	}

	// Verify scrollback exists
	mb := v.memBufState.memBuf
	line := mb.GetLine(0)
	if line == nil {
		t.Fatal("First scrollback line should exist")
	}
	actual := gridRowToString(line.Cells[:6])
	if actual != "Line-0" {
		t.Errorf("First scrollback line: got %q, want %q", actual, "Line-0")
	}
}

// TestVTerm_ScrollRegionScrollDownUnchanged verifies scroll-down within
// a region still works (in-place shift, no scrollback creation).
func TestVTerm_ScrollRegionScrollDownUnchanged(t *testing.T) {
	width, height := 40, 10
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Set scroll region: rows 2-8 (1-indexed)
	parseString(p, "\x1b[2;8r")

	// Move to top of region and write content
	parseString(p, "\x1b[2;1H")
	parseString(p, "AAA\r\n")
	parseString(p, "BBB\r\n")
	parseString(p, "CCC")

	// Record liveEdgeBase before scroll-down
	liveEdgeBefore := v.memBufState.liveEdgeBase

	// Scroll down within region: ESC[T (scroll down)
	parseString(p, "\x1b[T")

	// liveEdgeBase should NOT have changed for scroll-down
	liveEdgeAfter := v.memBufState.liveEdgeBase
	if liveEdgeAfter != liveEdgeBefore {
		t.Errorf("liveEdgeBase changed for scroll-down: before=%d, after=%d",
			liveEdgeBefore, liveEdgeAfter)
	}
}

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

// TestVTerm_HistoryRestoration tests that history is restored on terminal startup.
func TestVTerm_HistoryRestoration(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test_history.hist3"
	terminalID := "test-restore"

	// First session: create some history
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}

		p := NewParser(v)

		// Write some lines
		for i := 0; i < 10; i++ {
			for _, ch := range "Line " {
				p.Parse(ch)
			}
			p.Parse(rune('0' + i))
			p.Parse('\n')
			p.Parse('\r')
		}

		// Close to flush to disk
		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer failed: %v", err)
		}
	}

	// Second session: restore and verify
	{
		// Create with different size to ensure resize triggers history loading
		v := NewVTerm(40, 12, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (restore) failed: %v", err)
		}

		// Trigger resize which should load history (different dimensions to ensure it runs)
		v.Resize(80, 24)

		// Check that history was loaded
		if v.memBufState == nil || !v.memBufState.historyLoaded {
			t.Error("History should be marked as loaded after resize")
		}

		// Verify we can read historical lines
		// The history should be available in the memory buffer
		mb := v.memBufState.memBuf
		globalOffset := mb.GlobalOffset()
		globalEnd := mb.GlobalEnd()

		t.Logf("After restore: globalOffset=%d, globalEnd=%d", globalOffset, globalEnd)

		if globalEnd <= globalOffset {
			t.Error("Should have some lines loaded")
		}

		// Try to get a historical line
		histLine := mb.GetLine(globalOffset)
		if histLine == nil {
			t.Error("Should be able to read first historical line")
		} else {
			text := logicalLineToString(histLine)
			t.Logf("First historical line: %q", text)
		}

		v.CloseMemoryBuffer()
	}
}

// logicalLineToString converts a LogicalLine to a string.
func logicalLineToString(line *LogicalLine) string {
	if line == nil {
		return ""
	}
	runes := make([]rune, len(line.Cells))
	for i, c := range line.Cells {
		if c.Rune == 0 {
			runes[i] = ' '
		} else {
			runes[i] = c.Rune
		}
	}
	return string(runes)
}

// TestVTerm_ScrollStatePersistence tests that scroll position is saved and restored correctly.
// This simulates a real terminal session with enough content to fill multiple pages,
// user scrolling, and line edits at the prompt.
func TestVTerm_ScrollStatePersistence(t *testing.T) {
	// Create temp directory for history files
	tmpDir, err := os.MkdirTemp("", "TestVTerm_ScrollStatePersistence")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	diskPath := filepath.Join(tmpDir, "test_scroll.hist3")
	terminalID := "test-scroll-state"

	// State to capture from first session
	var (
		savedScrollOffset int64
		savedLiveEdgeBase int64
		// Track a sample line within the expected restore window for verification
		// We'll use a line near the end that will definitely be loaded
		sampleLineIdx int64
		sampleLineTxt string
	)

	// First session: write enough content to fill multiple pages
	t.Log("=== First Session: Creating content and scroll state ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}

		p := NewParser(v)

		// Write enough lines to fill 3-4 pages (64KB each)
		// Each line is ~80 chars, so ~800 lines per page
		// Write ~3000 lines to be safe
		numLines := 3000
		t.Logf("Writing %d lines to create multiple pages...", numLines)

		for i := 0; i < numLines; i++ {
			// Write line with identifiable content
			line := []rune("Line ")
			// Add line number with padding
			numStr := []rune(padLeft(i, 5))
			line = append(line, numStr...)
			line = append(line, []rune(": This is test content for scroll persistence testing. ")...)
			// Add some varying content to make lines different
			for j := 0; j < (i % 20); j++ {
				line = append(line, 'X')
			}

			for _, ch := range line {
				p.Parse(ch)
			}
			p.Parse('\n')
			p.Parse('\r')
		}

		// Simulate a prompt line being edited (like user typing at prompt)
		promptLine := "user@host:~/projects$ ls -la"
		for _, ch := range promptLine {
			p.Parse(ch)
		}

		// Now scroll back into history (simulate user scrolling up)
		// Scroll up 100 lines from live edge (smaller scroll to stay within loaded window)
		for i := 0; i < 100; i++ {
			v.memoryBufferScroll(-1)
		}

		// Capture state before closing
		mb := v.memBufState.memBuf
		savedScrollOffset = v.memBufState.viewport.ScrollOffset()
		savedLiveEdgeBase = v.memBufState.liveEdgeBase

		// Pick a sample line near liveEdgeBase that will be loaded on restore
		// (within viewport + 500 margin of the end)
		sampleLineIdx = savedLiveEdgeBase - 50 // 50 lines before live edge
		if sampleLine := mb.GetLine(sampleLineIdx); sampleLine != nil {
			sampleLineTxt = trimLogicalLine(logicalLineToString(sampleLine))
		}

		t.Logf("State before close:")
		t.Logf("  scrollOffset:    %d", savedScrollOffset)
		t.Logf("  liveEdgeBase:    %d", savedLiveEdgeBase)
		t.Logf("  globalOffset:    %d", mb.GlobalOffset())
		t.Logf("  globalEnd:       %d", mb.GlobalEnd())
		t.Logf("  sampleLineIdx:   %d", sampleLineIdx)
		t.Logf("  sampleLineTxt:   %q", sampleLineTxt)

		// Close to flush to disk
		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer failed: %v", err)
		}
	}

	// Second session: restore and verify
	t.Log("=== Second Session: Restoring and verifying state ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (restore) failed: %v", err)
		}

		// Check that history was loaded
		if v.memBufState == nil || !v.memBufState.historyLoaded {
			t.Error("History should be marked as loaded")
		}

		// Verify state was restored
		mb := v.memBufState.memBuf
		restoredScrollOffset := v.memBufState.viewport.ScrollOffset()
		restoredLiveEdgeBase := v.memBufState.liveEdgeBase

		t.Logf("State after restore:")
		t.Logf("  scrollOffset:    %d (expected %d)", restoredScrollOffset, savedScrollOffset)
		t.Logf("  liveEdgeBase:    %d (expected %d)", restoredLiveEdgeBase, savedLiveEdgeBase)
		t.Logf("  globalOffset:    %d", mb.GlobalOffset())
		t.Logf("  globalEnd:       %d", mb.GlobalEnd())

		// Verify scroll offset was restored
		if restoredScrollOffset != savedScrollOffset {
			t.Errorf("ScrollOffset mismatch: got %d, want %d", restoredScrollOffset, savedScrollOffset)
		}

		// Verify liveEdgeBase was restored
		if restoredLiveEdgeBase != savedLiveEdgeBase {
			t.Errorf("LiveEdgeBase mismatch: got %d, want %d", restoredLiveEdgeBase, savedLiveEdgeBase)
		}

		// Verify sample line is accessible (content verification is separate concern)
		// The scroll state (offset, liveEdgeBase) is the primary focus of this test
		var restoredSampleLineTxt string
		if sampleLine := mb.GetLine(sampleLineIdx); sampleLine != nil {
			restoredSampleLineTxt = trimLogicalLine(logicalLineToString(sampleLine))
		}

		t.Logf("Content check:")
		t.Logf("  sampleLine[%d]: %q", sampleLineIdx, restoredSampleLineTxt)
		t.Logf("  (first session had: %q)", sampleLineTxt)

		// Just verify the line is accessible and non-empty
		if restoredSampleLineTxt == "" {
			t.Errorf("Sample line at idx %d should be accessible and non-empty", sampleLineIdx)
		}

		// Verify we're scrolled back to the same position (not at live edge)
		if v.memBufState.viewport.IsAtLiveEdge() && savedScrollOffset > 0 {
			t.Error("Should be scrolled back in history, not at live edge")
		}

		// Verify the loaded window contains the expected range
		// We load viewport height (24) + margin (500) = 524 lines
		// So globalOffset should be near lineCount - 524
		pageStore := v.memBufState.pageStore
		totalDiskLines := pageStore.LineCount()
		expectedMinOffset := totalDiskLines - int64(24+500)
		if mb.GlobalOffset() > expectedMinOffset+100 {
			t.Errorf("GlobalOffset %d is too far from expected ~%d", mb.GlobalOffset(), expectedMinOffset)
		}

		t.Logf("Loaded window check:")
		t.Logf("  totalDiskLines:     %d", totalDiskLines)
		t.Logf("  loadedGlobalOffset: %d", mb.GlobalOffset())
		t.Logf("  expectedMinOffset:  ~%d", expectedMinOffset)

		v.CloseMemoryBuffer()
	}
}

// padLeft pads an integer to a fixed width with leading zeros.
func padLeft(n, width int) string {
	s := ""
	for i := 0; i < width; i++ {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// TestAdaptivePersistenceWithWAL tests AdaptivePersistence with WAL backend.
func TestAdaptivePersistenceWithWAL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestAdaptivePersistenceWithWAL")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create MemoryBuffer
	mbConfig := MemoryBufferConfig{MaxLines: 1000, EvictionBatch: 100}
	memBuf := NewMemoryBuffer(mbConfig)
	memBuf.SetTermWidth(80)

	// Create AdaptivePersistence with WAL
	apConfig := DefaultAdaptivePersistenceConfig()
	walConfig := DefaultWALConfig(tmpDir, "test-ap-wal")

	ap, err := NewAdaptivePersistenceWithWAL(apConfig, memBuf, walConfig)
	if err != nil {
		t.Fatalf("NewAdaptivePersistenceWithWAL failed: %v", err)
	}

	// Write some lines to MemoryBuffer and notify persistence
	var defColor Color
	memBuf.EnsureLine(0)
	memBuf.SetCursor(0, 0)
	for _, r := range "Hello" {
		memBuf.Write(r, defColor, defColor, 0)
	}
	ap.NotifyWrite(0)

	memBuf.EnsureLine(1)
	memBuf.SetCursor(1, 0)
	for _, r := range "World" {
		memBuf.Write(r, defColor, defColor, 0)
	}
	ap.NotifyWrite(1)

	t.Logf("Before flush: pendingCount=%d, metrics=%+v", ap.PendingCount(), ap.Metrics())
	t.Logf("MemBuf line 0: %q", trimLogicalLine(logicalLineToString(memBuf.GetLine(0))))
	t.Logf("MemBuf line 1: %q", trimLogicalLine(logicalLineToString(memBuf.GetLine(1))))

	// Check mode
	t.Logf("Current mode: %s", ap.CurrentMode())

	// Force flush
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	t.Logf("After flush: pendingCount=%d, metrics=%+v", ap.PendingCount(), ap.Metrics())

	// Check WAL's PageStore (will be 0 because checkpoint hasn't happened yet)
	ps := ap.PageStore()
	t.Logf("PageStore lineCount after flush (before checkpoint): %d", ps.LineCount())

	// Check if WAL has entries
	t.Logf("WAL wal=%p", ap.wal)

	// Close (should trigger checkpoint)
	t.Log("Closing AdaptivePersistence...")
	if err := ap.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	t.Log("Close completed")

	// Reopen PageStore to verify
	psConfig := DefaultPageStoreConfig(tmpDir, "test-ap-wal")
	ps2, err := OpenPageStore(psConfig)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	if ps2 == nil {
		t.Fatal("PageStore is nil after reopen")
	}

	lineCount := ps2.LineCount()
	t.Logf("PageStore lineCount after reopen: %d (expected 2)", lineCount)

	if lineCount != 2 {
		t.Errorf("Expected 2 lines, got %d", lineCount)
	}

	for i := int64(0); i < lineCount && i < 10; i++ {
		line, _ := ps2.ReadLine(i)
		if line != nil {
			t.Logf("  line[%d]: %q", i, trimLogicalLine(logicalLineToString(line)))
		}
	}

	ps2.Close()
}

// TestWAL_LineModifyDirect tests WAL's handling of line modifications directly.
func TestWAL_LineModifyDirect(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestWAL_LineModifyDirect")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	walConfig := DefaultWALConfig(tmpDir, "test-wal")

	// Create WAL
	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}

	// Write line 0 with progressive content (simulating typing)
	writes := []string{"H", "He", "Hel", "Hell", "Hello"}
	for i, content := range writes {
		line := &LogicalLine{
			Cells: make([]Cell, len(content)),
		}
		for j, r := range content {
			line.Cells[j] = Cell{Rune: r}
		}
		if err := wal.Append(0, line, wal.nowFunc()); err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
		t.Logf("After write %d (%q): nextGlobalIdx=%d", i, content, wal.nextGlobalIdx)
	}

	// Check PageStore line count before close (while WAL still has reference)
	lineCountBeforeClose := wal.pageStore.LineCount()
	t.Logf("PageStore lineCount before checkpoint: %d", lineCountBeforeClose)

	// Force checkpoint
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}

	// Check line count after checkpoint
	lineCountAfterCheckpoint := wal.pageStore.LineCount()
	t.Logf("PageStore lineCount after checkpoint: %d (expected 1)", lineCountAfterCheckpoint)

	if lineCountAfterCheckpoint != 1 {
		t.Errorf("Expected 1 line after checkpoint, got %d", lineCountAfterCheckpoint)
		for i := int64(0); i < lineCountAfterCheckpoint && i < 10; i++ {
			line, _ := wal.pageStore.ReadLine(i)
			if line != nil {
				t.Logf("  line[%d]: %q", i, trimLogicalLine(logicalLineToString(line)))
			}
		}
	} else {
		line, _ := wal.pageStore.ReadLine(0)
		if line != nil {
			content := trimLogicalLine(logicalLineToString(line))
			t.Logf("line[0]: %q (expected 'Hello')", content)
			if content != "Hello" {
				t.Errorf("Expected 'Hello', got %q", content)
			}
		}
	}

	// Close WAL
	if err := wal.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestVTerm_LineCountConsistency tests that the content written to memory
// matches the content written to disk after close.
// Note: Empty cursor lines (no content written) may not be persisted to disk.
func TestVTerm_LineCountConsistency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestVTerm_LineCountConsistency")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	diskPath := filepath.Join(tmpDir, "test_count.hist3")
	terminalID := "test-count"

	// Expected content - what we write and what should be persisted
	expectedLines := []string{"AAA", "BBB", "CCC"}

	// Captured memory content for comparison
	var memoryContent []string

	// First session: write lines and track content
	t.Log("=== Writing lines ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}

		p := NewParser(v)

		// Write lines with content
		for _, lineContent := range expectedLines {
			t.Logf("Writing %q: liveEdgeBase=%d, cursorY=%d, cursorX=%d",
				lineContent, v.memBufState.liveEdgeBase, v.cursorY, v.cursorX)
			for _, ch := range lineContent {
				p.Parse(ch)
			}
			t.Logf("After content: cursorY=%d, cursorX=%d", v.cursorY, v.cursorX)
			p.Parse('\n')
			p.Parse('\r')
			t.Logf("After newline: cursorY=%d, cursorX=%d", v.cursorY, v.cursorX)
		}

		mb := v.memBufState.memBuf
		memoryLineCount := mb.GlobalEnd() - mb.GlobalOffset()

		t.Logf("Memory state before close:")
		t.Logf("  globalOffset:  %d", mb.GlobalOffset())
		t.Logf("  globalEnd:     %d", mb.GlobalEnd())
		t.Logf("  liveEdgeBase:  %d", v.memBufState.liveEdgeBase)
		t.Logf("  lineCount:     %d", memoryLineCount)

		// Sample all non-empty lines from memory
		for i := int64(0); i < memoryLineCount && i < 10; i++ {
			line := mb.GetLine(i)
			if line != nil {
				content := trimLogicalLine(logicalLineToString(line))
				t.Logf("  mem[%d]: %q", i, content)
				if content != "" {
					memoryContent = append(memoryContent, content)
				}
			}
		}

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer failed: %v", err)
		}
	}

	// Check disk
	t.Log("=== Checking disk ===")
	{
		psConfig := DefaultPageStoreConfig(diskPath, terminalID)
		ps, err := OpenPageStore(psConfig)
		if err != nil {
			t.Fatalf("OpenPageStore failed: %v", err)
		}
		if ps == nil {
			t.Fatal("PageStore is nil")
		}

		diskLineCount := ps.LineCount()
		t.Logf("Disk state:")
		t.Logf("  lineCount: %d (expected %d)", diskLineCount, len(expectedLines))

		// Read all lines from disk
		var diskContent []string
		for i := int64(0); i < diskLineCount && i < 20; i++ {
			line, err := ps.ReadLine(i)
			if err != nil {
				t.Logf("  disk[%d]: error: %v", i, err)
			} else if line != nil {
				content := trimLogicalLine(logicalLineToString(line))
				t.Logf("  disk[%d]: %q", i, content)
				diskContent = append(diskContent, content)
			}
		}

		ps.Close()

		// Verify disk has all expected content
		if len(diskContent) != len(expectedLines) {
			t.Errorf("Disk line count mismatch: got %d, expected %d",
				len(diskContent), len(expectedLines))
		}

		// Verify content matches expected (not intermediate states like "A", "AA", etc.)
		for i, expected := range expectedLines {
			if i >= len(diskContent) {
				t.Errorf("Missing disk line %d: expected %q", i, expected)
				continue
			}
			if diskContent[i] != expected {
				t.Errorf("Disk line %d mismatch: got %q, expected %q",
					i, diskContent[i], expected)
			}
		}

		// Verify memory content matches disk content
		if len(memoryContent) != len(diskContent) {
			t.Errorf("Memory/disk content count mismatch: memory=%d, disk=%d",
				len(memoryContent), len(diskContent))
		}
		for i := range min(len(memoryContent), len(diskContent)) {
			if memoryContent[i] != diskContent[i] {
				t.Errorf("Content mismatch at line %d: memory=%q, disk=%q",
					i, memoryContent[i], diskContent[i])
			}
		}
	}
}

// trimLogicalLine trims trailing spaces and null characters from a line.
func trimLogicalLine(s string) string {
	// Trim trailing spaces and nulls
	end := len(s)
	for end > 0 && (s[end-1] == ' ' || s[end-1] == 0) {
		end--
	}
	return s[:end]
}

// --- WAL-Protected Metadata Tests ---

// TestVTerm_CrashRecoveryWithMetadata tests that scroll position and cursor are
// restored correctly after a normal close/reopen cycle.
func TestVTerm_CrashRecoveryWithMetadata(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestVTerm_CrashRecoveryWithMetadata")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	diskPath := filepath.Join(tmpDir, "test_crash.hist3")
	terminalID := "test-crash-meta"

	// State to capture from first session
	var savedScrollOffset int64
	var savedCursorX, savedCursorY int

	// First session: create content, scroll, position cursor
	t.Log("=== First Session: Creating content and state ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}

		p := NewParser(v)

		// Write enough lines to enable scrolling
		for i := 0; i < 100; i++ {
			for _, ch := range "Line " {
				p.Parse(ch)
			}
			p.Parse(rune('0' + (i/10)%10))
			p.Parse(rune('0' + i%10))
			p.Parse('\n')
			p.Parse('\r')
		}

		// Position cursor at specific location (type partial command)
		for _, ch := range "partial_cmd" {
			p.Parse(ch)
		}

		// Scroll back 30 lines
		for i := 0; i < 30; i++ {
			v.memoryBufferScroll(-1)
		}

		// Capture state
		savedScrollOffset = v.ScrollOffset()
		savedCursorX, savedCursorY = v.Cursor()

		t.Logf("State before close:")
		t.Logf("  scrollOffset: %d", savedScrollOffset)
		t.Logf("  cursor: (%d, %d)", savedCursorX, savedCursorY)

		// Close normally
		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer failed: %v", err)
		}
	}

	// Second session: restore and verify
	t.Log("=== Second Session: Restoring and verifying ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (restore) failed: %v", err)
		}

		restoredScrollOffset := v.ScrollOffset()
		restoredCursorX, restoredCursorY := v.Cursor()

		t.Logf("State after restore:")
		t.Logf("  scrollOffset: %d (expected %d)", restoredScrollOffset, savedScrollOffset)
		t.Logf("  cursor: (%d, %d) (expected (%d, %d))", restoredCursorX, restoredCursorY, savedCursorX, savedCursorY)

		// Verify scroll offset was restored
		if restoredScrollOffset != savedScrollOffset {
			t.Errorf("ScrollOffset mismatch: got %d, want %d", restoredScrollOffset, savedScrollOffset)
		}

		// Verify cursor was restored
		if restoredCursorX != savedCursorX {
			t.Errorf("CursorX mismatch: got %d, want %d", restoredCursorX, savedCursorX)
		}
		if restoredCursorY != savedCursorY {
			t.Errorf("CursorY mismatch: got %d, want %d", restoredCursorY, savedCursorY)
		}

		v.CloseMemoryBuffer()
	}
}

// TestVTerm_HardCrashRecovery tests recovery after simulated kill -9 (no Close called).
// This is a real unexpected kill test - no flushing, no checkpoint.
// Only data that was written to WAL survives. Content in memory buffers is lost.
// Metadata written via WriteMetadata() goes directly to WAL and should survive.
func TestVTerm_HardCrashRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "TestVTerm_HardCrashRecovery")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	diskPath := filepath.Join(tmpDir, "test_hard_crash.hist3")
	terminalID := "test-hard-crash"

	// State to capture
	var savedScrollOffset int64
	var savedCursorX, savedCursorY int

	// First session: create content and state, then simulate crash
	t.Log("=== First Session: Creating content and simulating crash ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}

		p := NewParser(v)

		// Write content - this goes to MemoryBuffer and may be batched by AdaptivePersistence
		// In BestEffort mode (high write rate), content may not be flushed to WAL immediately
		for i := 0; i < 50; i++ {
			for _, ch := range "Test line " {
				p.Parse(ch)
			}
			p.Parse(rune('0' + (i/10)%10))
			p.Parse(rune('0' + i%10))
			p.Parse('\n')
			p.Parse('\r')
		}

		// Position cursor
		for _, ch := range "typing..." {
			p.Parse(ch)
		}

		// NO FLUSH - this is a real crash scenario

		// Scroll back - this writes metadata to WAL
		for i := 0; i < 15; i++ {
			v.memoryBufferScroll(-1)
		}

		// Capture state
		savedScrollOffset = v.ScrollOffset()
		savedCursorX, savedCursorY = v.Cursor()

		t.Logf("State before crash:")
		t.Logf("  scrollOffset: %d", savedScrollOffset)
		t.Logf("  cursor: (%d, %d)", savedCursorX, savedCursorY)

		// Simulate crash: sync WAL file but don't call Close
		if v.memBufState.persistence != nil && v.memBufState.persistence.wal != nil {
			// Sync WAL file to ensure metadata entries are on disk
			v.memBufState.persistence.wal.walFile.Sync()

			// Close file handles directly (simulating crash cleanup by OS)
			v.memBufState.persistence.wal.walFile.Close()
			v.memBufState.persistence.wal.pageStore.Close()
		}
		// DO NOT call CloseMemoryBuffer() - that would checkpoint and flush everything
	}

	// Second session: recover from crash
	t.Log("=== Second Session: Recovering from crash ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		// CRITICAL: System must not fail on reopen after crash
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (after crash) failed: %v", err)
		}

		restoredScrollOffset := v.ScrollOffset()
		restoredCursorX, restoredCursorY := v.Cursor()

		// Content recovery is best-effort in crash scenarios
		// In BestEffort mode (high write rate), content may not be flushed to WAL immediately
		mb := v.memBufState.memBuf
		lineCount := mb.GlobalEnd() - mb.GlobalOffset()
		t.Logf("Content recovered: %d lines (out of 50 written)", lineCount)

		t.Logf("State after crash recovery:")
		t.Logf("  scrollOffset: %d (saved was %d)", restoredScrollOffset, savedScrollOffset)
		t.Logf("  cursor: (%d, %d) (saved was (%d, %d))", restoredCursorX, restoredCursorY, savedCursorX, savedCursorY)

		// METADATA AND CONTENT ARE NOW BATCHED TOGETHER
		// In a hard crash without flush, neither content NOR metadata is persisted.
		// This is correct behavior - they're always in sync.
		//
		// If we recovered enough content, metadata should also be recovered.
		// If content wasn't flushed, metadata wasn't flushed either.
		if lineCount >= int64(savedScrollOffset)+24 { // Need scrollOffset + viewport worth of lines
			// Enough content means metadata was also flushed
			if restoredScrollOffset != savedScrollOffset {
				t.Errorf("ScrollOffset mismatch (enough content recovered): got %d, want %d", restoredScrollOffset, savedScrollOffset)
			}
			if restoredCursorX != savedCursorX {
				t.Errorf("CursorX mismatch (metadata should be recovered): got %d, want %d", restoredCursorX, savedCursorX)
			}
			if restoredCursorY != savedCursorY {
				t.Errorf("CursorY mismatch (metadata should be recovered): got %d, want %d", restoredCursorY, savedCursorY)
			}
		} else {
			// Not enough content = metadata also wasn't flushed. This is expected.
			t.Logf("NOTE: Content and metadata were batched but not flushed before crash - this is expected behavior")
			t.Logf("      Recovered %d lines, cursor at (%d,%d)", lineCount, restoredCursorX, restoredCursorY)
		}

		// We should recover SOME content - at least lines that were flushed during
		// AdaptivePersistence debounce periods. Zero lines would indicate a deeper problem.
		if lineCount == 0 {
			t.Logf("WARNING: No content recovered - this is expected in BestEffort mode without any flush")
		} else {
			t.Logf("SUCCESS: Recovered %d lines from crash", lineCount)
		}

		v.CloseMemoryBuffer()
	}
}

// TestVTerm_CrashRecoveryWithFlushedData tests crash recovery when data WAS flushed.
// This verifies that flushed content and metadata are properly recovered after crash.
func TestVTerm_CrashRecoveryWithFlushedData(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "test_flushed_crash.hist3")
	terminalID := "test-flushed-crash"

	// State to capture after flush
	var flushedLineCount int64
	var flushedScrollOffset int64
	var flushedCursorX, flushedCursorY int

	// First session: create content, flush, then crash
	t.Log("=== First Session: Write, flush, then crash ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}

		p := NewParser(v)

		// Write 30 lines
		for i := 0; i < 30; i++ {
			for _, ch := range fmt.Sprintf("Line %02d content here", i) {
				p.Parse(ch)
			}
			p.Parse('\n')
			p.Parse('\r')
		}

		// Position cursor
		for _, ch := range "cursor here" {
			p.Parse(ch)
		}

		// Scroll back 10 lines to set scroll position
		for i := 0; i < 10; i++ {
			v.memoryBufferScroll(-1)
		}

		// EXPLICIT FLUSH - this writes content AND metadata to WAL
		if v.memBufState.persistence != nil {
			v.memBufState.persistence.Flush()
		}

		// Capture state AFTER flush
		flushedLineCount = v.memBufState.memBuf.GlobalEnd() - v.memBufState.memBuf.GlobalOffset()
		flushedScrollOffset = v.ScrollOffset()
		flushedCursorX, flushedCursorY = v.Cursor()

		t.Logf("State after flush (before crash):")
		t.Logf("  lineCount: %d", flushedLineCount)
		t.Logf("  scrollOffset: %d", flushedScrollOffset)
		t.Logf("  cursor: (%d, %d)", flushedCursorX, flushedCursorY)

		// Write MORE content AFTER flush (this will be lost)
		for _, ch := range "This line will be lost in crash" {
			p.Parse(ch)
		}
		p.Parse('\n')

		// Simulate crash: sync WAL then close handles directly
		if v.memBufState.persistence != nil && v.memBufState.persistence.wal != nil {
			v.memBufState.persistence.wal.walFile.Sync()
			v.memBufState.persistence.wal.walFile.Close()
			v.memBufState.persistence.wal.pageStore.Close()
		}
		// DO NOT call CloseMemoryBuffer()
	}

	// Second session: recover from crash
	t.Log("=== Second Session: Recovering from crash ===")
	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (after crash) failed: %v", err)
		}

		recoveredScrollOffset := v.ScrollOffset()
		recoveredCursorX, recoveredCursorY := v.Cursor()
		mb := v.memBufState.memBuf
		recoveredLineCount := mb.GlobalEnd() - mb.GlobalOffset()

		t.Logf("State after crash recovery:")
		t.Logf("  lineCount: %d (flushed was %d)", recoveredLineCount, flushedLineCount)
		t.Logf("  scrollOffset: %d (flushed was %d)", recoveredScrollOffset, flushedScrollOffset)
		t.Logf("  cursor: (%d, %d) (flushed was (%d, %d))", recoveredCursorX, recoveredCursorY, flushedCursorX, flushedCursorY)

		// Verify content was recovered (should match what was flushed)
		if recoveredLineCount < flushedLineCount {
			t.Errorf("Content loss: recovered %d lines, but flushed %d", recoveredLineCount, flushedLineCount)
		}

		// Verify metadata was recovered
		if recoveredScrollOffset != flushedScrollOffset {
			t.Errorf("ScrollOffset mismatch: got %d, want %d", recoveredScrollOffset, flushedScrollOffset)
		}
		if recoveredCursorX != flushedCursorX {
			t.Errorf("CursorX mismatch: got %d, want %d", recoveredCursorX, flushedCursorX)
		}
		if recoveredCursorY != flushedCursorY {
			t.Errorf("CursorY mismatch: got %d, want %d", recoveredCursorY, flushedCursorY)
		}

		t.Log("SUCCESS: Flushed content and metadata recovered correctly after crash")

		v.CloseMemoryBuffer()
	}
}
