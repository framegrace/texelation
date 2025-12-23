package parser

import (
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
