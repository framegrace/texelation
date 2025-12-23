package parser

import (
	"strings"
	"testing"
)

// TestDisplayBuffer_GetLogicalPos verifies that physical viewport coordinates
// are correctly mapped to logical line indices and offsets.
func TestDisplayBuffer_GetLogicalPos(t *testing.T) {
	// Setup: 10-column terminal
	// Committed History:
	//   Line 0: "0123456789" (10 chars, exact fit)
	//   Line 1: "012345678901234" (15 chars, wraps to 2 rows)
	// Current Line (Uncommitted):
	//   Line 2: "abcde" (5 chars)
	
	// Physical Layout (Width=10):
	// Row 0: "0123456789" (Line 0)
	// Row 1: "0123456789" (Line 1, Part 1)
	// Row 2: "01234"      (Line 1, Part 2)
	// Row 3: "abcde"      (Current Line)

	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 100})
	
	// Add Line 0
	l0 := NewLogicalLine()
	for i := 0; i < 10; i++ { l0.Append(Cell{Rune: rune('0'+i)}) }
	history.Append(l0)

	// Add Line 1
	l1 := NewLogicalLine()
	for i := 0; i < 15; i++ { l1.Append(Cell{Rune: rune('0'+(i%10))}) }
	history.Append(l1)

	// Create DisplayBuffer
	db := NewDisplayBuffer(history, DisplayBufferConfig{Width: 10, Height: 10})
	
	// Set Current Line
	for i := 0; i < 5; i++ { db.currentLine.Append(Cell{Rune: rune('a'+i)}) }
	db.rebuildCurrentLinePhysical()
	db.scrollToLiveEdge() // Should put us at the bottom

	tests := []struct {
		name          string
		physX, physY  int
		wantLineIdx   int // -1 for current line, >=0 for history
		wantOffset    int
		wantFound     bool
	}{
		// Line 0 (Committed)
		{ "Line 0 Start", 0, 0, 0, 0, true },
		{ "Line 0 End",   9, 0, 0, 9, true },
		
		// Line 1 (Committed, Wrapped)
		{ "Line 1 Row 1 Start", 0, 1, 1, 0, true },
		{ "Line 1 Row 1 End",   9, 1, 1, 9, true },
		{ "Line 1 Row 2 Start", 0, 2, 1, 10, true },
		{ "Line 1 Row 2 End",   4, 2, 1, 14, true },
		// Past end of content on wrapped row
		{ "Line 1 Row 2 Void",  8, 2, 1, 18, true }, // Maps to offset 18 (virtual)

		// Current Line (Uncommitted)
		{ "Current Start", 0, 3, -1, 0, true },
		{ "Current End",   4, 3, -1, 4, true },
		{ "Current Void",  8, 3, -1, 8, true },

		// Out of bounds
		{ "Out of Bounds Y", 0, 10, 0, 0, false },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lineIdx, offset, found := db.GetLogicalPos(tt.physX, tt.physY)
			
			if found != tt.wantFound {
				t.Errorf("GetLogicalPos(%d, %d) found = %v, want %v", 
					tt.physX, tt.physY, found, tt.wantFound)
			}
			if !found { return }

			if lineIdx != tt.wantLineIdx {
				t.Errorf("GetLogicalPos(%d, %d) lineIdx = %d, want %d", 
					tt.physX, tt.physY, lineIdx, tt.wantLineIdx)
			}
			if offset != tt.wantOffset {
				t.Errorf("GetLogicalPos(%d, %d) offset = %d, want %d", 
					tt.physX, tt.physY, offset, tt.wantOffset)
			}
		})
	}
}

// TestDisplayBuffer_GetLogicalPos_Scrolled verifies mapping when scrolled up.
func TestDisplayBuffer_GetLogicalPos_Scrolled(t *testing.T) {
	// Setup similar to above but scrolled so Line 0 is at top
	// Only showing 2 lines height
	
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 100})
	l0 := NewLogicalLine(); l0.Append(Cell{Rune: 'A'})
	history.Append(l0)
	
	db := NewDisplayBuffer(history, DisplayBufferConfig{Width: 10, Height: 2})
	// Current line 'B'
	db.currentLine.Append(Cell{Rune: 'B'})
	db.rebuildCurrentLinePhysical()
	
	// Layout:
	// Row 0: "A" (History 0)
	// Row 1: "B" (Current)
	
	db.scrollToLiveEdge()

	// Verify live edge mapping
	idx, off, found := db.GetLogicalPos(0, 0) // "A"
	if !found || idx != 0 || off != 0 {
		t.Errorf("LiveEdge: Want 0,0. Got %d,%d", idx, off)
	}

	idx, off, found = db.GetLogicalPos(0, 1) // "B"
	if !found || idx != -1 || off != 0 {
		t.Errorf("LiveEdge: Want -1,0. Got %d,%d", idx, off)
	}
}

// TestDisplayBuffer_LogicalEditor verifies cursor tracking and editing
// on the logical level.
func TestDisplayBuffer_LogicalEditor(t *testing.T) {
	// Setup: 10-column terminal
	db := NewDisplayBuffer(nil, DisplayBufferConfig{Width: 10, Height: 5})
	
	// 1. Initial State: Cursor at 0,0 (Current Line)
	// We haven't implemented SetCursor yet, but let's assume we call it.
	// For this test, we'll verify the logical cursor state directly if possible,
	// or observing effects of Write().
	
	// To test "SetCursor", we need to expose the logical cursor state or use it.
	// Let's assume we add:
	// db.SetCursor(x, y)
	// db.Write(rune)
	
	db.SetCursor(0, 0)
	db.Write('A', DefaultFG, DefaultBG, 0, false)
	db.Write('B', DefaultFG, DefaultBG, 0, false)
	db.Write('C', DefaultFG, DefaultBG, 0, false)
	
	// Should have "ABC" in current line
	if db.currentLine.Len() != 3 {
		t.Errorf("After writing ABC, len=%d, want 3", db.currentLine.Len())
	}
	
	// 2. Wrap: Write enough to wrap
	// "ABC" + "DEFGHIJ" (7 more) -> "ABCDEFGHIJ" (10 chars, full row)
	for i := 0; i < 7; i++ { db.Write(rune('D'+i), DefaultFG, DefaultBG, 0, false) }
	
	// Now at offset 10 (start of next physical row)
	// Physical cursor should effectively be at 0,1 (handled by vterm, but logical cursor should be offset 10)
	
	db.Write('K', DefaultFG, DefaultBG, 0, false)
	// Current Line: "ABCDEFGHIJK"
	if db.currentLine.Len() != 11 {
		t.Errorf("After wrap write K, len=%d, want 11", db.currentLine.Len())
	}
	
	// 3. Move Cursor Back and Overwrite (Simulate user moving cursor left)
	// Move to 0,0 physically -> Logical Offset 0
	db.SetCursor(0, 0) 
	db.Write('X', DefaultFG, DefaultBG, 0, false)
	
	// Current Line: "XBCDEFGHIJK"
	if db.currentLine.Cells[0].Rune != 'X' {
		t.Errorf("Overwrite at 0 failed. Got %c", db.currentLine.Cells[0].Rune)
	}
	
	// 4. Move Cursor to Wrapped Row and Overwrite
	// Move to 0,1 physically -> Logical Offset 10 ('K')
	db.SetCursor(0, 1)
	db.Write('Y', DefaultFG, DefaultBG, 0, false)
	
	// Current Line: "XBCDEFGHIJY"
	if db.currentLine.Cells[10].Rune != 'Y' {
		t.Errorf("Overwrite at wrap 10 failed. Got %c", db.currentLine.Cells[10].Rune)
	}
}

// TestDisplayBuffer_LogicalErase verifies Erase operations (EL 0/1/2).
func TestDisplayBuffer_LogicalErase(t *testing.T) {
	db := NewDisplayBuffer(nil, DisplayBufferConfig{Width: 10, Height: 5})
	
	// Setup: "0123456789ABCDE" (15 chars)
	for i := 0; i < 15; i++ { 
		if i < 10 { db.Write(rune('0'+i), DefaultFG, DefaultBG, 0, false) } else { db.Write(rune('A'+(i-10)), DefaultFG, DefaultBG, 0, false) }
	}
	
	// State: "0123456789ABCDE"
	//        Row 0: "0123456789"
	//        Row 1: "ABCDE"
	
	// 1. EL 0 (Cursor to End) at 0,1 (Offset 10)
	// Should erase "ABCDE"
	db.SetCursor(0, 1)
	db.Erase(0) // EraseToEnd
	
	if db.currentLine.Len() != 10 {
		t.Errorf("After EL 0 at offset 10, len=%d, want 10", db.currentLine.Len())
	}
	
	// 2. EL 1 (Start to Cursor) at 5,0 (Offset 5)
	// Should erase "012345" -> "      6789" (Inclusive of cursor)
	db.SetCursor(5, 0)
	db.Erase(1) // EraseStart
	
	if db.currentLine.Cells[0].Rune != ' ' {
		t.Errorf("After EL 1, cell 0 is %c, want space", db.currentLine.Cells[0].Rune)
	}
	if db.currentLine.Cells[5].Rune != ' ' {
		t.Errorf("After EL 1, cell 5 is %c, want space", db.currentLine.Cells[5].Rune)
	}
	if db.currentLine.Cells[6].Rune != '6' {
		t.Errorf("After EL 1, cell 6 is %c, want '6'", db.currentLine.Cells[6].Rune)
	}
	
	// 3. EL 2 (Entire Line)
	db.Erase(2) // EraseLine
	if db.currentLine.Len() != 0 {
		t.Errorf("After EL 2, len=%d, want 0", db.currentLine.Len())
	}
}

// TestDisplayBuffer_ResizeCursorAdjustment verifies cursor X/Y update on resize.
func TestDisplayBuffer_ResizeCursorAdjustment(t *testing.T) {
	// Setup: Width 20.
	db := NewDisplayBuffer(nil, DisplayBufferConfig{Width: 20, Height: 5})
	
	// Current Line: 25 chars.
	// "0123456789012345678901234"
	// Width 20: 
	// Row 0: "01234567890123456789" (20 chars)
	// Row 1: "01234" (5 chars)
	
	for i := 0; i < 25; i++ {
		db.Write(rune('0'+(i%10)), DefaultFG, DefaultBG, 0, false)
	}
	// Cursor is at Offset 25 (after last char).
	// Physical: Row 1, Col 5.
	
	// db.SetCursor is NOT called by Resize. vterm calls Resize, then updates cursor.
	// We need a way to ask DB "Where is the cursor physically now?"
	
	// Resize to Width 10.
	// "0123456789" (Row 0)
	// "0123456789" (Row 1)
	// "01234"      (Row 2)
	// Cursor at Offset 25 -> Row 2, Col 5.
	
	db.Resize(10, 5)
	
	// We expect a method GetPhysicalCursorPos() to return 5, 2 (relative to viewport top? or relative to current line start?)
	// It should return viewport coordinates (x, y).
	
	// Since we haven't implemented it yet, this test will fail to compile if we try to call it.
	// I'll comment out the assertion for now or assume the method exists.
	// Let's assume: x, y, ok := db.GetPhysicalCursorPos()
	
	x, y, ok := db.GetPhysicalCursorPos()
	if !ok {
		t.Fatalf("GetPhysicalCursorPos returned not found")
	}
	// Expected: Offset 25. Width 10.
	// 0..9 (Row 0)
	// 10..19 (Row 1)
	// 20..24 (Row 2)
	// Offset 25 is start of virtual Row 2? 
	// No, 20..24 is 5 chars. 20,21,22,23,24.
	// Offset 25 is after '4'.
	// So Row 2, Col 5.
	// Total rows: 3. ViewportTop: 0 (3<5).
	// So Y=2.
	
	if x != 5 || y != 2 {
		t.Errorf("Want 5,2. Got %d,%d", x, y)
	}
}

// TestDisplayBuffer_ResizeReflow_RoundTrip verifies cursor position stability after shrink and expand.
func TestDisplayBuffer_ResizeReflow_RoundTrip(t *testing.T) {
	// Setup: Width 20, Height 5.
	// History: 4 lines (full screen with prompt).
	// Prompt: "Prompt> " (8 chars).
	history := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 100})
	db := NewDisplayBuffer(history, DisplayBufferConfig{Width: 20, Height: 5})
	
	// Add 4 lines of history
	for i := 0; i < 4; i++ {
		for j := 0; j < 10; j++ { db.Write(rune('0'+i), DefaultFG, DefaultBG, 0, false) }
		db.CommitCurrentLine()
	}
	
	// Write Prompt
	for _, r := range "Prompt> " {
		db.Write(r, DefaultFG, DefaultBG, 0, false)
	}
	
	db.scrollToLiveEdge()
	
	// Initial State:
	// Committed: 4 lines.
	// Current: 1 line.
	// Total: 5 lines.
	// Height: 5.
	// ViewportTop: 5 - 5 = 0.
	// Cursor (Offset 8) -> Row 4, Col 8.
	
	x, y, found := db.GetPhysicalCursorPos()
	if !found || x != 8 || y != 4 {
		t.Fatalf("Initial: Want 8,4. Got %d,%d", x, y)
	}
	
	// Shrink to 10.
	// History lines (10 chars): wrap to 1 line (exact fit).
	// Wait, if exact fit (10 chars, width 10), it takes 1 physical line.
	// Let's make them longer to force wrap.
	// Actually 10 chars at width 10 fits in 1 line.
	// Let's assume prompt "Prompt> " (8 chars) wraps? No.
	
	// Let's use history lines of 15 chars.
	// Reset and redo setup for clarity.
	db = NewDisplayBuffer(history, DisplayBufferConfig{Width: 20, Height: 5})
	history.Clear()
	for i := 0; i < 4; i++ {
		for j := 0; j < 15; j++ { db.Write(rune('0'+i), DefaultFG, DefaultBG, 0, false) }
		db.CommitCurrentLine()
	}
	for _, r := range "Prompt> " { db.Write(r, DefaultFG, DefaultBG, 0, false) }
	db.scrollToLiveEdge()
	
	// Resize to 10.
	// History (15 chars) -> 2 lines each.
	// 4 history lines -> 8 physical lines.
	// Current (8 chars) -> 1 physical line.
	// Total: 9 lines.
	// ViewportTop: 9 - 5 = 4.
	// Rows visible: 4, 5, 6, 7, 8.
	// Row 8 is Current Line.
	// Cursor should be at Row 4 (relative to viewport) -> y=4.
	
	db.Resize(10, 5)
	
	x, y, found = db.GetPhysicalCursorPos()
	if !found || y != 4 {
		t.Fatalf("Shrink: Want y=4. Got %d,%d", x, y)
	}
	
	// Expand back to 20.
	// History -> 1 line each.
	// Total: 5 lines.
	// ViewportTop: 0.
	// Cursor should be at Row 4.
	
	db.Resize(20, 5)
	
	x, y, found = db.GetPhysicalCursorPos()
	if !found || y != 4 {
		t.Errorf("Expand: Want y=4. Got %d,%d. ViewportTop=%d", x, y, db.viewportTop)
	}
}

// Helper to stringify cells for testing
func cellsToStringLogicalTest(cells []Cell) string {
	var sb strings.Builder
	for _, c := range cells {
		if c.Rune == 0 {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(c.Rune)
		}
	}
	return sb.String()
}
