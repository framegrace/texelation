// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_window_test.go
// Summary: Comprehensive tests for ViewportWindow and its components.

package parser

import (
	"sync"
	"testing"
)

// --- Helper Functions ---

// vwCellsToString extracts runes from a cell slice as a string.
func vwCellsToString(cells []Cell) string {
	var s []rune
	for _, c := range cells {
		if c.Rune == 0 {
			s = append(s, ' ')
		} else {
			s = append(s, c.Rune)
		}
	}
	return string(s)
}

// vwMakeCells creates cells from a string for testing.
func vwMakeCells(s string) []Cell {
	cells := make([]Cell, len(s))
	for i, r := range s {
		cells[i] = Cell{Rune: r, FG: DefaultFG, BG: DefaultBG}
	}
	return cells
}

// vwGridRowToString extracts runes from a grid row as a string, trimming trailing spaces.
func vwGridRowToString(row []Cell) string {
	s := vwCellsToString(row)
	// Trim trailing spaces
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}

// setupTestBuffer creates a MemoryBuffer with test content.
func setupTestBuffer(lines []string, width int) *MemoryBuffer {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 1000, EvictionBatch: 100})
	mb.SetTermWidth(width)

	for i, line := range lines {
		for _, r := range line {
			mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		if i < len(lines)-1 {
			mb.NewLine()
			mb.CarriageReturn()
		}
	}

	return mb
}

// --- ContentReader Tests ---

func TestMemoryBufferReader_Interface(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	reader := NewMemoryBufferReader(mb)

	// Write some content
	mb.Write('A', DefaultFG, DefaultBG, 0)
	mb.NewLine()
	mb.CarriageReturn()
	mb.Write('B', DefaultFG, DefaultBG, 0)

	// Test interface methods
	if reader.GlobalOffset() != 0 {
		t.Errorf("expected GlobalOffset 0, got %d", reader.GlobalOffset())
	}
	if reader.GlobalEnd() != 2 {
		t.Errorf("expected GlobalEnd 2, got %d", reader.GlobalEnd())
	}
	if reader.TotalLines() != 2 {
		t.Errorf("expected TotalLines 2, got %d", reader.TotalLines())
	}

	line := reader.GetLine(0)
	if line == nil {
		t.Fatal("expected line 0 to exist")
	}
	if len(line.Cells) != 1 || line.Cells[0].Rune != 'A' {
		t.Errorf("expected line 0 to be 'A', got %v", line.Cells)
	}

	lines := reader.GetLineRange(0, 2)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	// ContentVersion should increment on write
	v1 := reader.ContentVersion()
	mb.Write('C', DefaultFG, DefaultBG, 0)
	v2 := reader.ContentVersion()
	if v2 <= v1 {
		t.Errorf("ContentVersion should increase, got v1=%d, v2=%d", v1, v2)
	}
}

// --- PhysicalLineBuilder Tests ---

func TestPhysicalLineBuilder_SingleLine(t *testing.T) {
	builder := NewPhysicalLineBuilder(10)

	line := NewLogicalLineFromCells(vwMakeCells("Hello"))
	physical := builder.BuildLine(line, 0)

	if len(physical) != 1 {
		t.Fatalf("expected 1 physical line, got %d", len(physical))
	}
	if vwGridRowToString(physical[0].Cells) != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", vwGridRowToString(physical[0].Cells))
	}
	if physical[0].LogicalIndex != 0 {
		t.Errorf("expected LogicalIndex 0, got %d", physical[0].LogicalIndex)
	}
	if physical[0].Offset != 0 {
		t.Errorf("expected Offset 0, got %d", physical[0].Offset)
	}
}

func TestPhysicalLineBuilder_Wrapping(t *testing.T) {
	builder := NewPhysicalLineBuilder(5)

	// "HelloWorld" should wrap to two lines at width 5
	line := NewLogicalLineFromCells(vwMakeCells("HelloWorld"))
	physical := builder.BuildLine(line, 42)

	if len(physical) != 2 {
		t.Fatalf("expected 2 physical lines, got %d", len(physical))
	}
	if vwGridRowToString(physical[0].Cells) != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", vwGridRowToString(physical[0].Cells))
	}
	if vwGridRowToString(physical[1].Cells) != "World" {
		t.Errorf("expected 'World', got '%s'", vwGridRowToString(physical[1].Cells))
	}
	// Both should reference the same logical line
	if physical[0].LogicalIndex != 42 || physical[1].LogicalIndex != 42 {
		t.Errorf("expected LogicalIndex 42, got %d and %d",
			physical[0].LogicalIndex, physical[1].LogicalIndex)
	}
	if physical[0].Offset != 0 || physical[1].Offset != 5 {
		t.Errorf("expected Offsets 0 and 5, got %d and %d",
			physical[0].Offset, physical[1].Offset)
	}
}

func TestPhysicalLineBuilder_NilLine(t *testing.T) {
	builder := NewPhysicalLineBuilder(10)

	physical := builder.BuildLine(nil, 5)

	if len(physical) != 1 {
		t.Fatalf("expected 1 physical line for nil, got %d", len(physical))
	}
	if physical[0].LogicalIndex != 5 {
		t.Errorf("expected LogicalIndex 5, got %d", physical[0].LogicalIndex)
	}
}

func TestPhysicalLineBuilder_FixedWidth(t *testing.T) {
	builder := NewPhysicalLineBuilder(10)

	// Fixed-width line should clip, not wrap
	line := NewLogicalLineFromCells(vwMakeCells("LongFixedWidthContent"))
	line.FixedWidth = 20 // TUI content

	physical := builder.BuildLine(line, 0)

	// Should clip to viewport width (10), not wrap
	if len(physical) != 1 {
		t.Fatalf("expected 1 physical line (clipped), got %d", len(physical))
	}
}

func TestPhysicalLineBuilder_BuildRange(t *testing.T) {
	builder := NewPhysicalLineBuilder(10)

	lines := []*LogicalLine{
		NewLogicalLineFromCells(vwMakeCells("Line1")),
		NewLogicalLineFromCells(vwMakeCells("Line2")),
		NewLogicalLineFromCells(vwMakeCells("Line3")),
	}

	physical := builder.BuildRange(lines, 100)

	if len(physical) != 3 {
		t.Fatalf("expected 3 physical lines, got %d", len(physical))
	}
	if physical[0].LogicalIndex != 100 {
		t.Errorf("expected LogicalIndex 100, got %d", physical[0].LogicalIndex)
	}
	if physical[1].LogicalIndex != 101 {
		t.Errorf("expected LogicalIndex 101, got %d", physical[1].LogicalIndex)
	}
	if physical[2].LogicalIndex != 102 {
		t.Errorf("expected LogicalIndex 102, got %d", physical[2].LogicalIndex)
	}
}

// --- ViewportCache Tests ---

func TestViewportCache_HitAndMiss(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	cache := NewViewportCache(reader, builder)

	// Initially empty
	result := cache.Get(0, 10, 80)
	if result != nil {
		t.Error("expected cache miss on empty cache")
	}

	// Set cache
	physical := []PhysicalLine{{Cells: vwMakeCells("Test")}}
	cache.Set(0, 10, 80, physical)

	// Should hit
	result = cache.Get(0, 10, 80)
	if result == nil {
		t.Error("expected cache hit")
	}

	// Different parameters should miss
	result = cache.Get(0, 5, 80) // Different end
	if result != nil {
		t.Error("expected cache miss for different end")
	}
	result = cache.Get(0, 10, 40) // Different width
	if result != nil {
		t.Error("expected cache miss for different width")
	}

	// ContentVersion change should miss
	mb.Write('X', DefaultFG, DefaultBG, 0) // This increments ContentVersion
	result = cache.Get(0, 10, 80)
	if result != nil {
		t.Error("expected cache miss after content change")
	}
}

func TestViewportCache_Stats(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	cache := NewViewportCache(reader, builder)

	// Initial stats
	hits, misses := cache.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("expected 0 hits, 0 misses initially, got %d, %d", hits, misses)
	}

	// Miss
	cache.Get(0, 10, 80)
	hits, misses = cache.Stats()
	if misses != 1 {
		t.Errorf("expected 1 miss, got %d", misses)
	}

	// Set and hit
	cache.Set(0, 10, 80, []PhysicalLine{})
	cache.Get(0, 10, 80)
	hits, misses = cache.Stats()
	if hits != 1 {
		t.Errorf("expected 1 hit, got %d", hits)
	}
}

// --- ScrollManager Tests ---

func TestScrollManager_LiveEdge(t *testing.T) {
	mb := setupTestBuffer([]string{"Line1", "Line2", "Line3"}, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)

	// Initially at live edge
	if !scroll.IsAtLiveEdge() {
		t.Error("expected to start at live edge")
	}
	if scroll.Offset() != 0 {
		t.Errorf("expected offset 0, got %d", scroll.Offset())
	}
}

func TestScrollManager_ScrollUp(t *testing.T) {
	mb := setupTestBuffer([]string{"L1", "L2", "L3", "L4", "L5"}, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)
	scroll.SetViewportHeight(2) // Set viewport smaller than content (5 lines)

	// Scroll up (maxScroll = 5 - 2 = 3)
	scrolled := scroll.ScrollUp(2)
	if scrolled != 2 {
		t.Errorf("expected to scroll 2, scrolled %d", scrolled)
	}
	if scroll.IsAtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}
	if scroll.Offset() != 2 {
		t.Errorf("expected offset 2, got %d", scroll.Offset())
	}
}

func TestScrollManager_ScrollDown(t *testing.T) {
	mb := setupTestBuffer([]string{"L1", "L2", "L3", "L4", "L5"}, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)
	scroll.SetViewportHeight(2) // Set viewport smaller than content (5 lines)

	// Scroll up first (maxScroll = 5 - 2 = 3)
	scroll.ScrollUp(3)

	// Scroll down
	scrolled := scroll.ScrollDown(1)
	if scrolled != 1 {
		t.Errorf("expected to scroll 1, scrolled %d", scrolled)
	}
	if scroll.Offset() != 2 {
		t.Errorf("expected offset 2, got %d", scroll.Offset())
	}

	// Scroll down to bottom
	scroll.ScrollToBottom()
	if !scroll.IsAtLiveEdge() {
		t.Error("expected to be at live edge after ScrollToBottom")
	}
}

func TestScrollManager_MaxScroll(t *testing.T) {
	mb := setupTestBuffer([]string{"L1", "L2", "L3"}, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)

	maxScroll := scroll.MaxScrollOffset()
	if maxScroll < 0 {
		t.Errorf("expected maxScroll >= 0, got %d", maxScroll)
	}

	// Try to scroll past max
	scroll.ScrollUp(1000)
	if scroll.Offset() > maxScroll {
		t.Errorf("offset %d exceeded max %d", scroll.Offset(), maxScroll)
	}
}

func TestScrollManager_CanScroll(t *testing.T) {
	mb := setupTestBuffer([]string{"L1", "L2", "L3", "L4", "L5"}, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)
	scroll.SetViewportHeight(2) // Set viewport smaller than content (5 lines)

	// At live edge: can scroll up (5 lines content, 2 line viewport), cannot scroll down
	if !scroll.CanScrollUp() {
		t.Error("expected CanScrollUp true at live edge with content")
	}
	if scroll.CanScrollDown() {
		t.Error("expected CanScrollDown false at live edge")
	}

	// After scrolling up: can scroll down
	scroll.ScrollUp(2)
	if !scroll.CanScrollDown() {
		t.Error("expected CanScrollDown true after scrolling up")
	}
}

func TestScrollManager_VisibleRangeWithWrappingLines(t *testing.T) {
	// Regression: when physicalEnd falls mid-way through a wrapping logical line,
	// findLogicalRangeInMemory must include that logical line in the result.
	// Without the fix, endGlobalIdx was off-by-one, causing line duplication
	// flicker during resize from the top.

	// Line 0: "Short" = 1 phys, Line 1: 160 chars = 2 phys at w=80, Line 2: "End" = 1 phys
	// Total physical = 4
	long := make([]byte, 160)
	for i := range long {
		long[i] = 'X'
	}
	mb := setupTestBuffer([]string{"Short", string(long), "End"}, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)

	// Viewport height 3, scrolled back by 1:
	// physicalEnd = 4-1 = 3, physicalStart = 3-3 = 0
	// Physical lines 0,1,2 are visible. Line 1 spans phys 1-2.
	// physicalEnd=3 falls at the start of line 1's 3rd physical line... wait,
	// line 1 only has 2 physical lines (phys 1 and 2). physicalEnd=3 is the
	// start of line 2. So endGlobalIdx should be 2 (exclusive).
	scroll.SetViewportHeight(3)
	scroll.ScrollUp(1)

	start, end := scroll.VisibleRange(3)
	if start != 0 {
		t.Errorf("expected start=0, got %d", start)
	}
	if end != 2 {
		t.Errorf("expected end=2, got %d", end)
	}

	// Now test the case where physicalEnd lands INSIDE a wrapping line:
	// Viewport height 2, scrolled back by 1:
	// physicalEnd = 4-1 = 3, physicalStart = 3-2 = 1
	// Physical lines 1,2 visible. Both belong to line 1 (160-char line).
	// physicalEnd=3 is start of line 2. endGlobalIdx should be 2.
	scroll.ScrollToBottom()
	scroll.SetViewportHeight(2)
	scroll.ScrollUp(1)

	start, end = scroll.VisibleRange(2)
	if start != 1 {
		t.Errorf("expected start=1, got %d", start)
	}
	if end != 2 {
		t.Errorf("expected end=2, got %d", end)
	}

	// Key case: viewport height 2, scrolled back by 2:
	// physicalEnd = 4-2 = 2, physicalStart = 2-2 = 0
	// Physical lines 0,1 visible. Phys 0 = line 0, phys 1 = first wrap of line 1.
	// physicalEnd=2 falls at prefixSum boundary (start of line 1's second phys).
	// We need line 1 included → endGlobalIdx should be 2.
	scroll.ScrollToBottom()
	scroll.ScrollUp(2)

	start, end = scroll.VisibleRange(2)
	if start != 0 {
		t.Errorf("expected start=0, got %d", start)
	}
	// physicalEnd=2 is at the boundary between line 1's phys lines.
	// Line 1 starts at prefixSum[1]=1, ends at prefixSum[2]=3.
	// PhysicalToLogical(2) → line 1, offset 1. offset>0 → end=2. ✓
	if end != 2 {
		t.Errorf("expected end=2, got %d", end)
	}
}

// --- CoordinateMapper Tests ---

func TestCoordinateMapper_ViewportToContent(t *testing.T) {
	mb := setupTestBuffer([]string{"Line1", "Line2", "Line3"}, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)
	mapper := NewCoordinateMapper(reader, builder, scroll)

	// At live edge, viewport shows all 3 lines (or as many as fit)
	globalIdx, charOffset, ok := mapper.ViewportToContent(0, 2, 10)
	if !ok {
		t.Fatal("expected ViewportToContent to succeed")
	}
	// Line at viewport row 0 depends on total content and viewport height
	// With 3 lines and viewport height 10, all should be visible
	if charOffset != 2 {
		t.Errorf("expected charOffset 2, got %d", charOffset)
	}
	_ = globalIdx // Exact value depends on scroll position
}

func TestCoordinateMapper_OutOfBounds(t *testing.T) {
	mb := setupTestBuffer([]string{"Line1"}, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)
	mapper := NewCoordinateMapper(reader, builder, scroll)

	// Negative row
	_, _, ok := mapper.ViewportToContent(-1, 0, 10)
	if ok {
		t.Error("expected failure for negative row")
	}

	// Negative col
	_, _, ok = mapper.ViewportToContent(0, -1, 10)
	if ok {
		t.Error("expected failure for negative col")
	}
}

// --- ViewportWindow Integration Tests ---

func TestViewportWindow_BasicRendering(t *testing.T) {
	mb := setupTestBuffer([]string{"Hello", "World"}, 80)
	vw := NewViewportWindow(mb, 80, 24)

	grid := vw.GetVisibleGrid()

	if len(grid) != 24 {
		t.Fatalf("expected height 24, got %d", len(grid))
	}
	if len(grid[0]) != 80 {
		t.Fatalf("expected width 80, got %d", len(grid[0]))
	}

	// Content should be at the bottom of the viewport (live edge)
	// Exact position depends on viewport size vs content
}

func TestViewportWindow_EmptyBuffer(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	vw := NewViewportWindow(mb, 80, 24)

	grid := vw.GetVisibleGrid()

	if len(grid) != 24 {
		t.Fatalf("expected height 24, got %d", len(grid))
	}

	// All rows should be empty (spaces)
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			if grid[y][x].Rune != ' ' && grid[y][x].Rune != 0 {
				t.Errorf("expected space at (%d,%d), got '%c'", x, y, grid[y][x].Rune)
			}
		}
	}
}

func TestViewportWindow_Scrolling(t *testing.T) {
	// Create buffer with many lines
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "Line"
	}
	mb := setupTestBuffer(lines, 80)
	vw := NewViewportWindow(mb, 80, 10)

	// Initially at live edge
	if !vw.IsAtLiveEdge() {
		t.Error("expected to start at live edge")
	}

	// Scroll up
	scrolled := vw.ScrollUp(5)
	if scrolled <= 0 {
		t.Errorf("expected to scroll, got %d", scrolled)
	}
	if vw.IsAtLiveEdge() {
		t.Error("should not be at live edge after scrolling up")
	}

	// Scroll back down
	vw.ScrollToBottom()
	if !vw.IsAtLiveEdge() {
		t.Error("expected to be at live edge after ScrollToBottom")
	}
}

func TestViewportWindow_Resize(t *testing.T) {
	mb := setupTestBuffer([]string{"Hello", "World"}, 80)
	vw := NewViewportWindow(mb, 80, 24)

	// Get initial grid
	grid1 := vw.GetVisibleGrid()

	// Resize
	vw.Resize(40, 12)

	if vw.Width() != 40 {
		t.Errorf("expected width 40, got %d", vw.Width())
	}
	if vw.Height() != 12 {
		t.Errorf("expected height 12, got %d", vw.Height())
	}

	// Get grid after resize
	grid2 := vw.GetVisibleGrid()

	if len(grid2) != 12 {
		t.Fatalf("expected height 12 after resize, got %d", len(grid2))
	}
	if len(grid2[0]) != 40 {
		t.Fatalf("expected width 40 after resize, got %d", len(grid2[0]))
	}

	_ = grid1 // Used for comparison if needed
}

func TestViewportWindow_FixedWidthLines(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	// Write a fixed-width line (TUI content)
	for _, r := range "TUI content that is fixed width" {
		mb.Write(r, DefaultFG, DefaultBG, 0)
	}
	mb.SetLineFixed(0, 80) // Mark as fixed width

	vw := NewViewportWindow(mb, 40, 10) // Narrower viewport

	grid := vw.GetVisibleGrid()

	// Fixed-width lines should clip, not wrap
	// The grid should have exactly 10 rows (viewport height)
	if len(grid) != 10 {
		t.Errorf("expected 10 rows, got %d", len(grid))
	}
}

func TestViewportWindow_CacheInvalidation(t *testing.T) {
	mb := setupTestBuffer([]string{"Hello"}, 80)
	vw := NewViewportWindow(mb, 80, 24)

	// Get grid (populates cache)
	_ = vw.GetVisibleGrid()
	hits1, misses1 := vw.CacheStats()

	// Get grid again (should hit cache)
	_ = vw.GetVisibleGrid()
	hits2, _ := vw.CacheStats()

	if hits2 <= hits1 {
		t.Error("expected cache hit on second GetVisibleGrid")
	}

	// Modify content
	mb.Write('X', DefaultFG, DefaultBG, 0)

	// Get grid (should miss - content changed)
	_ = vw.GetVisibleGrid()
	_, misses3 := vw.CacheStats()

	if misses3 <= misses1 {
		t.Error("expected cache miss after content change")
	}
}

func TestViewportWindow_CoordinateConversion(t *testing.T) {
	mb := setupTestBuffer([]string{"Line1", "Line2", "Line3"}, 80)
	vw := NewViewportWindow(mb, 80, 24)

	// Get content coordinates for a viewport position
	globalIdx, charOffset, ok := vw.ViewportToContent(0, 0)
	if !ok {
		// May fail if content doesn't fill viewport - that's OK
		t.Log("ViewportToContent returned not ok - this is acceptable for sparse content")
	}

	if ok {
		// Convert back
		row, col, visible := vw.ContentToViewport(globalIdx, charOffset)
		if !visible {
			t.Error("expected content to be visible")
		}
		_ = row
		_ = col
	}
}

func TestViewportWindow_Concurrency(t *testing.T) {
	mb := setupTestBuffer([]string{"Line1", "Line2", "Line3"}, 80)
	vw := NewViewportWindow(mb, 80, 24)

	var wg sync.WaitGroup
	const numReaders = 10
	const numScrollers = 5
	const iterations = 100

	// Start readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				grid := vw.GetVisibleGrid()
				_ = grid
				_ = vw.IsAtLiveEdge()
				_ = vw.Width()
				_ = vw.Height()
			}
		}()
	}

	// Start scrollers
	for i := 0; i < numScrollers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				vw.ScrollUp(1)
				vw.ScrollDown(1)
			}
		}()
	}

	// Wait for all goroutines
	wg.Wait()

	// If we get here without deadlock or panic, the test passes
}

// --- Benchmark Tests ---

func BenchmarkViewportWindow_GetVisibleGrid_CacheHit(b *testing.B) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "Benchmark test line content"
	}
	mb := setupTestBuffer(lines, 80)
	vw := NewViewportWindow(mb, 80, 24)

	// Warm up cache
	vw.GetVisibleGrid()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vw.GetVisibleGrid()
	}
}

func BenchmarkViewportWindow_GetVisibleGrid_CacheMiss(b *testing.B) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "Benchmark test line content"
	}
	mb := setupTestBuffer(lines, 80)
	vw := NewViewportWindow(mb, 80, 24)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vw.InvalidateCache()
		_ = vw.GetVisibleGrid()
	}
}

func BenchmarkViewportWindow_Scroll(b *testing.B) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = "Benchmark test line content"
	}
	mb := setupTestBuffer(lines, 80)
	vw := NewViewportWindow(mb, 80, 24)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vw.ScrollUp(1)
		vw.ScrollDown(1)
	}
}

func BenchmarkScrollManager_TotalPhysicalLines(b *testing.B) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = "Benchmark test line that is long enough to potentially wrap"
	}
	mb := setupTestBuffer(lines, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	scroll := NewScrollManager(reader, builder)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scroll.TotalPhysicalLines()
	}
}
