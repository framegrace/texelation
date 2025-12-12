package parser

// DisplayBuffer manages the physical lines shown in the terminal viewport.
// It maintains a window of physical lines around the visible area, loading
// from ScrollbackHistory on demand.
//
// Architecture (inspired by SNES tile scrolling):
//
//	┌─────────────────────────────────────────┐
//	│           SCROLLBACK HISTORY            │
//	│   (Logical lines - width independent)   │
//	│   (Disk-backed, supports global index)  │
//	└─────────────────────────────────────────┘
//	                    │
//	                    │ Load on demand
//	                    ▼
//	┌─────────────────────────────────────────┐
//	│            DISPLAY BUFFER               │
//	│   ┌─────────────────────────────────┐   │
//	│   │     Off-screen ABOVE (margin)   │   │
//	│   ├─────────────────────────────────┤   │
//	│   │     VISIBLE VIEWPORT            │   │
//	│   ├─────────────────────────────────┤   │
//	│   │     Off-screen BELOW (margin)   │   │
//	│   └─────────────────────────────────┘   │
//	└─────────────────────────────────────────┘
type DisplayBuffer struct {
	// lines contains physical lines currently loaded in the buffer.
	// Index 0 is the topmost loaded line.
	lines []PhysicalLine

	// width is the current terminal width for wrapping.
	width int

	// height is the viewport height (visible rows).
	height int

	// viewportTop is the index into 'lines' where the visible viewport starts.
	viewportTop int

	// marginAbove is how many off-screen lines to keep above viewport.
	marginAbove int

	// marginBelow is how many off-screen lines to keep below viewport.
	marginBelow int

	// atLiveEdge indicates whether the viewport is following live output.
	// When true, new content auto-scrolls into view.
	// When false, the user has scrolled up and viewport stays put.
	atLiveEdge bool

	// history is a reference to the scrollback history for loading lines.
	history *ScrollbackHistory

	// globalTopIndex tracks which GLOBAL logical line corresponds to lines[0].
	// This is the anchor for the display buffer into history, using global indices
	// that work across disk and memory.
	globalTopIndex int64

	// currentLine is the live line being edited (not yet committed to history).
	// This is always conceptually at the bottom of the display.
	currentLine *LogicalLine

	// currentLinePhysical is the wrapped version of currentLine at current width.
	currentLinePhysical []PhysicalLine
}

// DisplayBufferConfig holds configuration for creating a DisplayBuffer.
type DisplayBufferConfig struct {
	Width       int
	Height      int
	MarginAbove int // Defaults to 200
	MarginBelow int // Defaults to 50
}

// NewDisplayBuffer creates a new display buffer attached to the given history.
func NewDisplayBuffer(history *ScrollbackHistory, config DisplayBufferConfig) *DisplayBuffer {
	if config.MarginAbove <= 0 {
		config.MarginAbove = 200
	}
	if config.MarginBelow <= 0 {
		config.MarginBelow = 50
	}
	if config.Width <= 0 {
		config.Width = 80
	}
	if config.Height <= 0 {
		config.Height = 24
	}

	db := &DisplayBuffer{
		lines:          make([]PhysicalLine, 0),
		width:          config.Width,
		height:         config.Height,
		viewportTop:    0,
		marginAbove:    config.MarginAbove,
		marginBelow:    config.MarginBelow,
		atLiveEdge:     true,
		history:        history,
		globalTopIndex: 0,
		currentLine:    NewLogicalLine(),
	}

	db.rebuildCurrentLinePhysical()

	// If history has content, load the bottom portion into lines
	if history != nil && history.TotalLen() > 0 {
		db.loadInitialHistory()
	}

	return db
}

// loadInitialHistory loads the bottom portion of history into lines.
// Called when creating a display buffer with existing history.
func (db *DisplayBuffer) loadInitialHistory() {
	if db.history == nil || db.history.TotalLen() == 0 {
		return
	}

	// Calculate how many lines we need to show the live edge
	linesNeeded := db.height + db.marginAbove
	totalLines := db.history.TotalLen()

	// Start from the end of history and work backwards
	db.globalTopIndex = totalLines
	physicalLoaded := 0

	// Walk backwards through logical lines until we have enough physical lines
	for db.globalTopIndex > 0 && physicalLoaded < linesNeeded {
		db.globalTopIndex--
		line := db.history.GetGlobal(db.globalTopIndex)
		if line != nil {
			physical := line.WrapToWidth(db.width)
			physicalLoaded += len(physical)
		}
	}

	// Now load those lines using global range
	db.lines = db.history.WrapGlobalToWidth(db.globalTopIndex, totalLines, db.width)

	// Position viewport at the live edge (bottom)
	db.scrollToLiveEdge()
}

// Width returns the current terminal width.
func (db *DisplayBuffer) Width() int {
	return db.width
}

// Height returns the viewport height.
func (db *DisplayBuffer) Height() int {
	return db.height
}

// AtLiveEdge returns whether the viewport is following live output.
func (db *DisplayBuffer) AtLiveEdge() bool {
	return db.atLiveEdge
}

// CurrentLine returns the current (uncommitted) logical line being edited.
func (db *DisplayBuffer) CurrentLine() *LogicalLine {
	return db.currentLine
}

// rebuildCurrentLinePhysical re-wraps the current line to the current width.
func (db *DisplayBuffer) rebuildCurrentLinePhysical() {
	db.currentLinePhysical = db.currentLine.WrapToWidth(db.width)
	// Mark as current line (not from history)
	for i := range db.currentLinePhysical {
		db.currentLinePhysical[i].LogicalIndex = -1
	}
}

// CommitCurrentLine moves the current line into history and starts a new one.
// This is called when a line feed (LF) occurs.
func (db *DisplayBuffer) CommitCurrentLine() {
	if db.history != nil {
		db.history.Append(db.currentLine.Clone())
	}

	// Add the committed line's physical representation to our buffer
	committed := db.currentLine.WrapToWidth(db.width)
	// Use global index (TotalLen - 1 is the just-appended line)
	globalIdx := int(db.history.TotalLen()) - 1
	for i := range committed {
		committed[i].LogicalIndex = globalIdx
	}
	db.lines = append(db.lines, committed...)

	// Start fresh current line
	db.currentLine = NewLogicalLine()
	db.rebuildCurrentLinePhysical()

	// If at live edge, scroll to keep viewport at bottom
	if db.atLiveEdge {
		db.scrollToLiveEdge()
	}

	// Trim excess lines above if needed
	db.trimAbove()
}

// scrollToLiveEdge adjusts viewportTop so the viewport shows the bottom content.
// When content exceeds viewport height, the latest content is at the bottom.
// When content is less than viewport height, content starts at the top (row 0).
func (db *DisplayBuffer) scrollToLiveEdge() {
	totalLines := db.contentLineCount()
	// viewportTop = totalLines - height
	// But never go negative - content starts at top when it doesn't fill the screen
	db.viewportTop = totalLines - db.height
	if db.viewportTop < 0 {
		db.viewportTop = 0
	}
	db.atLiveEdge = true
}

// contentLineCount returns the number of lines that have actual content.
// Empty current line doesn't count toward content for viewport positioning.
func (db *DisplayBuffer) contentLineCount() int {
	total := len(db.lines)
	// Only count current line if it has content
	if db.currentLine != nil && db.currentLine.Len() > 0 {
		total += len(db.currentLinePhysical)
	}
	return total
}

// trimAbove removes lines from the top that exceed marginAbove.
func (db *DisplayBuffer) trimAbove() {
	excessAbove := db.viewportTop - db.marginAbove
	if excessAbove > 0 {
		// Count how many logical lines we're removing
		// Walk through removed physical lines and find the new globalTopIndex
		newGlobalTop := db.globalTopIndex
		for i := 0; i < excessAbove && i < len(db.lines); i++ {
			// When we cross to a new logical line, advance the global index
			if i > 0 && db.lines[i].LogicalIndex != db.lines[i-1].LogicalIndex {
				newGlobalTop = int64(db.lines[i].LogicalIndex)
			}
		}
		if excessAbove < len(db.lines) {
			newGlobalTop = int64(db.lines[excessAbove].LogicalIndex)
		}

		// Remove excess lines from the top
		db.lines = db.lines[excessAbove:]
		db.viewportTop -= excessAbove
		db.globalTopIndex = newGlobalTop
	}
}

// loadAbove loads more lines from history above the current buffer.
// Uses global indices and triggers disk loading if needed.
func (db *DisplayBuffer) loadAbove(count int) {
	if db.history == nil || db.globalTopIndex <= 0 {
		return
	}

	// Calculate how many logical lines to load
	linesToLoad := int64(count)
	if linesToLoad > db.globalTopIndex {
		linesToLoad = db.globalTopIndex
	}
	startIdx := db.globalTopIndex - linesToLoad

	// Ensure ScrollbackHistory has these lines loaded from disk
	// LoadAbove returns how many were loaded, but we use global range which
	// will read from disk as needed via GetGlobalRange
	if db.history.CanLoadAbove() {
		// Try to load into memory first for better performance
		db.history.LoadAbove(int(linesToLoad))
	}

	// Wrap those logical lines to physical using global range
	physical := db.history.WrapGlobalToWidth(startIdx, db.globalTopIndex, db.width)

	// Prepend to our lines
	db.lines = append(physical, db.lines...)
	db.viewportTop += len(physical)
	db.globalTopIndex = startIdx
}

// ScrollUp scrolls the viewport up by the given number of lines.
// Returns how many lines were actually scrolled.
func (db *DisplayBuffer) ScrollUp(lines int) int {
	if lines <= 0 {
		return 0
	}

	// Check if we need to load more content from history
	if db.viewportTop < lines {
		needed := lines - db.viewportTop
		db.loadAbove(needed + db.marginAbove) // Load extra for margin
	}

	// Scroll up
	actual := min(lines, db.viewportTop)
	db.viewportTop -= actual

	if actual > 0 {
		db.atLiveEdge = false
	}

	return actual
}

// ScrollDown scrolls the viewport down by the given number of lines.
// Returns how many lines were actually scrolled.
func (db *DisplayBuffer) ScrollDown(lines int) int {
	if lines <= 0 {
		return 0
	}

	totalLines := db.contentLineCount()
	// Live edge position: totalLines - height (can be negative)
	liveEdgeViewportTop := totalLines - db.height

	actual := min(lines, liveEdgeViewportTop-db.viewportTop)
	if actual < 0 {
		actual = 0
	}
	db.viewportTop += actual

	// Check if we've reached the live edge
	if db.viewportTop >= liveEdgeViewportTop {
		db.atLiveEdge = true
	}

	return actual
}

// ScrollToBottom scrolls the viewport to the live edge.
func (db *DisplayBuffer) ScrollToBottom() {
	db.scrollToLiveEdge()
}

// GetViewport returns the physical lines currently visible in the viewport.
// The returned slice has exactly 'height' elements, padded with empty lines if needed.
func (db *DisplayBuffer) GetViewport() []PhysicalLine {
	result := make([]PhysicalLine, db.height)

	// Combine committed lines and current line physical representation
	allLines := append(db.lines, db.currentLinePhysical...)

	for i := 0; i < db.height; i++ {
		bufferIdx := db.viewportTop + i
		if bufferIdx >= 0 && bufferIdx < len(allLines) {
			result[i] = allLines[bufferIdx]
		} else {
			// Empty line (padding at the end)
			result[i] = PhysicalLine{
				Cells:        make([]Cell, 0),
				LogicalIndex: -1,
				Offset:       0,
			}
		}
	}

	return result
}

// GetViewportAsCells returns the viewport as a 2D Cell grid (height x width).
// This is the format expected by the terminal renderer.
func (db *DisplayBuffer) GetViewportAsCells() [][]Cell {
	viewport := db.GetViewport()
	result := make([][]Cell, db.height)

	for y, line := range viewport {
		row := make([]Cell, db.width)
		// Fill with spaces
		for x := range row {
			row[x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
		// Copy cells from physical line
		for x, cell := range line.Cells {
			if x < db.width {
				row[x] = cell
			}
		}
		result[y] = row
	}

	return result
}

// Resize changes the display buffer dimensions and reflows content.
// This is O(viewport + margins), not O(total history).
func (db *DisplayBuffer) Resize(newWidth, newHeight int) {
	if newWidth <= 0 || newHeight <= 0 {
		return
	}

	oldWidth := db.width
	oldHeight := db.height
	db.width = newWidth
	db.height = newHeight

	if oldWidth == newWidth {
		// Only height changed - adjust viewport position
		db.resizeHeight(oldHeight, newHeight)
		return
	}

	// Width changed - need to rewrap visible content
	db.rewrap()
}

// resizeHeight handles vertical-only resize, preserving scroll position.
func (db *DisplayBuffer) resizeHeight(oldHeight, newHeight int) {
	totalLines := db.contentLineCount()

	if db.atLiveEdge {
		// At live edge - keep content at bottom of viewport
		// viewportTop = totalLines - newHeight (can be negative if content < height)
		db.viewportTop = totalLines - newHeight

		// If we grew and need more lines from history, load them
		if newHeight > oldHeight && db.viewportTop < db.marginAbove {
			needed := db.marginAbove - db.viewportTop
			if needed > 0 {
				db.loadAbove(needed)
			}
			// Recalculate after loading
			totalLines = db.contentLineCount()
			db.viewportTop = totalLines - newHeight
		}
	} else {
		// Not at live edge - keep the same content at the top of viewport
		// The viewportTop index stays the same, but we may need to:
		// - Load more lines if we grew and don't have enough below
		// - Clamp viewportTop if we shrank and it's now past valid range

		maxViewportTop := totalLines - newHeight
		// Don't allow negative viewport when not at live edge (stay at top of content)
		if maxViewportTop < 0 {
			maxViewportTop = 0
		}

		if db.viewportTop > maxViewportTop {
			db.viewportTop = maxViewportTop
			// Check if we've reached the live edge
			if db.viewportTop >= totalLines-newHeight {
				db.atLiveEdge = true
			}
		}
	}
}

// rewrap rebuilds the display buffer at the current width.
func (db *DisplayBuffer) rewrap() {
	// Remember where we were anchored
	wasAtLiveEdge := db.atLiveEdge

	// Remember anchor for scroll position preservation (using global index)
	var anchorLogicalIdx int = -1
	var anchorWrapOffset int = 0

	if !wasAtLiveEdge && len(db.lines) > 0 && db.viewportTop < len(db.lines) {
		// Find which logical line is at the top of viewport
		anchorLine := db.lines[db.viewportTop]
		anchorLogicalIdx = anchorLine.LogicalIndex
		anchorWrapOffset = anchorLine.Offset
	}

	// Rebuild from history
	db.lines = make([]PhysicalLine, 0)
	totalLines := db.history.TotalLen()

	if db.history != nil && totalLines > 0 {
		// Load a window of history around what we need
		linesNeeded := db.height + db.marginAbove + db.marginBelow

		if anchorLogicalIdx >= 0 {
			// Load from before the anchor point all the way to the end of history.
			// This ensures the user can scroll down to the live edge after resize.
			// We load from (anchor - marginAbove) to totalLines.
			startIdx := int64(max(0, anchorLogicalIdx-db.marginAbove))

			db.globalTopIndex = startIdx
			// Load all the way to the end so user can scroll down to live edge
			db.lines = db.history.WrapGlobalToWidth(startIdx, totalLines, db.width)
		} else {
			// Start from the end of history and work backwards
			db.globalTopIndex = totalLines
			physicalLoaded := 0

			// Walk backwards through logical lines until we have enough physical lines
			for db.globalTopIndex > 0 && physicalLoaded < linesNeeded {
				db.globalTopIndex--
				line := db.history.GetGlobal(db.globalTopIndex)
				if line != nil {
					physical := line.WrapToWidth(db.width)
					physicalLoaded += len(physical)
				}
			}

			// Now load those lines using global range
			db.lines = db.history.WrapGlobalToWidth(db.globalTopIndex, totalLines, db.width)
		}
	}

	// Rebuild current line
	db.rebuildCurrentLinePhysical()

	// Position viewport
	if wasAtLiveEdge {
		db.scrollToLiveEdge()
	} else if anchorLogicalIdx >= 0 {
		// Try to maintain scroll position based on anchor logical line
		db.scrollToLogicalLine(anchorLogicalIdx, anchorWrapOffset)
	} else {
		db.scrollToLiveEdge()
	}
}

// scrollToLogicalLine positions the viewport so the given logical line
// (at the given wrap offset) is at the top of the viewport.
func (db *DisplayBuffer) scrollToLogicalLine(logicalIdx, wrapOffset int) {
	// Find the physical line that corresponds to this logical line
	for i, line := range db.lines {
		if line.LogicalIndex == logicalIdx {
			// Found the start of this logical line
			// Add wrap offset (clamped to available physical lines for this logical)
			targetIdx := i + wrapOffset

			// Count how many physical lines this logical line spans
			physicalCount := 0
			for j := i; j < len(db.lines) && db.lines[j].LogicalIndex == logicalIdx; j++ {
				physicalCount++
			}

			// Clamp wrap offset
			if wrapOffset >= physicalCount {
				targetIdx = i + physicalCount - 1
			}

			// Set viewport
			if targetIdx >= 0 && targetIdx < len(db.lines) {
				db.viewportTop = targetIdx
				db.atLiveEdge = false

				// Load more above if needed
				if db.viewportTop < db.marginAbove {
					db.loadAbove(db.marginAbove - db.viewportTop)
				}
				return
			}
		}
	}

	// Logical line not found in buffer - fall back to live edge
	db.scrollToLiveEdge()
}

// SetCell sets a cell in the current line at the given logical X position.
// Also updates the physical representation.
func (db *DisplayBuffer) SetCell(logicalX int, cell Cell) {
	db.currentLine.SetCell(logicalX, cell)
	db.rebuildCurrentLinePhysical()

	// Update the visible line in the buffer if at live edge
	if db.atLiveEdge {
		db.scrollToLiveEdge()
	}
}

// TotalPhysicalLines returns the total number of physical lines in the buffer
// (committed + current line).
func (db *DisplayBuffer) TotalPhysicalLines() int {
	return len(db.lines) + len(db.currentLinePhysical)
}

// ViewportTopLine returns the current viewport top position.
func (db *DisplayBuffer) ViewportTopLine() int {
	return db.viewportTop
}

// CanScrollUp returns true if there's content above the viewport to scroll to.
func (db *DisplayBuffer) CanScrollUp() bool {
	return db.viewportTop > 0 || db.globalTopIndex > 0
}

// CanScrollDown returns true if there's content below the viewport to scroll to.
func (db *DisplayBuffer) CanScrollDown() bool {
	return !db.atLiveEdge
}
