// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_window.go
// Summary: ViewportWindow provides a terminal-sized view into MemoryBuffer.
//
// Architecture:
//
//	ViewportWindow is a pure view layer that reads from MemoryBuffer and
//	produces a terminal-sized grid for rendering. It handles:
//
//	  - Scrolling (user navigation into history)
//	  - Wrapping (logical lines to physical lines)
//	  - Fixed-width lines (TUI content that shouldn't reflow)
//	  - Caching (avoid rebuilding on every render)
//	  - Coordinate conversion (for mouse clicks, selections)
//
//	This is Phase 3 of the terminal architecture:
//	  - Phase 1: MemoryBuffer (storage)
//	  - Phase 2: AdaptivePersistence (disk writes)
//	  - Phase 3: ViewportWindow (view layer) <- YOU ARE HERE
//
//	ViewportWindow coordinates several focused components:
//	  - ContentReader: abstracts read access to MemoryBuffer
//	  - PhysicalLineBuilder: converts LogicalLine to PhysicalLine
//	  - ViewportCache: caches rendered output
//	  - ScrollManager: manages scroll state
//	  - CoordinateMapper: converts between coordinate systems
//
// Thread-safety:
//
//	All public methods are thread-safe via RWMutex.
//	Read operations (GetVisibleGrid, coordinate queries) use read locks.
//	Write operations (Scroll, Resize) use write locks.

package parser

import "sync"

// ViewportWindow provides a terminal-sized view into MemoryBuffer.
// Thread-safe for concurrent access.
type ViewportWindow struct {
	// Dimensions
	width  int
	height int

	// Components (dependency injection enables testability)
	reader      ContentReader
	builder     *PhysicalLineBuilder
	cache       *ViewportCache
	scroll      *ScrollManager
	coordinates *CoordinateMapper

	mu sync.RWMutex
}

// NewViewportWindow creates a new viewport reading from the given MemoryBuffer.
func NewViewportWindow(memBuf *MemoryBuffer, width, height int) *ViewportWindow {
	if width <= 0 {
		width = DefaultWidth
	}
	if height <= 0 {
		height = DefaultHeight
	}

	// Build component graph
	reader := NewMemoryBufferReader(memBuf)
	builder := NewPhysicalLineBuilder(width)
	cache := NewViewportCache(reader, builder)
	scroll := NewScrollManager(reader, builder)
	scroll.SetViewportHeight(height) // Set initial viewport height for scroll limits
	coordinates := NewCoordinateMapper(reader, builder, scroll)

	return &ViewportWindow{
		width:       width,
		height:      height,
		reader:      reader,
		builder:     builder,
		cache:       cache,
		scroll:      scroll,
		coordinates: coordinates,
	}
}

// SetPageStore enables disk fallback for evicted lines.
// When a PageStore is set, the viewport can display lines that have been
// evicted from the MemoryBuffer by reading them from disk.
// This enables history navigation beyond the in-memory window.
func (vw *ViewportWindow) SetPageStore(pageStore *PageStore) {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	if mbr, ok := vw.reader.(*MemoryBufferReader); ok {
		mbr.SetPageStore(pageStore)
		// Invalidate cache since readable range may have changed
		vw.cache.Invalidate()
	}
}

// Reader returns the content reader used by this viewport.
// Use this to access content with PageStore fallback support.
func (vw *ViewportWindow) Reader() ContentReader {
	return vw.reader
}

// --- Core Rendering ---

// GetVisibleGrid returns the current viewport as a 2D cell grid.
// This is the primary rendering method. The grid is [height][width] cells.
// Caches the result; subsequent calls return cached grid if content unchanged.
func (vw *ViewportWindow) GetVisibleGrid() [][]Cell {
	// Full lock required: VisibleRange calls ensureIndexValid which mutates
	// the PhysicalLineIndex (Build, HandleAppend, ensurePrefixSum).
	vw.mu.Lock()
	defer vw.mu.Unlock()

	startGlobal, endGlobal := vw.scroll.VisibleRange(vw.height)

	// Try cache first
	if physical := vw.cache.Get(startGlobal, endGlobal, vw.width); physical != nil {
		return vw.physicalLinesToGrid(physical)
	}

	// Cache miss - rebuild
	lines := vw.reader.GetLineRange(startGlobal, endGlobal)
	physical := vw.builder.BuildRange(lines, startGlobal)
	vw.cache.Set(startGlobal, endGlobal, vw.width, physical)

	return vw.physicalLinesToGrid(physical)
}

// physicalLinesToGrid converts PhysicalLine slices to a 2D Cell grid.
// Must be called with lock held.
func (vw *ViewportWindow) physicalLinesToGrid(physical []PhysicalLine) [][]Cell {
	grid := make([][]Cell, vw.height)

	// Calculate which physical lines are visible in the viewport
	totalPhysical := len(physical)
	physicalEnd := totalPhysical
	physicalStart := max(physicalEnd-vw.height, 0)

	for y := 0; y < vw.height; y++ {
		grid[y] = make([]Cell, vw.width)

		physIdx := physicalStart + y
		if physIdx >= 0 && physIdx < totalPhysical {
			// Copy cells from physical line
			pl := physical[physIdx]
			for x := 0; x < vw.width && x < len(pl.Cells); x++ {
				grid[y][x] = pl.Cells[x]
			}
			// Pad with spaces
			for x := len(pl.Cells); x < vw.width; x++ {
				grid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
			}
		} else {
			// Empty row
			for x := 0; x < vw.width; x++ {
				grid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
			}
		}
	}

	return grid
}

// --- Scrolling ---

// ScrollUp scrolls backward (toward older content) by n lines.
// Returns the actual number of lines scrolled.
func (vw *ViewportWindow) ScrollUp(lines int) int {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	return vw.scroll.ScrollUp(lines)
}

// ScrollDown scrolls forward (toward newer content) by n lines.
// Returns the actual number of lines scrolled.
func (vw *ViewportWindow) ScrollDown(lines int) int {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	return vw.scroll.ScrollDown(lines)
}

// ScrollToBottom jumps to the live edge (most recent content).
func (vw *ViewportWindow) ScrollToBottom() {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	vw.scroll.ScrollToBottom()
}

// ScrollToTop jumps to the oldest content.
func (vw *ViewportWindow) ScrollToTop() {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	vw.scroll.ScrollToTop()
}

// IsAtLiveEdge returns true if showing the most recent content.
// When at live edge, new content automatically appears.
func (vw *ViewportWindow) IsAtLiveEdge() bool {
	vw.mu.RLock()
	defer vw.mu.RUnlock()

	return vw.scroll.IsAtLiveEdge()
}

// CanScrollUp returns true if there's older content to scroll to.
func (vw *ViewportWindow) CanScrollUp() bool {
	// Full lock: CanScrollUp calls TotalPhysicalLines which mutates the index.
	vw.mu.Lock()
	defer vw.mu.Unlock()

	return vw.scroll.CanScrollUp()
}

// CanScrollDown returns true if scrolled back (not at live edge).
func (vw *ViewportWindow) CanScrollDown() bool {
	vw.mu.RLock()
	defer vw.mu.RUnlock()

	return vw.scroll.CanScrollDown()
}

// ScrollOffset returns the current scroll offset (physical lines from bottom).
func (vw *ViewportWindow) ScrollOffset() int64 {
	vw.mu.RLock()
	defer vw.mu.RUnlock()

	return vw.scroll.Offset()
}

// ScrollToOffset sets an absolute scroll position.
// Clamps to valid range [0, MaxScrollOffset()].
func (vw *ViewportWindow) ScrollToOffset(offset int64) {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	vw.scroll.ScrollToOffset(offset)
}

// TotalPhysicalLines returns the total number of physical lines at current width.
func (vw *ViewportWindow) TotalPhysicalLines() int64 {
	// Full lock: TotalPhysicalLines calls ensureIndexValid which mutates the index.
	vw.mu.Lock()
	defer vw.mu.Unlock()

	return vw.scroll.TotalPhysicalLines()
}

// --- Resize ---

// Resize changes the viewport dimensions.
// Invalidates cache and triggers reflow on next GetVisibleGrid().
func (vw *ViewportWindow) Resize(newWidth, newHeight int) {
	if newWidth <= 0 {
		newWidth = DefaultWidth
	}
	if newHeight <= 0 {
		newHeight = DefaultHeight
	}

	vw.mu.Lock()
	defer vw.mu.Unlock()

	if newWidth == vw.width && newHeight == vw.height {
		return
	}

	vw.width = newWidth
	vw.height = newHeight
	vw.builder.SetWidth(newWidth)
	vw.scroll.SetViewportHeight(newHeight)
	vw.scroll.InvalidateIndex()
	vw.cache.Invalidate()
}

// Width returns the current viewport width.
func (vw *ViewportWindow) Width() int {
	vw.mu.RLock()
	defer vw.mu.RUnlock()

	return vw.width
}

// Height returns the current viewport height.
func (vw *ViewportWindow) Height() int {
	vw.mu.RLock()
	defer vw.mu.RUnlock()

	return vw.height
}

// Builder returns the physical line builder for line wrapping calculations.
// Used by VTerm.ScrollToGlobalLine to compute scroll offsets.
func (vw *ViewportWindow) Builder() *PhysicalLineBuilder {
	return vw.builder
}

// --- Coordinate Conversion ---

// ViewportToContent converts viewport coordinates (row, col) to content coordinates.
// Returns (globalLineIdx, charOffset, ok).
// ok is false if coordinates are out of bounds.
func (vw *ViewportWindow) ViewportToContent(row, col int) (globalLineIdx int64, charOffset int, ok bool) {
	// Full lock: ViewportToContent calls VisibleRange which mutates the index.
	vw.mu.Lock()
	defer vw.mu.Unlock()

	return vw.coordinates.ViewportToContent(row, col, vw.height)
}

// ContentToViewport converts content coordinates to viewport coordinates.
// Returns (row, col, visible).
// visible is false if the content is not currently on screen.
func (vw *ViewportWindow) ContentToViewport(globalLineIdx int64, charOffset int) (row, col int, visible bool) {
	// Full lock: ContentToViewport calls VisibleRange which mutates the index.
	vw.mu.Lock()
	defer vw.mu.Unlock()

	return vw.coordinates.ContentToViewport(globalLineIdx, charOffset, vw.height)
}

// --- Cache Management ---

// InvalidateCache clears the cached grid.
// Next GetVisibleGrid() call will rebuild from MemoryBuffer.
func (vw *ViewportWindow) InvalidateCache() {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	vw.cache.Invalidate()
}

// CacheStats returns cache hit/miss statistics for debugging.
func (vw *ViewportWindow) CacheStats() (hits, misses int64) {
	vw.mu.RLock()
	defer vw.mu.RUnlock()

	return vw.cache.Stats()
}
