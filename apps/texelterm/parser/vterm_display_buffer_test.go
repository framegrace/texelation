// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"path/filepath"
	"testing"
)

func TestVTerm_DisplayBufferInit(t *testing.T) {
	v := NewVTerm(80, 24)

	// Display buffer is always enabled now
	if !v.IsDisplayBufferEnabled() {
		t.Error("display buffer should be enabled by default")
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
	t.Skip("Skip: tests old scroll architecture, needs rewrite for ViewportState")
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

	// Carriage return - use the VTerm method which updates cursorX and syncs display buffer
	v.CarriageReturn()

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

func TestVTerm_DisplayBufferRightPromptLineFeed(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	for _, r := range "LEFT" {
		p.Parse(r)
	}

	// Move far right (CUF 500), write right prompt, move back left.
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('5')
	p.Parse('0')
	p.Parse('0')
	p.Parse('C')
	p.Parse('R')
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('1')
	p.Parse('D')
	p.Parse('\n')

	grid := v.Grid()
	if len(grid) == 0 || len(grid[0]) < 20 {
		t.Fatalf("unexpected grid size: %dx%d", len(grid), len(grid[0]))
	}

	if grid[0][0].Rune != 'L' {
		t.Errorf("expected left prompt at col 0, got %q", grid[0][0].Rune)
	}
	if grid[0][19].Rune != 'R' {
		t.Errorf("expected right prompt at col 19, got %q", grid[0][19].Rune)
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

// TestVTerm_DisplayBufferBackspaceErase tests the BS+SPACE+BS pattern
// that shells use to visually erase a character.
func TestVTerm_DisplayBufferBackspaceErase(t *testing.T) {
	t.Skip("Skip: tests old logical line architecture, needs rewrite for ViewportState")
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write "Hello"
	for _, r := range "Hello" {
		v.placeChar(r)
	}

	// Verify initial state
	line := v.displayBufferGetCurrentLine()
	if cellsToString(line.Cells) != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", cellsToString(line.Cells))
	}

	// Simulate BS + SPACE + BS (shell's erase sequence)
	v.Backspace()    // Move cursor left (pos 4)
	v.placeChar(' ') // Overwrite 'o' with space (pos 5)
	v.Backspace()    // Move cursor back (pos 4)

	// Check the logical line content
	line = v.displayBufferGetCurrentLine()
	got := cellsToString(line.Cells)
	if got != "Hell " {
		t.Errorf("expected 'Hell ' after BS+SPACE+BS, got '%s'", got)
	}

	// Also verify Grid() returns the correct content
	grid := v.Grid()
	gridLine := ""
	for x := 0; x < 5; x++ {
		gridLine += string(grid[0][x].Rune)
	}
	if gridLine != "Hell " {
		t.Errorf("expected Grid to show 'Hell ', got '%s'", gridLine)
	}
}

// TestVTerm_DisplayBufferBackspaceXSync tests that cursorX and currentLogicalX
// stay synchronized during the backspace erase pattern.
func TestVTerm_DisplayBufferBackspaceXSync(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Helper to get currentLogicalX
	getLogicalX := func() int {
		if v.displayBuf == nil {
			return -1
		}
		return v.displayBuf.display.GetCursorOffset()
	}

	// Write "Hello"
	for i, r := range "Hello" {
		v.placeChar(r)
		t.Logf("After '%c': cursorX=%d, logicalX=%d", r, v.GetCursorX(), getLogicalX())
		if v.GetCursorX() != i+1 {
			t.Errorf("After '%c': expected cursorX=%d, got %d", r, i+1, v.GetCursorX())
		}
		if getLogicalX() != i+1 {
			t.Errorf("After '%c': expected logicalX=%d, got %d", r, i+1, getLogicalX())
		}
	}

	// cursorX=5, logicalX=5 (both pointing past the last char)

	// BS should decrement both
	v.Backspace()
	t.Logf("After BS: cursorX=%d, logicalX=%d", v.GetCursorX(), getLogicalX())
	if v.GetCursorX() != 4 {
		t.Errorf("After BS: expected cursorX=4, got %d", v.GetCursorX())
	}
	if getLogicalX() != 4 {
		t.Errorf("After BS: expected logicalX=4, got %d", getLogicalX())
	}

	// SPACE should write at position 4 and advance both to 5
	v.placeChar(' ')
	t.Logf("After SPACE: cursorX=%d, logicalX=%d", v.GetCursorX(), getLogicalX())
	if v.GetCursorX() != 5 {
		t.Errorf("After SPACE: expected cursorX=5, got %d", v.GetCursorX())
	}
	if getLogicalX() != 5 {
		t.Errorf("After SPACE: expected logicalX=5, got %d", getLogicalX())
	}

	// BS again
	v.Backspace()
	t.Logf("After 2nd BS: cursorX=%d, logicalX=%d", v.GetCursorX(), getLogicalX())
	if v.GetCursorX() != 4 {
		t.Errorf("After 2nd BS: expected cursorX=4, got %d", v.GetCursorX())
	}
	if getLogicalX() != 4 {
		t.Errorf("After 2nd BS: expected logicalX=4, got %d", getLogicalX())
	}
}

// TestVTerm_DisplayBufferBackspaceEraseCellValues tests that the actual Cell
// values are correct after the BS+SPACE+BS pattern - checking both Rune and attributes.
func TestVTerm_DisplayBufferBackspaceEraseCellValues(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write "Hello"
	for _, r := range "Hello" {
		v.placeChar(r)
	}

	// Get the grid and check the initial Cell values
	grid := v.Grid()
	for x := 0; x < 5; x++ {
		cell := grid[0][x]
		t.Logf("Before BS: grid[0][%d] = Rune=%q (0x%02x), FG=%+v, BG=%+v, Attr=%d",
			x, cell.Rune, cell.Rune, cell.FG, cell.BG, cell.Attr)
	}

	// Simulate BS + SPACE + BS
	v.Backspace()
	v.placeChar(' ')
	v.Backspace()

	// Get the grid again and check Cell values
	grid = v.Grid()
	for x := 0; x < 6; x++ {
		cell := grid[0][x]
		t.Logf("After BS+SP+BS: grid[0][%d] = Rune=%q (0x%02x), FG=%+v, BG=%+v, Attr=%d",
			x, cell.Rune, cell.Rune, cell.FG, cell.BG, cell.Attr)
	}

	// Position 4 should now be a SPACE (0x20), not 'o'
	cell4 := grid[0][4]
	if cell4.Rune != ' ' {
		t.Errorf("grid[0][4].Rune should be ' ' (0x20), got %q (0x%02x)", cell4.Rune, cell4.Rune)
	}
	// Verify it's not the null character
	if cell4.Rune == 0 {
		t.Errorf("grid[0][4].Rune is null (0x00), should be space (0x20)")
	}
}

// TestVTerm_DisplayBufferBackspaceEraseWithParser tests the BS+SPACE+BS pattern
// through the parser, exactly as the real terminal would receive it.
func TestVTerm_DisplayBufferBackspaceEraseWithParser(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	t.Logf("DisplayBuffer enabled: %v", v.IsDisplayBufferEnabled())
	if !v.IsDisplayBufferEnabled() {
		t.Fatal("Display buffer should be enabled")
	}

	getGridLine := func() string {
		grid := v.Grid()
		if len(grid) == 0 || len(grid[0]) == 0 {
			return ""
		}
		result := ""
		for x := 0; x < min(10, len(grid[0])); x++ {
			c := grid[0][x]
			if c.Rune == 0 || c.Rune == ' ' {
				result += "_"
			} else {
				result += string(c.Rune)
			}
		}
		return result
	}

	// Type "Hello" through parser
	for _, r := range "Hello" {
		p.Parse(r)
		_ = v.Grid() // Simulate render after each char
	}

	gridLine := getGridLine()
	t.Logf("After 'Hello': %s (cursor at %d)", gridLine, v.GetCursorX())
	if gridLine != "Hello_____" {
		t.Errorf("expected 'Hello_____', got '%s'", gridLine)
	}

	// BS (0x08) through parser
	p.Parse('\b')
	gridLine = getGridLine()
	t.Logf("After BS: %s (cursor at %d)", gridLine, v.GetCursorX())
	if v.GetCursorX() != 4 {
		t.Errorf("expected cursor at 4, got %d", v.GetCursorX())
	}

	// SPACE (0x20) through parser - this erases 'o'
	p.Parse(' ')
	gridLine = getGridLine()
	t.Logf("After SPACE: %s (cursor at %d)", gridLine, v.GetCursorX())
	if gridLine != "Hell______" {
		t.Errorf("expected 'Hell______' after SPACE, got '%s'", gridLine)
	}

	// BS (0x08) through parser
	p.Parse('\b')
	gridLine = getGridLine()
	t.Logf("After final BS: %s (cursor at %d)", gridLine, v.GetCursorX())
	if gridLine != "Hell______" {
		t.Errorf("expected 'Hell______' after final BS, got '%s'", gridLine)
	}
	if v.GetCursorX() != 4 {
		t.Errorf("expected cursor at 4, got %d", v.GetCursorX())
	}
}

// TestVTerm_DisplayBufferBackspaceEraseWithInterleavedGridCalls tests the BS+SPACE+BS
// pattern with Grid() calls after each operation, simulating real terminal behavior.
func TestVTerm_DisplayBufferBackspaceEraseWithInterleavedGridCalls(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	getGridLine := func() string {
		grid := v.Grid()
		if len(grid) == 0 || len(grid[0]) == 0 {
			return ""
		}
		result := ""
		for x := 0; x < min(10, len(grid[0])); x++ {
			c := grid[0][x]
			if c.Rune == 0 || c.Rune == ' ' {
				result += "_"
			} else {
				result += string(c.Rune)
			}
		}
		return result
	}

	// Write "Hello" with Grid() call after each char
	for _, r := range "Hello" {
		v.placeChar(r)
		_ = v.Grid() // Simulate render after each char
	}

	// Verify state: "Hello_____"
	gridLine := getGridLine()
	t.Logf("After 'Hello': %s", gridLine)
	if gridLine != "Hello_____" {
		t.Errorf("expected 'Hello_____', got '%s'", gridLine)
	}

	// BS (cursor 5 -> 4)
	v.Backspace()
	gridLine = getGridLine()
	t.Logf("After BS: %s (cursor at %d)", gridLine, v.GetCursorX())
	// Content should still be "Hello" but cursor at 4
	if gridLine != "Hello_____" {
		t.Errorf("expected 'Hello_____' after BS, got '%s'", gridLine)
	}
	if v.GetCursorX() != 4 {
		t.Errorf("expected cursor at 4, got %d", v.GetCursorX())
	}

	// SPACE (overwrites 'o' with space, cursor 4 -> 5)
	v.placeChar(' ')
	gridLine = getGridLine()
	t.Logf("After SPACE: %s (cursor at %d)", gridLine, v.GetCursorX())
	// Content should now be "Hell " (with trailing space)
	if gridLine != "Hell______" {
		t.Errorf("expected 'Hell______' after SPACE, got '%s'", gridLine)
	}
	if v.GetCursorX() != 5 {
		t.Errorf("expected cursor at 5, got %d", v.GetCursorX())
	}

	// BS (cursor 5 -> 4)
	v.Backspace()
	gridLine = getGridLine()
	t.Logf("After final BS: %s (cursor at %d)", gridLine, v.GetCursorX())
	// Content should still be "Hell " with cursor at 4
	if gridLine != "Hell______" {
		t.Errorf("expected 'Hell______' after final BS, got '%s'", gridLine)
	}
	if v.GetCursorX() != 4 {
		t.Errorf("expected cursor at 4, got %d", v.GetCursorX())
	}
}

// TestVTerm_DisplayBufferBackspaceEraseWithEL tests backspace using EL (Erase Line)
// Some shells use: CUB (cursor back) + EL 0 (erase to end of line)
func TestVTerm_DisplayBufferBackspaceEraseWithEL(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	getGridLine := func() string {
		grid := v.Grid()
		if len(grid) == 0 || len(grid[0]) == 0 {
			return ""
		}
		result := ""
		for x := 0; x < min(10, len(grid[0])); x++ {
			c := grid[0][x]
			if c.Rune == 0 || c.Rune == ' ' {
				result += "_"
			} else {
				result += string(c.Rune)
			}
		}
		return result
	}

	// Type "Hello" through parser
	for _, r := range "Hello" {
		p.Parse(r)
	}

	gridLine := getGridLine()
	t.Logf("After 'Hello': %s (cursor at %d)", gridLine, v.GetCursorX())
	if gridLine != "Hello_____" {
		t.Errorf("expected 'Hello_____', got '%s'", gridLine)
	}

	// Simulate backspace using BS + EL 0
	// First move cursor back with BS
	p.Parse('\b')
	t.Logf("After BS: cursor at %d", v.GetCursorX())
	if v.GetCursorX() != 4 {
		t.Errorf("expected cursor at 4, got %d", v.GetCursorX())
	}

	// Then send EL 0 (CSI K) to erase from cursor to end of line
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('K')

	gridLine = getGridLine()
	t.Logf("After EL: %s (cursor at %d)", gridLine, v.GetCursorX())
	// EL 0 erases from cursor to end, so 'o' should be gone and we should have "Hell"
	if gridLine != "Hell______" {
		t.Errorf("expected 'Hell______' after EL, got '%s'", gridLine)
	}
	if v.GetCursorX() != 4 {
		t.Errorf("expected cursor at 4 after EL, got %d", v.GetCursorX())
	}
}

// TestVTerm_DisplayBufferBackspaceEraseWithDCH tests the DCH (Delete Character) sequence
// that modern shells like bash use for backspace erase: CUB (cursor back) + DCH
func TestVTerm_DisplayBufferBackspaceEraseWithDCH(t *testing.T) {
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	getGridLine := func() string {
		grid := v.Grid()
		if len(grid) == 0 || len(grid[0]) == 0 {
			return ""
		}
		result := ""
		for x := 0; x < min(10, len(grid[0])); x++ {
			c := grid[0][x]
			if c.Rune == 0 || c.Rune == ' ' {
				result += "_"
			} else {
				result += string(c.Rune)
			}
		}
		return result
	}

	// Type "Hello" through parser
	for _, r := range "Hello" {
		p.Parse(r)
	}

	gridLine := getGridLine()
	t.Logf("After 'Hello': %s (cursor at %d)", gridLine, v.GetCursorX())
	if gridLine != "Hello_____" {
		t.Errorf("expected 'Hello_____', got '%s'", gridLine)
	}

	// Simulate bash's backspace using DCH (Delete Character)
	// First move cursor back with BS
	p.Parse('\b')
	t.Logf("After BS: cursor at %d", v.GetCursorX())
	if v.GetCursorX() != 4 {
		t.Errorf("expected cursor at 4, got %d", v.GetCursorX())
	}

	// Then send DCH (CSI P) to delete the character
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('P')

	gridLine = getGridLine()
	t.Logf("After DCH: %s (cursor at %d)", gridLine, v.GetCursorX())
	// DCH shifts content left, so 'o' should be gone and we should have "Hell"
	if gridLine != "Hell______" {
		t.Errorf("expected 'Hell______' after DCH, got '%s'", gridLine)
	}
	if v.GetCursorX() != 4 {
		t.Errorf("expected cursor at 4 after DCH, got %d", v.GetCursorX())
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
	v.SetCursorPos(v.cursorY, 5)

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
	v.SetCursorPos(v.cursorY, 0)

	// Erase 5 characters (ECH 5)
	v.EraseCharacters(5)

	line := v.displayBufferGetCurrentLine()
	got := cellsToString(line.Cells)
	if got != "      World" {
		t.Errorf("expected '      World' after erase chars, got '%s'", got)
	}
}

func TestVTerm_DisplayBufferResizeReflowContent(t *testing.T) {
	t.Skip("Skip: tests old reflow architecture, needs rewrite for ViewportState")
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
	t.Skip("Skip: tests old reflow architecture, needs rewrite for ViewportState")
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
	t.Skip("Skip: tests old logical offset architecture, needs rewrite for ViewportState")
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write some content
	for _, r := range "Hello World" {
		v.placeChar(r)
	}

	// Cursor should be at position 11
	if v.displayBuf.display.GetCursorOffset() != 11 {
		t.Errorf("expected logicalX 11, got %d", v.displayBuf.display.GetCursorOffset())
	}

	// Resize narrower
	v.Resize(10, 5)

	// Logical X should be unchanged (still position 11 in logical line)
	if v.displayBuf.display.GetCursorOffset() != 11 {
		t.Errorf("expected logicalX 11 after resize, got %d", v.displayBuf.display.GetCursorOffset())
	}
}

func TestVTerm_DisplayBufferResizeWhileScrolledUp(t *testing.T) {
	t.Skip("Skip: tests old scroll architecture, needs rewrite for ViewportState")
	// Regression test: resizing while scrolled up should preserve full history
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()

	// Write 50 lines of content
	for i := 0; i < 50; i++ {
		text := "Line " + string(rune('A'+i%26))
		for _, r := range text {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	// Verify we have 50 lines
	histLen := v.displayBufferHistoryLen()
	if histLen != 50 {
		t.Fatalf("expected 50 lines in history, got %d", histLen)
	}

	// Scroll up a lot (away from live edge)
	v.Scroll(-30)

	// Should not be at live edge
	if v.displayBufferAtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Now resize (this was triggering the bug)
	v.Resize(15, 5)

	// History should still be fully intact
	histLenAfter := v.displayBufferHistoryLen()
	if histLenAfter != 50 {
		t.Errorf("expected 50 lines after resize, got %d (history was truncated!)", histLenAfter)
	}

	// Should be able to scroll down to live edge
	v.ScrollToLiveEdge()

	if !v.displayBufferAtLiveEdge() {
		t.Error("should be at live edge after ScrollToLiveEdge")
	}

	// Grid should still work and show content
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil after resize while scrolled")
	}
}

func TestVTerm_DisplayBufferScrollPreservesContent(t *testing.T) {
	t.Skip("Skip: tests old scroll architecture, needs rewrite for ViewportState")
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
	t.Skip("Skip: tests old history architecture, needs rewrite for ViewportState")
	v := NewVTerm(10, 5)
	v.EnableDisplayBuffer()

	// Write a line, then empty line, then another line
	// Use CR+LF to properly start at column 0 on each new line
	for _, r := range "First" {
		v.placeChar(r)
	}
	v.CarriageReturn()
	v.LineFeed()

	// Empty line (just CR+LF)
	v.CarriageReturn()
	v.LineFeed()

	for _, r := range "Third" {
		v.placeChar(r)
	}
	v.CarriageReturn()
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
	if line2 == nil {
		t.Errorf("expected line2 to exist in history, got nil")
		return
	}
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
	t.Skip("Skip: tests old history architecture, needs rewrite for ViewportState")
	// Test performance with large history using disk backing
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "large_history.hist")

	v := NewVTerm(80, 24)
	err := v.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 1000, // Small memory window to test disk spilling
		MarginAbove:    200,
		MarginBelow:    50,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk failed: %v", err)
	}
	defer v.CloseDisplayBuffer()

	// Write 10000 lines
	for i := 0; i < 10000; i++ {
		text := "This is line number " + string(rune('0'+i%10))
		for _, r := range text {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	// Should have 10000 total lines (disk + memory)
	if v.displayBufferHistoryTotalLen() != 10000 {
		t.Errorf("expected 10000 total lines, got %d", v.displayBufferHistoryTotalLen())
	}

	// Memory should be limited
	if v.displayBufferHistoryLen() > 1000 {
		t.Errorf("expected <= 1000 lines in memory, got %d", v.displayBufferHistoryLen())
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

	// Should still have 10000 total lines
	if v.displayBufferHistoryTotalLen() != 10000 {
		t.Errorf("expected 10000 total lines after scroll, got %d", v.displayBufferHistoryTotalLen())
	}
}

func TestVTerm_DisplayBufferDiskScrolling(t *testing.T) {
	t.Skip("Skip: tests old scroll/history architecture, needs rewrite for ViewportState")
	// Test that scrolling back into disk history works correctly
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "scroll_test.hist")

	v := NewVTerm(80, 10) // Small viewport
	err := v.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 20, // Very small memory to force disk usage
		MarginAbove:    10,
		MarginBelow:    5,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk failed: %v", err)
	}
	defer v.CloseDisplayBuffer()

	// Write 100 lines with identifiable content
	for i := 0; i < 100; i++ {
		text := "Line number " + string(rune('A'+(i%26)))
		for _, r := range text {
			v.placeChar(r)
		}
		// Pad with index
		for _, r := range " [" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + "]" {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	// Verify total count
	if v.displayBufferHistoryTotalLen() != 100 {
		t.Errorf("expected 100 total lines, got %d", v.displayBufferHistoryTotalLen())
	}

	// Memory should be limited
	if v.displayBufferHistoryLen() > 20 {
		t.Errorf("expected <= 20 lines in memory, got %d", v.displayBufferHistoryLen())
	}

	// At live edge, we should see the latest lines
	if !v.displayBufferAtLiveEdge() {
		t.Error("should be at live edge after writing")
	}

	// Scroll up a lot to go back into disk history
	v.Scroll(-80) // This should trigger loading from disk

	// Should no longer be at live edge
	if v.displayBufferAtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Grid should still work
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid() returned nil after scroll into disk history")
	}

	// Scroll back to live edge
	v.ScrollToLiveEdge()

	if !v.displayBufferAtLiveEdge() {
		t.Error("should be at live edge after ScrollToLiveEdge")
	}

	// Total should still be 100
	if v.displayBufferHistoryTotalLen() != 100 {
		t.Errorf("expected 100 total lines after scroll cycle, got %d", v.displayBufferHistoryTotalLen())
	}
}

func TestVTerm_DisplayBufferPersistAndReload(t *testing.T) {
	t.Skip("Skip: tests old persistence architecture, needs rewrite for ViewportState")
	// Test that history persists across close/reopen
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "persist_test.hist")

	// Create terminal and write some content
	v := NewVTerm(80, 10)
	err := v.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 50,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk failed: %v", err)
	}

	// Write 30 lines
	for i := 0; i < 30; i++ {
		for _, r := range "Persistent line" {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	// Close to flush to disk
	if err := v.CloseDisplayBuffer(); err != nil {
		t.Fatalf("CloseDisplayBuffer failed: %v", err)
	}

	// Create new terminal and load from disk
	v2 := NewVTerm(80, 10)
	err = v2.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 50,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk on reload failed: %v", err)
	}
	defer v2.CloseDisplayBuffer()

	// Should have 30 lines from disk
	if v2.displayBufferHistoryTotalLen() != 30 {
		t.Errorf("expected 30 lines after reload, got %d", v2.displayBufferHistoryTotalLen())
	}
}

func TestVTerm_DisplayBufferAppendAfterReload(t *testing.T) {
	t.Skip("Skip: tests old persistence architecture, needs rewrite for ViewportState")
	// Regression test: must be able to append new lines after loading existing history
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "append_test.hist")

	// Create terminal and write initial content
	v := NewVTerm(80, 10)
	err := v.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 50,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk failed: %v", err)
	}

	// Write 10 lines
	for i := 0; i < 10; i++ {
		for _, r := range "Initial line" {
			v.placeChar(r)
		}
		v.LineFeed()
	}

	// Close to flush to disk
	if err := v.CloseDisplayBuffer(); err != nil {
		t.Fatalf("CloseDisplayBuffer failed: %v", err)
	}

	// Reopen and append more lines
	v2 := NewVTerm(80, 10)
	err = v2.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 50,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk on reload failed: %v", err)
	}

	// Verify initial content loaded
	if v2.displayBufferHistoryTotalLen() != 10 {
		t.Fatalf("expected 10 lines after reload, got %d", v2.displayBufferHistoryTotalLen())
	}

	// Append 5 more lines (this was failing before the fix)
	for i := 0; i < 5; i++ {
		for _, r := range "New line after reload" {
			v2.placeChar(r)
		}
		v2.LineFeed()
	}

	// Should now have 15 lines total
	if v2.displayBufferHistoryTotalLen() != 15 {
		t.Errorf("expected 15 lines after appending, got %d", v2.displayBufferHistoryTotalLen())
	}

	// Close and reopen to verify persistence
	if err := v2.CloseDisplayBuffer(); err != nil {
		t.Fatalf("CloseDisplayBuffer failed: %v", err)
	}

	v3 := NewVTerm(80, 10)
	err = v3.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
		MaxMemoryLines: 50,
	})
	if err != nil {
		t.Fatalf("EnableDisplayBufferWithDisk on second reload failed: %v", err)
	}
	defer v3.CloseDisplayBuffer()

	// Should still have 15 lines
	if v3.displayBufferHistoryTotalLen() != 15 {
		t.Errorf("expected 15 lines after second reload, got %d", v3.displayBufferHistoryTotalLen())
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

// TestVTerm_DisplayBufferInsertMode tests that insert mode (IRM) works correctly
// with the display buffer - when insert mode is enabled, new characters should
// shift existing content right rather than overwriting.
func TestVTerm_DisplayBufferInsertMode(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	getGridLine := func() string {
		grid := v.Grid()
		if len(grid) == 0 || len(grid[0]) == 0 {
			return ""
		}
		result := ""
		for x := 0; x < min(10, len(grid[0])); x++ {
			c := grid[0][x]
			if c.Rune == 0 || c.Rune == ' ' {
				result += "_"
			} else {
				result += string(c.Rune)
			}
		}
		return result
	}

	// Type "ABC"
	for _, r := range "ABC" {
		p.Parse(r)
	}

	gridLine := getGridLine()
	t.Logf("After 'ABC': %s (cursor at %d)", gridLine, v.GetCursorX())
	if gridLine != "ABC_______" {
		t.Errorf("expected 'ABC_______', got '%s'", gridLine)
	}

	// Move cursor back to position 1 (between A and B)
	p.Parse('\x1b') // ESC
	p.Parse('[')
	p.Parse('2')
	p.Parse('D') // CUB 2 (cursor back 2)

	if v.GetCursorX() != 1 {
		t.Errorf("expected cursor at 1, got %d", v.GetCursorX())
	}

	// Enable insert mode (CSI 4 h)
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('4')
	p.Parse('h')

	// Verify insert mode is enabled
	if !v.insertMode {
		t.Fatal("insert mode should be enabled")
	}

	// Type "XY" in insert mode - should shift BC right, resulting in "AXYBC"
	for _, r := range "XY" {
		p.Parse(r)
	}

	gridLine = getGridLine()
	t.Logf("After 'XY' in insert mode: %s (cursor at %d)", gridLine, v.GetCursorX())
	if gridLine != "AXYBC_____" {
		t.Errorf("expected 'AXYBC_____', got '%s'", gridLine)
	}

	// Disable insert mode (CSI 4 l)
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('4')
	p.Parse('l')

	// Verify insert mode is disabled
	if v.insertMode {
		t.Fatal("insert mode should be disabled")
	}

	// Type "Z" in replace mode - should overwrite at current position
	p.Parse('Z')

	gridLine = getGridLine()
	t.Logf("After 'Z' in replace mode: %s (cursor at %d)", gridLine, v.GetCursorX())
	if gridLine != "AXYZC_____" {
		t.Errorf("expected 'AXYZC_____', got '%s'", gridLine)
	}
}

// TestVTerm_DisplayBufferInsertModeAtEndOfLine tests insert mode when cursor
// is at the end of existing content - should effectively be like append.
func TestVTerm_DisplayBufferInsertModeAtEndOfLine(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	getGridLine := func() string {
		grid := v.Grid()
		if len(grid) == 0 || len(grid[0]) == 0 {
			return ""
		}
		result := ""
		for x := 0; x < min(10, len(grid[0])); x++ {
			c := grid[0][x]
			if c.Rune == 0 || c.Rune == ' ' {
				result += "_"
			} else {
				result += string(c.Rune)
			}
		}
		return result
	}

	// Type "ABC"
	for _, r := range "ABC" {
		p.Parse(r)
	}

	// Enable insert mode
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('4')
	p.Parse('h')

	// Type "DEF" at the end - should append
	for _, r := range "DEF" {
		p.Parse(r)
	}

	gridLine := getGridLine()
	t.Logf("After 'DEF' at end in insert mode: %s", gridLine)
	if gridLine != "ABCDEF____" {
		t.Errorf("expected 'ABCDEF____', got '%s'", gridLine)
	}
}

// TestVTerm_DisplayBufferInsertModeMatchesHistoryBuffer tests that the display
// buffer and history buffer stay in sync when using insert mode.
func TestVTerm_DisplayBufferInsertModeMatchesHistoryBuffer(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Type "ABC"
	for _, r := range "ABC" {
		p.Parse(r)
	}

	// Move cursor back to position 1
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('2')
	p.Parse('D')

	// Enable insert mode
	p.Parse('\x1b')
	p.Parse('[')
	p.Parse('4')
	p.Parse('h')

	// Type "XY"
	for _, r := range "XY" {
		p.Parse(r)
	}

	// Get content from display buffer
	dbLine := v.displayBufferGetCurrentLine()
	dbContent := cellsToString(dbLine.Cells)

	// Get content from history buffer
	histLine := v.getHistoryLine(0)
	histContent := cellsToString(histLine)

	t.Logf("Display buffer: %q", dbContent)
	t.Logf("History buffer: %q", histContent)

	if dbContent != histContent {
		t.Errorf("display buffer (%q) doesn't match history buffer (%q)", dbContent, histContent)
	}

	if dbContent != "AXYBC" {
		t.Errorf("expected 'AXYBC', got %q", dbContent)
	}
}
