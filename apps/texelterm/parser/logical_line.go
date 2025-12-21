package parser

// LogicalLine represents a single logical line of terminal content.
// A logical line is width-independent - it stores the full unwrapped content
// and can be reflowed to any terminal width on demand.
//
// This is the unit of storage for scrollback history. Physical (wrapped)
// lines are derived from logical lines based on current terminal width.
type LogicalLine struct {
	// Cells contains the full content of the line, unbounded by terminal width.
	// May be empty for blank lines.
	Cells []Cell
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
	return NewLogicalLineFromCells(l.Cells)
}

// PhysicalLine represents a single physical (wrapped) line for display.
// It references a portion of a LogicalLine.
type PhysicalLine struct {
	// Cells for this physical row (width-bounded)
	Cells []Cell
	// LogicalIndex is the index into ScrollbackHistory for this line's source.
	// -1 means this is part of the "current" uncommitted logical line.
	LogicalIndex int
	// Offset is the starting position within the logical line's cells.
	Offset int
}

// WrapToWidth converts a logical line into one or more physical lines
// at the given terminal width. Returns at least one line (empty logical
// lines produce one empty physical line).
func (l *LogicalLine) WrapToWidth(width int) []PhysicalLine {
	if width <= 0 {
		width = 80 // Fallback to reasonable default
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
