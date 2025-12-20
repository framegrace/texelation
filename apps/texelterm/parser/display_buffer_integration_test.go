package parser

import (
	"strings"
	"testing"
)

// gridToString converts a grid to a string representation for debugging.
func gridToString(grid [][]Cell) string {
	var sb strings.Builder
	for y, row := range grid {
		sb.WriteString("[")
		for _, cell := range row {
			if cell.Rune == 0 || cell.Rune == ' ' {
				sb.WriteRune('.')
			} else {
				sb.WriteRune(cell.Rune)
			}
		}
		sb.WriteString("]")
		if y < len(grid)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// TestDisplayBuffer_FreshTerminal tests that a fresh terminal (no history) works correctly.
func TestDisplayBuffer_FreshTerminal(t *testing.T) {
	v := NewVTerm(20, 5) // Small terminal for easier debugging
	v.EnableDisplayBuffer()

	// Initial state checks
	t.Logf("Initial state:")
	t.Logf("  cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
	t.Logf("  displayBuf.currentLogicalX=%d", v.displayBuf.currentLogicalX)

	if v.cursorX != 0 || v.cursorY != 0 {
		t.Errorf("Expected cursor at (0,0), got (%d,%d)", v.cursorX, v.cursorY)
	}

	// Simulate shell writing a prompt: "$ "
	for _, ch := range "$ " {
		v.placeChar(ch)
	}

	t.Logf("After prompt '$ ':")
	t.Logf("  cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
	t.Logf("  displayBuf.currentLogicalX=%d", v.displayBuf.currentLogicalX)

	// Get the grid
	grid := v.Grid()
	t.Logf("Grid:\n%s", gridToString(grid))

	// Check that prompt appears at row 0
	row0 := ""
	for _, cell := range grid[0] {
		if cell.Rune != 0 {
			row0 += string(cell.Rune)
		} else {
			row0 += " "
		}
	}
	// Check that the first two characters are "$ "
	if len(row0) < 2 || row0[0:2] != "$ " {
		t.Errorf("Expected row 0 to start with '$ ', got %q", row0[:min(10, len(row0))])
	}

	// Cursor should be at (2, 0) - after the prompt
	if v.cursorX != 2 || v.cursorY != 0 {
		t.Errorf("Expected cursor at (2,0), got (%d,%d)", v.cursorX, v.cursorY)
	}
}

// TestDisplayBuffer_LineFeed tests that line feed works correctly.
func TestDisplayBuffer_LineFeed(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write first line
	for _, ch := range "Line1" {
		v.placeChar(ch)
	}

	t.Logf("After 'Line1':")
	t.Logf("  cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)

	// Carriage return + line feed (typical behavior)
	v.CarriageReturn()
	v.LineFeed()

	t.Logf("After CR+LF:")
	t.Logf("  cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
	t.Logf("  displayBuf.currentLogicalX=%d", v.displayBuf.currentLogicalX)

	// Write second line
	for _, ch := range "Line2" {
		v.placeChar(ch)
	}

	t.Logf("After 'Line2':")
	t.Logf("  cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)

	grid := v.Grid()
	t.Logf("Grid:\n%s", gridToString(grid))

	// Check both lines appear
	row0 := strings.TrimRight(cellsToStringTest(grid[0]), " ")
	row1 := strings.TrimRight(cellsToStringTest(grid[1]), " ")

	if row0 != "Line1" {
		t.Errorf("Expected row 0 = 'Line1', got %q", row0)
	}
	if row1 != "Line2" {
		t.Errorf("Expected row 1 = 'Line2', got %q", row1)
	}

	// Cursor should be at (5, 1)
	if v.cursorX != 5 || v.cursorY != 1 {
		t.Errorf("Expected cursor at (5,1), got (%d,%d)", v.cursorX, v.cursorY)
	}
}

// TestDisplayBuffer_CursorMatchesContent tests that cursor position matches where content appears.
func TestDisplayBuffer_CursorMatchesContent(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write a character
	v.placeChar('X')

	grid := v.Grid()
	cursorX, cursorY := v.Cursor()

	// The character should appear at the position BEFORE the cursor
	// (cursor is at the next position after writing)
	charX := cursorX - 1
	charY := cursorY

	if charX < 0 {
		t.Fatalf("Cursor position invalid after writing")
	}

	actualChar := grid[charY][charX].Rune
	if actualChar != 'X' {
		t.Errorf("Expected 'X' at (%d,%d), found %q", charX, charY, string(actualChar))
		t.Logf("Grid:\n%s", gridToString(grid))
	}
}

// TestDisplayBuffer_MultipleLinesFillScreen tests behavior when content fills the screen.
func TestDisplayBuffer_MultipleLinesFillScreen(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write 5 lines (exactly filling the screen)
	for i := 1; i <= 5; i++ {
		for _, ch := range "Line" {
			v.placeChar(ch)
		}
		v.placeChar(rune('0' + i))
		if i < 5 {
			v.CarriageReturn()
			v.LineFeed()
		}
	}

	grid := v.Grid()
	t.Logf("Grid after 5 lines:\n%s", gridToString(grid))

	// All 5 lines should be visible
	for i := 0; i < 5; i++ {
		expected := "Line" + string(rune('1'+i))
		actual := strings.TrimRight(cellsToStringTest(grid[i]), " ")
		if actual != expected {
			t.Errorf("Row %d: expected %q, got %q", i, expected, actual)
		}
	}

	// Cursor should be at end of last line
	if v.cursorX != 5 || v.cursorY != 4 {
		t.Errorf("Expected cursor at (5,4), got (%d,%d)", v.cursorX, v.cursorY)
	}
}

// TestDisplayBuffer_ScrollOnOverflow tests that scrolling works when content overflows.
func TestDisplayBuffer_ScrollOnOverflow(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write 6 lines (one more than screen height)
	for i := 1; i <= 6; i++ {
		for _, ch := range "Line" {
			v.placeChar(ch)
		}
		v.placeChar(rune('0' + i))
		if i < 6 {
			v.CarriageReturn()
			v.LineFeed()
		}
	}

	grid := v.Grid()
	t.Logf("Grid after 6 lines:\n%s", gridToString(grid))

	// After scrolling, lines 2-6 should be visible (line 1 scrolled off)
	expectedLines := []string{"Line2", "Line3", "Line4", "Line5", "Line6"}
	for i, expected := range expectedLines {
		actual := strings.TrimRight(cellsToStringTest(grid[i]), " ")
		if actual != expected {
			t.Errorf("Row %d: expected %q, got %q", i, expected, actual)
		}
	}

	// Cursor should be at end of last line, which is at screen row 4
	if v.cursorY != 4 {
		t.Errorf("Expected cursorY=4 (bottom of screen), got %d", v.cursorY)
	}
}

// TestDisplayBuffer_BackspaceErases tests that backspace properly erases content.
func TestDisplayBuffer_BackspaceErases(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write "ABCD"
	for _, ch := range "ABCD" {
		v.placeChar(ch)
	}

	t.Logf("After 'ABCD': cursorX=%d, logicalX=%d", v.cursorX, v.displayBuf.currentLogicalX)

	// Backspace
	v.Backspace()

	t.Logf("After BS: cursorX=%d, logicalX=%d", v.cursorX, v.displayBuf.currentLogicalX)

	// Erase to end of line (what bash does on backspace)
	v.ClearLine(0) // EL 0

	grid := v.Grid()
	t.Logf("After EL 0:\n%s", gridToString(grid))

	// Should show "ABC" (D was erased)
	row0 := strings.TrimRight(cellsToStringTest(grid[0]), " ")
	if row0 != "ABC" {
		t.Errorf("Expected 'ABC' after backspace+erase, got %q", row0)
	}
}

// TestDisplayBuffer_WithDiskPersistence tests loading from disk doesn't break cursor.
func TestDisplayBuffer_WithDiskPersistence(t *testing.T) {
	// Create a temporary file for persistence
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test.hist2"

	// Create first terminal and write some content
	v1 := NewVTerm(20, 5)
	err := v1.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 100,
		MarginAbove:    10,
		MarginBelow:    5,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk failed: %v", err)
	}

	// Write 3 lines
	for i := 1; i <= 3; i++ {
		for _, ch := range "Line" {
			v1.placeChar(ch)
		}
		v1.placeChar(rune('0' + i))
		v1.CarriageReturn()
		v1.LineFeed()
	}

	// Close to flush to disk
	if err := v1.CloseDisplayBuffer(); err != nil {
		t.Fatalf("CloseDisplayBuffer failed: %v", err)
	}

	// Create second terminal loading from same disk
	v2 := NewVTerm(20, 5)
	err = v2.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 100,
		MarginAbove:    10,
		MarginBelow:    5,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk (load) failed: %v", err)
	}

	t.Logf("After loading from disk:")
	t.Logf("  cursorX=%d, cursorY=%d", v2.cursorX, v2.cursorY)
	t.Logf("  history TotalLen=%d", v2.displayBuf.history.TotalLen())

	grid := v2.Grid()
	t.Logf("Grid:\n%s", gridToString(grid))

	// Now write new content - it should appear where the cursor is
	for _, ch := range "NEW" {
		v2.placeChar(ch)
	}

	grid = v2.Grid()
	t.Logf("After writing 'NEW':\n%s", gridToString(grid))

	// Find where "NEW" appears
	foundRow := -1
	for y, row := range grid {
		rowStr := cellsToStringTest(row)
		if strings.Contains(rowStr, "NEW") {
			foundRow = y
			break
		}
	}

	if foundRow == -1 {
		t.Error("Could not find 'NEW' in grid")
	} else {
		t.Logf("'NEW' found at row %d, cursor at row %d", foundRow, v2.cursorY)
		// The cursor row should match where content appears
		// (cursor is at next position after NEW, so content is at cursor row)
		if foundRow != v2.cursorY {
			t.Errorf("Content row (%d) doesn't match cursor row (%d)", foundRow, v2.cursorY)
		}
	}
}

// TestDisplayBuffer_ResizeKeepsLiveEdge tests that resize keeps cursor at live edge position.
func TestDisplayBuffer_ResizeKeepsLiveEdge(t *testing.T) {
	v := NewVTerm(20, 10) // Start with 10 rows
	v.EnableDisplayBuffer()

	// Write only 3 lines (less than screen height)
	for i := 1; i <= 3; i++ {
		for _, ch := range "Line" {
			v.placeChar(ch)
		}
		v.placeChar(rune('0' + i))
		if i < 3 {
			v.CarriageReturn()
			v.LineFeed()
		}
	}

	t.Logf("Before resize: cursorY=%d (height=10)", v.cursorY)

	// Cursor should be at row 2 (after Line1, Line2, Line3)
	if v.cursorY != 2 {
		t.Errorf("Before resize: expected cursorY=2, got %d", v.cursorY)
	}

	// Resize to a larger terminal
	v.Resize(20, 15)

	t.Logf("After resize to 15 rows: cursorY=%d", v.cursorY)

	// Cursor should still be at row 2 (live edge hasn't moved)
	// NOT at row 14 (bottom of new screen)
	if v.cursorY != 2 {
		t.Errorf("After resize: expected cursorY=2 (live edge), got %d", v.cursorY)
	}

	// Content should still be at rows 0-2
	grid := v.Grid()
	for i := 0; i < 3; i++ {
		expected := "Line" + string(rune('1'+i))
		actual := strings.TrimRight(cellsToStringTest(grid[i]), " ")
		if actual != expected {
			t.Errorf("Row %d: expected %q, got %q", i, expected, actual)
		}
	}

	// Write more content - it should appear at the cursor position
	v.CarriageReturn()
	v.LineFeed()
	for _, ch := range "NewLine" {
		v.placeChar(ch)
	}

	grid = v.Grid()
	t.Logf("After adding NewLine: cursorY=%d", v.cursorY)
	t.Logf("Grid:\n%s", gridToString(grid))

	// NewLine should appear at row 3
	row3 := strings.TrimRight(cellsToStringTest(grid[3]), " ")
	if row3 != "NewLine" {
		t.Errorf("Expected 'NewLine' at row 3, got %q", row3)
	}
}

// TestDisplayBuffer_ResizeWithFullScreen tests resize when content fills screen.
func TestDisplayBuffer_ResizeWithFullScreen(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write 5 lines (exactly filling the screen)
	for i := 1; i <= 5; i++ {
		for _, ch := range "Line" {
			v.placeChar(ch)
		}
		v.placeChar(rune('0' + i))
		if i < 5 {
			v.CarriageReturn()
			v.LineFeed()
		}
	}

	t.Logf("Before resize: cursorY=%d (height=5)", v.cursorY)

	// Cursor should be at row 4 (bottom)
	if v.cursorY != 4 {
		t.Errorf("Before resize: expected cursorY=4, got %d", v.cursorY)
	}

	// Resize to larger terminal
	v.Resize(20, 10)

	t.Logf("After resize to 10 rows: cursorY=%d", v.cursorY)

	// With 5 lines of content and 10 row viewport, content is at rows 0-4
	// Cursor should be at row 4 (live edge = after Line5)
	if v.cursorY != 4 {
		t.Errorf("After resize: expected cursorY=4 (live edge), got %d", v.cursorY)
	}
}

func cellsToStringTest(cells []Cell) string {
	var sb strings.Builder
	for _, cell := range cells {
		if cell.Rune == 0 {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(cell.Rune)
		}
	}
	return sb.String()
}
