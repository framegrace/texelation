# Scrollback Reflow Plan

## Status: Planning Phase
**Created**: 2025-12-11
**Branch**: `feature/fix-scrollback-reflow`

## Problem Statement

The current scrollback implementation stores **physical lines** (wrapped to terminal width) with a `Wrapped` flag. This causes issues:

1. **Reflow disabled for loaded history**: After loading persistence, reflow is skipped because `Wrapped` flags may be inconsistent across width changes
2. **Long lines break**: Lines wrapped at a different width appear broken when viewed at current width
3. **Full reflow is O(N)**: Reflowing entire history on resize is slow for large histories

## Solution: Display Buffer Architecture

Inspired by tile-based scrolling in classic games (SNES, etc.), we separate storage from display:

```
┌─────────────────────────────────────────┐
│           SCROLLBACK HISTORY            │
│   (Logical lines - width independent)   │
│   []LogicalLine - append only, large    │
└─────────────────────────────────────────┘
                    │
                    │ Load/Unload lines on demand
                    ▼
┌─────────────────────────────────────────┐
│            DISPLAY BUFFER               │
│   (Physical lines - current width)      │
│                                         │
│   ┌─────────────────────────────────┐   │
│   │     Off-screen ABOVE            │   │  ← Unwrapped lines ready to scroll in
│   │     (variable size)             │   │
│   ├─────────────────────────────────┤   │
│   │     VISIBLE VIEWPORT            │   │  ← What user sees (height rows)
│   │     (height rows)               │   │
│   ├─────────────────────────────────┤   │
│   │     Off-screen BELOW            │   │  ← Unwrapped lines for scroll down
│   │     (variable size)             │   │
│   └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

### Key Properties

| Property | Scrollback History | Display Buffer |
|----------|-------------------|----------------|
| Width-dependent | No | Yes |
| Resize cost | None | O(viewport) rebuild |
| Size | All history (100k+ lines) | Viewport + margins |
| Persistence | Yes (disk) | No (ephemeral) |
| Scroll cost | O(1) lookup | O(1) index shift |
| Line format | Logical (unwrapped) | Physical (wrapped) |

## Data Structures

### LogicalLine

A single logical line - content as written by the program, unbounded length.

```go
// LogicalLine represents a complete line of terminal output.
// Content is stored unwrapped - the full line regardless of terminal width.
type LogicalLine struct {
    Cells []Cell  // Full unwrapped content, any length

    // Metadata for efficient operations
    ID        uint64    // Unique ID for tracking (optional)
    Timestamp time.Time // When line was completed (optional, for persistence)
}

// PhysicalLineCount returns how many display rows this line needs at given width.
func (l *LogicalLine) PhysicalLineCount(width int) int {
    if len(l.Cells) == 0 {
        return 1  // Empty line still takes one row
    }
    return (len(l.Cells) + width - 1) / width
}

// WrapToWidth returns this logical line wrapped to physical lines.
func (l *LogicalLine) WrapToWidth(width int) [][]Cell {
    if len(l.Cells) == 0 {
        return [][]Cell{{}}  // One empty row
    }

    count := l.PhysicalLineCount(width)
    result := make([][]Cell, count)

    for i := 0; i < count; i++ {
        start := i * width
        end := min(start+width, len(l.Cells))
        result[i] = make([]Cell, end-start)
        copy(result[i], l.Cells[start:end])
    }

    return result
}
```

### ScrollbackHistory

The authoritative storage of all terminal output.

```go
// ScrollbackHistory stores terminal output as logical (unwrapped) lines.
// This is the source of truth - width independent and persistent.
type ScrollbackHistory struct {
    lines   []LogicalLine
    maxSize int  // Maximum lines to retain

    // Current line being built (not yet committed)
    current LogicalLine

    mu sync.RWMutex
}

// Append commits the current line and starts a new one.
func (h *ScrollbackHistory) Append() {
    h.mu.Lock()
    defer h.mu.Unlock()

    h.lines = append(h.lines, h.current)
    h.current = LogicalLine{}

    // Trim if over max size
    if len(h.lines) > h.maxSize {
        h.lines = h.lines[len(h.lines)-h.maxSize:]
    }
}

// AppendCell adds a cell to the current line being built.
func (h *ScrollbackHistory) AppendCell(c Cell) {
    h.mu.Lock()
    defer h.mu.Unlock()
    h.current.Cells = append(h.current.Cells, c)
}

// SetCell updates a cell in the current line (for overwrites).
func (h *ScrollbackHistory) SetCell(index int, c Cell) {
    h.mu.Lock()
    defer h.mu.Unlock()

    // Extend if needed
    for len(h.current.Cells) <= index {
        h.current.Cells = append(h.current.Cells, Cell{Rune: ' '})
    }
    h.current.Cells[index] = c
}

// Length returns total committed lines.
func (h *ScrollbackHistory) Length() int {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return len(h.lines)
}

// Line returns a logical line by index.
func (h *ScrollbackHistory) Line(index int) *LogicalLine {
    h.mu.RLock()
    defer h.mu.RUnlock()

    if index < 0 || index >= len(h.lines) {
        return nil
    }
    return &h.lines[index]
}

// CurrentLine returns the line being built (for display buffer sync).
func (h *ScrollbackHistory) CurrentLine() *LogicalLine {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return &h.current
}
```

### DisplayBuffer

The viewport with off-screen margins for smooth scrolling.

```go
// DisplayBuffer manages the visible viewport plus off-screen margins.
// Physical lines are wrapped to current terminal width.
type DisplayBuffer struct {
    // Physical rows at current width
    rows  [][]Cell
    width int

    // Viewport window within rows slice
    viewportTop    int  // Index into rows where visible viewport starts
    viewportHeight int  // Number of visible rows

    // Mapping to history
    // "rows[anchorRow] is the first physical line of history.lines[anchorLogical]"
    anchorLogical int  // Which logical line in history
    anchorRow     int  // Which row in display buffer

    // Reference to history for loading more content
    history *ScrollbackHistory
}

// NewDisplayBuffer creates a display buffer for the given dimensions.
func NewDisplayBuffer(width, height int, history *ScrollbackHistory) *DisplayBuffer {
    db := &DisplayBuffer{
        rows:           make([][]Cell, height),
        width:          width,
        viewportTop:    0,
        viewportHeight: height,
        history:        history,
    }

    // Initialize empty rows
    for i := range db.rows {
        db.rows[i] = make([]Cell, width)
    }

    return db
}

// Viewport returns the visible portion of the display buffer.
func (db *DisplayBuffer) Viewport() [][]Cell {
    end := db.viewportTop + db.viewportHeight
    if end > len(db.rows) {
        end = len(db.rows)
    }
    return db.rows[db.viewportTop:end]
}

// TotalRows returns total rows in buffer (including off-screen).
func (db *DisplayBuffer) TotalRows() int {
    return len(db.rows)
}

// RowsAbove returns count of off-screen rows above viewport.
func (db *DisplayBuffer) RowsAbove() int {
    return db.viewportTop
}

// RowsBelow returns count of off-screen rows below viewport.
func (db *DisplayBuffer) RowsBelow() int {
    return len(db.rows) - db.viewportTop - db.viewportHeight
}
```

### DisplayBuffer Operations

```go
// ScrollUp moves viewport up by n physical lines.
// Returns actual lines scrolled (may be less if at top of history).
func (db *DisplayBuffer) ScrollUp(n int) int {
    scrolled := 0

    for i := 0; i < n; i++ {
        // Ensure we have content above
        if db.RowsAbove() == 0 {
            if !db.loadAbove() {
                break  // No more history
            }
        }

        db.viewportTop--
        scrolled++
    }

    // Optionally trim excess rows below
    db.trimBelow(100)  // Keep max 100 rows below viewport

    return scrolled
}

// ScrollDown moves viewport down by n physical lines.
// Returns actual lines scrolled.
func (db *DisplayBuffer) ScrollDown(n int) int {
    scrolled := 0

    for i := 0; i < n; i++ {
        // Check if we're at live edge (can't scroll past current output)
        if db.atLiveEdge() {
            break
        }

        // Ensure we have content below
        if db.RowsBelow() == 0 {
            if !db.loadBelow() {
                break  // At live edge
            }
        }

        db.viewportTop++
        scrolled++
    }

    // Optionally trim excess rows above
    db.trimAbove(100)  // Keep max 100 rows above viewport

    return scrolled
}

// loadAbove loads the previous logical line from history into display buffer.
// Returns false if no more history available.
func (db *DisplayBuffer) loadAbove() bool {
    prevLogical := db.anchorLogical - 1
    if prevLogical < 0 {
        return false  // At start of history
    }

    line := db.history.Line(prevLogical)
    if line == nil {
        return false
    }

    // Wrap to current width
    wrapped := line.WrapToWidth(db.width)

    // Prepend to rows
    db.rows = append(wrapped, db.rows...)

    // Adjust indices
    db.viewportTop += len(wrapped)
    db.anchorRow += len(wrapped)
    db.anchorLogical = prevLogical

    return true
}

// loadBelow loads the next logical line from history into display buffer.
// Returns false if no more history (at live edge).
func (db *DisplayBuffer) loadBelow() bool {
    // Calculate which logical line is at bottom of current buffer
    // (This requires tracking - simplified here)

    // ... implementation depends on tracking bottom anchor
    return false
}

// Resize rebuilds the display buffer for new dimensions.
func (db *DisplayBuffer) Resize(newWidth, newHeight int) {
    if newWidth == db.width && newHeight == db.viewportHeight {
        return
    }

    // Remember which logical line was at top of viewport
    topLogical := db.logicalLineAtViewportTop()
    topWrapOffset := db.wrapOffsetAtViewportTop()

    // Rebuild buffer at new width
    db.width = newWidth
    db.viewportHeight = newHeight
    db.rows = nil

    // Load content around the anchor point
    db.rebuildAround(topLogical, topWrapOffset, newWidth, newHeight)
}

// rebuildAround reconstructs the display buffer centered on a logical line.
func (db *DisplayBuffer) rebuildAround(logicalIndex, wrapOffset, width, height int) {
    // Load the anchor logical line
    line := db.history.Line(logicalIndex)
    if line == nil {
        // Empty history - just create empty buffer
        db.rows = make([][]Cell, height)
        for i := range db.rows {
            db.rows[i] = make([]Cell, width)
        }
        db.viewportTop = 0
        return
    }

    // Wrap anchor line
    wrapped := line.WrapToWidth(width)

    // Position in buffer so wrapOffset is at viewport top
    db.rows = wrapped
    db.viewportTop = min(wrapOffset, len(wrapped)-1)
    db.anchorLogical = logicalIndex
    db.anchorRow = 0

    // Load more above if needed
    for db.RowsAbove() < height {
        if !db.loadAbove() {
            break
        }
    }

    // Load more below if needed
    for len(db.rows)-db.viewportTop < height*2 {
        if !db.loadBelow() {
            break
        }
    }
}
```

## Terminal Integration

### VTerm Changes

The VTerm structure needs to use both history and display buffer:

```go
type VTerm struct {
    // Dimensions
    width, height int

    // Scrollback (authoritative, width-independent)
    history *ScrollbackHistory

    // Display (ephemeral, current-width)
    display *DisplayBuffer

    // Cursor position (in display buffer coordinates)
    cursorX, cursorY int

    // ... other existing fields (alt screen, modes, etc.)
}
```

### Character Placement

```go
func (v *VTerm) placeChar(r rune) {
    cell := Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}

    // 1. Update history (logical line, no width limit)
    v.history.SetCell(v.logicalCursorX, cell)
    v.logicalCursorX++

    // 2. Update display buffer (physical position)
    v.display.SetCell(v.cursorX, v.cursorY, cell)

    // 3. Advance cursor
    v.cursorX++
    if v.cursorX >= v.width {
        // Wrap to next physical line
        v.cursorX = 0
        v.cursorY++

        // Ensure display buffer has room
        v.display.EnsureRow(v.cursorY)
    }
}
```

### Line Feed

```go
func (v *VTerm) lineFeed() {
    // 1. Commit logical line to history
    v.history.Append()
    v.logicalCursorX = 0

    // 2. Move cursor in display
    v.cursorY++

    // 3. Scroll display if needed
    if v.cursorY >= v.height {
        v.display.ScrollContent(1)  // Push one line into off-screen above
        v.cursorY = v.height - 1
    }
}
```

### Resize

```go
func (v *VTerm) Resize(width, height int) {
    oldWidth := v.width
    v.width = width
    v.height = height

    // Rebuild display buffer - O(viewport), not O(history)!
    v.display.Resize(width, height)

    // Reposition cursor
    // ... cursor adjustment logic
}
```

## Persistence

### Storage Format

Store logical lines directly - no `Wrapped` flag needed:

```
Header: "TXHIST02" (8 bytes) + Flags (4 bytes) + Version (4 bytes)

For each logical line:
  [cell_count: 4 bytes]
  [cells: cell_count * 16 bytes each]
    - rune: 4 bytes
    - fg: 4 bytes
    - bg: 4 bytes
    - attr: 1 byte
    - padding: 3 bytes
```

The `Wrapped` flag is no longer stored - wrapping is determined at render time.

### Migration

On loading old format (TXHIST01):
1. Read physical lines with `Wrapped` flags
2. Join consecutive wrapped lines into logical lines
3. Store as new format

```go
func migrateOldHistory(oldLines [][]Cell) []LogicalLine {
    var result []LogicalLine
    var current []Cell

    for _, line := range oldLines {
        current = append(current, line...)

        // Check if this line wraps to next
        wrapped := len(line) > 0 && line[len(line)-1].Wrapped

        if !wrapped {
            // End of logical line
            result = append(result, LogicalLine{Cells: current})
            current = nil
        }
    }

    // Handle trailing content
    if len(current) > 0 {
        result = append(result, LogicalLine{Cells: current})
    }

    return result
}
```

## Implementation Phases

### Phase 1: Data Structures
- [ ] Implement `LogicalLine` with wrapping methods
- [ ] Implement `ScrollbackHistory` with append/get operations
- [ ] Implement `DisplayBuffer` with viewport management
- [ ] Unit tests for each structure

### Phase 2: Display Buffer Operations
- [ ] Implement `loadAbove()` / `loadBelow()`
- [ ] Implement `ScrollUp()` / `ScrollDown()`
- [ ] Implement `Resize()`
- [ ] Implement trimming logic
- [ ] Unit tests for scroll and resize

### Phase 3: VTerm Integration
- [ ] Add history and display buffer to VTerm
- [ ] Modify `placeChar()` for dual writes
- [ ] Modify `lineFeed()` for history commits
- [ ] Modify `Resize()` to rebuild display
- [ ] Modify `Grid()` to return display viewport
- [ ] Update scroll handling

### Phase 4: Persistence
- [ ] New storage format for logical lines
- [ ] Migration from old format
- [ ] Update `HistoryManager` / `HistoryStore`
- [ ] Test persistence round-trip

### Phase 5: Edge Cases & Polish
- [ ] Alt screen handling (no scrollback, unchanged)
- [ ] Scroll regions/margins
- [ ] Cursor positioning across resize
- [ ] Performance testing with large histories
- [ ] Memory usage optimization

## Design Decisions

### Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2025-12-11 | Separate history from display buffer | Enables O(viewport) resize instead of O(history) |
| 2025-12-11 | Store logical lines in history | Width-independent storage, natural reflow |
| 2025-12-11 | SNES-style off-screen margins | Smooth scrolling without per-scroll reflow |

### Open Questions

1. **Off-screen margin size**: Fixed (e.g., 100 rows) or dynamic?
2. **Trim policy**: When to remove distant off-screen content?
3. **Current line handling**: How to sync in-progress line between history and display?
4. **Cursor tracking**: Need `logicalCursorX` separate from display `cursorX`?

## References

- Current implementation: `apps/texelterm/parser/vterm.go`
- History management: `apps/texelterm/parser/history.go`
- Storage format: `apps/texelterm/parser/history_store.go`
