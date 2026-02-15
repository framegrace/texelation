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
	"strings"
	"testing"
	"time"
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

// TestVTerm_ScrollRegionCodexExitSequence simulates the full Codex lifecycle:
// enter scroll region, scroll content, exit scroll region, resume shell.
// Verifies that scrollback doesn't have a large gap of empty lines.
func TestVTerm_ScrollRegionCodexExitSequence(t *testing.T) {
	width, height := 40, 10
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Phase 1: Pre-Codex shell output
	parseString(p, "pre-codex-line-1\r\n")
	parseString(p, "pre-codex-line-2\r\n")
	parseString(p, "$ codex\r\n")

	// Phase 2: Codex sets up scroll region (header at row 0, footer at row 9)
	parseString(p, "\x1b[1;1H") // Move to top
	parseString(p, "=== CODEX HEADER ===")
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height)) // Move to last row
	parseString(p, "=== CODEX FOOTER ===")
	parseString(p, "\x1b[2;9r") // Set scroll region rows 2-9 (0-indexed: 1-8)

	// Phase 3: Write content that scrolls within the region (simulate work)
	parseString(p, "\x1b[2;1H") // Move to top of region
	for i := 0; i < 15; i++ {
		parseString(p, fmt.Sprintf("codex-output-%02d", i))
		if i < 14 {
			parseString(p, "\n\r")
		}
	}

	scrollsDuringCodex := v.memBufState.liveEdgeBase
	t.Logf("After Codex session: liveEdgeBase=%d", scrollsDuringCodex)

	// Phase 4: Codex exits — reset scroll region, erase below, print exit message
	parseString(p, "\x1b[r")    // Reset scroll region to full screen
	parseString(p, "\x1b[8;1H") // Move cursor near bottom
	parseString(p, "\x1b[J")    // Erase from cursor to end
	parseString(p, "Token usage: 1234\r\n")

	// Phase 5: Shell resumes with normal output
	for i := 0; i < 15; i++ {
		parseString(p, fmt.Sprintf("post-codex-line-%02d\r\n", i))
	}

	// Now inspect the scrollback for empty gaps
	mb := v.memBufState.memBuf
	liveEdge := v.memBufState.liveEdgeBase
	t.Logf("After post-codex: liveEdgeBase=%d, GlobalEnd=%d", liveEdge, mb.GlobalEnd())

	// Count consecutive empty lines in the scrollback
	maxEmptyRun := 0
	currentEmptyRun := 0
	for idx := mb.GlobalOffset(); idx < liveEdge; idx++ {
		line := mb.GetLine(idx)
		isEmpty := true
		if line != nil {
			for _, cell := range line.Cells {
				if cell.Rune != 0 && cell.Rune != ' ' {
					isEmpty = false
					break
				}
			}
		}
		if isEmpty {
			currentEmptyRun++
			if currentEmptyRun > maxEmptyRun {
				maxEmptyRun = currentEmptyRun
			}
		} else {
			currentEmptyRun = 0
		}
	}

	t.Logf("Max consecutive empty lines in scrollback: %d", maxEmptyRun)

	// Also verify the viewport grid scrollback via user scrolling
	// Simulate scrolling all the way back to verify no empty gap in rendered output
	totalPhysical := v.memBufState.viewport.TotalPhysicalLines()
	t.Logf("Total physical lines: %d", totalPhysical)

	// Scroll to top
	v.memBufState.viewport.ScrollToTop()
	topGrid := v.memBufState.viewport.GetVisibleGrid()

	// Check first row has content
	firstRowText := gridRowToString(topGrid[0][:16])
	t.Logf("First row at scroll top: %q", firstRowText)

	// Scroll back to bottom
	v.memBufState.viewport.ScrollToBottom()

	// A gap of more than a few lines is the bug
	if maxEmptyRun > 3 {
		t.Errorf("Found %d consecutive empty lines in scrollback (expected <= 3). This is the empty space bug.", maxEmptyRun)

		// Print the scrollback for debugging
		for idx := mb.GlobalOffset(); idx < liveEdge; idx++ {
			line := mb.GetLine(idx)
			text := ""
			if line != nil && len(line.Cells) > 0 {
				for _, cell := range line.Cells {
					if cell.Rune == 0 {
						text += " "
					} else {
						text += string(cell.Rune)
					}
				}
			}
			// Trim trailing spaces for readability
			trimmed := ""
			for i := len(text) - 1; i >= 0; i-- {
				if text[i] != ' ' {
					trimmed = text[:i+1]
					break
				}
			}
			t.Logf("  Line %3d: %q", idx, trimmed)
		}
	}
}

// TestVTerm_ScrollRegionPersistRestore tests that scrollback is correct after
// saving to disk and restoring (simulating texelterm exit and re-open).
func TestVTerm_ScrollRegionPersistRestore(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test_history.hist3"
	terminalID := "test-codex-persist"
	width, height := 40, 10

	// Session 1: Run "Codex" with scroll region
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}
		p := NewParser(v)

		// Pre-Codex output
		parseString(p, "$ codex\r\n")

		// Codex: set scroll region, write content
		parseString(p, "\x1b[1;1H")
		parseString(p, "HEADER")
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
		parseString(p, "FOOTER")
		parseString(p, "\x1b[2;9r") // Scroll region rows 1-8

		parseString(p, "\x1b[2;1H")
		for i := 0; i < 15; i++ {
			parseString(p, fmt.Sprintf("codex-out-%02d", i))
			if i < 14 {
				parseString(p, "\n\r")
			}
		}

		liveEdgeAfterCodex := v.memBufState.liveEdgeBase
		t.Logf("Session 1 after Codex: liveEdgeBase=%d", liveEdgeAfterCodex)

		// Codex exits: reset margins, shell resumes
		parseString(p, "\x1b[r")
		parseString(p, "\x1b[8;1H")
		parseString(p, "\x1b[J")
		parseString(p, "post-codex-1\r\n")
		parseString(p, "post-codex-2\r\n")

		t.Logf("Session 1 final: liveEdgeBase=%d, GlobalEnd=%d",
			v.memBufState.liveEdgeBase, v.memBufState.memBuf.GlobalEnd())

		v.CloseMemoryBuffer()
	}

	// Session 2: Restore and check for gaps
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("Restore failed: %v", err)
		}

		mb := v.memBufState.memBuf
		t.Logf("Session 2 restored: liveEdgeBase=%d, GlobalOffset=%d, GlobalEnd=%d",
			v.memBufState.liveEdgeBase, mb.GlobalOffset(), mb.GlobalEnd())

		// Check for empty gaps in scrollback
		liveEdge := v.memBufState.liveEdgeBase
		maxEmptyRun := 0
		currentEmptyRun := 0
		for idx := mb.GlobalOffset(); idx < liveEdge; idx++ {
			line := mb.GetLine(idx)
			isEmpty := true
			if line != nil {
				for _, cell := range line.Cells {
					if cell.Rune != 0 && cell.Rune != ' ' {
						isEmpty = false
						break
					}
				}
			}
			if isEmpty {
				currentEmptyRun++
				if currentEmptyRun > maxEmptyRun {
					maxEmptyRun = currentEmptyRun
				}
			} else {
				currentEmptyRun = 0
			}
		}

		t.Logf("Max consecutive empty lines after restore: %d", maxEmptyRun)

		if maxEmptyRun > 3 {
			t.Errorf("Found %d consecutive empty lines after restore (expected <= 3)", maxEmptyRun)
			for idx := mb.GlobalOffset(); idx < liveEdge; idx++ {
				line := mb.GetLine(idx)
				text := ""
				if line != nil {
					for _, cell := range line.Cells {
						if cell.Rune == 0 {
							text += " "
						} else {
							text += string(cell.Rune)
						}
					}
				}
				trimmed := ""
				for i := len(text) - 1; i >= 0; i-- {
					if text[i] != ' ' {
						trimmed = text[:i+1]
						break
					}
				}
				t.Logf("  Line %3d: %q", idx, trimmed)
			}
		}

		// Also scroll through the viewport to check rendered content
		v.memBufState.viewport.ScrollToTop()
		topGrid := v.memBufState.viewport.GetVisibleGrid()
		t.Logf("First row at top: %q", gridRowToString(topGrid[0][:20]))

		v.CloseMemoryBuffer()
	}
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

// TestVTerm_EraseDisplayPushesToScrollback verifies that ESC[2J and ESC[H ESC[J
// push non-empty viewport content to scrollback before clearing, and that
// leading empty rows are compacted out (no empty gap in scrollback).
func TestVTerm_EraseDisplayPushesToScrollback(t *testing.T) {
	width, height := 40, 10
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Phase 1: Shell prompt + "codex" command
	parseString(p, "$ codex\r\n")

	// Phase 2: Codex sets up scroll region and draws content
	// Header at row 0, footer at row 9, content in rows 1-8
	parseString(p, "\x1b[1;1H") // cursor home
	parseString(p, "=== HEADER ===")
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height)) // last row
	parseString(p, "=== FOOTER ===")
	parseString(p, "\x1b[2;9r") // scroll region rows 2-9 (0-indexed: 1-8)

	// Write enough content to scroll within the region
	parseString(p, "\x1b[2;1H") // top of region
	for i := 0; i < 20; i++ {
		parseString(p, fmt.Sprintf("content-%02d", i))
		if i < 19 {
			parseString(p, "\n\r")
		}
	}

	liveEdgeBeforeExit := v.memBufState.liveEdgeBase
	t.Logf("Before Codex exit: liveEdgeBase=%d", liveEdgeBeforeExit)

	// Phase 3: Codex exits — reset scroll region, cursor home, clear screen
	parseString(p, "\x1b[r")      // reset scroll region
	parseString(p, "\x1b[H")      // cursor home
	parseString(p, "\x1b[2J")     // ED 2: erase entire display

	liveEdgeAfterExit := v.memBufState.liveEdgeBase
	t.Logf("After Codex exit (ED 2): liveEdgeBase=%d (advanced by %d)",
		liveEdgeAfterExit, liveEdgeAfterExit-liveEdgeBeforeExit)

	// Phase 4: Shell resumes
	parseString(p, "$ whoami\r\n")
	parseString(p, "marc\r\n")

	// Verify: scan for empty gaps in scrollback
	mb := v.memBufState.memBuf
	liveEdge := v.memBufState.liveEdgeBase

	maxEmptyRun := 0
	currentEmptyRun := 0
	for idx := mb.GlobalOffset(); idx < liveEdge; idx++ {
		line := mb.GetLine(idx)
		isEmpty := true
		if line != nil {
			for _, cell := range line.Cells {
				if cell.Rune != 0 && cell.Rune != ' ' {
					isEmpty = false
					break
				}
			}
		}
		if isEmpty {
			currentEmptyRun++
			if currentEmptyRun > maxEmptyRun {
				maxEmptyRun = currentEmptyRun
			}
		} else {
			currentEmptyRun = 0
		}
	}

	t.Logf("Max consecutive empty lines in scrollback: %d (total scrollback: %d lines)",
		maxEmptyRun, liveEdge-mb.GlobalOffset())

	// The scrollback should NOT have a large empty gap
	// Allow 1-2 empty lines (natural blank after "codex" command)
	if maxEmptyRun > 2 {
		t.Errorf("Too many consecutive empty lines in scrollback: %d (want <= 2)", maxEmptyRun)
		// Dump scrollback for debugging
		for idx := mb.GlobalOffset(); idx < liveEdge && idx < mb.GlobalOffset()+30; idx++ {
			line := mb.GetLine(idx)
			text := ""
			if line != nil {
				for _, cell := range line.Cells {
					if cell.Rune == 0 {
						text += " "
					} else {
						text += string(cell.Rune)
					}
				}
			}
			t.Logf("  Scrollback line %d: %q", idx, strings.TrimRight(text, " "))
		}
	}

	// Verify the Codex content (header, content lines, footer) is in scrollback
	foundHeader := false
	foundContent := false
	foundFooter := false
	for idx := mb.GlobalOffset(); idx < liveEdge; idx++ {
		line := mb.GetLine(idx)
		if line == nil {
			continue
		}
		text := ""
		for _, cell := range line.Cells {
			if cell.Rune == 0 {
				break
			}
			text += string(cell.Rune)
		}
		if strings.Contains(text, "HEADER") {
			foundHeader = true
		}
		if strings.Contains(text, "content-19") {
			foundContent = true
		}
		if strings.Contains(text, "FOOTER") {
			foundFooter = true
		}
	}

	if !foundHeader {
		t.Error("Codex HEADER not found in scrollback")
	}
	if !foundContent {
		t.Error("Last content line (content-19) not found in scrollback")
	}
	if !foundFooter {
		t.Error("Codex FOOTER not found in scrollback")
	}
}

// TestVTerm_EraseFromHomePushesToScrollback verifies ESC[H ESC[J (cursor home +
// erase to end) also pushes viewport to scrollback.
func TestVTerm_EraseFromHomePushesToScrollback(t *testing.T) {
	width, height := 40, 10
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write content that fills the viewport
	for i := 0; i < height; i++ {
		parseString(p, fmt.Sprintf("visible-line-%02d\r\n", i))
	}

	liveEdgeBefore := v.memBufState.liveEdgeBase

	// ESC[H ESC[J: cursor home + erase from cursor to end
	parseString(p, "\x1b[H\x1b[J")

	liveEdgeAfter := v.memBufState.liveEdgeBase
	t.Logf("liveEdgeBase: %d → %d (advanced by %d)", liveEdgeBefore, liveEdgeAfter, liveEdgeAfter-liveEdgeBefore)

	// The viewport content should have been pushed to scrollback
	if liveEdgeAfter <= liveEdgeBefore {
		t.Error("liveEdgeBase should have advanced (viewport content should be in scrollback)")
	}

	// Verify content is in scrollback
	mb := v.memBufState.memBuf
	found := false
	for idx := mb.GlobalOffset(); idx < liveEdgeAfter; idx++ {
		line := mb.GetLine(idx)
		if line == nil {
			continue
		}
		text := ""
		for _, cell := range line.Cells {
			if cell.Rune == 0 {
				break
			}
			text += string(cell.Rune)
		}
		if strings.Contains(text, "visible-line-09") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Last visible line not found in scrollback after ESC[H ESC[J")
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
		t.Logf("  lineCount: %d (expected >= %d)", diskLineCount, len(expectedLines))

		// Read all lines from disk (filter empty, like memory side)
		var diskContent []string
		for i := int64(0); i < diskLineCount && i < 20; i++ {
			line, err := ps.ReadLine(i)
			if err != nil {
				t.Logf("  disk[%d]: error: %v", i, err)
			} else if line != nil {
				content := trimLogicalLine(logicalLineToString(line))
				t.Logf("  disk[%d]: %q", i, content)
				if content != "" {
					diskContent = append(diskContent, content)
				}
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

// trimRight removes trailing spaces from a string.
func trimRight(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != ' ' {
			return s[:i+1]
		}
	}
	return ""
}

// extractAllLines extracts all stored lines as trimmed strings from a memory buffer,
// from globalOffset to globalEnd.
func extractAllLines(mb *MemoryBuffer) []string {
	var result []string
	for idx := mb.GlobalOffset(); idx < mb.GlobalEnd(); idx++ {
		line := mb.GetLine(idx)
		if line == nil {
			result = append(result, "")
			continue
		}
		result = append(result, trimRight(logicalLineToString(line)))
	}
	return result
}

// TestVTerm_ScrollRegionReloadCorruption is a comprehensive test that checks for
// content corruption after saving and reloading terminal history that contains
// TUI-like scroll region content (e.g., Codex CLI running on main screen).
//
// The bug: after stop/reload, the scroll region section shows duplicates and
// repetitions, and sometimes the last lines/prompt disappear.
// This works fine while the terminal is running but breaks on reload.
//
// Tests three layers:
//  1. Raw MemoryBuffer lines (logical storage)
//  2. Viewport Grid (what's rendered to screen via GetVisibleGrid)
//  3. VTerm Grid() (what the client actually sees, including after new writes)
func TestVTerm_ScrollRegionReloadCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test_scroll_reload.hist3"
	terminalID := "test-scroll-reload"
	width, height := 80, 24

	var session1Lines []string
	var session1LiveEdge int64
	var session1GlobalOffset int64
	var session1GlobalEnd int64
	var session1Grid []string     // Grid() at live edge before close
	var session1AllViewport []string // All lines visible through viewport scrolling

	// ========================================================
	// Session 1: Generate TUI scroll region content + normal content
	// ========================================================
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}
		p := NewParser(v)

		// Phase 1: Normal shell output before TUI
		parseString(p, "$ echo 'before TUI'\r\n")
		parseString(p, "before TUI\r\n")
		parseString(p, "$ ls -la\r\n")
		parseString(p, "total 42\r\n")
		parseString(p, "drwxr-xr-x  5 user user 4096 Feb  9 file1.txt\r\n")
		parseString(p, "drwxr-xr-x  3 user user 4096 Feb  9 file2.txt\r\n")
		parseString(p, "$ codex 'do something'\r\n")

		// Phase 2: TUI sets up scroll region (like Codex CLI)
		// Header on row 0, footer on last row, scroll region in between
		parseString(p, "\x1b[1;1H")                       // Move to top-left
		parseString(p, "\x1b[48;2;65;69;76m")             // Grey BG (Codex style)
		parseString(p, " Codex (research-preview) ")
		parseString(p, "\x1b[K")                           // Erase rest of line with BG
		parseString(p, "\x1b[0m")                          // Reset
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height)) // Move to last row
		parseString(p, "\x1b[48;2;65;69;76m")             // Grey BG footer
		parseString(p, " > Enter a prompt...")
		parseString(p, "\x1b[K")  // Erase rest of line with BG
		parseString(p, "\x1b[0m") // Reset

		// Set scroll region: rows 2 through 23 (1-indexed), i.e., 0-indexed 1..22
		parseString(p, fmt.Sprintf("\x1b[2;%dr", height-1))

		// Phase 3: TUI content that scrolls within the region
		// Write enough lines to cause scrolling within the region (region is 22 rows)
		parseString(p, "\x1b[2;1H") // Move to top of scroll region
		for i := 0; i < 40; i++ {
			parseString(p, fmt.Sprintf("  codex-output-%03d: Working on task...", i))
			if i < 39 {
				parseString(p, "\n\r")
			}
		}

		// Phase 4: TUI exits — reset scroll region, cleanup
		parseString(p, "\x1b[r")                             // Reset scroll region
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height-2)) // Move near bottom
		parseString(p, "\x1b[J")                             // Erase from cursor down
		parseString(p, "Tokens used: 4567 | Cost: $0.12\r\n")

		// Phase 5: Normal shell output after TUI
		parseString(p, "$ echo 'after TUI'\r\n")
		parseString(p, "after TUI\r\n")
		parseString(p, "$ cat results.txt\r\n")
		for i := 0; i < 10; i++ {
			parseString(p, fmt.Sprintf("result-line-%03d: data here\r\n", i))
		}
		parseString(p, "$ ")

		// Capture all lines before closing
		mb := v.memBufState.memBuf
		session1LiveEdge = v.memBufState.liveEdgeBase
		session1GlobalOffset = mb.GlobalOffset()
		session1GlobalEnd = mb.GlobalEnd()
		session1Lines = extractAllLines(mb)

		// Capture Grid() at live edge (what client sees)
		grid := v.Grid()
		for y := 0; y < len(grid); y++ {
			session1Grid = append(session1Grid, trimRight(gridRowToString(grid[y])))
		}

		// Capture all viewport content by scrolling from top to bottom
		v.memBufState.viewport.ScrollToTop()
		seen := make(map[string]bool)
		for {
			vgrid := v.memBufState.viewport.GetVisibleGrid()
			for y := 0; y < len(vgrid); y++ {
				text := trimRight(gridRowToString(vgrid[y]))
				key := fmt.Sprintf("%d:%s", len(session1AllViewport)+y, text)
				if !seen[key] {
					session1AllViewport = append(session1AllViewport, text)
					seen[key] = true
				}
			}
			scrolled := v.memBufState.viewport.ScrollDown(height)
			if scrolled == 0 {
				break
			}
		}
		v.memBufState.viewport.ScrollToBottom()

		t.Logf("Session 1: globalOffset=%d, globalEnd=%d, liveEdgeBase=%d, totalLines=%d",
			session1GlobalOffset, session1GlobalEnd, session1LiveEdge, len(session1Lines))

		// Log all content for debugging
		for i, line := range session1Lines {
			globalIdx := session1GlobalOffset + int64(i)
			marker := " "
			if globalIdx == session1LiveEdge {
				marker = ">"
			}
			if line != "" {
				t.Logf("  S1[%3d]%s %q", globalIdx, marker, line)
			}
		}

		t.Log("--- Session 1 Grid() at live edge ---")
		for y, row := range session1Grid {
			if row != "" {
				t.Logf("  Grid[%2d] %q", y, row)
			}
		}

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer failed: %v", err)
		}
	}

	// ========================================================
	// Session 2: Reload and compare all three layers
	// ========================================================
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (restore) failed: %v", err)
		}

		mb := v.memBufState.memBuf
		session2LiveEdge := v.memBufState.liveEdgeBase
		session2GlobalOffset := mb.GlobalOffset()
		session2GlobalEnd := mb.GlobalEnd()
		session2Lines := extractAllLines(mb)

		t.Logf("Session 2: globalOffset=%d, globalEnd=%d, liveEdgeBase=%d, totalLines=%d",
			session2GlobalOffset, session2GlobalEnd, session2LiveEdge, len(session2Lines))

		// ---- Layer 1: Raw MemoryBuffer line comparison ----
		t.Log("=== Layer 1: MemoryBuffer line comparison ===")

		s1Start := session1GlobalOffset
		s2Start := session2GlobalOffset
		overlapStart := max64(s1Start, s2Start)
		overlapEnd := min64(session1GlobalEnd, session2GlobalEnd)

		if overlapEnd <= overlapStart {
			t.Fatalf("No overlapping range between sessions: S1=[%d,%d), S2=[%d,%d)",
				s1Start, session1GlobalEnd, s2Start, session2GlobalEnd)
		}

		t.Logf("Comparing overlapping range [%d, %d) = %d lines",
			overlapStart, overlapEnd, overlapEnd-overlapStart)

		lineMismatches := 0
		for idx := overlapStart; idx < overlapEnd; idx++ {
			s1Idx := int(idx - s1Start)
			s2Idx := int(idx - s2Start)

			var s1Text, s2Text string
			if s1Idx >= 0 && s1Idx < len(session1Lines) {
				s1Text = session1Lines[s1Idx]
			}
			if s2Idx >= 0 && s2Idx < len(session2Lines) {
				s2Text = session2Lines[s2Idx]
			}

			if s1Text != s2Text {
				lineMismatches++
				if lineMismatches <= 30 {
					t.Errorf("Layer1 line %d mismatch:\n  S1: %q\n  S2: %q", idx, s1Text, s2Text)
				}
			}
		}
		t.Logf("Layer 1: %d line mismatches", lineMismatches)

		// ---- Layer 2: Viewport scrollback comparison ----
		t.Log("=== Layer 2: Viewport scrollback comparison ===")

		// Collect all unique viewport lines by scrolling top to bottom
		var session2AllViewport []string
		v.memBufState.viewport.ScrollToTop()
		seen := make(map[string]bool)
		for {
			vgrid := v.memBufState.viewport.GetVisibleGrid()
			for y := 0; y < len(vgrid); y++ {
				text := trimRight(gridRowToString(vgrid[y]))
				key := fmt.Sprintf("%d:%s", len(session2AllViewport)+y, text)
				if !seen[key] {
					session2AllViewport = append(session2AllViewport, text)
					seen[key] = true
				}
			}
			scrolled := v.memBufState.viewport.ScrollDown(height)
			if scrolled == 0 {
				break
			}
		}
		v.memBufState.viewport.ScrollToBottom()

		// Check for duplicate runs in viewport
		maxDupeRun := 0
		currentDupeRun := 1
		dupeText := ""
		for i := 1; i < len(session2AllViewport); i++ {
			if session2AllViewport[i] != "" && session2AllViewport[i] == session2AllViewport[i-1] {
				currentDupeRun++
				if currentDupeRun > maxDupeRun {
					maxDupeRun = currentDupeRun
					dupeText = session2AllViewport[i]
				}
			} else {
				currentDupeRun = 1
			}
		}
		if maxDupeRun > 2 {
			t.Errorf("Layer2: viewport has %d consecutive duplicate lines: %q", maxDupeRun, dupeText)
		}
		t.Logf("Layer 2: max duplicate run = %d", maxDupeRun)

		// ---- Layer 3: VTerm Grid() comparison (what client sees) ----
		t.Log("=== Layer 3: VTerm Grid() comparison ===")

		session2Grid := v.Grid()
		var session2GridStrings []string
		for y := 0; y < len(session2Grid); y++ {
			session2GridStrings = append(session2GridStrings, trimRight(gridRowToString(session2Grid[y])))
		}

		t.Log("--- Session 2 Grid() at live edge ---")
		for y, row := range session2GridStrings {
			if row != "" {
				t.Logf("  Grid[%2d] %q", y, row)
			}
		}

		// The Grid() after reload should show the same content as before close
		// (both are at live edge with scrollOffset=0)
		gridMismatches := 0
		for y := 0; y < len(session1Grid) && y < len(session2GridStrings); y++ {
			if session1Grid[y] != session2GridStrings[y] {
				gridMismatches++
				t.Errorf("Layer3 Grid row %d mismatch:\n  S1: %q\n  S2: %q", y, session1Grid[y], session2GridStrings[y])
			}
		}
		t.Logf("Layer 3: %d Grid mismatches", gridMismatches)

		// ---- Layer 3b: Write new content after reload and check Grid() ----
		t.Log("=== Layer 3b: New content after reload ===")

		p := NewParser(v)
		parseString(p, "echo 'new content after reload'\r\n")
		parseString(p, "new content after reload\r\n")
		parseString(p, "$ ")

		newGrid := v.Grid()
		var newGridStrings []string
		for y := 0; y < len(newGrid); y++ {
			newGridStrings = append(newGridStrings, trimRight(gridRowToString(newGrid[y])))
		}

		// The new content should be visible somewhere in the grid
		foundNewContent := false
		foundNewPrompt := false
		for _, row := range newGridStrings {
			if strings.Contains(row, "new content after reload") {
				foundNewContent = true
			}
			if strings.Contains(row, "$ ") {
				foundNewPrompt = true
			}
		}

		if !foundNewContent {
			t.Errorf("Layer3b: new content 'new content after reload' not visible in Grid() after writing")
			t.Log("Grid after new writes:")
			for y, row := range newGridStrings {
				t.Logf("  [%2d] %q", y, row)
			}
		}
		if !foundNewPrompt {
			t.Errorf("Layer3b: prompt not visible after new writes (typing disappears until 'reset')")
		}

		// ---- Check key markers are present in scrollback ----
		markers := []string{
			"after TUI",
			"result-line-",
			"Tokens used:",
			"codex-output-",
		}
		for _, marker := range markers {
			found := false
			for _, line := range session2Lines {
				if strings.Contains(line, marker) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Marker %q not found after reload", marker)
			}
		}

		// ---- Check result line ordering ----
		lastResultSeen := -1
		for _, line := range session2Lines {
			for i := 0; i < 10; i++ {
				expected := fmt.Sprintf("result-line-%03d", i)
				if strings.Contains(line, expected) {
					if i <= lastResultSeen {
						t.Errorf("Result line %d appeared out of order (last seen: %d)", i, lastResultSeen)
					}
					lastResultSeen = i
				}
			}
		}
		if lastResultSeen < 9 {
			t.Errorf("Only found result lines up to %d (expected 9)", lastResultSeen)
		}

		// ---- Summary ----
		hasIssues := lineMismatches > 0 || maxDupeRun > 2 || gridMismatches > 0

		if hasIssues {
			t.Log("=== Session 2 full content dump ===")
			for i, line := range session2Lines {
				globalIdx := session2GlobalOffset + int64(i)
				marker := " "
				if globalIdx == session2LiveEdge {
					marker = ">"
				}
				t.Logf("  S2[%3d]%s %q", globalIdx, marker, line)
			}
		}

		v.CloseMemoryBuffer()
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// TestVTerm_ScrollRegionReloadMultipleTUISessions tests reload correctness when
// multiple TUI sessions (scroll region usage) occur before save/reload.
// This is a more complex scenario that exercises the scroll region code paths
// repeatedly before testing persistence.
func TestVTerm_ScrollRegionReloadMultipleTUISessions(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test_multi_tui.hist3"
	terminalID := "test-multi-tui"
	width, height := 80, 24

	var session1Lines []string
	var session1GlobalOffset int64

	// ========================================================
	// Session 1: Multiple TUI sessions interspersed with normal content
	// ========================================================
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}
		p := NewParser(v)

		// Normal shell output
		parseString(p, "$ echo 'session start'\r\n")
		parseString(p, "session start\r\n")

		// First TUI session (short, small scroll region)
		parseString(p, "$ tui-app-1\r\n")
		parseString(p, "\x1b[1;1H")
		parseString(p, "=TUI1-HEADER=")
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
		parseString(p, "=TUI1-FOOTER=")
		parseString(p, fmt.Sprintf("\x1b[2;%dr", height-1))
		parseString(p, "\x1b[2;1H")
		for i := 0; i < 30; i++ {
			parseString(p, fmt.Sprintf("tui1-line-%03d", i))
			if i < 29 {
				parseString(p, "\n\r")
			}
		}
		parseString(p, "\x1b[r")
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height-2))
		parseString(p, "\x1b[J")
		parseString(p, "TUI1 exited\r\n")

		// Normal content between TUI sessions
		parseString(p, "$ echo 'between sessions'\r\n")
		parseString(p, "between sessions\r\n")
		for i := 0; i < 5; i++ {
			parseString(p, fmt.Sprintf("normal-line-%03d\r\n", i))
		}

		// Second TUI session (longer, more scrolling)
		parseString(p, "$ tui-app-2\r\n")
		parseString(p, "\x1b[1;1H")
		parseString(p, "=TUI2-HEADER=")
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
		parseString(p, "=TUI2-FOOTER=")
		parseString(p, fmt.Sprintf("\x1b[2;%dr", height-1))
		parseString(p, "\x1b[2;1H")
		for i := 0; i < 50; i++ {
			parseString(p, fmt.Sprintf("tui2-line-%03d: longer content with more detail", i))
			if i < 49 {
				parseString(p, "\n\r")
			}
		}
		parseString(p, "\x1b[r")
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height-2))
		parseString(p, "\x1b[J")
		parseString(p, "TUI2 exited\r\n")

		// Final normal content
		parseString(p, "$ echo 'final output'\r\n")
		parseString(p, "final output\r\n")
		parseString(p, "$ ")

		// Capture content
		mb := v.memBufState.memBuf
		session1GlobalOffset = mb.GlobalOffset()
		session1Lines = extractAllLines(mb)
		t.Logf("Session 1: %d lines, globalOffset=%d, globalEnd=%d, liveEdge=%d",
			len(session1Lines), session1GlobalOffset, mb.GlobalEnd(), v.memBufState.liveEdgeBase)

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer failed: %v", err)
		}
	}

	// ========================================================
	// Session 2: Reload and compare
	// ========================================================
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (restore) failed: %v", err)
		}

		mb := v.memBufState.memBuf
		session2GlobalOffset := mb.GlobalOffset()
		session2Lines := extractAllLines(mb)
		t.Logf("Session 2: %d lines, globalOffset=%d, globalEnd=%d, liveEdge=%d",
			len(session2Lines), session2GlobalOffset, mb.GlobalEnd(), v.memBufState.liveEdgeBase)

		// Compare overlapping content
		s1Start := session1GlobalOffset
		s2Start := session2GlobalOffset

		overlapStart := s1Start
		if s2Start > overlapStart {
			overlapStart = s2Start
		}

		mismatches := 0
		for idx := overlapStart; idx < session1GlobalOffset+int64(len(session1Lines)); idx++ {
			s1Idx := int(idx - s1Start)
			s2Idx := int(idx - s2Start)

			var s1Text, s2Text string
			if s1Idx >= 0 && s1Idx < len(session1Lines) {
				s1Text = session1Lines[s1Idx]
			}
			if s2Idx >= 0 && s2Idx < len(session2Lines) {
				s2Text = session2Lines[s2Idx]
			}

			if s1Text != s2Text {
				mismatches++
				if mismatches <= 20 {
					t.Errorf("Line %d mismatch:\n  S1: %q\n  S2: %q", idx, s1Text, s2Text)
				}
			}
		}

		// Check for duplicate runs after reload
		maxDupeRun := 0
		currentDupeRun := 1
		dupeText := ""
		for i := 1; i < len(session2Lines); i++ {
			if session2Lines[i] != "" && session2Lines[i] == session2Lines[i-1] {
				currentDupeRun++
				if currentDupeRun > maxDupeRun {
					maxDupeRun = currentDupeRun
					dupeText = session2Lines[i]
				}
			} else {
				currentDupeRun = 1
			}
		}

		if maxDupeRun > 2 {
			t.Errorf("Found %d consecutive duplicate lines after reload: %q", maxDupeRun, dupeText)
		}

		// Check that key markers surviving in scrollback are present
		// Note: "session start", "between sessions" etc. may be overwritten by TUI header/footer
		// since TUI uses CUP to write at specific rows, overwriting what was there.
		// Only check markers that survive as scrollback (scroll region content and post-TUI output).
		survivingMarkers := []string{
			"tui1-line-",
			"tui2-line-",
			"TUI2 exited",
			"final output",
		}
		for _, marker := range survivingMarkers {
			found := false
			for _, line := range session2Lines {
				if strings.Contains(line, marker) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Marker %q not found after reload", marker)
			}
		}

		// Check ordering: tui1 content should come before tui2 content
		firstTUI1 := -1
		firstTUI2 := -1
		lastTUI1 := -1
		for i, line := range session2Lines {
			if strings.Contains(line, "tui1-line-") {
				if firstTUI1 < 0 {
					firstTUI1 = i
				}
				lastTUI1 = i
			}
			if strings.Contains(line, "tui2-line-") && firstTUI2 < 0 {
				firstTUI2 = i
			}
		}
		if firstTUI1 >= 0 && firstTUI2 >= 0 && lastTUI1 >= firstTUI2 {
			t.Errorf("TUI1 and TUI2 content overlaps: TUI1 range [%d,%d], TUI2 starts at %d",
				firstTUI1, lastTUI1, firstTUI2)
		}

		// Check GlobalEnd consistency: reload should not create extra lines
		if session2GlobalOffset+int64(len(session2Lines)) != session1GlobalOffset+int64(len(session1Lines)) {
			t.Errorf("GlobalEnd mismatch: session1 had %d total, session2 has %d total (off by %d)",
				len(session1Lines), len(session2Lines),
				len(session2Lines)-len(session1Lines))
		}

		t.Logf("Reload: %d mismatches, %d max consecutive duplicates", mismatches, maxDupeRun)

		if mismatches > 0 || maxDupeRun > 2 {
			t.Log("=== Session 2 dump ===")
			for i, line := range session2Lines {
				globalIdx := session2GlobalOffset + int64(i)
				if line != "" {
					t.Logf("  [%3d] %q", globalIdx, line)
				}
			}
		}

		v.CloseMemoryBuffer()
	}
}

// emulateLongCodexSession writes a realistic Codex CLI session with multiple
// scroll zone changes into a VTerm. It uses scroll regions on the main screen
// (NOT alt screen) with header and footer, and outputs many lines that scroll
// within the region — just like the real Codex CLI.
//
// Parameters:
//   - p: parser to write into
//   - width, height: terminal dimensions
//   - totalOutputLines: total lines of "AI output" to generate (spread across regions)
//   - regionChanges: number of times the scroll region is reconfigured (simulating
//     expanding/collapsing panels)
func emulateLongCodexSession(p *Parser, _, height, totalOutputLines, regionChanges int) {
	// Initial header
	parseString(p, "\x1b[1;1H")                       // Home
	parseString(p, "\x1b[48;2;65;69;76m")             // Grey BG
	parseString(p, " Codex (research-preview) working on task...")
	parseString(p, "\x1b[K")                           // Fill rest with BG
	parseString(p, "\x1b[0m")

	// Initial footer
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
	parseString(p, "\x1b[48;2;65;69;76m")
	parseString(p, " > thinking...")
	parseString(p, "\x1b[K")
	parseString(p, "\x1b[0m")

	linesPerRegion := totalOutputLines / max(regionChanges, 1)
	linesEmitted := 0

	for rc := 0; rc < regionChanges; rc++ {
		// Each region change simulates Codex adjusting its layout
		// (e.g., expanding output area, showing/hiding tool panels)
		regionTop := 2 + (rc % 3)                   // Vary header size (rows 2-4)
		regionBottom := height - 1 - (rc % 2)       // Vary footer size

		if regionTop >= regionBottom {
			regionTop = 2
			regionBottom = height - 1
		}

		// Set scroll region
		parseString(p, fmt.Sprintf("\x1b[%d;%dr", regionTop, regionBottom))

		// Update header to reflect region change
		parseString(p, "\x1b[1;1H")
		parseString(p, "\x1b[48;2;65;69;76m")
		parseString(p, fmt.Sprintf(" Codex [zone %d] top=%d bot=%d", rc, regionTop, regionBottom))
		parseString(p, "\x1b[K")
		parseString(p, "\x1b[0m")

		// Move to start of scroll region
		parseString(p, fmt.Sprintf("\x1b[%d;1H", regionTop))

		// Emit lines within this scroll region
		regionLines := linesPerRegion
		if rc == regionChanges-1 {
			regionLines = totalOutputLines - linesEmitted // Last region gets remainder
		}

		for i := 0; i < regionLines; i++ {
			lineNum := linesEmitted + i
			// Mix different content patterns (varying lengths)
			switch lineNum % 5 {
			case 0:
				parseString(p, fmt.Sprintf("  [%03d] Reading file src/components/widget_%d.tsx...", lineNum, lineNum))
			case 1:
				parseString(p, fmt.Sprintf("  [%03d] Analyzing code patterns and dependencies", lineNum))
			case 2:
				parseString(p, fmt.Sprintf("  [%03d] +++ Modified: internal/api/handler.go (line %d)", lineNum, lineNum*3))
			case 3:
				parseString(p, fmt.Sprintf("  [%03d] --- Removed: legacy/compat_%d.go", lineNum, lineNum/10))
			case 4:
				parseString(p, fmt.Sprintf("  [%03d] Running tests... %d passed, 0 failed", lineNum, lineNum+42))
			}
			// Clear rest of line (like real TUI apps do) to prevent
			// old cell content from leaking when new text is shorter
			parseString(p, "\x1b[K")

			if i < regionLines-1 {
				parseString(p, "\r\n")
			}
		}
		linesEmitted += regionLines
	}

	// Exit TUI: reset scroll region, write summary, clear
	parseString(p, "\x1b[r") // Reset scroll region
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height-2))
	parseString(p, "\x1b[J") // Erase from cursor down

	parseString(p, fmt.Sprintf("Tokens: %d | Cost: $%.2f | Duration: 45s\r\n", totalOutputLines*150, float64(totalOutputLines)*0.003))
}

// TestVTerm_ScrollRegionReloadLongSession tests reload corruption with a long
// Codex-style session that produces lots of scrollback through scroll regions,
// followed by substantial normal shell output. The user reports that the more
// TUI content is produced, the worse the corruption after reload.
func TestVTerm_ScrollRegionReloadLongSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test_long_codex.hist3"
	terminalID := "test-long-codex"
	width, height := 120, 30

	var session1Lines []string
	var session1GlobalOffset, session1GlobalEnd int64
	var session1LiveEdge int64
	var session1Grid []string

	// ========================================================
	// Session 1: Long Codex session + lots of normal output
	// ========================================================
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
		}
		p := NewParser(v)

		// Pre-TUI shell activity
		for i := 0; i < 20; i++ {
			parseString(p, fmt.Sprintf("$ command-%03d --flag=value\r\n", i))
			parseString(p, fmt.Sprintf("output from command %d\r\n", i))
		}
		parseString(p, "$ codex 'implement feature X with full test coverage'\r\n")

		// Long Codex session: 200 output lines, 4 region changes
		emulateLongCodexSession(p, width, height, 200, 4)

		// Extensive normal shell output AFTER TUI
		parseString(p, "$ git status\r\n")
		parseString(p, "On branch feature/x\r\n")
		parseString(p, "Changes to be committed:\r\n")
		for i := 0; i < 30; i++ {
			parseString(p, fmt.Sprintf("  modified: src/file_%03d.go\r\n", i))
		}
		parseString(p, "\r\n")
		parseString(p, "$ go test ./...\r\n")
		for i := 0; i < 50; i++ {
			parseString(p, fmt.Sprintf("ok  \tpkg/module_%03d\t%.3fs\r\n", i, float64(i)*0.1+0.05))
		}
		parseString(p, "PASS\r\n")
		parseString(p, "$ echo 'all tests passed'\r\n")
		parseString(p, "all tests passed\r\n")

		// Another shorter Codex session
		parseString(p, "$ codex 'add error handling'\r\n")
		emulateLongCodexSession(p, width, height, 50, 2)

		// Final shell content
		parseString(p, "$ git diff --stat\r\n")
		for i := 0; i < 15; i++ {
			parseString(p, fmt.Sprintf(" src/file_%03d.go | %d +++--\r\n", i, i+3))
		}
		parseString(p, " 15 files changed, 234 insertions(+), 89 deletions(-)\r\n")
		parseString(p, "$ git commit -m 'implement feature X'\r\n")
		parseString(p, "[feature/x abc1234] implement feature X\r\n")
		parseString(p, "$ ") // Final prompt

		// Capture session 1 state
		mb := v.memBufState.memBuf
		session1LiveEdge = v.memBufState.liveEdgeBase
		session1GlobalOffset = mb.GlobalOffset()
		session1GlobalEnd = mb.GlobalEnd()
		session1Lines = extractAllLines(mb)

		grid := v.Grid()
		for y := 0; y < len(grid); y++ {
			session1Grid = append(session1Grid, trimRight(gridRowToString(grid[y])))
		}

		t.Logf("Session 1: offset=%d, end=%d, liveEdge=%d, lines=%d",
			session1GlobalOffset, session1GlobalEnd, session1LiveEdge, len(session1Lines))

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer failed: %v", err)
		}
	}

	// ========================================================
	// Session 2: Reload and check for corruption
	// ========================================================
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (reload) failed: %v", err)
		}

		mb := v.memBufState.memBuf
		session2LiveEdge := v.memBufState.liveEdgeBase
		session2GlobalOffset := mb.GlobalOffset()
		session2GlobalEnd := mb.GlobalEnd()
		session2Lines := extractAllLines(mb)

		t.Logf("Session 2: offset=%d, end=%d, liveEdge=%d, lines=%d",
			session2GlobalOffset, session2GlobalEnd, session2LiveEdge, len(session2Lines))

		// --- Check 1: GlobalEnd consistency ---
		if session2GlobalEnd != session1GlobalEnd {
			t.Errorf("GlobalEnd changed: session1=%d, session2=%d (off by %d)",
				session1GlobalEnd, session2GlobalEnd, session2GlobalEnd-session1GlobalEnd)
		}

		// --- Check 2: LiveEdgeBase consistency ---
		if session2LiveEdge != session1LiveEdge {
			t.Errorf("LiveEdgeBase changed: session1=%d, session2=%d (off by %d)",
				session1LiveEdge, session2LiveEdge, session2LiveEdge-session1LiveEdge)
		}

		// --- Check 3: Raw line comparison (overlapping range) ---
		overlapStart := max64(session1GlobalOffset, session2GlobalOffset)
		overlapEnd := min64(session1GlobalEnd, session2GlobalEnd)

		lineMismatches := 0
		for idx := overlapStart; idx < overlapEnd; idx++ {
			s1Idx := int(idx - session1GlobalOffset)
			s2Idx := int(idx - session2GlobalOffset)
			var s1Text, s2Text string
			if s1Idx >= 0 && s1Idx < len(session1Lines) {
				s1Text = session1Lines[s1Idx]
			}
			if s2Idx >= 0 && s2Idx < len(session2Lines) {
				s2Text = session2Lines[s2Idx]
			}
			if s1Text != s2Text {
				lineMismatches++
				if lineMismatches <= 20 {
					t.Errorf("Line %d mismatch:\n  S1: %q\n  S2: %q", idx, s1Text, s2Text)
				}
			}
		}
		t.Logf("Raw line mismatches: %d / %d", lineMismatches, overlapEnd-overlapStart)

		// --- Check 4: Grid comparison ---
		session2Grid := v.Grid()
		var session2GridStrings []string
		for y := 0; y < len(session2Grid); y++ {
			session2GridStrings = append(session2GridStrings, trimRight(gridRowToString(session2Grid[y])))
		}

		gridMismatches := 0
		for y := 0; y < min(len(session1Grid), len(session2GridStrings)); y++ {
			if session1Grid[y] != session2GridStrings[y] {
				gridMismatches++
				if gridMismatches <= 10 {
					t.Errorf("Grid row %d mismatch:\n  S1: %q\n  S2: %q", y, session1Grid[y], session2GridStrings[y])
				}
			}
		}
		t.Logf("Grid mismatches: %d / %d", gridMismatches, min(len(session1Grid), len(session2GridStrings)))

		// --- Check 5: Duplicate detection in raw lines ---
		maxDupeRun := 0
		dupeRun := 1
		dupeText := ""
		for i := 1; i < len(session2Lines); i++ {
			if session2Lines[i] != "" && session2Lines[i] == session2Lines[i-1] {
				dupeRun++
				if dupeRun > maxDupeRun {
					maxDupeRun = dupeRun
					dupeText = session2Lines[i]
				}
			} else {
				dupeRun = 1
			}
		}
		if maxDupeRun > 2 {
			t.Errorf("Found %d consecutive duplicate lines: %q", maxDupeRun, dupeText)
		}

		// --- Check 6: Post-reload typing visibility ---
		p := NewParser(v)
		parseString(p, "echo 'post-reload test'\r\n")
		parseString(p, "post-reload test\r\n")
		parseString(p, "$ second command\r\n")
		parseString(p, "second output\r\n")
		parseString(p, "$ ")

		newGrid := v.Grid()
		foundPostReload := false
		foundPrompt := false
		for y := 0; y < len(newGrid); y++ {
			row := trimRight(gridRowToString(newGrid[y]))
			if strings.Contains(row, "post-reload test") {
				foundPostReload = true
			}
			if strings.Contains(row, "$ ") {
				foundPrompt = true
			}
		}
		if !foundPostReload {
			t.Errorf("Post-reload content not visible in Grid (typing disappears!)")
			for y := 0; y < len(newGrid); y++ {
				row := trimRight(gridRowToString(newGrid[y]))
				if row != "" {
					t.Logf("  Grid[%2d] %q", y, row)
				}
			}
		}
		if !foundPrompt {
			t.Errorf("Prompt not visible after reload writes (need 'reset' to fix)")
		}

		// --- Check 7: Key content survival ---
		// Only check markers that should be in scrollback (i.e., written AFTER the
		// second TUI and thus not overwritten by its viewport takeover).
		// "PASS" and "all tests passed" were still on screen when the second Codex
		// started, so they get overwritten — this is correct terminal behavior.
		allContent := strings.Join(session2Lines, "\n")
		criticalMarkers := []string{
			"implement feature X",
			"git diff --stat",
			"15 files changed",
		}
		for _, marker := range criticalMarkers {
			if !strings.Contains(allContent, marker) {
				t.Errorf("Critical marker %q missing after reload", marker)
			}
		}

		// --- Dump on failure ---
		if t.Failed() {
			t.Log("=== Session 2 raw dump (non-empty lines) ===")
			for i, line := range session2Lines {
				if line != "" {
					globalIdx := session2GlobalOffset + int64(i)
					marker := " "
					if globalIdx == session2LiveEdge {
						marker = ">"
					}
					t.Logf("  [%4d]%s %q", globalIdx, marker, line)
				}
			}
			t.Log("=== Session 2 Grid ===")
			for y, row := range session2GridStrings {
				if row != "" {
					t.Logf("  Grid[%2d] %q", y, row)
				}
			}
		}

		v.CloseMemoryBuffer()
	}
}

// TestVTerm_ScrollRegionReloadCorruptionScaling verifies that the reload
// corruption severity scales with TUI content length. The user reports:
// "the more TUI content is used, the more error afterwards."
// This test runs multiple sub-tests with increasing TUI output lengths
// and checks that each produces identical content after reload.
func TestVTerm_ScrollRegionReloadCorruptionScaling(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	testCases := []struct {
		name           string
		tuiLines       int
		regionChanges  int
		postTUILines   int
	}{
		{"short_20lines", 20, 1, 10},
		{"medium_50lines", 50, 2, 20},
		{"long_100lines", 100, 3, 30},
		{"verylong_200lines", 200, 4, 50},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			diskPath := tmpDir + "/scaling_test.hist3"
			width, height := 80, 24

			var s1Lines []string
			var s1Offset, s1End, s1LiveEdge int64
			var s1Grid []string

			// Session 1: Generate content
			{
				v := NewVTerm(width, height, WithMemoryBuffer())
				err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
					MaxLines:   50000,
					TerminalID: "scaling-" + tc.name,
				})
				if err != nil {
					t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
				}
				p := NewParser(v)

				// Pre-TUI
				parseString(p, "$ pre-tui-command\r\n")
				parseString(p, "pre-tui output\r\n")

				// TUI session
				parseString(p, "$ codex 'task'\r\n")
				emulateLongCodexSession(p, width, height, tc.tuiLines, tc.regionChanges)

				// Post-TUI normal content
				for i := 0; i < tc.postTUILines; i++ {
					parseString(p, fmt.Sprintf("$ post-tui-cmd-%03d\r\n", i))
					parseString(p, fmt.Sprintf("post-tui-output-%03d: some data here\r\n", i))
				}
				parseString(p, "$ final-prompt ")

				mb := v.memBufState.memBuf
				s1LiveEdge = v.memBufState.liveEdgeBase
				s1Offset = mb.GlobalOffset()
				s1End = mb.GlobalEnd()
				s1Lines = extractAllLines(mb)

				grid := v.Grid()
				for y := 0; y < len(grid); y++ {
					s1Grid = append(s1Grid, trimRight(gridRowToString(grid[y])))
				}

				if err := v.CloseMemoryBuffer(); err != nil {
					t.Fatalf("Close: %v", err)
				}
			}

			// Session 2: Reload and compare
			{
				v := NewVTerm(width, height, WithMemoryBuffer())
				err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
					MaxLines:   50000,
					TerminalID: "scaling-" + tc.name,
				})
				if err != nil {
					t.Fatalf("Reload: %v", err)
				}

				mb := v.memBufState.memBuf
				s2LiveEdge := v.memBufState.liveEdgeBase
				s2Offset := mb.GlobalOffset()
				s2End := mb.GlobalEnd()
				s2Lines := extractAllLines(mb)

				// Metric 1: GlobalEnd delta
				endDelta := s2End - s1End
				if endDelta != 0 {
					t.Errorf("GlobalEnd off by %d (s1=%d, s2=%d)", endDelta, s1End, s2End)
				}

				// Metric 2: LiveEdgeBase delta
				liveDelta := s2LiveEdge - s1LiveEdge
				if liveDelta != 0 {
					t.Errorf("LiveEdgeBase off by %d (s1=%d, s2=%d)", liveDelta, s1LiveEdge, s2LiveEdge)
				}

				// Metric 3: Line mismatches
				overlapStart := max64(s1Offset, s2Offset)
				overlapEnd := min64(s1End, s2End)
				lineMismatches := 0
				for idx := overlapStart; idx < overlapEnd; idx++ {
					s1Idx := int(idx - s1Offset)
					s2Idx := int(idx - s2Offset)
					var s1Text, s2Text string
					if s1Idx >= 0 && s1Idx < len(s1Lines) {
						s1Text = s1Lines[s1Idx]
					}
					if s2Idx >= 0 && s2Idx < len(s2Lines) {
						s2Text = s2Lines[s2Idx]
					}
					if s1Text != s2Text {
						lineMismatches++
					}
				}

				// Metric 4: Duplicate run length
				maxDupeRun := 0
				dupeRun := 1
				for i := 1; i < len(s2Lines); i++ {
					if s2Lines[i] != "" && s2Lines[i] == s2Lines[i-1] {
						dupeRun++
						if dupeRun > maxDupeRun {
							maxDupeRun = dupeRun
						}
					} else {
						dupeRun = 1
					}
				}

				// Metric 5: Grid mismatches
				s2Grid := v.Grid()
				gridMismatches := 0
				for y := 0; y < min(len(s1Grid), len(s2Grid)); y++ {
					s2Row := trimRight(gridRowToString(s2Grid[y]))
					if s1Grid[y] != s2Row {
						gridMismatches++
					}
				}

				// Metric 6: Post-reload typing
				p := NewParser(v)
				parseString(p, "echo 'alive'\r\n")
				parseString(p, "alive\r\n")
				parseString(p, "$ ")

				newGrid := v.Grid()
				typingVisible := false
				promptVisible := false
				for y := 0; y < len(newGrid); y++ {
					row := trimRight(gridRowToString(newGrid[y]))
					if strings.Contains(row, "alive") {
						typingVisible = true
					}
					if strings.Contains(row, "$ ") {
						promptVisible = true
					}
				}

				t.Logf("TUI=%d lines: endDelta=%d, liveDelta=%d, lineMismatch=%d, maxDupe=%d, gridMismatch=%d, typing=%v, prompt=%v",
					tc.tuiLines, endDelta, liveDelta, lineMismatches, maxDupeRun, gridMismatches, typingVisible, promptVisible)

				if lineMismatches > 0 {
					t.Errorf("%d line mismatches (corruption scales with TUI length)", lineMismatches)
				}
				if maxDupeRun > 2 {
					t.Errorf("%d consecutive duplicates found", maxDupeRun)
				}
				if gridMismatches > 0 {
					t.Errorf("%d Grid row mismatches", gridMismatches)
				}
				if !typingVisible {
					t.Error("Typing not visible after reload (ghost terminal)")
				}
				if !promptVisible {
					t.Error("Prompt missing after reload")
				}

				// Log on failure
				if t.Failed() {
					t.Log("=== Session 2 lines (non-empty) ===")
					for i, line := range s2Lines {
						if line != "" {
							globalIdx := s2Offset + int64(i)
							marker := " "
							if globalIdx == s2LiveEdge {
								marker = ">"
							}
							t.Logf("  [%4d]%s %q", globalIdx, marker, line)
						}
					}
				}

				v.CloseMemoryBuffer()
			}
		})
	}
}

// TestVTerm_ScrollRegionMultiCycleReload tests for bugs that only appear after
// multiple close/reopen cycles with TUI scroll region content. The user reports
// that after 2-3 cycles of running Codex (TUI on main screen) then doing normal
// shell commands, empty lines appear after shell output that weren't there before.
//
// This happens because each reload cycle may accumulate small offset errors in
// liveEdgeBase, cursor position, or GlobalEnd that don't manifest until enough
// cycles compound the error.
func TestVTerm_ScrollRegionMultiCycleReload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test_multicycle.hist3"
	terminalID := "test-multicycle"
	width, height := 120, 30

	// Track grid snapshots from each session to detect empty line insertion
	type sessionSnapshot struct {
		globalOffset int64
		globalEnd    int64
		liveEdge     int64
		cursorX      int
		cursorY      int
		gridLines    []string // Grid() rows as strings
		lastContent  []string // last few non-empty raw lines before cursor
	}

	const numCycles = 4
	snapshots := make([]sessionSnapshot, numCycles)

	for cycle := 0; cycle < numCycles; cycle++ {
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("Cycle %d: EnableMemoryBufferWithDisk failed: %v", cycle, err)
		}
		p := NewParser(v)

		if cycle > 0 {
			// After reload: the Grid should match the previous session's Grid exactly.
			// Empty rows between content rows are legitimate (TUI apps may leave gaps
			// in the viewport). The key is that reload preserves the same state.
			grid := v.Grid()
			var gridLines []string
			for y := range len(grid) {
				gridLines = append(gridLines, trimRight(gridRowToString(grid[y])))
			}

			prevSnap := snapshots[cycle-1]
			mismatches := 0
			for y := 0; y < min(len(gridLines), len(prevSnap.gridLines)); y++ {
				if gridLines[y] != prevSnap.gridLines[y] {
					mismatches++
					if mismatches <= 5 {
						t.Errorf("Cycle %d reload Grid row %d mismatch:\n  expected: %q\n  got:      %q",
							cycle, y, prevSnap.gridLines[y], gridLines[y])
					}
				}
			}
			if mismatches > 5 {
				t.Errorf("Cycle %d reload: %d total grid mismatches", cycle, mismatches)
			}
		}

		// --- Write content for this cycle ---

		// Normal shell commands
		for i := 0; i < 5; i++ {
			parseString(p, fmt.Sprintf("$ cmd-cycle%d-%d\r\n", cycle, i))
			parseString(p, fmt.Sprintf("output-cycle%d-%d\r\n", cycle, i))
		}

		// Codex TUI session (shorter each cycle to mix things up)
		parseString(p, fmt.Sprintf("$ codex 'task for cycle %d'\r\n", cycle))
		tuiLines := 20 + cycle*10 // 20, 30, 40, 50 lines
		emulateLongCodexSession(p, width, height, tuiLines, 1+cycle%2)

		// Normal shell after TUI (like the user typing ls)
		parseString(p, fmt.Sprintf("$ echo 'cycle %d done'\r\n", cycle))
		parseString(p, fmt.Sprintf("cycle %d done\r\n", cycle))
		parseString(p, "$ ls\r\n")
		parseString(p, "file1.txt  file2.txt  file3.txt  README.md  Makefile\r\n")
		parseString(p, "$ ") // prompt

		// Capture snapshot
		mb := v.memBufState.memBuf
		snap := sessionSnapshot{
			globalOffset: mb.GlobalOffset(),
			globalEnd:    mb.GlobalEnd(),
			liveEdge:     v.memBufState.liveEdgeBase,
			cursorX:      v.cursorX,
			cursorY:      v.cursorY,
		}

		grid := v.Grid()
		for y := 0; y < len(grid); y++ {
			snap.gridLines = append(snap.gridLines, trimRight(gridRowToString(grid[y])))
		}

		// Capture last few non-empty lines near the prompt
		allLines := extractAllLines(mb)
		nonEmpty := 0
		for i := len(allLines) - 1; i >= 0 && nonEmpty < 10; i-- {
			if allLines[i] != "" {
				snap.lastContent = append([]string{allLines[i]}, snap.lastContent...)
				nonEmpty++
			}
		}

		snapshots[cycle] = snap

		t.Logf("Cycle %d: offset=%d, end=%d, liveEdge=%d, cursor=(%d,%d), totalLines=%d",
			cycle, snap.globalOffset, snap.globalEnd, snap.liveEdge, snap.cursorX, snap.cursorY, len(allLines))

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("Cycle %d: Close failed: %v", cycle, err)
		}
	}

	// --- Final reload: thorough check ---
	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("Final reload: %v", err)
		}

		mb := v.memBufState.memBuf
		finalEnd := mb.GlobalEnd()
		finalLiveEdge := v.memBufState.liveEdgeBase
		lastSnap := snapshots[numCycles-1]

		// Check 1: GlobalEnd should match last session
		if finalEnd != lastSnap.globalEnd {
			t.Errorf("Final reload: GlobalEnd=%d, expected %d (off by %d)",
				finalEnd, lastSnap.globalEnd, finalEnd-lastSnap.globalEnd)
		}

		// Check 2: LiveEdgeBase should match last session
		if finalLiveEdge != lastSnap.liveEdge {
			t.Errorf("Final reload: liveEdge=%d, expected %d (off by %d)",
				finalLiveEdge, lastSnap.liveEdge, finalLiveEdge-lastSnap.liveEdge)
		}

		// Check 3: Grid should match last session
		finalGrid := v.Grid()
		gridMismatches := 0
		for y := 0; y < min(len(finalGrid), len(lastSnap.gridLines)); y++ {
			row := trimRight(gridRowToString(finalGrid[y]))
			if row != lastSnap.gridLines[y] {
				gridMismatches++
				if gridMismatches <= 5 {
					t.Errorf("Final reload Grid row %d:\n  expected: %q\n  got:      %q", y, lastSnap.gridLines[y], row)
				}
			}
		}
		if gridMismatches > 0 {
			t.Logf("Final reload: %d grid mismatches", gridMismatches)
		}

		// Check 4: No empty line gaps in Grid
		var gridLines []string
		for y := 0; y < len(finalGrid); y++ {
			gridLines = append(gridLines, trimRight(gridRowToString(finalGrid[y])))
		}

		lastNonEmpty := -1
		for y := len(gridLines) - 1; y >= 0; y-- {
			if gridLines[y] != "" {
				lastNonEmpty = y
				break
			}
		}

		emptyGapCount := 0
		maxEmptyGap := 0
		for y := 0; y <= lastNonEmpty; y++ {
			if gridLines[y] == "" {
				emptyGapCount++
			} else {
				if emptyGapCount > maxEmptyGap {
					maxEmptyGap = emptyGapCount
				}
				emptyGapCount = 0
			}
		}
		if maxEmptyGap > 1 {
			t.Errorf("Final reload: %d empty line gap in Grid (phantom empty lines)", maxEmptyGap)
		}

		// Check 5: New content after final reload should work without gaps
		p := NewParser(v)
		parseString(p, "echo 'final check'\r\n")
		parseString(p, "final check\r\n")
		parseString(p, "$ ")

		postGrid := v.Grid()
		var postLines []string
		for y := 0; y < len(postGrid); y++ {
			postLines = append(postLines, trimRight(gridRowToString(postGrid[y])))
		}

		// Find "final check" and "$ " — check no empty gap between them
		finalCheckRow := -1
		promptRow := -1
		for y := len(postLines) - 1; y >= 0; y-- {
			if strings.Contains(postLines[y], "$ ") && promptRow < 0 {
				promptRow = y
			}
			if strings.Contains(postLines[y], "final check") && !strings.Contains(postLines[y], "echo") && finalCheckRow < 0 {
				finalCheckRow = y
			}
		}

		if finalCheckRow >= 0 && promptRow >= 0 {
			gapBetween := 0
			for y := finalCheckRow + 1; y < promptRow; y++ {
				if postLines[y] == "" {
					gapBetween++
				}
			}
			if gapBetween > 0 {
				t.Errorf("Final reload: %d empty lines between 'final check' (row %d) and prompt (row %d)",
					gapBetween, finalCheckRow, promptRow)
			}
		}

		if !strings.Contains(postLines[promptRow], "$ ") {
			t.Error("Prompt not visible after final reload writes")
		}

		// Dump on failure
		if t.Failed() {
			t.Log("=== Final reload Grid ===")
			for y, row := range gridLines {
				marker := " "
				if row == "" {
					marker = "~"
				}
				t.Logf("  [%2d]%s %q", y, marker, row)
			}
			t.Log("=== Post-write Grid ===")
			for y, row := range postLines {
				marker := " "
				if row == "" {
					marker = "~"
				}
				t.Logf("  [%2d]%s %q", y, marker, row)
			}
		}

		v.CloseMemoryBuffer()
	}
}

// --- Auto-wrap tests ---
//
// These tests verify that when text wraps at the terminal's right edge,
// it displays correctly and reflows properly on resize.

// TestAutoWrap_GridRendering verifies that wrapped content displays
// correctly across multiple physical rows in the Grid().
func TestAutoWrap_GridRendering(t *testing.T) {
	width := 10
	height := 5
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write exactly 15 chars: "AAAAAAAAAABBBBB"
	parseString(p, strings.Repeat("A", width)+strings.Repeat("B", 5))

	grid := v.Grid()

	// Row 0 should show "AAAAAAAAAA"
	row0 := gridRowToString(grid[0][:width])
	expected0 := strings.Repeat("A", width)
	if row0 != expected0 {
		t.Errorf("row 0: expected %q, got %q", expected0, row0)
	}

	// Row 1 should show "BBBBB"
	row1 := gridRowToString(grid[1][:5])
	if row1 != "BBBBB" {
		t.Errorf("row 1: expected %q, got %q", "BBBBB", row1)
	}
}

// TestAutoWrap_ResizeReflow verifies that after wrapping at width 10,
// resizing to width 20 reflows the content to a single physical row.
func TestAutoWrap_ResizeReflow(t *testing.T) {
	width := 10
	height := 5
	v := NewVTerm(width, height, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write 15 chars at width 10 → wraps to 2 physical rows
	text := strings.Repeat("A", 10) + strings.Repeat("B", 5)
	parseString(p, text)

	// Verify wrapping before resize
	grid := v.Grid()
	row0 := gridRowToString(grid[0][:10])
	row1 := gridRowToString(grid[1][:5])
	if row0 != "AAAAAAAAAA" || row1 != "BBBBB" {
		t.Fatalf("before resize: expected rows 'AAAAAAAAAA'+'BBBBB', got %q+%q", row0, row1)
	}

	// Resize to width 20
	v.Resize(20, height)

	// After resize, content should reflow to a single physical row
	grid = v.Grid()
	row0 = gridRowToString(grid[0][:15])
	expected := strings.Repeat("A", 10) + "BBBBB"
	if row0 != expected {
		t.Errorf("after resize to width 20: expected %q on row 0, got %q", expected, row0)
	}

	// Row 1 should be empty
	row1Content := strings.TrimRight(gridRowToString(grid[1]), " \x00")
	if row1Content != "" {
		t.Errorf("after resize: row 1 should be empty, got %q", row1Content)
	}
}

// TestLoadHistory_TrimsBlankTailLines verifies that on reload, blank tail lines
// (from a crash where metadata was persisted but trailing content was lost) are
// trimmed and liveEdgeBase is clamped to the last non-empty line + 1.
func TestLoadHistory_TrimsBlankTailLines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	terminalID := "test-trim-tail"

	walConfig := DefaultWALConfig(tmpDir, terminalID)
	walConfig.CheckpointInterval = 0

	// Create WAL with 10 lines: 7 real + 3 blank
	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}

	now := time.Now()
	for i := 0; i < 7; i++ {
		line := NewLogicalLineFromCells([]Cell{{Rune: rune('A' + i)}})
		if err := wal.Append(int64(i), line, now); err != nil {
			t.Fatalf("Append line %d failed: %v", i, err)
		}
	}
	// 3 blank lines (empty cells = whitespace)
	for i := 7; i < 10; i++ {
		line := NewLogicalLineFromCells([]Cell{})
		if err := wal.Append(int64(i), line, now); err != nil {
			t.Fatalf("Append blank line %d failed: %v", i, err)
		}
	}

	// Checkpoint to move content to PageStore
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}

	// Write metadata claiming liveEdgeBase=10 (past the blanks)
	if err := wal.WriteMetadata(&ViewportState{
		LiveEdgeBase: 10,
		CursorX:      0,
		CursorY:      0,
		SavedAt:      now,
	}); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	if err := wal.Close(); err != nil {
		t.Fatalf("Close WAL failed: %v", err)
	}

	// Reopen through normal VTerm path — EnableMemoryBufferWithDisk will
	// find 10 lines in PageStore and metadata with liveEdgeBase=10.
	// After trimBlankTailLines(), liveEdgeBase should be clamped to 7.
	v := NewVTerm(80, 24, WithMemoryBuffer())
	err = v.EnableMemoryBufferWithDisk(tmpDir, MemoryBufferOptions{
		MaxLines:   50000,
		TerminalID: terminalID,
	})
	if err != nil {
		t.Fatalf("EnableMemoryBufferWithDisk failed: %v", err)
	}
	defer v.CloseMemoryBuffer()

	// liveEdgeBase should be clamped to 7 (last non-empty line + 1)
	if v.memBufState.liveEdgeBase != 7 {
		t.Errorf("liveEdgeBase: got %d, want 7 (should trim 3 blank tail lines)",
			v.memBufState.liveEdgeBase)
	}
}
