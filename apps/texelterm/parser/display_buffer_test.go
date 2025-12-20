package parser

import (
	"testing"
)

func TestDisplayBuffer_New(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  80,
		Height: 24,
	})

	if db.Width() != 80 {
		t.Errorf("expected width 80, got %d", db.Width())
	}
	if db.Height() != 24 {
		t.Errorf("expected height 24, got %d", db.Height())
	}
	if !db.AtLiveEdge() {
		t.Error("should start at live edge")
	}
}

func TestDisplayBuffer_SetCell(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  80,
		Height: 24,
	})

	db.SetCell(0, Cell{Rune: 'A', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(1, Cell{Rune: 'B', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(2, Cell{Rune: 'C', FG: DefaultFG, BG: DefaultBG})

	currentLine := db.CurrentLine()
	if currentLine.Len() != 3 {
		t.Errorf("expected current line length 3, got %d", currentLine.Len())
	}

	if cellsToString(currentLine.Cells) != "ABC" {
		t.Errorf("expected 'ABC', got '%s'", cellsToString(currentLine.Cells))
	}
}

func TestDisplayBuffer_CommitCurrentLine(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  80,
		Height: 24,
	})

	db.SetCell(0, Cell{Rune: 'H', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(1, Cell{Rune: 'i', FG: DefaultFG, BG: DefaultBG})

	db.CommitCurrentLine()

	// History should have the line
	if h.Len() != 1 {
		t.Errorf("expected 1 line in history, got %d", h.Len())
	}

	// Current line should be empty
	if db.CurrentLine().Len() != 0 {
		t.Errorf("current line should be empty after commit, got %d", db.CurrentLine().Len())
	}

	// Committed line should be in display buffer
	if db.TotalPhysicalLines() < 1 {
		t.Error("committed line should appear in display buffer")
	}
}

func TestDisplayBuffer_GetViewport(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  10,
		Height: 3,
	})

	// Add some content
	db.SetCell(0, Cell{Rune: 'A'})
	db.CommitCurrentLine()
	db.SetCell(0, Cell{Rune: 'B'})
	db.CommitCurrentLine()
	db.SetCell(0, Cell{Rune: 'C'})

	viewport := db.GetViewport()

	if len(viewport) != 3 {
		t.Fatalf("viewport should have 3 lines, got %d", len(viewport))
	}

	// Should see A, B, C (current line)
	if len(viewport[0].Cells) > 0 && viewport[0].Cells[0].Rune != 'A' {
		t.Errorf("expected 'A' in first viewport line")
	}
	if len(viewport[1].Cells) > 0 && viewport[1].Cells[0].Rune != 'B' {
		t.Errorf("expected 'B' in second viewport line")
	}
	if len(viewport[2].Cells) > 0 && viewport[2].Cells[0].Rune != 'C' {
		t.Errorf("expected 'C' in third viewport line (current)")
	}
}

func TestDisplayBuffer_GetViewportAsCells(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  5,
		Height: 2,
	})

	db.SetCell(0, Cell{Rune: 'H'})
	db.SetCell(1, Cell{Rune: 'i'})

	cells := db.GetViewportAsCells()

	if len(cells) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(cells))
	}
	if len(cells[0]) != 5 {
		t.Fatalf("expected 5 cols, got %d", len(cells[0]))
	}

	// First row should be empty (padding)
	// Second row should have "Hi" + spaces
	// Actually with height=2 and one current line, let's check
	// The current line should be visible

	found := false
	for _, row := range cells {
		if row[0].Rune == 'H' && row[1].Rune == 'i' {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'Hi' in viewport")
	}
}

func TestDisplayBuffer_Scroll(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:       80,
		Height:      3,
		MarginAbove: 10,
		MarginBelow: 10,
	})

	// Add more lines than viewport
	for i := 0; i < 10; i++ {
		db.SetCell(0, Cell{Rune: rune('0' + i)})
		db.CommitCurrentLine()
	}
	db.SetCell(0, Cell{Rune: 'X'}) // Current line

	if !db.AtLiveEdge() {
		t.Error("should be at live edge")
	}

	// Scroll up
	scrolled := db.ScrollUp(2)
	if scrolled != 2 {
		t.Errorf("expected to scroll 2, got %d", scrolled)
	}
	if db.AtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Scroll back down
	db.ScrollToBottom()
	if !db.AtLiveEdge() {
		t.Error("should be at live edge after ScrollToBottom")
	}
}

func TestDisplayBuffer_Resize(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  10,
		Height: 5,
	})

	// Add a long line that wraps
	for i := 0; i < 15; i++ {
		db.SetCell(i, Cell{Rune: rune('A' + i%26)})
	}
	db.CommitCurrentLine()

	// At width 10, this is 2 physical lines
	// At width 5, this is 3 physical lines

	db.Resize(5, 5)

	if db.Width() != 5 {
		t.Errorf("expected width 5, got %d", db.Width())
	}

	// Should still be at live edge
	if !db.AtLiveEdge() {
		t.Error("should stay at live edge after resize")
	}
}

func TestDisplayBuffer_WrapPreservesContent(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  10,
		Height: 5,
	})

	// Add "ABCDEFGHIJ" (10 chars)
	text := "ABCDEFGHIJ"
	for i, r := range text {
		db.SetCell(i, Cell{Rune: r, FG: DefaultFG, BG: DefaultBG})
	}
	db.CommitCurrentLine()

	// Resize to width 4 - should wrap to 3 lines: "ABCD", "EFGH", "IJ"
	db.Resize(4, 5)

	// Verify the history still has the logical line
	if h.Len() != 1 {
		t.Fatalf("expected 1 logical line in history, got %d", h.Len())
	}

	line := h.Get(0)
	if line.Len() != 10 {
		t.Errorf("logical line should still have 10 cells, got %d", line.Len())
	}

	// Verify wrapping works correctly
	wrapped := line.WrapToWidth(4)
	if len(wrapped) != 3 {
		t.Errorf("expected 3 physical lines at width 4, got %d", len(wrapped))
	}
}

func TestDisplayBuffer_LiveEdgeBehavior(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:       80,
		Height:      3,
		MarginAbove: 100,
		MarginBelow: 50,
	})

	// Add 10 lines
	for i := 0; i < 10; i++ {
		db.SetCell(0, Cell{Rune: rune('A' + i)})
		db.CommitCurrentLine()
	}

	// Scroll up (user is reading history)
	db.ScrollUp(5)
	if db.AtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Add new content - should go to live edge but viewport stays
	db.SetCell(0, Cell{Rune: 'Z'})
	db.CommitCurrentLine()

	// Viewport should still be scrolled up
	if db.AtLiveEdge() {
		t.Error("adding content should not yank viewport to live edge")
	}

	// New content is in history
	if h.Len() != 11 {
		t.Errorf("expected 11 lines in history, got %d", h.Len())
	}
}

func TestDisplayBuffer_CanScroll(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  80,
		Height: 5,
	})

	// Empty buffer - can't scroll anywhere
	if db.CanScrollUp() {
		t.Error("empty buffer should not be able to scroll up")
	}
	if db.CanScrollDown() {
		t.Error("empty buffer should not be able to scroll down")
	}

	// Add content
	for i := 0; i < 10; i++ {
		db.SetCell(0, Cell{Rune: 'X'})
		db.CommitCurrentLine()
	}

	if !db.CanScrollUp() {
		t.Error("should be able to scroll up with content above")
	}
	if db.CanScrollDown() {
		t.Error("should not be able to scroll down when at live edge")
	}

	// Scroll up
	db.ScrollUp(3)

	if !db.CanScrollUp() {
		t.Error("should still be able to scroll up")
	}
	if !db.CanScrollDown() {
		t.Error("should be able to scroll down after scrolling up")
	}
}

func TestDisplayBuffer_ResizePreservesScrollPosition(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:       20,
		Height:      5,
		MarginAbove: 50,
		MarginBelow: 20,
	})

	// Add 30 lines of content
	for i := 0; i < 30; i++ {
		for j := 0; j < 20; j++ {
			db.SetCell(j, Cell{Rune: rune('A' + (i % 26))})
		}
		db.CommitCurrentLine()
	}

	// Should be at live edge
	if !db.AtLiveEdge() {
		t.Error("should be at live edge initially")
	}

	// Scroll up to somewhere in the middle (say, 15 lines up)
	db.ScrollUp(15)
	if db.AtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Remember which logical line is at viewport top
	viewport := db.GetViewport()
	anchorLogicalIdx := viewport[0].LogicalIndex
	anchorContent := viewport[0].Cells
	if len(anchorContent) == 0 {
		t.Fatal("anchor line should have content")
	}
	anchorChar := anchorContent[0].Rune

	// Resize to a different width
	db.Resize(15, 5)

	// Should NOT jump to live edge
	if db.AtLiveEdge() {
		t.Error("resize should preserve scroll position, not jump to live edge")
	}

	// Check that the same logical line is at or near the viewport top
	newViewport := db.GetViewport()
	found := false
	for i := 0; i < 3; i++ { // Allow some tolerance due to wrap changes
		if i < len(newViewport) && newViewport[i].LogicalIndex == anchorLogicalIdx {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected logical line %d to still be visible near viewport top after resize", anchorLogicalIdx)
	}

	// The content should still be the same letter pattern
	foundChar := false
	for _, line := range newViewport {
		if len(line.Cells) > 0 && line.Cells[0].Rune == anchorChar {
			foundChar = true
			break
		}
	}
	if !foundChar {
		t.Errorf("expected to find content starting with '%c' in viewport after resize", anchorChar)
	}
}

func TestDisplayBuffer_ResizeAtLiveEdgeStaysAtLiveEdge(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  20,
		Height: 5,
	})

	// Add some content
	for i := 0; i < 10; i++ {
		db.SetCell(0, Cell{Rune: rune('0' + i)})
		db.CommitCurrentLine()
	}

	// Should be at live edge
	if !db.AtLiveEdge() {
		t.Error("should be at live edge")
	}

	// Resize
	db.Resize(15, 5)

	// Should still be at live edge
	if !db.AtLiveEdge() {
		t.Error("resize should keep us at live edge when we were at live edge")
	}
}

func TestDisplayBuffer_VerticalResizePreservesScrollPosition(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:       80,
		Height:      10,
		MarginAbove: 50,
		MarginBelow: 20,
	})

	// Add 50 lines of content
	for i := 0; i < 50; i++ {
		for j := 0; j < 10; j++ {
			db.SetCell(j, Cell{Rune: rune('A' + (i % 26))})
		}
		db.CommitCurrentLine()
	}

	// Scroll up to somewhere in the middle
	db.ScrollUp(20)
	if db.AtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Remember viewport top
	oldViewportTop := db.ViewportTopLine()

	// Vertical resize (grow) - should preserve scroll position
	db.Resize(80, 15)

	// Should still NOT be at live edge
	if db.AtLiveEdge() {
		t.Error("vertical resize should preserve scroll position, not jump to live edge")
	}

	// ViewportTop should be the same (same content at top)
	if db.ViewportTopLine() != oldViewportTop {
		t.Errorf("viewportTop changed from %d to %d after vertical grow", oldViewportTop, db.ViewportTopLine())
	}

	// Now resize back smaller
	db.Resize(80, 10)

	// Should still preserve position
	if db.ViewportTopLine() != oldViewportTop {
		t.Errorf("viewportTop changed from %d to %d after vertical shrink", oldViewportTop, db.ViewportTopLine())
	}
}

func TestDisplayBuffer_VerticalResizeAtLiveEdge(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:       80,
		Height:      10,
		MarginAbove: 50,
		MarginBelow: 20,
	})

	// Add 30 lines of content
	for i := 0; i < 30; i++ {
		for j := 0; j < 10; j++ {
			db.SetCell(j, Cell{Rune: rune('A' + (i % 26))})
		}
		db.CommitCurrentLine()
	}

	// Should be at live edge
	if !db.AtLiveEdge() {
		t.Error("should be at live edge")
	}

	// Remember which content is at bottom
	viewport := db.GetViewport()
	lastLineContent := viewport[9].Cells // Last visible line

	// Grow vertically
	db.Resize(80, 15)

	// Should still be at live edge
	if !db.AtLiveEdge() {
		t.Error("should stay at live edge after vertical grow")
	}

	// The same content should now be at the new bottom (line 14)
	newViewport := db.GetViewport()
	newLastLineContent := newViewport[14].Cells

	// Compare content
	if len(lastLineContent) > 0 && len(newLastLineContent) > 0 {
		if lastLineContent[0].Rune != newLastLineContent[0].Rune {
			t.Errorf("bottom content changed after resize: was '%c', now '%c'",
				lastLineContent[0].Rune, newLastLineContent[0].Rune)
		}
	}
}

func TestDisplayBuffer_InsertCell(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  80,
		Height: 24,
	})

	// Set up initial content "ABC"
	db.SetCell(0, Cell{Rune: 'A', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(1, Cell{Rune: 'B', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(2, Cell{Rune: 'C', FG: DefaultFG, BG: DefaultBG})

	// Insert 'X' at position 1 - should become "AXBC"
	db.InsertCell(1, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})

	currentLine := db.CurrentLine()
	if currentLine.Len() != 4 {
		t.Errorf("expected current line length 4, got %d", currentLine.Len())
	}

	if cellsToString(currentLine.Cells) != "AXBC" {
		t.Errorf("expected 'AXBC', got '%s'", cellsToString(currentLine.Cells))
	}
}

func TestDisplayBuffer_InsertCell_RebuildsPhysical(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  5, // Short width to test wrapping
		Height: 10,
	})

	// Set up "ABCD" on current line
	db.SetCell(0, Cell{Rune: 'A', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(1, Cell{Rune: 'B', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(2, Cell{Rune: 'C', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(3, Cell{Rune: 'D', FG: DefaultFG, BG: DefaultBG})

	// Insert 'X' - line becomes "AXBCD" which is 5 chars (exactly fits in width)
	db.InsertCell(1, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})

	currentLine := db.CurrentLine()
	if cellsToString(currentLine.Cells) != "AXBCD" {
		t.Errorf("expected 'AXBCD', got '%s'", cellsToString(currentLine.Cells))
	}

	// Insert another char - should cause wrap
	db.InsertCell(2, Cell{Rune: 'Y', FG: DefaultFG, BG: DefaultBG})

	currentLine = db.CurrentLine()
	if cellsToString(currentLine.Cells) != "AXYBCD" {
		t.Errorf("expected 'AXYBCD', got '%s'", cellsToString(currentLine.Cells))
	}

	// Verify physical lines are rebuilt (content should wrap to 2 lines)
	if db.TotalPhysicalLines() < 2 {
		t.Error("expected content to wrap to multiple physical lines")
	}
}

func TestDisplayBuffer_InsertCell_AtStart(t *testing.T) {
	h := NewScrollbackHistory(ScrollbackHistoryConfig{MaxMemoryLines: 1000})
	db := NewDisplayBuffer(h, DisplayBufferConfig{
		Width:  80,
		Height: 24,
	})

	db.SetCell(0, Cell{Rune: 'A', FG: DefaultFG, BG: DefaultBG})
	db.SetCell(1, Cell{Rune: 'B', FG: DefaultFG, BG: DefaultBG})

	db.InsertCell(0, Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG})

	currentLine := db.CurrentLine()
	if cellsToString(currentLine.Cells) != "XAB" {
		t.Errorf("expected 'XAB', got '%s'", cellsToString(currentLine.Cells))
	}
}
