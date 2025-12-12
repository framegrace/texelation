package parser

// ScrollbackHistory stores logical lines for terminal scrollback.
// This is the authoritative source of history - width-independent and persisted.
//
// Lines are stored in chronological order: index 0 is the oldest line,
// index Len()-1 is the most recent committed line.
type ScrollbackHistory struct {
	// lines stores all committed logical lines
	lines []*LogicalLine

	// maxLines is the maximum number of logical lines to retain.
	// When exceeded, oldest lines are discarded.
	maxLines int

	// dirty tracks whether history has uncommitted changes for persistence
	dirty bool
}

// NewScrollbackHistory creates a new scrollback history with the given capacity.
func NewScrollbackHistory(maxLines int) *ScrollbackHistory {
	if maxLines <= 0 {
		maxLines = 10000 // Default to 10k lines
	}
	return &ScrollbackHistory{
		lines:    make([]*LogicalLine, 0, min(maxLines, 1000)), // Pre-allocate reasonably
		maxLines: maxLines,
		dirty:    false,
	}
}

// Len returns the number of logical lines in history.
func (h *ScrollbackHistory) Len() int {
	return len(h.lines)
}

// MaxLines returns the maximum capacity.
func (h *ScrollbackHistory) MaxLines() int {
	return h.maxLines
}

// Get returns the logical line at the given index.
// Returns nil if index is out of bounds.
func (h *ScrollbackHistory) Get(index int) *LogicalLine {
	if index < 0 || index >= len(h.lines) {
		return nil
	}
	return h.lines[index]
}

// Append adds a new logical line to the end of history.
// If history exceeds maxLines, the oldest line is discarded.
func (h *ScrollbackHistory) Append(line *LogicalLine) {
	h.lines = append(h.lines, line)
	h.dirty = true

	// Trim if over capacity
	if len(h.lines) > h.maxLines {
		// Remove oldest lines
		excess := len(h.lines) - h.maxLines
		// Help GC by clearing references
		for i := 0; i < excess; i++ {
			h.lines[i] = nil
		}
		h.lines = h.lines[excess:]
	}
}

// AppendCells is a convenience method that creates a logical line from cells and appends it.
func (h *ScrollbackHistory) AppendCells(cells []Cell) {
	h.Append(NewLogicalLineFromCells(cells))
}

// PrependLines adds lines to the beginning of history (older content loaded from disk).
// If this would exceed maxLines, the oldest of the NEW lines are discarded.
// This is used for on-demand loading when scrolling into older history.
func (h *ScrollbackHistory) PrependLines(lines []*LogicalLine) {
	if len(lines) == 0 {
		return
	}

	// Calculate how many we can actually prepend
	available := h.maxLines - len(h.lines)
	if available <= 0 {
		// Already at capacity, can't prepend
		return
	}

	// Take only what we can fit
	if len(lines) > available {
		// Take the most recent of the new lines (end of slice)
		lines = lines[len(lines)-available:]
	}

	// Prepend
	h.lines = append(lines, h.lines...)
	h.dirty = true
}

// Clear removes all lines from history.
func (h *ScrollbackHistory) Clear() {
	// Help GC
	for i := range h.lines {
		h.lines[i] = nil
	}
	h.lines = h.lines[:0]
	h.dirty = true
}

// IsDirty returns whether history has changes since last MarkClean.
func (h *ScrollbackHistory) IsDirty() bool {
	return h.dirty
}

// MarkClean clears the dirty flag (typically called after persisting).
func (h *ScrollbackHistory) MarkClean() {
	h.dirty = false
}

// GetRange returns a slice of logical lines from start (inclusive) to end (exclusive).
// Indices are clamped to valid bounds.
func (h *ScrollbackHistory) GetRange(start, end int) []*LogicalLine {
	if start < 0 {
		start = 0
	}
	if end > len(h.lines) {
		end = len(h.lines)
	}
	if start >= end {
		return nil
	}
	return h.lines[start:end]
}

// LastN returns the last n logical lines (or fewer if history is smaller).
func (h *ScrollbackHistory) LastN(n int) []*LogicalLine {
	if n <= 0 {
		return nil
	}
	start := len(h.lines) - n
	if start < 0 {
		start = 0
	}
	return h.lines[start:]
}

// All returns all logical lines. Use with caution for large histories.
func (h *ScrollbackHistory) All() []*LogicalLine {
	return h.lines
}

// WrapToWidth wraps a range of logical lines to physical lines at the given width.
// Returns physical lines with their LogicalIndex fields set correctly.
func (h *ScrollbackHistory) WrapToWidth(start, end, width int) []PhysicalLine {
	lines := h.GetRange(start, end)
	var result []PhysicalLine

	for i, line := range lines {
		logicalIdx := start + i
		physical := line.WrapToWidth(width)
		for j := range physical {
			physical[j].LogicalIndex = logicalIdx
		}
		result = append(result, physical...)
	}

	return result
}

// WrapAllToWidth wraps all logical lines to physical lines at the given width.
func (h *ScrollbackHistory) WrapAllToWidth(width int) []PhysicalLine {
	return h.WrapToWidth(0, len(h.lines), width)
}

// PhysicalLineCount returns how many physical lines the history would produce
// at the given width. Useful for scroll calculations without materializing lines.
func (h *ScrollbackHistory) PhysicalLineCount(width int) int {
	if width <= 0 {
		width = 80
	}
	count := 0
	for _, line := range h.lines {
		cells := len(line.Cells)
		if cells == 0 {
			count++ // Empty line still counts as one physical line
		} else {
			count += (cells + width - 1) / width // Ceiling division
		}
	}
	return count
}

// FindLogicalIndexForPhysicalLine returns the logical line index and offset
// for a given physical line number at the specified width.
// Returns (-1, 0) if physicalLine is out of bounds.
func (h *ScrollbackHistory) FindLogicalIndexForPhysicalLine(physicalLine, width int) (logicalIndex, offset int) {
	if width <= 0 {
		width = 80
	}
	if physicalLine < 0 {
		return -1, 0
	}

	currentPhysical := 0
	for i, line := range h.lines {
		cells := len(line.Cells)
		var linesForThis int
		if cells == 0 {
			linesForThis = 1
		} else {
			linesForThis = (cells + width - 1) / width
		}

		if currentPhysical+linesForThis > physicalLine {
			// The target physical line is within this logical line
			offsetWithinLogical := physicalLine - currentPhysical
			return i, offsetWithinLogical * width
		}
		currentPhysical += linesForThis
	}

	return -1, 0 // Past end of history
}
