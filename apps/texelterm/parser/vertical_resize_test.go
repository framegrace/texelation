package parser

import (
	"fmt"
	"strings"
	"testing"
)

// parseVTermString is a helper that writes a string to VTerm via the parser.
func parseVTermString(p *Parser, s string) {
	for _, r := range s {
		p.Parse(r)
	}
}

// cellsToStr converts cells to string (local version to avoid redeclaration)
func cellsToStr(cells []Cell) string {
	var sb strings.Builder
	for _, c := range cells {
		if c.Rune != 0 {
			sb.WriteRune(c.Rune)
		} else {
			sb.WriteRune(' ')
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

// TestVerticalResize_Basic tests basic vertical resize operations
func TestVerticalResize_Basic(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write some content to create scrollback
	for i := 0; i < 30; i++ {
		parseVTermString(p, fmt.Sprintf("Line %02d: This is test content\r\n", i))
	}

	mb := v.memBufState.memBuf

	t.Logf("Before resize (80x24):")
	t.Logf("  GlobalOffset=%d, GlobalEnd=%d, TotalLines=%d", mb.GlobalOffset(), mb.GlobalEnd(), mb.TotalLines())
	t.Logf("  liveEdgeBase=%d", v.memBufState.liveEdgeBase)
	t.Logf("  Cursor: (%d, %d)", v.cursorX, v.cursorY)
	t.Logf("  Height=%d", v.height)

	// Get visible grid before resize
	gridBefore := v.Grid()
	t.Logf("  Last visible line: %q", cellsToStr(gridBefore[v.height-1]))

	// Resize smaller (24 -> 12)
	v.Resize(80, 12)

	t.Logf("\nAfter resize to 80x12:")
	t.Logf("  GlobalOffset=%d, GlobalEnd=%d, TotalLines=%d", mb.GlobalOffset(), mb.GlobalEnd(), mb.TotalLines())
	t.Logf("  liveEdgeBase=%d", v.memBufState.liveEdgeBase)
	t.Logf("  Cursor: (%d, %d)", v.cursorX, v.cursorY)
	t.Logf("  Height=%d", v.height)

	gridAfter := v.Grid()
	t.Logf("  Grid size: %d rows", len(gridAfter))
	if len(gridAfter) > 0 {
		t.Logf("  Last visible line: %q", cellsToStr(gridAfter[len(gridAfter)-1]))
	}

	// Check cursor is still valid
	if v.cursorY >= v.height {
		t.Errorf("Cursor Y (%d) is >= height (%d)", v.cursorY, v.height)
	}
	if v.cursorY < 0 {
		t.Errorf("Cursor Y (%d) is negative", v.cursorY)
	}
}

// TestVerticalResize_ShrinkFromBottom simulates shrinking from the bottom
func TestVerticalResize_ShrinkFromBottom(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Fill the screen and create some scrollback
	for i := 0; i < 40; i++ {
		parseVTermString(p, fmt.Sprintf("Line %02d\r\n", i))
	}

	mb := v.memBufState.memBuf
	initialLiveEdgeBase := v.memBufState.liveEdgeBase

	t.Logf("Before shrink (80x24):")
	t.Logf("  liveEdgeBase=%d, cursor=(%d,%d)", initialLiveEdgeBase, v.cursorX, v.cursorY)
	t.Logf("  GlobalOffset=%d, GlobalEnd=%d", mb.GlobalOffset(), mb.GlobalEnd())

	// Get last lines content before resize
	gridBefore := v.Grid()
	for y := v.height - 5; y < v.height; y++ {
		t.Logf("  Row[%d]: %q", y, cellsToStr(gridBefore[y]))
	}

	// Shrink by 12 rows (24 -> 12)
	v.Resize(80, 12)

	t.Logf("\nAfter shrink to 80x12:")
	t.Logf("  liveEdgeBase=%d, cursor=(%d,%d)", v.memBufState.liveEdgeBase, v.cursorX, v.cursorY)
	t.Logf("  GlobalOffset=%d, GlobalEnd=%d", mb.GlobalOffset(), mb.GlobalEnd())

	gridAfter := v.Grid()
	t.Logf("  Grid has %d rows", len(gridAfter))
	for y := 0; y < len(gridAfter); y++ {
		t.Logf("  Row[%d]: %q", y, cellsToStr(gridAfter[y]))
	}

	// The liveEdgeBase should have adjusted so the same content is visible
	// If we were at the bottom before, we should still see the same last lines
	if v.cursorY >= v.height {
		t.Errorf("Cursor Y (%d) out of bounds (height=%d)", v.cursorY, v.height)
	}

	// Write something after resize to see if cursor works
	parseVTermString(p, "After resize content")

	gridAfterWrite := v.Grid()
	t.Logf("\nAfter writing content:")
	for y := 0; y < len(gridAfterWrite); y++ {
		content := cellsToStr(gridAfterWrite[y])
		if strings.TrimSpace(content) != "" {
			t.Logf("  Row[%d]: %q", y, content)
		}
	}
}

// TestVerticalResize_GrowFromBottom simulates growing from the bottom
func TestVerticalResize_GrowFromBottom(t *testing.T) {
	v := NewVTerm(80, 12, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Fill the small screen and create scrollback
	for i := 0; i < 30; i++ {
		parseVTermString(p, fmt.Sprintf("Line %02d\r\n", i))
	}

	mb := v.memBufState.memBuf

	t.Logf("Before grow (80x12):")
	t.Logf("  liveEdgeBase=%d, cursor=(%d,%d)", v.memBufState.liveEdgeBase, v.cursorX, v.cursorY)
	t.Logf("  GlobalOffset=%d, GlobalEnd=%d", mb.GlobalOffset(), mb.GlobalEnd())

	gridBefore := v.Grid()
	for y := 0; y < len(gridBefore); y++ {
		content := cellsToStr(gridBefore[y])
		if strings.TrimSpace(content) != "" {
			t.Logf("  Row[%d]: %q", y, content)
		}
	}

	// Grow by 12 rows (12 -> 24)
	v.Resize(80, 24)

	t.Logf("\nAfter grow to 80x24:")
	t.Logf("  liveEdgeBase=%d, cursor=(%d,%d)", v.memBufState.liveEdgeBase, v.cursorX, v.cursorY)
	t.Logf("  GlobalOffset=%d, GlobalEnd=%d", mb.GlobalOffset(), mb.GlobalEnd())

	gridAfter := v.Grid()
	t.Logf("  Grid has %d rows", len(gridAfter))
	for y := 0; y < len(gridAfter); y++ {
		content := cellsToStr(gridAfter[y])
		if strings.TrimSpace(content) != "" || y == v.cursorY {
			t.Logf("  Row[%d]: %q %s", y, content, func() string {
				if y == v.cursorY {
					return "<-- cursor"
				}
				return ""
			}())
		}
	}

	// After grow, cursor should still be valid
	if v.cursorY >= v.height {
		t.Errorf("Cursor Y (%d) out of bounds (height=%d)", v.cursorY, v.height)
	}

	// The last content line from before should still be in the same visual position
	// (or the grid should show scrollback filling the new space)
}

// TestVerticalResize_WithScrollback tests resize while scrolled back
func TestVerticalResize_WithScrollback(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Create a lot of scrollback
	for i := 0; i < 100; i++ {
		parseVTermString(p, fmt.Sprintf("Line %03d: Scrollback content\r\n", i))
	}

	mb := v.memBufState.memBuf

	t.Logf("Initial state (80x24):")
	t.Logf("  liveEdgeBase=%d, cursor=(%d,%d)", v.memBufState.liveEdgeBase, v.cursorX, v.cursorY)
	t.Logf("  GlobalOffset=%d, GlobalEnd=%d, TotalLines=%d", mb.GlobalOffset(), mb.GlobalEnd(), mb.TotalLines())
	t.Logf("  AtLiveEdge=%v", v.memoryBufferAtLiveEdge())

	// Scroll back 50 lines
	v.memoryBufferScroll(-50)
	t.Logf("\nAfter scrolling back 50 lines:")
	t.Logf("  AtLiveEdge=%v", v.memoryBufferAtLiveEdge())

	gridBeforeResize := v.Grid()
	t.Logf("  First visible line: %q", cellsToStr(gridBeforeResize[0]))

	// Resize while scrolled back
	v.Resize(80, 12)

	t.Logf("\nAfter resize to 80x12 (while scrolled back):")
	t.Logf("  liveEdgeBase=%d, cursor=(%d,%d)", v.memBufState.liveEdgeBase, v.cursorX, v.cursorY)
	t.Logf("  GlobalOffset=%d, GlobalEnd=%d", mb.GlobalOffset(), mb.GlobalEnd())
	t.Logf("  AtLiveEdge=%v", v.memoryBufferAtLiveEdge())

	gridAfterResize := v.Grid()
	t.Logf("  Grid has %d rows", len(gridAfterResize))
	t.Logf("  First visible line: %q", cellsToStr(gridAfterResize[0]))

	// Cursor should still be valid
	if v.cursorY >= v.height {
		t.Errorf("Cursor Y (%d) out of bounds (height=%d)", v.cursorY, v.height)
	}
}

// TestVerticalResize_CursorAtBottom tests resize when cursor is at the bottom
func TestVerticalResize_CursorAtBottom(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Fill exactly to the bottom
	for i := 0; i < 24; i++ {
		if i < 23 {
			parseVTermString(p, fmt.Sprintf("Line %02d\r\n", i))
		} else {
			// Last line without newline - cursor stays on this line
			parseVTermString(p, fmt.Sprintf("Line %02d", i))
		}
	}

	t.Logf("Before resize (cursor at bottom):")
	t.Logf("  cursor=(%d,%d), height=%d", v.cursorX, v.cursorY, v.height)

	// Shrink - cursor should adjust
	v.Resize(80, 12)

	t.Logf("\nAfter shrink to 80x12:")
	t.Logf("  cursor=(%d,%d), height=%d", v.cursorX, v.cursorY, v.height)
	t.Logf("  liveEdgeBase=%d", v.memBufState.liveEdgeBase)

	if v.cursorY >= v.height {
		t.Errorf("Cursor Y (%d) >= height (%d) after shrink", v.cursorY, v.height)
	}

	// Try to write at cursor position
	parseVTermString(p, "X")
	grid := v.Grid()
	t.Logf("  After write, cursor=(%d,%d)", v.cursorX, v.cursorY)

	// Find where 'X' ended up
	found := false
	for y := 0; y < len(grid); y++ {
		line := cellsToStr(grid[y])
		if strings.Contains(line, "X") {
			t.Logf("  'X' found at row %d: %q", y, line)
			found = true
		}
	}
	if !found {
		t.Error("Could not find 'X' in grid after write")
		for y := 0; y < len(grid); y++ {
			t.Logf("  Row[%d]: %q", y, cellsToStr(grid[y]))
		}
	}
}

// TestVerticalResize_RapidChanges tests rapid vertical size changes
func TestVerticalResize_RapidChanges(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write some content
	for i := 0; i < 50; i++ {
		parseVTermString(p, fmt.Sprintf("Line %02d\r\n", i))
	}

	sizes := []int{24, 12, 30, 8, 50, 20, 10, 24}

	for _, newHeight := range sizes {
		v.Resize(80, newHeight)

		// Check cursor validity
		if v.cursorY >= v.height {
			t.Errorf("After resize to %d: cursor Y (%d) >= height (%d)", newHeight, v.cursorY, v.height)
		}
		if v.cursorY < 0 {
			t.Errorf("After resize to %d: cursor Y (%d) is negative", newHeight, v.cursorY)
		}

		// Try to write
		parseVTermString(p, fmt.Sprintf("H=%d ", newHeight))

		// Get grid and verify it's valid
		grid := v.Grid()
		if len(grid) != newHeight {
			t.Errorf("After resize to %d: grid has %d rows", newHeight, len(grid))
		}
	}

	t.Logf("Final state:")
	t.Logf("  cursor=(%d,%d), height=%d", v.cursorX, v.cursorY, v.height)
	t.Logf("  liveEdgeBase=%d", v.memBufState.liveEdgeBase)

	grid := v.Grid()
	for y := 0; y < len(grid); y++ {
		content := cellsToStr(grid[y])
		if strings.TrimSpace(content) != "" {
			t.Logf("  Row[%d]: %q", y, content)
		}
	}
}

// TestVerticalResize_LiveEdgeBaseAdjustment specifically tests liveEdgeBase handling
func TestVerticalResize_LiveEdgeBaseAdjustment(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Create content that fills exactly the viewport
	for i := 0; i < 24; i++ {
		parseVTermString(p, fmt.Sprintf("Line %02d\r\n", i))
	}

	mb := v.memBufState.memBuf

	t.Logf("Initial (24 lines, 24 height):")
	t.Logf("  GlobalEnd=%d, liveEdgeBase=%d", mb.GlobalEnd(), v.memBufState.liveEdgeBase)
	t.Logf("  Expected: liveEdgeBase = GlobalEnd - height = %d - 24 = %d",
		mb.GlobalEnd(), mb.GlobalEnd()-24)

	// When we shrink, liveEdgeBase should adjust so the cursor line stays visible
	// If cursor was at bottom (row 23) with liveEdgeBase=0, GlobalEnd=24
	// After shrink to 12, cursor should move to row 11 (or less)
	// And liveEdgeBase should adjust to keep content aligned

	cursorGlobalBefore := v.memBufState.liveEdgeBase + int64(v.cursorY)
	t.Logf("  Cursor global line (before resize): %d", cursorGlobalBefore)

	v.Resize(80, 12)

	cursorGlobalAfter := v.memBufState.liveEdgeBase + int64(v.cursorY)
	t.Logf("\nAfter shrink to 12:")
	t.Logf("  GlobalEnd=%d, liveEdgeBase=%d", mb.GlobalEnd(), v.memBufState.liveEdgeBase)
	t.Logf("  Cursor: (%d,%d), global line: %d", v.cursorX, v.cursorY, cursorGlobalAfter)

	// The cursor should refer to the same global line content
	// (unless it was clamped)
	t.Logf("  liveEdgeBase + cursorY should allow cursor to be valid")

	if v.cursorY >= v.height {
		t.Errorf("Cursor out of bounds: %d >= %d", v.cursorY, v.height)
	}

	// Now grow back
	v.Resize(80, 24)

	cursorGlobalAfterGrow := v.memBufState.liveEdgeBase + int64(v.cursorY)
	t.Logf("\nAfter grow back to 24:")
	t.Logf("  GlobalEnd=%d, liveEdgeBase=%d", mb.GlobalEnd(), v.memBufState.liveEdgeBase)
	t.Logf("  Cursor: (%d,%d), global line: %d", v.cursorX, v.cursorY, cursorGlobalAfterGrow)
}
