package parser

// LogicalLine represents a single logical line of terminal content.
// A logical line is width-independent - it stores the full unwrapped content
// and can be reflowed to any terminal width on demand.
//
// This is the unit of storage for scrollback history. Physical (wrapped)
// lines are derived from logical lines based on current terminal width.
//
// When FixedWidth > 0, the line represents TUI content that should NOT be
// reflowed on resize. Instead, it is clipped (if viewport narrower) or
// padded (if viewport wider).
type LogicalLine struct {
	// Cells contains the full content of the line, unbounded by terminal width.
	// May be empty for blank lines.
	Cells []Cell

	// FixedWidth indicates this line should not reflow on resize.
	// When > 0, the line is clipped or padded to viewport width instead.
	// Set by CommitViewportAsFixedWidth() for TUI app content.
	// 0 means normal reflow behavior.
	FixedWidth int
}

// NewLogicalLine creates a new empty logical line.
func NewLogicalLine() *LogicalLine {
	return &LogicalLine{
		Cells: make([]Cell, 0),
	}
}

// NewLogicalLineFromCells creates a logical line from existing cells.
func NewLogicalLineFromCells(cells []Cell) *LogicalLine {
	// Make a copy to avoid aliasing
	copied := make([]Cell, len(cells))
	copy(copied, cells)
	return &LogicalLine{Cells: copied}
}

// Len returns the number of cells in the logical line.
func (l *LogicalLine) Len() int {
	return len(l.Cells)
}

// SetCell sets a cell at the given position, extending the line if necessary.
// This is used for in-place edits (e.g., after carriage return).
func (l *LogicalLine) SetCell(x int, cell Cell) {
	// Extend line if needed
	for len(l.Cells) <= x {
		l.Cells = append(l.Cells, Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG})
	}
	l.Cells[x] = cell
}

// Append adds a cell to the end of the logical line.
func (l *LogicalLine) Append(cell Cell) {
	l.Cells = append(l.Cells, cell)
}

// InsertCell inserts a cell at the given position, shifting existing cells right.
// Used for insert mode (IRM). Extends the line if needed.
func (l *LogicalLine) InsertCell(x int, cell Cell) {
	// If inserting at or beyond the end, just extend to that position
	if x >= len(l.Cells) {
		// Extend with spaces if needed
		for len(l.Cells) < x {
			l.Cells = append(l.Cells, Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG})
		}
		// Append the new cell at the end
		l.Cells = append(l.Cells, cell)
		return
	}
	// Make room for new cell
	l.Cells = append(l.Cells, Cell{})
	// Shift cells right
	copy(l.Cells[x+1:], l.Cells[x:])
	// Place the new cell
	l.Cells[x] = cell
}

// Truncate removes all cells from position x onwards.
// Used for operations like "erase to end of line" on the current logical line.
func (l *LogicalLine) Truncate(x int) {
	if x < len(l.Cells) {
		l.Cells = l.Cells[:x]
	}
}

// Clear removes all cells from the line.
func (l *LogicalLine) Clear() {
	l.Cells = l.Cells[:0]
}

// Clone creates a deep copy of the logical line.
func (l *LogicalLine) Clone() *LogicalLine {
	clone := NewLogicalLineFromCells(l.Cells)
	clone.FixedWidth = l.FixedWidth
	return clone
}

// PhysicalLine represents a single physical (wrapped) line for display.
// It references a portion of a LogicalLine.
type PhysicalLine struct {
	// Cells for this physical row (width-bounded)
	Cells []Cell
	// LogicalIndex is the index into the history for this line's source.
	// -1 means this is part of the "current" uncommitted logical line.
	LogicalIndex int
	// Offset is the starting position within the logical line's cells.
	Offset int
}

// ClipOrPadToWidth returns a single physical line for fixed-width content.
// Unlike WrapToWidth, this never breaks across multiple lines.
// - If content is longer than width: clips to width
// - If content is shorter than width: pads with spaces
// Used for TUI app content that should not reflow on resize.
func (l *LogicalLine) ClipOrPadToWidth(width int) PhysicalLine {
	if width <= 0 {
		width = DefaultWidth
	}

	cells := make([]Cell, width)
	for i := 0; i < width; i++ {
		if i < len(l.Cells) {
			cells[i] = l.Cells[i]
		} else {
			cells[i] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	return PhysicalLine{
		Cells:        cells,
		LogicalIndex: -1, // Caller should set this
		Offset:       0,
	}
}

// WrapToWidth converts a logical line into one or more physical lines
// at the given terminal width. Returns at least one line (empty logical
// lines produce one empty physical line).
//
// If FixedWidth > 0, uses ClipOrPadToWidth instead (no reflow).
func (l *LogicalLine) WrapToWidth(width int) []PhysicalLine {
	if width <= 0 {
		width = DefaultWidth
	}

	// Fixed-width lines don't reflow - they clip or pad
	if l.FixedWidth > 0 {
		return []PhysicalLine{l.ClipOrPadToWidth(width)}
	}

	if len(l.Cells) == 0 {
		// Empty logical line -> one empty physical line
		return []PhysicalLine{{
			Cells:        make([]Cell, 0),
			LogicalIndex: -1, // Caller should set this
			Offset:       0,
		}}
	}

	var result []PhysicalLine
	for offset := 0; offset < len(l.Cells); offset += width {
		end := offset + width
		if end > len(l.Cells) {
			end = len(l.Cells)
		}

		// Copy the slice to avoid aliasing
		cells := make([]Cell, end-offset)
		copy(cells, l.Cells[offset:end])

		result = append(result, PhysicalLine{
			Cells:        cells,
			LogicalIndex: -1, // Caller should set this
			Offset:       offset,
		})
	}

	return result
}

// TrimTrailingSpaces removes trailing space cells from the line.
// Useful for storage efficiency - trailing spaces don't need to be persisted.
func (l *LogicalLine) TrimTrailingSpaces() {
	for len(l.Cells) > 0 {
		last := l.Cells[len(l.Cells)-1]
		if last.Rune == ' ' && last.FG == DefaultFG && last.BG == DefaultBG && last.Attr == 0 {
			l.Cells = l.Cells[:len(l.Cells)-1]
		} else {
			break
		}
	}
}
