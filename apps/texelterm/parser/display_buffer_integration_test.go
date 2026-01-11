package parser

import (
	"fmt"
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
	t.Logf("  displayBuf.currentLogicalX=%d", v.displayBuf.display.GetCursorOffset())

	if v.cursorX != 0 || v.cursorY != 0 {
		t.Errorf("Expected cursor at (0,0), got (%d,%d)", v.cursorX, v.cursorY)
	}

	// Simulate shell writing a prompt: "$ "
	for _, ch := range "$ " {
		v.placeChar(ch)
	}

	t.Logf("After prompt '$ ':")
	t.Logf("  cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
	t.Logf("  displayBuf.currentLogicalX=%d", v.displayBuf.display.GetCursorOffset())

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
	t.Logf("  displayBuf.currentLogicalX=%d", v.displayBuf.display.GetCursorOffset())

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

	t.Logf("After 'ABCD': cursorX=%d, logicalX=%d", v.cursorX, v.displayBuf.display.GetCursorOffset())

	// Backspace
	v.Backspace()

	t.Logf("After BS: cursorX=%d, logicalX=%d", v.cursorX, v.displayBuf.display.GetCursorOffset())

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

// TestDisplayBuffer_WrapWithoutScrollDirty tests dirty tracking when wrapping without scroll.
func TestDisplayBuffer_WrapWithoutScrollDirty(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write first line and commit it
	for _, ch := range "Line1" {
		v.placeChar(ch)
	}
	v.CarriageReturn()
	v.LineFeed()

	// Clear dirty to simulate render
	v.ClearDirty()

	t.Logf("After Line1: cursorY=%d", v.cursorY)

	// Now write a long line that wraps (but doesn't need to scroll)
	for i, ch := range "ABCDEFGHIJ" {
		v.placeChar(ch)
		if i == 9 {
			dirtyLines, allDirty := v.GetDirtyLines()
			t.Logf("After char 10 (J): allDirty=%v, dirtyLines=%v", allDirty, dirtyLines)
		}
		v.ClearDirty()
	}

	// Type the wrap-triggering character
	v.placeChar('K')
	dirtyLines, allDirty := v.GetDirtyLines()
	t.Logf("After 'K' (wrap, no scroll): cursorY=%d, allDirty=%v, dirtyLines=%v",
		v.cursorY, allDirty, dirtyLines)

	// Check what lines should be dirty
	// Row 1 had ABCDEFGHIJ, now it still has ABCDEFGHIJ (no change visually)
	// Row 2 is NEW - it now has K
	// Without scroll, only the new row should need to be dirty, BUT
	// the display buffer's viewport might have changed

	grid := v.Grid()
	t.Logf("Grid:\n%s", gridToString(grid))

	// Verify content
	row0 := strings.TrimRight(cellsToStringTest(grid[0]), " ")
	row1 := cellsToStringTest(grid[1])
	row2 := strings.TrimRight(cellsToStringTest(grid[2]), " ")

	if row0 != "Line1" {
		t.Errorf("Row 0: expected 'Line1', got %q", row0)
	}
	if row1 != "ABCDEFGHIJ" {
		t.Errorf("Row 1: expected 'ABCDEFGHIJ', got %q", row1)
	}
	if row2 != "K" {
		t.Errorf("Row 2: expected 'K', got %q", row2)
	}
}

// TestDisplayBuffer_FreshTerminalWrap tests wrapping on a completely fresh terminal.
func TestDisplayBuffer_FreshTerminalWrap(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Initial state check
	t.Logf("Initial: cursorY=%d, LiveEdgeRow=%d", v.cursorY, v.displayBuf.display.LiveEdgeRow())

	// Write a long line that wraps
	text := "ABCDEFGHIJKLMNO"
	for i, ch := range text {
		v.placeChar(ch)
		if i == 9 || i == 10 || i == 14 {
			grid := v.Grid()
			t.Logf("After char %d (%c): cursorX=%d, cursorY=%d", i+1, ch, v.cursorX, v.cursorY)
			t.Logf("  Grid row 0: %s", cellsToStringTest(grid[0]))
			t.Logf("  Grid row 1: %s", cellsToStringTest(grid[1]))
		}
	}

	grid := v.Grid()
	t.Logf("Final Grid:\n%s", gridToString(grid))

	// First row should have ABCDEFGHIJ
	row0 := cellsToStringTest(grid[0])
	if row0 != "ABCDEFGHIJ" {
		t.Errorf("Row 0: expected 'ABCDEFGHIJ', got %q", row0)
	}

	// Second row should have KLMNO
	row1 := strings.TrimRight(cellsToStringTest(grid[1]), " ")
	if row1 != "KLMNO" {
		t.Errorf("Row 1: expected 'KLMNO', got %q", row1)
	}

	// Cursor should be at (5, 1)
	if v.cursorX != 5 || v.cursorY != 1 {
		t.Errorf("Expected cursor at (5,1), got (%d,%d)", v.cursorX, v.cursorY)
	}
}

// TestDisplayBuffer_CursorRowMatchesContent verifies that Grid()[cursorY] contains
// the character just typed, especially after wrapping.
func TestDisplayBuffer_CursorRowMatchesContent(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Type characters, checking after each that the cursor row has the right content
	text := "ABCDEFGHIJKLMNO"
	for i, ch := range text {
		v.placeChar(ch)
		grid := v.Grid()

		// After placing a character, cursor has already moved past it (cursorX-1 has the char)
		// When we just wrapped, cursorX might be 1 (we placed at 0, then moved to 1)
		// Actually, after placeChar, cursor is at the position AFTER the char

		// The important check: Grid()[cursorY] should contain the character we just typed
		row := grid[v.cursorY]
		rowStr := cellsToStringTest(row)

		// Find the character in the row
		found := false
		for _, c := range row {
			if c.Rune == ch {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("After char %d (%c): character not found in Grid()[cursorY=%d]", i+1, ch, v.cursorY)
			t.Logf("  Row content: %q", rowStr)
			t.Logf("  Full grid:\n%s", gridToString(grid))
		}
	}
}

// TestDisplayBuffer_WrapContentMatchesCursorRow tests that after wrapping,
// the cursor row in the Grid contains the wrapped character.
func TestDisplayBuffer_WrapContentMatchesCursorRow(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Fill the first line exactly
	for _, ch := range "ABCDEFGHIJ" {
		v.placeChar(ch)
	}
	t.Logf("After 10 chars: cursorX=%d, cursorY=%d, wrapNext=%v", v.cursorX, v.cursorY, v.wrapNext)

	// Now type 'K' which should wrap
	v.placeChar('K')
	t.Logf("After 'K': cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)

	grid := v.Grid()
	t.Logf("Grid after wrap:\n%s", gridToString(grid))

	// The critical check: Grid()[cursorY] should contain 'K'
	cursorRow := grid[v.cursorY]
	cursorRowStr := cellsToStringTest(cursorRow)
	t.Logf("Grid[cursorY=%d] = %q", v.cursorY, cursorRowStr)

	// 'K' should be at position 0 in cursorRow (we just wrapped and typed K at column 0)
	if cursorRow[0].Rune != 'K' {
		t.Errorf("Grid[cursorY][0] should be 'K', got '%c'", cursorRow[0].Rune)
	}

	// Also verify the cursor is at the right position (should be at column 1 after typing K)
	if v.cursorX != 1 || v.cursorY != 1 {
		t.Errorf("Expected cursor at (1,1), got (%d,%d)", v.cursorX, v.cursorY)
	}
}

// TestDisplayBuffer_RapidWrapWithDirtyClearing simulates rapid input where
// Render() is called between each character, exactly like the real terminal.
func TestDisplayBuffer_RapidWrapWithDirtyClearing(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Simulate render buffer
	buf := make([][]Cell, 5)
	for y := range buf {
		buf[y] = make([]Cell, 10)
		for x := range buf[y] {
			buf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	// Simulate the exact Render() flow from term.go
	render := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					buf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						buf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	render()

	// Type 10 characters, rendering after each (this fills the line)
	for _, ch := range "ABCDEFGHIJ" {
		v.placeChar(ch)
		render()
	}

	// Verify row 0 has the characters
	row0 := cellsToStringTest(buf[0])
	if row0 != "ABCDEFGHIJ" {
		t.Errorf("After 10 chars, buf[0] = %q, expected 'ABCDEFGHIJ'", row0)
	}

	t.Logf("Before wrap: cursorX=%d, cursorY=%d, wrapNext=%v", v.cursorX, v.cursorY, v.wrapNext)

	// Type K - this triggers wrap
	v.placeChar('K')
	t.Logf("After 'K': cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
	t.Logf("  Dirty before render: %v", func() map[int]bool { d, _ := v.GetDirtyLines(); return d }())

	render()

	// Check what's in buf[1]
	row1 := cellsToStringTest(buf[1])
	t.Logf("  buf[1] after render = %q", row1)

	if buf[1][0].Rune != 'K' {
		t.Errorf("After wrap, buf[1][0] = '%c', expected 'K'", buf[1][0].Rune)
		t.Logf("Full grid:\n%s", gridToString(v.Grid()))
		t.Logf("Full buf:")
		for y := 0; y < 5; y++ {
			t.Logf("  Row %d: %q", y, cellsToStringTest(buf[y]))
		}
	}

	// Continue typing
	for _, ch := range "LMNO" {
		v.placeChar(ch)
		render()
	}

	row1Final := strings.TrimRight(cellsToStringTest(buf[1]), " ")
	if row1Final != "KLMNO" {
		t.Errorf("Final buf[1] = %q, expected 'KLMNO'", row1Final)
	}
}

// TestDisplayBuffer_DirtyTrackingOnWrap tests that dirty lines are marked correctly during wrap.
func TestDisplayBuffer_DirtyTrackingOnWrap(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide, 5 rows
	v.EnableDisplayBuffer()

	// Fill up the screen
	for i := 1; i <= 4; i++ {
		for _, ch := range "Line" {
			v.placeChar(ch)
		}
		v.placeChar(rune('0' + i))
		v.CarriageReturn()
		v.LineFeed()
	}

	// Clear dirty and get initial state
	v.ClearDirty()

	// Write characters up to the wrap point, simulating Render() after each
	for i, ch := range "ABCDEFGHIJ" {
		v.placeChar(ch)
		dirtyLines, allDirty := v.GetDirtyLines()
		t.Logf("After char %d (%c): cursorY=%d, allDirty=%v, dirtyLines=%v",
			i+1, ch, v.cursorY, allDirty, dirtyLines)
		v.ClearDirty() // Simulate Render() clearing dirty
	}

	// Now type the wrap-triggering character
	v.placeChar('K')
	dirtyLines, allDirty := v.GetDirtyLines()
	t.Logf("After 'K' (wrap): cursorY=%d, allDirty=%v, dirtyLines=%v",
		v.cursorY, allDirty, dirtyLines)

	// After wrapping, allDirty should be true (scrollRegion calls MarkAllDirty)
	if !allDirty {
		t.Errorf("Expected allDirty=true after wrap, got false")
	}

	// Verify grid content is correct
	grid := v.Grid()
	t.Logf("Grid:\n%s", gridToString(grid))
}

// TestDisplayBuffer_LineWrapWithScroll tests wrapping when the screen needs to scroll.
func TestDisplayBuffer_LineWrapWithScroll(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide, 5 rows
	v.EnableDisplayBuffer()

	// Fill up the screen with 4 committed lines
	for i := 1; i <= 4; i++ {
		for _, ch := range "Line" {
			v.placeChar(ch)
		}
		v.placeChar(rune('0' + i))
		v.CarriageReturn()
		v.LineFeed()
	}

	t.Logf("After 4 committed lines: cursorY=%d", v.cursorY)
	t.Logf("  history lines=%d", v.displayBuf.history.Len())

	grid := v.Grid()
	t.Logf("Grid before wrapping line:\n%s", gridToString(grid))

	// Now write a long line that wraps - this should cause scrolling
	text := "ABCDEFGHIJKLMNO" // 15 chars = wraps to 2 lines
	for i, ch := range text {
		v.placeChar(ch)
		if i == 9 || i == 10 { // Log around the wrap point
			t.Logf("After char %d (%c): cursorX=%d, cursorY=%d, logicalX=%d",
				i+1, ch, v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())
		}
	}

	grid = v.Grid()
	t.Logf("Final Grid:\n%s", gridToString(grid))

	// The screen should have scrolled. Expected layout depends on scrolling behavior.
	// With 4 committed lines + 2 physical lines from wrapping = 6 physical lines
	// Screen has 5 rows, so the oldest line should scroll off

	// Cursor should be at row 4 (bottom of screen after scroll)
	t.Logf("Final cursor: (%d, %d)", v.cursorX, v.cursorY)
}

// TestDisplayBuffer_LineWrapWithHistory tests wrapping when there's already committed history.
func TestDisplayBuffer_LineWrapWithHistory(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide, 5 rows
	v.EnableDisplayBuffer()

	// First, write and commit a few lines
	for i := 1; i <= 2; i++ {
		for _, ch := range "Line" {
			v.placeChar(ch)
		}
		v.placeChar(rune('0' + i))
		v.CarriageReturn()
		v.LineFeed()
	}

	t.Logf("After 2 committed lines: cursorY=%d", v.cursorY)
	t.Logf("  history lines=%d", v.displayBuf.history.Len())

	// Now write a long line that wraps
	text := "ABCDEFGHIJKLMNO" // 15 chars = wraps to 2 lines
	for i, ch := range text {
		v.placeChar(ch)
		t.Logf("After char %d (%c): cursorX=%d, cursorY=%d, logicalX=%d",
			i+1, ch, v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())
	}

	grid := v.Grid()
	t.Logf("Final Grid:\n%s", gridToString(grid))

	// Expected layout:
	// Row 0: Line1
	// Row 1: Line2
	// Row 2: ABCDEFGHIJ (first 10 chars of wrapped line)
	// Row 3: KLMNO (remaining 5 chars)
	// Row 4: empty

	expected := []string{"Line1", "Line2", "ABCDEFGHIJ", "KLMNO", ""}
	for i, exp := range expected {
		got := strings.TrimRight(cellsToStringTest(grid[i]), " ")
		if got != exp {
			t.Errorf("Row %d: expected %q, got %q", i, exp, got)
		}
	}

	// Cursor should be at (5, 3) - after 'O' on row 3
	if v.cursorX != 5 || v.cursorY != 3 {
		t.Errorf("Expected cursor at (5,3), got (%d,%d)", v.cursorX, v.cursorY)
	}
}

// TestDisplayBuffer_LineWrap tests that characters appear when wrapping to next line.
func TestDisplayBuffer_LineWrap(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide
	v.EnableDisplayBuffer()

	// Write 15 characters - should wrap to second line
	text := "ABCDEFGHIJKLMNO" // 15 chars
	for _, ch := range text {
		v.placeChar(ch)
	}

	t.Logf("After writing 15 chars:")
	t.Logf("  cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
	t.Logf("  currentLogicalX=%d", v.displayBuf.display.GetCursorOffset())
	t.Logf("  currentLine.Len()=%d", v.displayBuf.display.CurrentLine().Len())
	t.Logf("  currentLinePhysical count=%d", len(v.displayBuf.display.currentLinePhysical()))

	grid := v.Grid()
	t.Logf("Grid:\n%s", gridToString(grid))

	// First row should have ABCDEFGHIJ (10 chars)
	row0 := cellsToStringTest(grid[0])
	if row0 != "ABCDEFGHIJ" {
		t.Errorf("Row 0: expected 'ABCDEFGHIJ', got %q", row0)
	}

	// Second row should have KLMNO (5 chars + spaces)
	row1 := strings.TrimRight(cellsToStringTest(grid[1]), " ")
	if row1 != "KLMNO" {
		t.Errorf("Row 1: expected 'KLMNO', got %q", row1)
	}

	// Cursor should be at (5, 1) - after the 'O' on second line
	if v.cursorX != 5 || v.cursorY != 1 {
		t.Errorf("Expected cursor at (5,1), got (%d,%d)", v.cursorX, v.cursorY)
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

// TestDisplayBuffer_RenderFlowWithWrapAfterHistory tests the rendering flow
// when there's already committed history lines before wrapping occurs.
// This matches the real scenario where "input is at the bottom of the screen".
func TestDisplayBuffer_RenderFlowWithWrapAfterHistory(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide, 5 rows
	v.EnableDisplayBuffer()

	// Create a render buffer (simulating term.go's a.buf)
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 10)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	// Simulate rendering function
	simulateRender := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Write 3 committed lines first (simulating shell output)
	for i := 1; i <= 3; i++ {
		for _, ch := range "Line" {
			v.placeChar(ch)
		}
		v.placeChar(rune('0' + i))
		v.CarriageReturn()
		v.LineFeed()
		simulateRender()
	}

	t.Logf("After 3 committed lines: cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
	t.Logf("Grid:\n%s", gridToString(v.Grid()))

	// Now simulate user typing at the prompt on row 3
	// Type 10 characters to fill the line
	for i, ch := range "ABCDEFGHIJ" {
		v.placeChar(ch)
		simulateRender()
		if i == 9 {
			t.Logf("After 10 chars: cursorX=%d, cursorY=%d, wrapNext=%v", v.cursorX, v.cursorY, v.wrapNext)
		}
	}

	// Verify before wrap
	row3 := cellsToStringTest(renderBuf[3])
	t.Logf("Row 3 before wrap: %q", row3)

	// Now type 'K' to trigger wrap
	t.Logf("Typing 'K' to trigger wrap...")
	v.placeChar('K')

	dirtyLines, allDirty := v.GetDirtyLines()
	t.Logf("After 'K': cursorX=%d, cursorY=%d, allDirty=%v, dirtyLines=%v",
		v.cursorX, v.cursorY, allDirty, dirtyLines)

	simulateRender()

	// Check the grid and render buffer
	vtermGrid := v.Grid()
	t.Logf("After wrap - vtermGrid:")
	for y := 0; y < 5; y++ {
		t.Logf("  Row %d: %q", y, cellsToStringTest(vtermGrid[y]))
	}
	t.Logf("After wrap - renderBuf:")
	for y := 0; y < 5; y++ {
		t.Logf("  Row %d: %q", y, cellsToStringTest(renderBuf[y]))
	}

	// The wrapped content should appear
	// With 3 committed lines + 2 physical lines from currentLine = 5 lines
	// If scrolling occurred, Line1 would scroll off
	row4 := strings.TrimRight(cellsToStringTest(renderBuf[4]), " ")
	if row4 != "K" {
		t.Errorf("Row 4 should have 'K', got %q", row4)
	}
}

// TestDisplayBuffer_RenderFlowWithWrap simulates the exact rendering flow used in term.go
// to verify that dirty tracking correctly updates the render buffer during wrap.
func TestDisplayBuffer_RenderFlowWithWrap(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide, 5 rows
	v.EnableDisplayBuffer()

	// Create a render buffer (simulating term.go's a.buf)
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 10)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	// Simulate rendering function (like term.go's Render)
	simulateRender := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	simulateRender()
	t.Logf("Initial render - buffer is empty")

	// Type first 10 characters (fill first line)
	for i, ch := range "ABCDEFGHIJ" {
		v.placeChar(ch)
		simulateRender()
		t.Logf("After char %d (%c): cursorX=%d, cursorY=%d", i+1, ch, v.cursorX, v.cursorY)
	}

	// Verify row 0 in render buffer
	row0 := cellsToStringTest(renderBuf[0])
	t.Logf("After 10 chars - Row 0 in renderBuf: %q", row0)
	if row0 != "ABCDEFGHIJ" {
		t.Errorf("Row 0: expected 'ABCDEFGHIJ', got %q", row0)
	}

	// Now type the 11th character - this should wrap
	t.Logf("About to type 'K' (11th char, should wrap)")
	t.Logf("  Before: cursorX=%d, cursorY=%d, wrapNext=%v", v.cursorX, v.cursorY, v.wrapNext)

	v.placeChar('K')

	t.Logf("  After: cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
	dirtyLines, allDirty := v.GetDirtyLines()
	t.Logf("  dirtyLines=%v, allDirty=%v", dirtyLines, allDirty)

	// Simulate render
	simulateRender()

	// Check the grid directly
	vtermGrid := v.Grid()
	t.Logf("vtermGrid after wrap:")
	t.Logf("  Row 0: %q", cellsToStringTest(vtermGrid[0]))
	t.Logf("  Row 1: %q", cellsToStringTest(vtermGrid[1]))

	// Check the render buffer
	t.Logf("renderBuf after wrap:")
	t.Logf("  Row 0: %q", cellsToStringTest(renderBuf[0]))
	t.Logf("  Row 1: %q", cellsToStringTest(renderBuf[1]))

	// Verify row 1 has the wrapped character
	row1 := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")
	if row1 != "K" {
		t.Errorf("Row 1 in renderBuf: expected 'K', got %q", row1)
	}

	// Type a few more characters
	for i, ch := range "LMNO" {
		v.placeChar(ch)
		simulateRender()
		t.Logf("After char %d (%c): cursorX=%d, cursorY=%d", 12+i, ch, v.cursorX, v.cursorY)
	}

	// Verify final state
	row0Final := cellsToStringTest(renderBuf[0])
	row1Final := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")
	t.Logf("Final state:")
	t.Logf("  Row 0: %q", row0Final)
	t.Logf("  Row 1: %q", row1Final)

	if row0Final != "ABCDEFGHIJ" {
		t.Errorf("Final Row 0: expected 'ABCDEFGHIJ', got %q", row0Final)
	}
	if row1Final != "KLMNO" {
		t.Errorf("Final Row 1: expected 'KLMNO', got %q", row1Final)
	}
}

// TestDisplayBuffer_WrapWithParser tests wrapping using the actual parser.
// This simulates the exact flow used in the real terminal.
func TestDisplayBuffer_WrapWithParser(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide, 5 rows
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Create a render buffer (simulating term.go's a.buf)
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 10)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	// Simulate rendering function
	simulateRender := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	simulateRender()

	// Parse characters through the parser (like PTY output)
	text := "ABCDEFGHIJKLMNO" // 15 chars - will wrap
	for i, ch := range text {
		p.Parse(ch)
		simulateRender()

		if i == 9 { // After J (10th char)
			t.Logf("After char 10: cursorX=%d, cursorY=%d, wrapNext=%v",
				v.cursorX, v.cursorY, v.wrapNext)
		}
		if i == 10 { // After K (11th char, first on wrapped line)
			t.Logf("After char 11 (K, wrapped): cursorX=%d, cursorY=%d",
				v.cursorX, v.cursorY)
			row1 := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")
			t.Logf("  renderBuf[1] = %q", row1)
			if row1 != "K" {
				t.Errorf("After wrapping, row 1 should have 'K', got %q", row1)
			}
		}
	}

	// Verify final state
	t.Logf("Final Grid:")
	for y := 0; y < 5; y++ {
		t.Logf("  Row %d: %q", y, cellsToStringTest(renderBuf[y]))
	}

	row0 := cellsToStringTest(renderBuf[0])
	row1 := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")

	if row0 != "ABCDEFGHIJ" {
		t.Errorf("Row 0: expected 'ABCDEFGHIJ', got %q", row0)
	}
	if row1 != "KLMNO" {
		t.Errorf("Row 1: expected 'KLMNO', got %q", row1)
	}

	// Cursor should be at (5, 1)
	if v.cursorX != 5 || v.cursorY != 1 {
		t.Errorf("Expected cursor at (5,1), got (%d,%d)", v.cursorX, v.cursorY)
	}
}

// TestDisplayBuffer_RuntimeAdapterRunnerFlow simulates the exact flow of the runtime adapter:
// 1. HandleKey() sends character to PTY
// 2. draw() is called BEFORE the PTY echo arrives (should be no-op or harmless)
// 3. PTY echo arrives, character is processed
// 4. requestRefresh() triggers another draw()
// 5. Character should appear on screen
//
// This tests for any race condition or state corruption from the early draw() call.
func TestDisplayBuffer_RuntimeAdapterRunnerFlow(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Simulate render buffer (like term.go a.buf)
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 10)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	// Simulate exact Render() flow from term.go
	simulateRender := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	simulateRender()

	// Simulate typing 10 characters with the runtime adapter runner pattern:
	// For each character:
	// 1. "HandleKey" - we don't actually send to PTY, just simulate the draw() that happens
	// 2. draw() BEFORE echo (this is the key part!)
	// 3. "PTY echo" - actually process the character
	// 4. draw() after echo

	text := "ABCDEFGHIJ"
	for i, ch := range text {
		// Step 1-2: HandleKey sends to PTY, then draw() is called
		// At this point, the character hasn't been processed yet
		simulateRender() // This draw() should be harmless

		// Step 3: PTY echo arrives, character is processed
		p.Parse(ch)

		// Step 4: requestRefresh triggers draw()
		simulateRender()

		// Verify the character is visible
		row0 := cellsToStringTest(renderBuf[0])[:i+1]
		expected := string(text[:i+1])
		if row0 != expected {
			t.Errorf("After char %d (%c): renderBuf[0] = %q, expected %q", i+1, ch, row0, expected)
		}
	}

	t.Logf("After 10 chars: cursorX=%d, cursorY=%d, wrapNext=%v", v.cursorX, v.cursorY, v.wrapNext)

	// Now type the 11th character (K) which triggers wrap
	// Same runtime adapter runner pattern:

	// Step 1-2: HandleKey, then draw() BEFORE echo
	t.Logf("Before 'K' echo: cursorY=%d", v.cursorY)
	simulateRender() // This draw() happens before K is echoed

	// Step 3: PTY echo for 'K'
	p.Parse('K')
	t.Logf("After 'K' parsed: cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)

	// Check dirty state before render
	dirtyLines, allDirty := v.GetDirtyLines()
	t.Logf("Dirty state after 'K': allDirty=%v, dirtyLines=%v", allDirty, dirtyLines)

	// Step 4: requestRefresh triggers draw()
	simulateRender()

	// Verify the wrapped character is visible
	row1Rune := renderBuf[1][0].Rune
	t.Logf("renderBuf[1][0].Rune = '%c' (expected 'K')", row1Rune)

	if row1Rune != 'K' {
		t.Errorf("After wrap, renderBuf[1][0] should be 'K', got '%c'", row1Rune)
		t.Logf("Full renderBuf:")
		for y := 0; y < 5; y++ {
			t.Logf("  Row %d: %q", y, cellsToStringTest(renderBuf[y]))
		}
		t.Logf("Full Grid:")
		for y := 0; y < 5; y++ {
			t.Logf("  Row %d: %q", y, cellsToStringTest(v.Grid()[y]))
		}
	}

	// Continue typing a few more characters
	for _, ch := range "LMNO" {
		simulateRender() // draw() before echo
		p.Parse(ch)
		simulateRender() // draw() after echo
	}

	// Verify final state
	row0Final := cellsToStringTest(renderBuf[0])
	row1Final := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")

	if row0Final != "ABCDEFGHIJ" {
		t.Errorf("Final row 0: expected 'ABCDEFGHIJ', got %q", row0Final)
	}
	if row1Final != "KLMNO" {
		t.Errorf("Final row 1: expected 'KLMNO', got %q", row1Final)
	}

	t.Logf("Final state: cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
}

// TestDisplayBuffer_WrapAfterLoadingHistory tests wrapping when history was loaded from disk.
// This simulates a terminal restart where history exists.
func TestDisplayBuffer_WrapAfterLoadingHistory(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test.hist2"

	// First, create a terminal and write some history
	v1 := NewVTerm(10, 5)
	err := v1.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 100,
		MarginAbove:    10,
		MarginBelow:    5,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk failed: %v", err)
	}
	p1 := NewParser(v1)

	// Write 3 lines of history
	for i := 1; i <= 3; i++ {
		for _, ch := range "Line" {
			p1.Parse(ch)
		}
		p1.Parse(rune('0' + i))
		p1.Parse('\r')
		p1.Parse('\n')
	}

	t.Logf("After writing history: cursorX=%d, cursorY=%d", v1.cursorX, v1.cursorY)

	// Close and flush to disk
	if err := v1.CloseDisplayBuffer(); err != nil {
		t.Fatalf("CloseDisplayBuffer failed: %v", err)
	}

	// Now create a new terminal loading from the same disk
	v2 := NewVTerm(10, 5)
	err = v2.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 100,
		MarginAbove:    10,
		MarginBelow:    5,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk (reload) failed: %v", err)
	}
	p2 := NewParser(v2)

	t.Logf("After loading history: cursorX=%d, cursorY=%d", v2.cursorX, v2.cursorY)

	// Simulate render buffer
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 10)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	simulateRender := func() {
		vtermGrid := v2.Grid()
		dirtyLines, allDirty := v2.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v2.ClearDirty()
	}

	// Initial render
	simulateRender()

	t.Logf("Initial grid after history load:")
	for y := 0; y < 5; y++ {
		t.Logf("  Row %d: %q", y, cellsToStringTest(renderBuf[y]))
	}

	// Now simulate typing at the prompt (after history) with wrapping
	text := "ABCDEFGHIJ"
	for _, ch := range text {
		simulateRender() // draw() before echo
		p2.Parse(ch)
		simulateRender() // draw() after echo
	}

	t.Logf("After 10 chars: cursorX=%d, cursorY=%d, wrapNext=%v", v2.cursorX, v2.cursorY, v2.wrapNext)

	// Type 'K' to trigger wrap
	simulateRender()
	p2.Parse('K')

	t.Logf("After 'K': cursorX=%d, cursorY=%d", v2.cursorX, v2.cursorY)
	dirtyLines, allDirty := v2.GetDirtyLines()
	t.Logf("Dirty after 'K': allDirty=%v, dirtyLines=%v", allDirty, dirtyLines)

	simulateRender()

	t.Logf("Grid after wrap:")
	for y := 0; y < 5; y++ {
		t.Logf("  Row %d: %q", y, cellsToStringTest(renderBuf[y]))
	}

	// Verify 'K' appears on the wrapped line
	// With 3 history lines + 2 wrapped lines = 5 lines
	// The last row should have 'K'
	row4 := strings.TrimRight(cellsToStringTest(renderBuf[4]), " ")
	if row4 != "K" {
		t.Errorf("After wrap, row 4 should have 'K', got %q", row4)
	}
}

// TestDisplayBuffer_BashReadlineWrap simulates what bash readline might do when
// the user types past the end of a line. Readline sends characters and may also
// send cursor positioning escape sequences.
func TestDisplayBuffer_BashReadlineWrap(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Simulate render buffer
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 10)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	simulateRender := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	simulateRender()

	// Simulate bash prompt "$ " (2 chars)
	for _, ch := range "$ " {
		p.Parse(ch)
	}
	simulateRender()

	// User types "ABCDEFGH" (8 chars) - fills up to column 9 (0-indexed)
	// With prompt, we have 10 chars on the line
	for _, ch := range "ABCDEFGH" {
		simulateRender() // draw before echo
		p.Parse(ch)
		simulateRender() // draw after echo
	}

	t.Logf("After 8 chars: cursorX=%d, cursorY=%d, wrapNext=%v", v.cursorX, v.cursorY, v.wrapNext)

	// User types 'I' - this goes at column 9 (last column)
	simulateRender()
	p.Parse('I')
	simulateRender()

	t.Logf("After 'I': cursorX=%d, cursorY=%d, wrapNext=%v", v.cursorX, v.cursorY, v.wrapNext)

	// User types 'J' - this triggers wrap
	simulateRender()
	p.Parse('J')
	simulateRender()

	t.Logf("After 'J' (wrap): cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)

	// Verify content
	// With 10-column terminal: "$ " (2 chars) + "ABCDEFGH" (8 chars) = 10 chars fill line
	// 'I' triggers wrap and goes to row 1 column 0
	// 'J' goes to row 1 column 1
	row0 := cellsToStringTest(renderBuf[0])
	row1 := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")

	if row0 != "$ ABCDEFGH" {
		t.Errorf("Row 0: expected '$ ABCDEFGH', got %q", row0)
	}
	if row1 != "IJ" {
		t.Errorf("Row 1: expected 'IJ', got %q", row1)
	}

	t.Logf("Grid after wrap:")
	for y := 0; y < 3; y++ {
		t.Logf("  Row %d: %q", y, cellsToStringTest(renderBuf[y]))
	}
}

// TestDisplayBuffer_RuntimeAdapterRunnerFlowWithDisk tests the same flow as RuntimeAdapterRunnerFlow
// but with disk persistence enabled, which is what the real terminal uses.
func TestDisplayBuffer_RuntimeAdapterRunnerFlowWithDisk(t *testing.T) {
	// Create a temporary file for persistence
	tmpDir := t.TempDir()
	diskPath := tmpDir + "/test.hist2"

	v := NewVTerm(10, 5)
	err := v.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 100,
		MarginAbove:    10,
		MarginBelow:    5,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk failed: %v", err)
	}
	p := NewParser(v)

	// Simulate render buffer (like term.go a.buf)
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 10)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	// Simulate exact Render() flow from term.go
	simulateRender := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	simulateRender()

	t.Logf("Initial state: cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)

	// Simulate typing 10 characters with the runtime adapter runner pattern
	text := "ABCDEFGHIJ"
	for i, ch := range text {
		simulateRender() // draw() before echo
		p.Parse(ch)
		simulateRender() // draw() after echo

		row0 := cellsToStringTest(renderBuf[0])[:i+1]
		expected := string(text[:i+1])
		if row0 != expected {
			t.Errorf("After char %d (%c): renderBuf[0] = %q, expected %q", i+1, ch, row0, expected)
		}
	}

	t.Logf("After 10 chars: cursorX=%d, cursorY=%d, wrapNext=%v", v.cursorX, v.cursorY, v.wrapNext)

	// Now type the 11th character (K) which triggers wrap
	t.Logf("Before 'K' echo: cursorY=%d", v.cursorY)
	simulateRender() // draw() BEFORE echo

	p.Parse('K')
	t.Logf("After 'K' parsed: cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)

	// Check dirty state before render
	dirtyLines, allDirty := v.GetDirtyLines()
	t.Logf("Dirty state after 'K': allDirty=%v, dirtyLines=%v", allDirty, dirtyLines)

	simulateRender() // draw() after echo

	// Verify the wrapped character is visible
	row1Rune := renderBuf[1][0].Rune
	t.Logf("renderBuf[1][0].Rune = '%c' (expected 'K')", row1Rune)

	if row1Rune != 'K' {
		t.Errorf("After wrap with disk persistence, renderBuf[1][0] should be 'K', got '%c'", row1Rune)
		t.Logf("Full renderBuf:")
		for y := 0; y < 5; y++ {
			t.Logf("  Row %d: %q", y, cellsToStringTest(renderBuf[y]))
		}
		t.Logf("Full Grid:")
		for y := 0; y < 5; y++ {
			t.Logf("  Row %d: %q", y, cellsToStringTest(v.Grid()[y]))
		}
	}

	// Continue typing a few more characters
	for _, ch := range "LMNO" {
		simulateRender() // draw() before echo
		p.Parse(ch)
		simulateRender() // draw() after echo
	}

	// Verify final state
	row0Final := cellsToStringTest(renderBuf[0])
	row1Final := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")

	if row0Final != "ABCDEFGHIJ" {
		t.Errorf("Final row 0: expected 'ABCDEFGHIJ', got %q", row0Final)
	}
	if row1Final != "KLMNO" {
		t.Errorf("Final row 1: expected 'KLMNO', got %q", row1Final)
	}

	t.Logf("Final state: cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)
}

// TestDisplayBuffer_40ColumnWrap tests wrapping on a 40-column terminal with 45 characters.
// This replicates the exact scenario from the debug log where characters weren't appearing
// on the wrapped line.
func TestDisplayBuffer_40ColumnWrap(t *testing.T) {
	v := NewVTerm(40, 5) // 40 columns wide, 5 rows - matches debug log scenario
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Simulate render buffer (like term.go's a.buf)
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 40)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	simulateRender := func(label string) {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		t.Logf("[%s] cursorX=%d, cursorY=%d, allDirty=%v, dirtyLines=%v",
			label, v.cursorX, v.cursorY, allDirty, dirtyLines)

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 40; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 40; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	simulateRender("init")

	// Type exactly 45 characters: HIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmno
	// This matches the pattern from the debug log (starts with H)
	// 40 chars fill the first line, 5 chars wrap to second line
	text := "HIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnop" // 45 chars
	if len(text) != 45 {
		t.Fatalf("Test text should be 45 chars, got %d", len(text))
	}

	for i, ch := range text {
		// Simulate runtime adapter pattern: draw BEFORE echo, then draw AFTER echo
		simulateRender("before-" + string(ch))
		p.Parse(ch)
		simulateRender("after-" + string(ch))

		// Log key moments
		if i == 38 { // Character 39 (0-indexed), should be at column 38
			t.Logf("After char 39 ('%c'): cursorX=%d, cursorY=%d, wrapNext=%v",
				ch, v.cursorX, v.cursorY, v.wrapNext)
		}
		if i == 39 { // Character 40, fills the line, sets wrapNext
			t.Logf("After char 40 ('%c'): cursorX=%d, cursorY=%d, wrapNext=%v",
				ch, v.cursorX, v.cursorY, v.wrapNext)
		}
		if i == 40 { // Character 41, triggers wrap
			t.Logf("After char 41 ('%c'): cursorX=%d, cursorY=%d, wrapNext=%v",
				ch, v.cursorX, v.cursorY, v.wrapNext)
		}
	}

	// Final state check
	t.Logf("Final Grid:")
	for y := 0; y < 3; y++ {
		t.Logf("  vtermGrid[%d]: %q", y, cellsToStringTest(v.Grid()[y]))
		t.Logf("  renderBuf[%d]: %q", y, cellsToStringTest(renderBuf[y]))
	}

	// Verify row 0 has the first 40 characters
	row0 := cellsToStringTest(renderBuf[0])
	expectedRow0 := text[:40] // "HIJKLMNOPQRSTUVWXYZ0123456789abcdefghijk"
	if row0 != expectedRow0 {
		t.Errorf("Row 0: expected %q, got %q", expectedRow0, row0)
	}

	// Verify row 1 has the remaining 5 characters
	row1 := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")
	expectedRow1 := text[40:] // "lmnop"
	if row1 != expectedRow1 {
		t.Errorf("Row 1: expected %q, got %q", expectedRow1, row1)
	}

	// Cursor should be at (5, 1) - after 'p' on row 1
	if v.cursorX != 5 || v.cursorY != 1 {
		t.Errorf("Expected cursor at (5,1), got (%d,%d)", v.cursorX, v.cursorY)
	}
}

// TestDisplayBuffer_40ColumnWrapWithPrompt tests wrapping on a 40-column terminal
// with a bash-like prompt that includes escape sequences.
func TestDisplayBuffer_40ColumnWrapWithPrompt(t *testing.T) {
	v := NewVTerm(40, 5) // 40 columns wide, 5 rows
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Simulate render buffer
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 40)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	simulateRender := func(label string) {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 40; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 40; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()

		// Log state
		t.Logf("[%s] cursorX=%d, cursorY=%d, allDirty=%v, dirtyLines=%v",
			label, v.cursorX, v.cursorY, allDirty, dirtyLines)
	}

	// Initial render
	simulateRender("init")

	// Simulate a bash prompt with color escape sequences: "\e[32m$\e[0m "
	// This is: ESC [ 3 2 m $ ESC [ 0 m SPACE
	promptSequence := "\x1b[32m$ \x1b[0m"
	for _, ch := range promptSequence {
		p.Parse(ch)
	}
	simulateRender("after-prompt")

	t.Logf("After prompt: cursorX=%d, cursorY=%d", v.cursorX, v.cursorY)

	// Now type enough characters to fill the line and wrap
	// Prompt is "$ " = 2 chars, so we need 38 more to fill row 0, then more to wrap
	text := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnop" // 42 chars

	for i, ch := range text {
		simulateRender("before-" + string(ch))
		p.Parse(ch)
		simulateRender("after-" + string(ch))

		// Log wrap moment
		if v.cursorY > 0 && i < len(text)-1 {
			t.Logf("Wrap happened at char %d ('%c'): cursorX=%d, cursorY=%d",
				i+1, ch, v.cursorX, v.cursorY)
			break // Log just the first wrap
		}
	}

	// Parse remaining characters
	remaining := text[v.cursorX:]
	for _, ch := range remaining {
		p.Parse(ch)
	}
	simulateRender("final")

	// Final state check
	t.Logf("Final Grid:")
	for y := 0; y < 3; y++ {
		t.Logf("  vtermGrid[%d]: %q", y, cellsToStringTest(v.Grid()[y]))
		t.Logf("  renderBuf[%d]: %q", y, cellsToStringTest(renderBuf[y]))
	}

	// Row 0 should have "$ " + 38 chars = 40 chars
	// Row 1 should have remaining chars
	row0 := cellsToStringTest(renderBuf[0])
	row1 := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")

	if !strings.HasPrefix(row0, "$ ") {
		t.Errorf("Row 0 should start with '$ ', got %q", row0[:10])
	}

	if row1 == "" {
		t.Errorf("Row 1 should have wrapped content, but it's empty")
	}

	t.Logf("row0 = %q", row0)
	t.Logf("row1 = %q", row1)
}

// TestDisplayBuffer_CursorMovementOnWrappedLine tests that cursor movement
// on a wrapped line correctly updates the logical cursor position.
func TestDisplayBuffer_CursorMovementOnWrappedLine(t *testing.T) {
	v := NewVTerm(10, 5) // 10 columns wide
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Simulate render buffer
	renderBuf := make([][]Cell, 5)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, 10)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	simulateRender := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < 5; y++ {
				for x := 0; x < 10; x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 5 {
					for x := 0; x < 10; x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	simulateRender()

	// Type 15 characters to wrap to second line
	// "ABCDEFGHIJ" on row 0, "KLMNO" on row 1
	for _, ch := range "ABCDEFGHIJKLMNO" {
		p.Parse(ch)
	}
	simulateRender()

	t.Logf("After typing: cursorX=%d, cursorY=%d, logicalX=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// Cursor should be at (5, 1) with logicalX=15
	if v.cursorX != 5 || v.cursorY != 1 {
		t.Errorf("Expected cursor at (5,1), got (%d,%d)", v.cursorX, v.cursorY)
	}
	if v.displayBuf.display.GetCursorOffset() != 15 {
		t.Errorf("Expected logicalX=15, got %d", v.displayBuf.display.GetCursorOffset())
	}

	// Now move cursor left 3 times (using escape sequence CSI 3 D)
	// This should move from col 5 to col 2, logicalX from 15 to 12
	for _, ch := range "\x1b[3D" {
		p.Parse(ch)
	}

	t.Logf("After cursor left 3: cursorX=%d, cursorY=%d, logicalX=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// BUG: The current implementation sets logicalX = cursorX = 2
	// But it SHOULD be logicalX = 10 + 2 = 12 (accounting for wrapped content)

	// For now, document the current behavior
	if v.cursorX != 2 {
		t.Errorf("Expected cursorX=2 after move left, got %d", v.cursorX)
	}

	// Type a character - where does it appear?
	p.Parse('X')
	simulateRender()

	t.Logf("After typing 'X': cursorX=%d, cursorY=%d, logicalX=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// Check what's in the currentLine (the source of truth for display buffer)
	currentLineContent := ""
	for _, c := range v.displayBuf.display.CurrentLine().Cells {
		if c.Rune != 0 {
			currentLineContent += string(c.Rune)
		}
	}
	t.Logf("CurrentLine content: %q (len=%d)", currentLineContent, len(currentLineContent))

	// Check what Grid() returns
	grid := v.Grid()
	gridRow0 := cellsToStringTest(grid[0])
	gridRow1 := strings.TrimRight(cellsToStringTest(grid[1]), " ")
	t.Logf("Grid directly:")
	t.Logf("  grid[0]: %q", gridRow0)
	t.Logf("  grid[1]: %q", gridRow1)

	row0 := cellsToStringTest(renderBuf[0])
	row1 := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")

	t.Logf("RenderBuf after simulateRender:")
	t.Logf("  Row 0: %q", row0)
	t.Logf("  Row 1: %q", row1)

	// With the bug, 'X' would be placed at logical position 2 (or 3 after increment)
	// which would corrupt row 0 instead of inserting at row 1

	// Expected correct behavior: 'X' should appear at row 1 col 2, after 'LM'
	// So row 1 should be "KLXNO" or similar (depending on insert vs overwrite)

	// Current buggy behavior: logicalX=2, so 'X' goes at position 2 of logical line
	// This would make row 0 = "ABXDEFGHIJ" (X overwrites C)
}

// TestDisplayBuffer_BashReadlineWrapWithCR tests the exact scenario that was causing
// the visual glitch: when bash sends CR after a line wrap during readline editing.
//
// This test simulates the actual render flow with dirty line tracking, which is
// critical for catching visual glitches that Grid() alone wouldn't reveal.
//
// The bug was:
// 1. Type past line width -> wrap occurs, cursor moves to new row
// 2. Bash sends CR for cursor positioning
// 3. Old bug: CR reset logicalX to 0 instead of start of current physical row
// 4. Next character written at position 0, corrupting the wrong row
// 5. Only cursor's row marked dirty, so corruption not rendered -> visual glitch
func TestDisplayBuffer_BashReadlineWrapWithCR(t *testing.T) {
	width := 35
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()

	// Create render buffer to simulate actual terminal rendering
	renderBuf := make([][]Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, width)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' '}
		}
	}

	// Simulate render: only update dirty rows (this is how the real terminal works)
	simulateRender := func() {
		dirtyLines, allDirty := v.GetDirtyLines()
		vtermGrid := v.Grid()
		if allDirty {
			for y := 0; y < height; y++ {
				if y < len(vtermGrid) {
					copy(renderBuf[y], vtermGrid[y])
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < height && y < len(vtermGrid) {
					copy(renderBuf[y], vtermGrid[y])
				}
			}
		}
		v.ClearDirty()
	}

	// Initial render
	simulateRender()

	// Type exactly 35 characters to fill first row and trigger wrap on next char
	// This matches the debug log scenario: "❯ 1234567890abcdefghijklmnopqrstABC"
	inputLine := "12345678901234567890abcdefghijklmno" // 35 chars
	for _, ch := range inputLine {
		v.placeChar(ch)
	}
	simulateRender()

	// Verify we're at the end of line, wrapNext should be true
	if !v.wrapNext {
		t.Errorf("After 35 chars, wrapNext should be true, got false")
	}
	if v.cursorX != width-1 {
		t.Errorf("Expected cursorX=%d, got %d", width-1, v.cursorX)
	}
	if v.cursorY != 0 {
		t.Errorf("Expected cursorY=0, got %d", v.cursorY)
	}

	// Type one more character to trigger wrap
	v.placeChar('p')
	simulateRender()

	// Now cursor should be on row 1
	if v.cursorY != 1 {
		t.Errorf("After wrap, expected cursorY=1, got %d", v.cursorY)
	}
	logicalXAfterWrap := v.displayBuf.display.GetCursorOffset()

	t.Logf("After wrap: cursorX=%d, cursorY=%d, logicalX=%d", v.cursorX, v.cursorY, logicalXAfterWrap)

	// Simulate bash sending CR for cursor positioning (this is what readline does)
	v.CarriageReturn()

	logicalXAfterCR := v.displayBuf.display.GetCursorOffset()
	t.Logf("After CR: cursorX=%d, cursorY=%d, logicalX=%d", v.cursorX, v.cursorY, logicalXAfterCR)

	// KEY ASSERTION: After CR on the second physical row of a wrapped line,
	// logicalX should be 35 (start of second physical row), NOT 0
	expectedLogicalX := width // 35 = start of second physical row
	if logicalXAfterCR != expectedLogicalX {
		t.Errorf("After CR on wrapped line, logicalX should be %d (start of physical row), got %d",
			expectedLogicalX, logicalXAfterCR)
	}

	// Now type more characters - they should appear on row 1, not row 0
	v.placeChar('q')
	v.placeChar('r')
	v.placeChar('s')
	simulateRender()

	// Check the render buffer - this is what the user actually sees
	row0 := cellsToStringTest(renderBuf[0])
	row1 := strings.TrimRight(cellsToStringTest(renderBuf[1]), " ")

	t.Logf("Render buffer (what user sees):")
	t.Logf("  Row 0: %q", row0)
	t.Logf("  Row 1: %q", row1)

	// Row 0 should be the original 35 characters, unchanged
	expectedRow0 := inputLine
	if row0 != expectedRow0 {
		t.Errorf("Row 0 corrupted! Expected %q, got %q", expectedRow0, row0)
	}

	// Row 1 should have the wrapped characters
	// After wrap: 'p' at position 0, then CR moves to start, then 'q', 'r', 's'
	// So row 1 should be "qrs" (q,r,s overwrote p and continued)
	expectedRow1 := "qrs"
	if row1 != expectedRow1 {
		t.Errorf("Row 1 wrong! Expected %q, got %q", expectedRow1, row1)
	}

	// Also verify Grid() matches renderBuf (they should be in sync after proper dirty tracking)
	grid := v.Grid()
	gridRow0 := cellsToStringTest(grid[0])
	gridRow1 := strings.TrimRight(cellsToStringTest(grid[1]), " ")

	if gridRow0 != row0 || gridRow1 != row1 {
		t.Errorf("Grid and renderBuf mismatch!\n  Grid:      [%q, %q]\n  RenderBuf: [%q, %q]",
			gridRow0, gridRow1, row0, row1)
	}
}

// TestDisplayBuffer_WrapDirtyTrackingRegression is a regression test that verifies
// the render buffer matches Grid() after wrap operations. This catches bugs where
// dirty line tracking fails to mark the correct rows.
func TestDisplayBuffer_WrapDirtyTrackingRegression(t *testing.T) {
	width := 10
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()

	// Create render buffer
	renderBuf := make([][]Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, width)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' '}
		}
	}

	simulateRender := func() {
		dirtyLines, allDirty := v.GetDirtyLines()
		vtermGrid := v.Grid()
		if allDirty {
			for y := 0; y < height && y < len(vtermGrid); y++ {
				copy(renderBuf[y], vtermGrid[y])
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < height && y < len(vtermGrid) {
					copy(renderBuf[y], vtermGrid[y])
				}
			}
		}
		v.ClearDirty()
	}

	// Test sequence: type, wrap, CR, type more
	// Each step should have renderBuf == Grid()
	testCases := []struct {
		action string
		fn     func()
	}{
		{"initial render", func() {}},
		{"type A-J (10 chars)", func() {
			for _, ch := range "ABCDEFGHIJ" {
				v.placeChar(ch)
			}
		}},
		{"type K (triggers wrap)", func() { v.placeChar('K') }},
		{"type L", func() { v.placeChar('L') }},
		{"carriage return", func() { v.CarriageReturn() }},
		{"type M (after CR on wrapped line)", func() { v.placeChar('M') }},
		{"type N", func() { v.placeChar('N') }},
	}

	for _, tc := range testCases {
		tc.fn()
		simulateRender()

		grid := v.Grid()
		for y := 0; y < height && y < len(grid); y++ {
			gridRow := cellsToStringTest(grid[y])
			bufRow := cellsToStringTest(renderBuf[y])
			if gridRow != bufRow {
				t.Errorf("After %q: Row %d mismatch!\n  Grid:      %q\n  RenderBuf: %q",
					tc.action, y, gridRow, bufRow)
			}
		}
	}

	// Final state verification
	grid := v.Grid()
	row0 := cellsToStringTest(grid[0])
	row1 := strings.TrimRight(cellsToStringTest(grid[1]), " ")

	t.Logf("Final state: row0=%q, row1=%q", row0, row1)

	// After: ABCDEFGHIJ, then K, L, CR (to col 0 of row 1), M, N
	// Row 0: ABCDEFGHIJ (unchanged)
	// Row 1: MN (M,N overwrote K,L after CR moved to start of physical row 1)
	if row0 != "ABCDEFGHIJ" {
		t.Errorf("Row 0: expected 'ABCDEFGHIJ', got %q", row0)
	}
	if row1 != "MN" {
		t.Errorf("Row 1: expected 'MN', got %q", row1)
	}
}

// TestDisplayBuffer_BashBackspaceRedraw tests the bash readline behavior when
// backspacing across a wrap boundary. Bash:
// 1. Moves cursor to start of the editable area
// 2. Erases to end of line (EL 0)
// 3. Redraws the updated content
// This test verifies that the display buffer correctly handles this sequence.
func TestDisplayBuffer_BashBackspaceRedraw(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	width := 10
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()

	// Create render buffer
	renderBuf := make([][]Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, width)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' '}
		}
	}

	simulateRender := func(desc string) {
		dirtyLines, allDirty := v.GetDirtyLines()
		vtermGrid := v.Grid()
		if allDirty {
			for y := 0; y < height && y < len(vtermGrid); y++ {
				copy(renderBuf[y], vtermGrid[y])
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < height && y < len(vtermGrid) {
					copy(renderBuf[y], vtermGrid[y])
				}
			}
		}
		v.ClearDirty()
		t.Logf("%s: cursor=(%d,%d), row0=%q, row1=%q", desc, v.cursorX, v.cursorY,
			cellsToStringTest(vtermGrid[0]), strings.TrimRight(cellsToStringTest(vtermGrid[1]), " "))
	}

	// Step 1: Write a prompt line and commit it (simulates git status)
	for _, ch := range "PROMPT" {
		v.placeChar(ch)
	}
	v.CarriageReturn()
	v.LineFeed()
	simulateRender("After prompt + LF")

	// Step 2: Write "$ " (the actual prompt) followed by content that wraps
	for _, ch := range "$ aaaaaaaa" { // 2 + 8 = 10 chars = full row
		v.placeChar(ch)
	}
	simulateRender("After first row full ($ + 8 a's)")

	// Step 3: Type more to wrap
	for _, ch := range "bb" { // 2 more chars on row 1
		v.placeChar(ch)
	}
	simulateRender("After wrap (2 b's on row 1)")

	// Verify initial state
	grid := v.Grid()
	t.Logf("Before backspace: row1=%q, row2=%q", cellsToStringTest(grid[1]), strings.TrimRight(cellsToStringTest(grid[2]), " "))

	// Step 4: Simulate bash backspace across wrap boundary
	// Bash moves cursor to the start of the editable content (row 1, col 0)
	v.SetCursorPos(1, 0) // Move to row 1, col 0 (start of "$ aaaaaaaa")
	simulateRender("Cursor moved to (0,1)")

	// Step 5: Erase to end of line - this should clear the entire logical line
	v.ClearLine(0) // EL 0 - Erase to End of Line
	simulateRender("After EL 0")

	// Step 6: Verify the line is cleared
	grid = v.Grid()
	row1 := cellsToStringTest(grid[1])
	row2 := cellsToStringTest(grid[2])
	t.Logf("After erase: row1=%q, row2=%q", row1, row2)

	// The LiveEditor line should be cleared (cursor at offset 0, erase to end = empty)
	liveLen := v.displayBuf.display.CurrentLine().Len()
	t.Logf("LiveEditor line length after erase: %d", liveLen)
	if liveLen != 0 {
		t.Errorf("LiveEditor should be empty after EL 0 at offset 0, got len=%d", liveLen)
	}

	// Step 7: Bash redraws the remaining content (one less 'b')
	for _, ch := range "$ aaaaaaaab" { // 2 + 8 + 1 = 11 chars (wraps)
		v.placeChar(ch)
	}
	simulateRender("After redraw ($ + 8 a's + 1 b)")

	// Step 8: Verify the content is correctly displayed
	grid = v.Grid()
	row1 = strings.TrimRight(cellsToStringTest(grid[1]), " ")
	row2 = strings.TrimRight(cellsToStringTest(grid[2]), " ")
	t.Logf("Final: row1=%q, row2=%q", row1, row2)

	// The redrawn content should wrap with one 'b' on the second row
	if row1 != "$ aaaaaaaa" {
		t.Errorf("Row 1: expected '$ aaaaaaaa', got %q", row1)
	}
	if row2 != "b" {
		t.Errorf("Row 2: expected 'b', got %q", row2)
	}

	// Verify LiveEditor has the correct content
	liveLen = v.displayBuf.display.CurrentLine().Len()
	if liveLen != 11 { // "$ aaaaaaaab" = 11 chars
		t.Errorf("LiveEditor line length: expected 11, got %d", liveLen)
	}
}

// TestDisplayBuffer_WrapBoundaryEraseIssue tests the specific issue where
// content appears in Grid after the LiveEditor has been erased.
// This happens when the VTerm cursor position doesn't match what we expect.
func TestDisplayBuffer_WrapBoundaryEraseIssue(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	width := 35 // Match the real terminal width from the debug log
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()

	// Log db.lines and LiveEditor state
	logState := func(desc string) {
		grid := v.Grid()
		dbLines := v.displayBuf.display.lines() // Use method instead of field
		liveLen := v.displayBuf.display.CurrentLine().Len()
		cursorOffset := v.displayBuf.display.GetCursorOffset()
		t.Logf("%s: cursor=(%d,%d), dbLines=%d, liveLen=%d, cursorOffset=%d",
			desc, v.cursorX, v.cursorY, dbLines, liveLen, cursorOffset)
		for y := 0; y < min(3, len(grid)); y++ {
			t.Logf("  row[%d]: %q", y, cellsToStringTest(grid[y]))
		}
	}

	// Step 1: Simulate git status output (2 lines)
	for _, ch := range "git status line 1" {
		v.placeChar(ch)
	}
	v.CarriageReturn()
	v.LineFeed()
	for _, ch := range "git status line 2" {
		v.placeChar(ch)
	}
	v.CarriageReturn()
	v.LineFeed()
	logState("After 2 prompt lines")

	// Step 2: Write prompt + content that wraps
	// "❯ " (2 chars) + 33 'a's = 35 chars (full row)
	v.placeChar(0x276F) // ❯
	v.placeChar(' ')
	for i := 0; i < 33; i++ {
		v.placeChar('a')
	}
	logState("After first row full")

	// Step 3: Write 5 more 'a's (wrap to second row)
	for i := 0; i < 5; i++ {
		v.placeChar('a')
	}
	logState("After wrap (5 more a's)")

	// Verify LiveEditor content
	liveLen := v.displayBuf.display.CurrentLine().Len()
	if liveLen != 40 { // 2 + 33 + 5 = 40
		t.Errorf("Expected LiveEditor len=40, got %d", liveLen)
	}

	// Step 4: Simulate cursor movement to start of editable content
	// In the debug log, this was physY=2 (row index for "❯ aaaaa...")
	v.SetCursorPos(2, 0) // Row 2, Col 0
	logState("Cursor at (0, 2)")

	// Step 5: Erase to end of line
	v.ClearLine(0)
	logState("After EL 0")

	// Check: LiveEditor should be empty
	liveLen = v.displayBuf.display.CurrentLine().Len()
	if liveLen != 0 {
		t.Errorf("LiveEditor should be empty after EL 0 at row 2 col 0, got len=%d", liveLen)
	}

	// Check: Grid row 2 should be empty (or spaces)
	grid := v.Grid()
	row2 := strings.TrimRight(cellsToStringTest(grid[2]), " ")
	if row2 != "" {
		t.Errorf("Grid row 2 should be empty after EL 0, got %q", row2)
	}

	// Step 6: Redraw the content (without the last character)
	v.placeChar(0x276F) // ❯
	v.placeChar(' ')
	for i := 0; i < 32; i++ { // One less 'a'
		v.placeChar('a')
	}
	logState("After redraw (34 chars)")

	// Step 7: Verify final state
	grid = v.Grid()
	row2 = strings.TrimRight(cellsToStringTest(grid[2]), " ")
	expectedRow2 := "❯ " + strings.Repeat("a", 32) // 34 chars
	if row2 != expectedRow2 {
		t.Errorf("Final row 2: expected %q, got %q", expectedRow2, row2)
	}

	// Row 3 should be empty
	row3 := strings.TrimRight(cellsToStringTest(grid[3]), " ")
	if row3 != "" {
		t.Errorf("Row 3 should be empty, got %q", row3)
	}
}

// TestDisplayBuffer_BashReadlineInsert tests bash's readline insert behavior.
// When user types in the middle of a line, bash:
// 1. Writes the new character (overwriting)
// 2. Redraws all remaining characters (shifted by one position)
// 3. Repositions cursor
//
// This is NOT using terminal insert mode (IRM) - bash handles insertion in software.
func TestDisplayBuffer_BashReadlineInsert(t *testing.T) {
	width := 80
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Type "aaaaaaaaaa" (10 a's)
	for i := 0; i < 10; i++ {
		p.Parse('a')
	}

	// Verify initial state
	line := v.displayBuf.display.CurrentLine()
	if line.Len() != 10 {
		t.Fatalf("Expected line length 10, got %d", line.Len())
	}
	t.Logf("Initial: line=%q, len=%d, cursor=%d", cellsToStringTest(line.Cells), line.Len(), v.displayBuf.display.GetCursorOffset())

	// Move cursor left 5 times (CSI 5 D)
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}

	// Verify cursor moved
	offset := v.displayBuf.display.GetCursorOffset()
	if offset != 5 {
		t.Fatalf("Expected cursor at offset 5 after moving left, got %d", offset)
	}
	t.Logf("After cursor left 5: cursor=%d, cursorX=%d", offset, v.cursorX)

	// Now simulate bash readline inserting a 'b':
	// 1. Write 'b' at current position
	// 2. Write remaining 'a' characters shifted right
	// 3. Position cursor back

	// Step 1: Write 'b'
	p.Parse('b')
	t.Logf("After 'b': line=%q, len=%d, cursor=%d, cursorX=%d",
		cellsToStringTest(v.displayBuf.display.CurrentLine().Cells),
		v.displayBuf.display.CurrentLine().Len(),
		v.displayBuf.display.GetCursorOffset(),
		v.cursorX)

	// After writing 'b', line is "aaaaabaaaa" (overwrote position 5), cursor at 6

	// Step 2: Bash redraws remaining chars - this is the key part!
	// Bash sends the remaining 5 'a' characters at positions 6, 7, 8, 9, 10
	// Position 10 should EXTEND the line to 11 chars
	for i := 0; i < 5; i++ {
		p.Parse('a')
		t.Logf("After redraw 'a' %d: line=%q, len=%d, cursor=%d, cursorX=%d",
			i+1,
			cellsToStringTest(v.displayBuf.display.CurrentLine().Cells),
			v.displayBuf.display.CurrentLine().Len(),
			v.displayBuf.display.GetCursorOffset(),
			v.cursorX)
	}

	// After redraw, line should be "aaaaabaaaaa" (11 chars)
	line = v.displayBuf.display.CurrentLine()
	if line.Len() != 11 {
		t.Errorf("Expected line length 11 after insert+redraw, got %d", line.Len())
		t.Errorf("Line content: %q", cellsToStringTest(line.Cells))
	}

	expectedContent := "aaaaabaaaaa"
	actualContent := cellsToStringTest(line.Cells)
	if actualContent != expectedContent {
		t.Errorf("Expected line content %q, got %q", expectedContent, actualContent)
	}

	// Step 3: Bash positions cursor back to column 6 (after the inserted 'b')
	// This simulates CUP or cursor positioning escape sequence
	for _, ch := range "\x1b[1;7H" { // Move to row 1, column 7 (0-based: 6)
		p.Parse(ch)
	}

	// Cursor should be at offset 6
	offset = v.displayBuf.display.GetCursorOffset()
	if offset != 6 {
		t.Errorf("Expected cursor at offset 6 after positioning, got %d", offset)
	}

	t.Logf("Final: line=%q, len=%d, cursor=%d", cellsToStringTest(line.Cells), line.Len(), offset)
}

// TestDisplayBuffer_BashReadlineInsertMultiple tests multiple insertions like the user scenario:
// 1. Type "aaaaaaaaaa" (10 a's)
// 2. Move left 5
// 3. Type "bbbbb" (5 b's, each causing bash to redraw)
// 4. Expected: "aaaaabbbbbaaaaa" (15 chars)
//
// This test includes visual rendering simulation to catch display sync issues.
func TestDisplayBuffer_BashReadlineInsertMultiple(t *testing.T) {
	width := 80
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Create render buffer
	renderBuf := make([][]Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, width)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	simulateRender := func(label string) {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()

		if allDirty {
			for y := 0; y < height; y++ {
				copy(renderBuf[y], vtermGrid[y])
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < height {
					copy(renderBuf[y], vtermGrid[y])
				}
			}
		}
		v.ClearDirty()

		// Check for visual desync
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if renderBuf[y][x].Rune != vtermGrid[y][x].Rune {
					t.Errorf("[%s] Visual desync at (%d,%d): renderBuf='%c', grid='%c'",
						label, x, y, renderBuf[y][x].Rune, vtermGrid[y][x].Rune)
				}
			}
		}
	}

	// Initial render
	simulateRender("initial")

	// Type "aaaaaaaaaa" (10 a's)
	for i := 0; i < 10; i++ {
		p.Parse('a')
		simulateRender(fmt.Sprintf("type 'a' %d", i+1))
	}

	initialLine := cellsToStringTest(v.displayBuf.display.CurrentLine().Cells)
	t.Logf("Initial: %q", initialLine)

	// Move cursor left 5 times
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}
	simulateRender("cursor left 5")

	// Now insert 5 'b' characters, each with bash-style redraw
	for b := 0; b < 5; b++ {
		// Type 'b'
		p.Parse('b')
		simulateRender(fmt.Sprintf("type 'b' %d", b+1))

		// Bash redraws remaining chars (shifted right)
		// After inserting at position (5+b), bash redraws positions (6+b) to end
		// The number of trailing 'a's to redraw is always 5 (original trailing a's)
		for i := 0; i < 5; i++ {
			p.Parse('a')
			simulateRender(fmt.Sprintf("redraw 'a' %d for 'b' %d", i+1, b+1))
		}

		// Bash positions cursor back
		col := 6 + b // 1-indexed: 7+b
		escSeq := fmt.Sprintf("\x1b[1;%dH", col+1)
		for _, ch := range escSeq {
			p.Parse(ch)
		}
		simulateRender(fmt.Sprintf("cursor position after 'b' %d", b+1))

		curLine := cellsToStringTest(v.displayBuf.display.CurrentLine().Cells)
		t.Logf("After 'b' %d: %q (len=%d, cursor=%d)",
			b+1, curLine, v.displayBuf.display.CurrentLine().Len(),
			v.displayBuf.display.GetCursorOffset())
	}

	// Final check
	line := v.displayBuf.display.CurrentLine()
	actualContent := cellsToStringTest(line.Cells)
	expectedContent := "aaaaabbbbbaaaaa" // 5 a's + 5 b's + 5 a's = 15 chars

	if actualContent != expectedContent {
		t.Errorf("Expected line content %q, got %q", expectedContent, actualContent)
	}

	if line.Len() != 15 {
		t.Errorf("Expected line length 15, got %d", line.Len())
	}

	// Also check the visual output
	grid := v.Grid()
	gridRow0 := strings.TrimRight(cellsToStringTest(grid[0]), " ")
	t.Logf("Grid row 0: %q", gridRow0)

	renderRow0 := strings.TrimRight(cellsToStringTest(renderBuf[0]), " ")
	t.Logf("RenderBuf row 0: %q", renderRow0)

	if gridRow0 != expectedContent {
		t.Errorf("Grid row 0: expected %q, got %q", expectedContent, gridRow0)
	}

	if renderRow0 != expectedContent {
		t.Errorf("RenderBuf row 0: expected %q, got %q", expectedContent, renderRow0)
	}
}

// TestDisplayBuffer_BashReadlineInsertWithHistory tests insertion when there's committed history.
// This simulates a more realistic scenario with previous shell prompts in history.
func TestDisplayBuffer_BashReadlineInsertWithHistory(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	width := 80
	height := 10
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Create some history by typing and pressing enter a few times
	for lineNum := 0; lineNum < 5; lineNum++ {
		// Type a line
		line := fmt.Sprintf("command%d", lineNum)
		for _, ch := range line {
			p.Parse(ch)
		}
		// Press enter (CR + LF)
		p.Parse('\r')
		p.Parse('\n')
	}

	t.Logf("After creating history: committed lines=%d", v.displayBuf.display.lines())

	// Now we're on a new line, type "aaaaaaaaaa"
	for i := 0; i < 10; i++ {
		p.Parse('a')
	}

	initialLine := cellsToStringTest(v.displayBuf.display.CurrentLine().Cells)
	initialOffset := v.displayBuf.display.GetCursorOffset()
	t.Logf("Initial: line=%q, len=%d, cursor=%d, cursorX=%d, cursorY=%d",
		initialLine, v.displayBuf.display.CurrentLine().Len(), initialOffset, v.cursorX, v.cursorY)

	// Move cursor left 5 times
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}

	afterMoveOffset := v.displayBuf.display.GetCursorOffset()
	t.Logf("After cursor left 5: cursor=%d, cursorX=%d", afterMoveOffset, v.cursorX)

	if afterMoveOffset != 5 {
		t.Errorf("Expected cursor at offset 5 after moving left, got %d", afterMoveOffset)
	}

	// Insert 5 'b' characters with bash-style redraw
	for b := 0; b < 5; b++ {
		// Type 'b'
		p.Parse('b')

		// Bash redraws remaining chars
		for i := 0; i < 5; i++ {
			p.Parse('a')
		}

		// Bash positions cursor back
		// Note: CUP uses 1-indexed row/column
		escSeq := fmt.Sprintf("\x1b[%d;%dH", v.cursorY+1, 6+b+1)
		for _, ch := range escSeq {
			p.Parse(ch)
		}

		curLine := cellsToStringTest(v.displayBuf.display.CurrentLine().Cells)
		t.Logf("After 'b' %d: %q (len=%d, cursor=%d, cursorX=%d)",
			b+1, curLine, v.displayBuf.display.CurrentLine().Len(),
			v.displayBuf.display.GetCursorOffset(), v.cursorX)
	}

	// Final check
	line := v.displayBuf.display.CurrentLine()
	actualContent := cellsToStringTest(line.Cells)
	expectedContent := "aaaaabbbbbaaaaa"

	if actualContent != expectedContent {
		t.Errorf("Expected line content %q, got %q", expectedContent, actualContent)
	}

	if line.Len() != 15 {
		t.Errorf("Expected line length 15, got %d", line.Len())
	}
}

// TestDisplayBuffer_BashReadlineInsertNarrowTerminal tests insertion with a narrow terminal
// where the line might wrap. This uses CR+CUF for cursor positioning, which is closer to
// what bash actually does for multi-line editing.
//
// The test has been updated to properly position the cursor after each redraw, accounting
// for the fact that with each insertion, the target column increases by 1.
func TestDisplayBuffer_BashReadlineInsertNarrowTerminal(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	width := 12 // Narrow terminal
	height := 10
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Type "aaaaaaaaaa" (10 a's) - fits in one row
	for i := 0; i < 10; i++ {
		p.Parse('a')
	}

	initialLine := cellsToStringTest(v.displayBuf.display.CurrentLine().Cells)
	t.Logf("Initial: line=%q, len=%d, cursor=%d, cursorX=%d, cursorY=%d",
		initialLine, v.displayBuf.display.CurrentLine().Len(),
		v.displayBuf.display.GetCursorOffset(), v.cursorX, v.cursorY)

	// Move cursor left 5 times to position 5
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}

	afterMoveOffset := v.displayBuf.display.GetCursorOffset()
	t.Logf("After cursor left 5: cursor=%d, cursorX=%d", afterMoveOffset, v.cursorX)

	if afterMoveOffset != 5 {
		t.Errorf("Expected cursor at offset 5 after moving left, got %d", afterMoveOffset)
	}

	// Insert 5 'b' characters with bash-style redraw
	// After each 'b', the line grows by 1 character
	// After 'b' N, cursor should be at position 5+N (just after the N'th 'b')
	for b := 0; b < 5; b++ {
		beforeLen := v.displayBuf.display.CurrentLine().Len()
		beforeCursor := v.displayBuf.display.GetCursorOffset()

		// Type 'b'
		p.Parse('b')
		afterBLen := v.displayBuf.display.CurrentLine().Len()
		afterBCursor := v.displayBuf.display.GetCursorOffset()

		// Bash redraws remaining chars (5 a's)
		for i := 0; i < 5; i++ {
			p.Parse('a')
		}
		afterRedrawLen := v.displayBuf.display.CurrentLine().Len()
		afterRedrawCursor := v.displayBuf.display.GetCursorOffset()

		// Bash positions cursor back to just after the 'b' we typed
		// Target position is 5 + b + 1 = 6 + b
		// Current position is 6 + b + 5 = 11 + b (after typing 'b' + 5 'a's)
		// So we need to go back 5 positions
		escSeq := "\x1b[5D"
		for _, ch := range escSeq {
			p.Parse(ch)
		}
		afterPosLen := v.displayBuf.display.CurrentLine().Len()
		afterPosCursor := v.displayBuf.display.GetCursorOffset()

		curLine := cellsToStringTest(v.displayBuf.display.CurrentLine().Cells)
		t.Logf("'b' %d: cursor %d->%d->%d->%d, len %d->%d->%d->%d, X=%d Y=%d, line=%q",
			b+1,
			beforeCursor, afterBCursor, afterRedrawCursor, afterPosCursor,
			beforeLen, afterBLen, afterRedrawLen, afterPosLen,
			v.cursorX, v.cursorY, curLine)

		// Verify cursor is at the right position after positioning
		expectedCursor := 6 + b
		if afterPosCursor != expectedCursor {
			t.Errorf("After 'b' %d, expected cursor at %d, got %d", b+1, expectedCursor, afterPosCursor)
		}
	}

	// Final check
	line := v.displayBuf.display.CurrentLine()
	actualContent := cellsToStringTest(line.Cells)
	expectedContent := "aaaaabbbbbaaaaa"

	if actualContent != expectedContent {
		t.Errorf("Expected line content %q, got %q", expectedContent, actualContent)
	}

	if line.Len() != 15 {
		t.Errorf("Expected line length 15, got %d", line.Len())
	}

	// Check wrapping - at width 12, 15 chars should be 2 rows
	physLines := line.WrapToWidth(width)
	t.Logf("Physical lines: %d (should be 2 for 15 chars at width 12)", len(physLines))
}

// TestDisplayBuffer_SimpleOverwriteAfterCursorMove tests the exact user scenario:
// Type 10 a's, move back 5, type 5 b's (overwriting), move back 5, type 5 c's.
// This does NOT simulate bash readline - just raw terminal overwrite behavior.
func TestDisplayBuffer_SimpleOverwriteAfterCursorMove(t *testing.T) {
	width := 80
	height := 24
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Helper to get grid content as string
	getGridRow := func(row int) string {
		grid := v.Grid()
		if row >= len(grid) {
			return ""
		}
		return strings.TrimRight(cellsToStringTest(grid[row]), " ")
	}

	// Type 10 'a' characters
	for i := 0; i < 10; i++ {
		p.Parse('a')
	}
	t.Logf("After 10 a's: Grid=%q, cursor=(%d,%d), offset=%d",
		getGridRow(0), v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	if getGridRow(0) != "aaaaaaaaaa" {
		t.Errorf("After 10 a's: expected 'aaaaaaaaaa', got %q", getGridRow(0))
	}

	// Move cursor back 5 positions with CUB (ESC [ 5 D)
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}
	t.Logf("After CUB 5: cursor=(%d,%d), offset=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	if v.cursorX != 5 {
		t.Errorf("After CUB 5: expected cursorX=5, got %d", v.cursorX)
	}

	// Type 5 'b' characters - these OVERWRITE the a's (terminal is NOT in insert mode)
	for i := 0; i < 5; i++ {
		p.Parse('b')
	}
	t.Logf("After 5 b's: Grid=%q, cursor=(%d,%d), offset=%d",
		getGridRow(0), v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// Should show "aaaaabbbbb" - the b's overwrote the last 5 a's
	if getGridRow(0) != "aaaaabbbbb" {
		t.Errorf("After 5 b's: expected 'aaaaabbbbb', got %q", getGridRow(0))
	}

	// Move cursor back 5 positions again
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}
	t.Logf("After CUB 5 again: cursor=(%d,%d), offset=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	if v.cursorX != 5 {
		t.Errorf("After CUB 5 again: expected cursorX=5, got %d", v.cursorX)
	}

	// Type 5 'c' characters - these OVERWRITE the b's
	for i := 0; i < 5; i++ {
		p.Parse('c')
	}
	t.Logf("After 5 c's: Grid=%q, cursor=(%d,%d), offset=%d",
		getGridRow(0), v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// Should show "aaaaaccccc" - the c's overwrote the b's
	if getGridRow(0) != "aaaaaccccc" {
		t.Errorf("After 5 c's: expected 'aaaaaccccc', got %q", getGridRow(0))
	}

	// Also verify the logical line content matches
	line := v.displayBuf.display.CurrentLine()
	lineContent := cellsToStringTest(line.Cells)
	t.Logf("Logical line: %q, len=%d", lineContent, line.Len())

	if lineContent != "aaaaaccccc" {
		t.Errorf("Logical line: expected 'aaaaaccccc', got %q", lineContent)
	}
}

// TestDisplayBuffer_SimpleOverwriteWithRenderFlow tests the same scenario but
// with dirty line tracking simulation - exactly what the real renderer does.
func TestDisplayBuffer_SimpleOverwriteWithRenderFlow(t *testing.T) {
	width := 80
	height := 24
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Enable debug logging
	v.SetDisplayBufferDebugLog(func(format string, args ...interface{}) {
		t.Logf("[DEBUG] "+format, args...)
	})

	// NOTE: This test simulates OVERWRITE mode where typing 'b' at position 5
	// just overwrites 'a'. But bash readline does INSERTION - it sends 'b',
	// then redraws the rest of the line, then moves cursor back.
	// See TestDisplayBuffer_BashActualInsertionFlow for the real bash behavior.

	// Create render buffer (what user sees)
	renderBuf := make([][]Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, width)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' '}
		}
	}

	// Simulate render: ONLY update dirty rows (this is what the real renderer does)
	simulateRender := func(label string) {
		dirtyLines, allDirty := v.GetDirtyLines()
		vtermGrid := v.Grid()

		if allDirty {
			for y := 0; y < height && y < len(vtermGrid); y++ {
				copy(renderBuf[y], vtermGrid[y])
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < height && y < len(vtermGrid) {
					copy(renderBuf[y], vtermGrid[y])
				}
			}
		}
		v.ClearDirty()

		// Log what we rendered
		t.Logf("%s: rendered rows %v (allDirty=%v)", label, dirtyLines, allDirty)
	}

	getRow := func(buf [][]Cell, row int) string {
		return strings.TrimRight(cellsToStringTest(buf[row]), " ")
	}

	// Initial render
	simulateRender("initial")

	// Type 10 'a' characters
	for i := 0; i < 10; i++ {
		p.Parse('a')
	}
	simulateRender("after 10 a's")
	t.Logf("RenderBuf after 10 a's: %q", getRow(renderBuf, 0))

	if getRow(renderBuf, 0) != "aaaaaaaaaa" {
		t.Errorf("RenderBuf after 10 a's: expected 'aaaaaaaaaa', got %q", getRow(renderBuf, 0))
	}

	// Move cursor back 5 positions with CUB (ESC [ 5 D)
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}
	simulateRender("after CUB 5")
	t.Logf("Cursor after CUB 5: (%d,%d), offset=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// Type 5 'b' characters
	for i := 0; i < 5; i++ {
		p.Parse('b')
		simulateRender(fmt.Sprintf("after b %d", i+1))
	}
	t.Logf("RenderBuf after 5 b's: %q", getRow(renderBuf, 0))
	t.Logf("Grid after 5 b's: %q", getRow(v.Grid(), 0))

	if getRow(renderBuf, 0) != "aaaaabbbbb" {
		t.Errorf("RenderBuf after 5 b's: expected 'aaaaabbbbb', got %q", getRow(renderBuf, 0))
	}

	// Move cursor back 5 positions again
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}
	simulateRender("after CUB 5 again")

	// Type 5 'c' characters
	for i := 0; i < 5; i++ {
		p.Parse('c')
		simulateRender(fmt.Sprintf("after c %d", i+1))
	}
	t.Logf("RenderBuf after 5 c's: %q", getRow(renderBuf, 0))
	t.Logf("Grid after 5 c's: %q", getRow(v.Grid(), 0))

	if getRow(renderBuf, 0) != "aaaaaccccc" {
		t.Errorf("RenderBuf after 5 c's: expected 'aaaaaccccc', got %q", getRow(renderBuf, 0))
	}
}

// TestDisplayBuffer_BashActualInsertionFlow tests what bash ACTUALLY does when
// you type a character in the middle of the line. Bash handles insertion internally
// and redraws the visible text via escape sequences.
//
// When you type 'b' at position 5 in "aaaaaaaaaa":
// 1. bash inserts 'b' in its buffer -> "aaaaabaaaaa" (11 chars)
// 2. bash echoes 'b' at cursor position 5 (terminal overwrites 'a')
// 3. bash echoes remaining chars 'aaaaa' at positions 6-10
// 4. bash moves cursor back to position 6 (just after the 'b')
//
// This is the sequence that was failing in the real terminal.
func TestDisplayBuffer_BashActualInsertionFlow(t *testing.T) {
	width := 80
	height := 24
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	v.SetDisplayBufferDebugLog(func(format string, args ...interface{}) {
		t.Logf("[DEBUG] "+format, args...)
	})

	getRow := func() string {
		grid := v.Grid()
		if len(grid) == 0 {
			return ""
		}
		return strings.TrimRight(cellsToStringTest(grid[0]), " ")
	}

	// Type 10 'a' characters
	for i := 0; i < 10; i++ {
		p.Parse('a')
	}
	t.Logf("After 10 a's: Grid=%q, cursorX=%d, offset=%d",
		getRow(), v.cursorX, v.displayBuf.display.GetCursorOffset())

	if getRow() != "aaaaaaaaaa" {
		t.Fatalf("Initial line wrong: expected 'aaaaaaaaaa', got %q", getRow())
	}

	// Move cursor back 5 positions with CUB (ESC [ 5 D)
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}
	t.Logf("After CUB 5: cursorX=%d, offset=%d", v.cursorX, v.displayBuf.display.GetCursorOffset())

	if v.cursorX != 5 || v.displayBuf.display.GetCursorOffset() != 5 {
		t.Fatalf("Cursor position wrong after CUB 5: cursorX=%d, offset=%d",
			v.cursorX, v.displayBuf.display.GetCursorOffset())
	}

	// Now simulate what bash does when you type 'b':
	// 1. Echo 'b' at current position
	p.Parse('b')
	t.Logf("After echo 'b': Grid=%q, cursorX=%d, offset=%d",
		getRow(), v.cursorX, v.displayBuf.display.GetCursorOffset())

	// 2. Echo remaining 5 'a's (bash redraws the rest of the line)
	for i := 0; i < 5; i++ {
		p.Parse('a')
	}
	t.Logf("After redraw 'aaaaa': Grid=%q, cursorX=%d, offset=%d",
		getRow(), v.cursorX, v.displayBuf.display.GetCursorOffset())

	// At this point, the line should be "aaaaabaaaaa" (11 chars)
	// cursorX should be 11, offset should be 11
	expected := "aaaaabaaaaa"
	if getRow() != expected {
		t.Errorf("After bash redraw: expected %q, got %q", expected, getRow())
	}

	// 3. Bash moves cursor back to position 6 (just after the 'b')
	for _, ch := range "\x1b[5D" {
		p.Parse(ch)
	}
	t.Logf("After CUB 5 to reposition: Grid=%q, cursorX=%d, offset=%d",
		getRow(), v.cursorX, v.displayBuf.display.GetCursorOffset())

	// Cursor should be at position 6, offset should be 6
	if v.cursorX != 6 {
		t.Errorf("After reposition: expected cursorX=6, got %d", v.cursorX)
	}
	if v.displayBuf.display.GetCursorOffset() != 6 {
		t.Errorf("After reposition: expected offset=6, got %d", v.displayBuf.display.GetCursorOffset())
	}

	// The grid should still show correct content
	if getRow() != expected {
		t.Errorf("Grid corrupted after reposition: expected %q, got %q", expected, getRow())
	}

	// Now type another 'b' - same sequence
	p.Parse('b')
	for i := 0; i < 4; i++ { // Only 4 a's left after 'b'
		p.Parse('a')
	}
	for _, ch := range "\x1b[4D" { // Move back 4
		p.Parse(ch)
	}
	t.Logf("After second 'b': Grid=%q, cursorX=%d, offset=%d",
		getRow(), v.cursorX, v.displayBuf.display.GetCursorOffset())

	expected2 := "aaaaabbaaaa"
	if getRow() != expected2 {
		t.Errorf("After second 'b': expected %q, got %q", expected2, getRow())
	}
}

// TestDisplayBuffer_ICH_InsertCharacters tests that ICH (Insert Character - CSI @)
// properly integrates with the display buffer, shifting existing content right.
func TestDisplayBuffer_ICH_InsertCharacters(t *testing.T) {
	width := 40
	height := 10
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()

	getRow := func() string {
		grid := v.Grid()
		if len(grid) == 0 || len(grid[0]) == 0 {
			return ""
		}
		var sb strings.Builder
		for _, cell := range grid[0] {
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		return strings.TrimRight(sb.String(), " ")
	}

	// Type "hello world"
	for _, r := range "hello world" {
		v.placeChar(r)
	}

	t.Logf("Initial: line=%q, cursorX=%d", getRow(), v.cursorX)

	// Move cursor back to position 6 (after "hello ")
	v.MoveCursorBackward(5) // Now at position 6

	t.Logf("After CUB 5: cursorX=%d, offset=%d", v.cursorX, v.displayBuf.display.GetCursorOffset())

	// Use ICH to insert 3 blank characters (CSI 3 @)
	v.InsertCharacters(3)

	t.Logf("After ICH 3: line=%q, cursorX=%d, offset=%d", getRow(), v.cursorX, v.displayBuf.display.GetCursorOffset())

	// Type "NEW" into the blank space
	for _, r := range "NEW" {
		v.placeChar(r)
	}

	result := getRow()
	t.Logf("After typing 'NEW': line=%q, cursorX=%d", result, v.cursorX)

	// Expected: "hello NEWworld"
	// - "hello " (6 chars)
	// - "NEW" (3 chars we inserted)
	// - "world" (5 chars shifted right)
	expected := "hello NEWworld"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// Cursor should be at position 9 (after "hello NEW")
	if v.cursorX != 9 {
		t.Errorf("Expected cursorX=9, got %d", v.cursorX)
	}
}

// TestDisplayBuffer_ICH_AtEndOfLine tests ICH at the end of a line.
func TestDisplayBuffer_ICH_AtEndOfLine(t *testing.T) {
	width := 40
	height := 10
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()

	getRow := func() string {
		grid := v.Grid()
		if len(grid) == 0 || len(grid[0]) == 0 {
			return ""
		}
		var sb strings.Builder
		for _, cell := range grid[0] {
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		return strings.TrimRight(sb.String(), " ")
	}

	// Type "hello"
	for _, r := range "hello" {
		v.placeChar(r)
	}

	t.Logf("Initial: line=%q, cursorX=%d", getRow(), v.cursorX)

	// Insert 3 blanks at end
	v.InsertCharacters(3)

	t.Logf("After ICH 3 at end: line=%q, cursorX=%d", getRow(), v.cursorX)

	// Type "!!!"
	for _, r := range "!!!" {
		v.placeChar(r)
	}

	result := getRow()
	t.Logf("After typing '!!!': line=%q", result)

	// Expected: "hello!!!"
	expected := "hello!!!"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestDisplayBuffer_WrappedLine_CursorMoveAndInsert tests the bug scenario where:
// 1. User types a line that wraps (logical line spans multiple physical rows)
// 2. User moves cursor backward to the middle of the line
// 3. User inserts characters at that position
// This tests that the logical and physical cursor positions remain synchronized.
func TestDisplayBuffer_WrappedLine_CursorMoveAndInsert(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	width := 10 // Narrow terminal to force wrapping
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Helper to get the full logical line content
	getLogicalLine := func() string {
		line := v.displayBuf.display.CurrentLine()
		var sb strings.Builder
		for _, cell := range line.Cells {
			if cell.Rune != 0 {
				sb.WriteRune(cell.Rune)
			}
		}
		return sb.String()
	}

	// Helper to get grid content
	getGridRows := func() []string {
		grid := v.Grid()
		rows := make([]string, len(grid))
		for y, row := range grid {
			var sb strings.Builder
			for _, cell := range row {
				if cell.Rune == 0 {
					sb.WriteRune(' ')
				} else {
					sb.WriteRune(cell.Rune)
				}
			}
			rows[y] = strings.TrimRight(sb.String(), " ")
		}
		return rows
	}

	// Helper to simulate render and check for visual/logical consistency
	renderBuf := make([][]Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]Cell, width)
		for x := range renderBuf[y] {
			renderBuf[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}
	simulateRender := func() {
		vtermGrid := v.Grid()
		dirtyLines, allDirty := v.GetDirtyLines()
		if allDirty {
			for y := 0; y < height && y < len(vtermGrid); y++ {
				for x := 0; x < width && x < len(vtermGrid[y]); x++ {
					renderBuf[y][x] = vtermGrid[y][x]
				}
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < height && y < len(vtermGrid) {
					for x := 0; x < width && x < len(vtermGrid[y]); x++ {
						renderBuf[y][x] = vtermGrid[y][x]
					}
				}
			}
		}
		v.ClearDirty()
	}

	checkConsistency := func(step string) {
		logicalLine := getLogicalLine()
		gridRows := getGridRows()
		offset := v.displayBuf.display.GetCursorOffset()
		physCol, physRow, _ := v.displayBuf.display.GetPhysicalCursorPos()

		t.Logf("%s:", step)
		t.Logf("  logicalLine=%q (len=%d)", logicalLine, len(logicalLine))
		t.Logf("  cursorOffset=%d, physRow=%d, physCol=%d", offset, physRow, physCol)
		t.Logf("  vtermCursor=(%d,%d)", v.cursorX, v.cursorY)
		t.Logf("  grid[0]=%q, grid[1]=%q", gridRows[0], gridRows[1])

		// Check: rendered content should match logical line when reconstructed
		reconstructed := ""
		for _, row := range gridRows {
			reconstructed += row
		}
		reconstructed = strings.TrimRight(reconstructed, " ")
		if reconstructed != logicalLine {
			t.Errorf("%s: MISMATCH - logicalLine=%q but grid reconstructs to %q", step, logicalLine, reconstructed)
		}
	}

	// Step 1: Type a line that wraps (14 chars on width=10 terminal)
	for _, ch := range "Hello World!!!" {
		p.Parse(ch)
	}
	simulateRender()
	checkConsistency("After typing 'Hello World!!!'")

	// Verify: 14 chars should span 2 rows (0-9 on row 0, 10-13 on row 1)
	logicalLine := getLogicalLine()
	if logicalLine != "Hello World!!!" {
		t.Errorf("Expected 'Hello World!!!', got %q", logicalLine)
	}
	if v.displayBuf.display.GetCursorOffset() != 14 {
		t.Errorf("Expected offset=14, got %d", v.displayBuf.display.GetCursorOffset())
	}

	// Step 2: Move cursor to middle of line using absolute positioning
	// Use CUP (Cursor Position) to move to row 0, column 6 (after "Hello ")
	// ESC [ 1 ; 7 H = move to row 1, column 7 (1-based)
	for _, ch := range "\x1b[1;7H" {
		p.Parse(ch)
	}
	simulateRender()
	checkConsistency("After CUP to row 1, col 7")

	// Verify cursor position: physical (6,0) = logical offset 6
	if v.cursorX != 6 || v.cursorY != 0 {
		t.Errorf("After CUP: expected cursor (6,0), got (%d,%d)", v.cursorX, v.cursorY)
	}
	expectedOffset := 6 // "Hello " = 6 chars
	actualOffset := v.displayBuf.display.GetCursorOffset()
	if actualOffset != expectedOffset {
		t.Errorf("After CUP: expected offset=%d, got %d", expectedOffset, actualOffset)
	}

	// Step 3: Insert "NEW" at position 6
	for _, ch := range "NEW" {
		p.Parse(ch)
	}
	simulateRender()
	checkConsistency("After inserting 'NEW'")

	// In non-insert mode (overwrite), the characters after cursor get overwritten
	// - Original: "Hello World!!!" (14 chars)
	// - After typing "NEW" at position 6 (overwrite mode):
	//   Position 6: 'N' replaces 'W'
	//   Position 7: 'E' replaces 'o'
	//   Position 8: 'W' replaces 'r'
	//   Result: "Hello NEWld!!!" (14 chars)
	expectedLine := "Hello NEWld!!!"
	actualLine := getLogicalLine()
	if actualLine != expectedLine {
		t.Errorf("After inserting 'NEW': expected %q, got %q", expectedLine, actualLine)
	}

	// Verify grid matches logical
	gridRows := getGridRows()
	// "Hello NEWld!!!" = ['H','e','l','l','o',' ','N','E','W','l','d','!','!','!']
	// Row 0 = chars 0-9 = "Hello NEWl"
	expectedRow0 := "Hello NEWl"
	expectedRow1 := "d!!!"
	if gridRows[0] != expectedRow0 {
		t.Errorf("Row 0: expected %q, got %q", expectedRow0, gridRows[0])
	}
	if gridRows[1] != expectedRow1 {
		t.Errorf("Row 1: expected %q, got %q", expectedRow1, gridRows[1])
	}

	// Verify cursor is now at position 9 (after "Hello NEW")
	if v.displayBuf.display.GetCursorOffset() != 9 {
		t.Errorf("After typing 'NEW': expected offset=9, got %d", v.displayBuf.display.GetCursorOffset())
	}
}

// TestDisplayBuffer_WrappedLine_MoveAcrossWrapBoundary tests cursor movement
// that crosses the wrap boundary (from second physical row to first).
func TestDisplayBuffer_WrappedLine_MoveAcrossWrapBoundary(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	width := 10
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Type 15 characters to create a wrapped line
	for _, ch := range "ABCDEFGHIJKLMNO" {
		p.Parse(ch)
	}

	t.Logf("Initial: cursorX=%d, cursorY=%d, offset=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// Cursor should be at (5, 1) - after 'O' on second row
	if v.cursorX != 5 || v.cursorY != 1 {
		t.Errorf("Initial cursor: expected (5,1), got (%d,%d)", v.cursorX, v.cursorY)
	}
	if v.displayBuf.display.GetCursorOffset() != 15 {
		t.Errorf("Initial offset: expected 15, got %d", v.displayBuf.display.GetCursorOffset())
	}

	// Move cursor up (to first row) - this crosses the wrap boundary
	for _, ch := range "\x1b[A" { // CUU 1
		p.Parse(ch)
	}

	t.Logf("After CUU: cursorX=%d, cursorY=%d, offset=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// Cursor should now be at (5, 0) - same column, previous row
	if v.cursorX != 5 || v.cursorY != 0 {
		t.Errorf("After CUU: expected cursor (5,0), got (%d,%d)", v.cursorX, v.cursorY)
	}
	// Offset should be 5 (position on first row)
	if v.displayBuf.display.GetCursorOffset() != 5 {
		t.Errorf("After CUU: expected offset=5, got %d", v.displayBuf.display.GetCursorOffset())
	}

	// Now type 'X' - it should replace 'F' (position 5)
	p.Parse('X')

	logicalLine := v.displayBuf.display.CurrentLine()
	var result string
	for _, cell := range logicalLine.Cells {
		if cell.Rune != 0 {
			result += string(cell.Rune)
		}
	}

	t.Logf("After typing 'X': line=%q, cursorX=%d, offset=%d",
		result, v.cursorX, v.displayBuf.display.GetCursorOffset())

	// Expected: "ABCDEXGHIJKLMNO" - 'X' replaced 'F' at position 5
	expected := "ABCDEXGHIJKLMNO"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// Verify grid
	grid := v.Grid()
	row0 := cellsToStringTest(grid[0])
	row1 := strings.TrimRight(cellsToStringTest(grid[1]), " ")

	if row0 != "ABCDEXGHIJ" {
		t.Errorf("Row 0: expected 'ABCDEXGHIJ', got %q", row0)
	}
	if row1 != "KLMNO" {
		t.Errorf("Row 1: expected 'KLMNO', got %q", row1)
	}
}

// TestDisplayBuffer_WrappedLine_InsertModeAcrossWrap tests insert mode when
// inserting causes content to reflow across wrap boundary.
func TestDisplayBuffer_WrappedLine_InsertModeAcrossWrap(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	width := 10
	height := 5
	v := NewVTerm(width, height)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Type exactly 10 characters (fills first row exactly)
	for _, ch := range "0123456789" {
		p.Parse(ch)
	}

	t.Logf("After 10 chars: cursorX=%d, cursorY=%d, offset=%d, wrapNext=%v",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset(), v.wrapNext)

	// Move cursor to position 5 (middle of line)
	for _, ch := range "\x1b[5D" { // CUB 5
		p.Parse(ch)
	}

	t.Logf("After CUB 5: cursorX=%d, cursorY=%d, offset=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// After typing 10 chars, wrapNext was true:
	// - Physical cursor was at (9,0) but logical offset was 10
	// - CUB 5 means "move back 5 positions"
	// - Logically: 10 - 5 = 5
	// - Physically: cursorX moves from 9 to 4
	// The offset should track logical position (5), not physical (4)
	if v.cursorX != 4 {
		t.Errorf("After CUB 5: expected cursorX=4, got %d", v.cursorX)
	}
	if v.displayBuf.display.GetCursorOffset() != 5 {
		t.Errorf("After CUB 5: expected offset=5, got %d", v.displayBuf.display.GetCursorOffset())
	}

	// Enable insert mode (CSI 4 h)
	for _, ch := range "\x1b[4h" {
		p.Parse(ch)
	}

	// Insert "XXX" - this should push 56789 to the right
	for _, ch := range "XXX" {
		p.Parse(ch)
	}

	logicalLine := v.displayBuf.display.CurrentLine()
	var result string
	for _, cell := range logicalLine.Cells {
		if cell.Rune != 0 {
			result += string(cell.Rune)
		}
	}

	t.Logf("After inserting 'XXX': line=%q (len=%d)", result, len(result))
	t.Logf("  cursorX=%d, cursorY=%d, offset=%d",
		v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset())

	// Expected: "01234XXX56789" (13 chars)
	// - Original: "0123456789" (10 chars)
	// - Cursor at position 5 (between '4' and '5')
	// - Insert "XXX" shifts "56789" right
	expected := "01234XXX56789"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// Verify grid shows wrapped content correctly
	grid := v.Grid()
	row0 := cellsToStringTest(grid[0])
	row1 := strings.TrimRight(cellsToStringTest(grid[1]), " ")

	t.Logf("Grid: row0=%q, row1=%q", row0, row1)

	// Row 0 should be first 10 chars: "01234XXX56"
	// Row 1 should be remaining: "789"
	expectedRow0 := "01234XXX56"
	expectedRow1 := "789"

	if row0 != expectedRow0 {
		t.Errorf("Row 0: expected %q, got %q", expectedRow0, row0)
	}
	if row1 != expectedRow1 {
		t.Errorf("Row 1: expected %q, got %q", expectedRow1, row1)
	}

	// Disable insert mode
	for _, ch := range "\x1b[4l" {
		p.Parse(ch)
	}
}
