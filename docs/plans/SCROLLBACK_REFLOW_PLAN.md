# Scrollback Reflow Plan

## Status: Approved - Ready for Implementation
**Created**: 2025-12-11
**Approved**: 2025-12-11
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

## Tricky Aspects - Deep Dive

### 1. What is a "Logical Line" Really?

**The fundamental question**: When does a logical line end?

In terminal emulation, there are several ways content moves to a new line:

| Event | What happens | New logical line? |
|-------|--------------|-------------------|
| Program sends `\n` (LF) | Cursor moves down | **Yes** |
| Program sends `\r\n` | CR then LF | **Yes** |
| Cursor hits right edge (wrap) | Cursor moves to col 0, row+1 | **No** - same logical line |
| Cursor at bottom + LF | Screen scrolls up | **Yes** (old top line goes to history) |
| `CSI n E` (CNL) | Cursor to next line | **Yes** |
| Manual cursor move down | `CSI B` or `CSI H` | **Ambiguous** - see below |

**The ambiguity**: If a program does `printf("Hello"); move_cursor_down(); printf("World")`, are "Hello" and "World" on separate logical lines?

**Proposed answer**: Yes. Any explicit line feed or cursor-down movement ends the logical line. Only auto-wrap (hitting right edge) continues the same logical line.

This matches user intuition: "scroll up one line" means "show me the previous thing the program printed on its own line".

### 2. The Visible Screen vs History

**Current model** (what vterm.go does now):
```
History buffer: [line0, line1, line2, ..., lineN]
                                          ↑
                              topHistoryLine = N - height

Visible screen is history[topHistoryLine : topHistoryLine + height]
Cursor position (cursorX, cursorY) is relative to visible screen
Actual history index = cursorY + topHistoryLine
```

The visible screen IS the tail of history. There's no separate "screen buffer" for main screen.

**New model** (what we're proposing):
```
History (logical):  [L0, L1, L2, ..., LN]  (each Li can be any length)

Display buffer (physical):
  [row0]  ← from L(N-5) wrap 0
  [row1]  ← from L(N-5) wrap 1  (L(N-5) is a long line)
  [row2]  ← from L(N-4) wrap 0
  [row3]  ← from L(N-3) wrap 0
  [row4]  ← from L(N-2) wrap 0
  [row5]  ← from L(N-1) wrap 0
  [row6]  ← from L(N) wrap 0    ← CURRENT LINE (incomplete)
  [row7]  ← from L(N) wrap 1    ← CURRENT LINE continued
          ↑
       cursorY points here, cursorX within the row
```

**Key insight**: The display buffer is a **materialized view** of history wrapped to current width. It's not the source of truth.

### 3. The "Current Line" Problem - Detailed

When the user/program is typing, we have an incomplete logical line. Let's trace through a scenario:

**Terminal is 10 columns wide. User types "Hello World, this is a test"**

Step by step:
```
After "Hello":
  History.current = [H,e,l,l,o]
  Display row 0:    [H,e,l,l,o,_,_,_,_,_]  (cursor at col 5)

After "Hello Worl":
  History.current = [H,e,l,l,o, ,W,o,r,l]
  Display row 0:    [H,e,l,l,o, ,W,o,r,l]  (cursor at col 9, at edge)

After "Hello World":  ← 'd' causes wrap!
  History.current = [H,e,l,l,o, ,W,o,r,l,d]  (11 chars, logical line)
  Display row 0:    [H,e,l,l,o, ,W,o,r,l]    (first 10 chars)
  Display row 1:    [d,_,_,_,_,_,_,_,_,_]    (char 11, cursor at col 1)

After "Hello World, this is a test":  (27 chars)
  History.current = [H,e,l,l,o, ,W,o,r,l,d,,, ,t,h,i,s, ,i,s, ,a, ,t,e,s,t]
  Display row 0:    [H,e,l,l,o, ,W,o,r,l]    (chars 0-9)
  Display row 1:    [d,,, ,t,h,i,s, ,i]       (chars 10-19)
  Display row 2:    [s, ,a, ,t,e,s,t,_,_]    (chars 20-27, cursor at col 7)
```

**Solution: Dual tracking**

```go
type VTerm struct {
    // Logical position (in current incomplete line)
    logicalX int  // 0-based index into history.current.Cells

    // Physical position (in display buffer)
    cursorX int  // 0 to width-1
    cursorY int  // 0 to height-1 (relative to viewport top)
}
```

**On each character**:
```go
func (v *VTerm) placeChar(r rune) {
    // 1. Write to logical line (unbounded)
    v.history.SetCell(v.logicalX, Cell{Rune: r, ...})
    v.logicalX++

    // 2. Write to display buffer (bounded by width)
    v.display.SetCell(v.cursorX, v.cursorY, Cell{Rune: r, ...})
    v.cursorX++

    // 3. Handle wrap in display (but NOT in history!)
    if v.cursorX >= v.width {
        v.cursorX = 0
        v.cursorY++
        v.display.EnsureRow(v.cursorY)  // Grow display if needed
    }
}
```

**On line feed** (LF):
```go
func (v *VTerm) lineFeed() {
    // 1. Commit logical line to history
    v.history.CommitCurrent()
    v.logicalX = 0

    // 2. Move cursor in display
    v.cursorY++

    // 3. Handle scroll if at bottom
    if v.cursorY >= v.height {
        v.display.ScrollUp(1)
        v.cursorY = v.height - 1
    }
}
```

### 4. Carriage Return - The "Overwrite" Case

CR (`\r`) moves cursor to column 0 **without** starting a new line. This is used for:
- Progress bars: `\rProgress: 50%` then `\rProgress: 75%`
- Spinners: `\r/` then `\r-` then `\r\` then `\r|`

**What happens in our model**:

```
Program: "Loading...\rDone!     "

After "Loading...":
  History.current = [L,o,a,d,i,n,g,.,.,.]
  logicalX = 10

After "\r":
  logicalX = 0  (CR resets logical position too!)
  cursorX = 0

After "Done!":
  History.current = [D,o,n,e,!,n,g,.,.,.]  ← overwrote first 5 chars
  logicalX = 5
```

Wait, that's wrong! The trailing "ng..." is still there.

**Better handling**:
```go
func (v *VTerm) carriageReturn() {
    v.logicalX = 0
    v.cursorX = 0
    // Don't truncate - overwrites will happen naturally
}
```

But we need to handle the case where the new content is shorter than old:
```
"Loading...\rDone!" → should show "Done!ng..." or "Done!     "?
```

Actually, the terminal shows "Done!ng..." - it only overwrites what you write. The spaces in my example above were explicit: `"Done!     "` (with 5 spaces).

**Our model handles this correctly**: `SetCell` overwrites individual cells, leaving others untouched.

### 5. Screen Scroll vs History Scroll

There are TWO different kinds of scrolling:

**A. Screen scroll** (program output causes scroll):
- New line at bottom of screen pushes content up
- Top line goes into scrollback history
- User is at "live edge" - sees current output

**B. User scroll** (user scrolls through history):
- Viewport moves up/down through history
- Program output continues (but user doesn't see it immediately)
- When user returns to bottom, they see current state

**Current code conflates these** via `viewOffset`:
- `viewOffset = 0`: At live edge
- `viewOffset > 0`: Scrolled up into history

**New model keeps them separate**:

```go
type DisplayBuffer struct {
    rows        [][]Cell
    viewportTop int      // Which row index is at top of visible area

    // For user scrolling
    atLiveEdge  bool     // If true, new output appends and scrolls

    // Anchoring to history
    topLogical  int      // Which logical line is at top of buffer
    topWrap     int      // Which wrap segment of that line
}
```

**When at live edge** (`atLiveEdge = true`):
- New characters append to bottom
- LineFeed scrolls display up, appends new row at bottom
- Display buffer grows at bottom, trims at top

**When scrolled into history** (`atLiveEdge = false`):
- New output goes to history but NOT to display buffer
- Display buffer is frozen showing historical content
- When user scrolls back to bottom, rebuild display from history tail

### 6. Scroll Regions (DECSTBM) - The Hard Part

Programs can define a scroll region: "only scroll lines 5-20, leave 0-4 and 21-24 fixed".

```
┌────────────────────┐
│ Fixed header       │ ← Lines 0-4 never scroll
├────────────────────┤
│ Scrolling region   │ ← Lines 5-20 scroll when cursor at line 20 + LF
│                    │
├────────────────────┤
│ Fixed footer       │ ← Lines 21-24 never scroll
└────────────────────┘
```

**The problem**: When content scrolls within the region, does line 5 go to history?

**Answer: NO!** In the current code (`scrollRegion` in vterm_scroll.go:52-123), when `top > 0`, lines are shifted within the visible area but nothing goes to history. Only when scrolling at `top == 0` does content go to history.

**For our new model**:

Scroll regions operate **only on the display buffer**, not on history:

```go
func (v *VTerm) scrollRegion(n int, top int, bottom int) {
    if top == 0 && bottom == v.height-1 {
        // Full-screen scroll: content goes to history
        v.scrollWithHistory(n)
    } else {
        // Partial scroll region: display-only operation
        v.display.ShiftRows(top, bottom, n)
        // History is NOT affected
    }
}
```

**But wait**: What if a long logical line is partially in the scroll region and partially outside?

**Answer**: Scroll regions are a display-only concept. The logical line in history stays intact. Only the display buffer's physical rows shift. This might mean that after scrolling, the display buffer no longer matches a simple wrapping of history - and that's okay! The display buffer is ephemeral; on resize, we rebuild it from history.

### 7. Anchor Tracking - How to Map Display ↔ History

We need to answer: "Display row 17 corresponds to which logical line, which wrap segment?"

**Option A: Per-row metadata**
```go
type DisplayRow struct {
    Cells       []Cell
    LogicalIdx  int   // Which logical line
    WrapSegment int   // Which wrap (0 = first width chars, 1 = next width, etc.)
}
```

**Option B: Boundary markers**
```go
type DisplayBuffer struct {
    rows [][]Cell

    // Boundaries[i] = index of first display row of logical line i
    // (Only for logical lines currently in the buffer)
    boundaries map[int]int  // logicalIdx → first display row
}
```

**Option C: Walk from anchor**
```go
type DisplayBuffer struct {
    rows [][]Cell

    // The anchor: rows[anchorRow] is wrap 0 of history[anchorLogical]
    anchorLogical int
    anchorRow     int
}

func (db *DisplayBuffer) LogicalLineAt(displayRow int) (logicalIdx, wrapSegment int) {
    // Walk from anchor to displayRow, counting wraps
    // This is O(displayRow - anchorRow) but display buffer is small
}
```

**Recommendation**: Option C (walk from anchor) is simplest and sufficient. Display buffer is ~500 rows max; walking is fast.

### 8. The Resize Operation

**Goal**: When terminal resizes, rebuild display buffer to show same content at new width.

**Step 1: Remember position**
```go
// Before resize, note which logical line is at top of viewport
topLogical, topWrap := db.LogicalLineAtViewportTop()
```

**Step 2: Rebuild**
```go
db.rows = nil
db.width = newWidth
db.viewportHeight = newHeight

// Re-wrap logical lines starting from topLogical
for logIdx := topLogical; len(db.rows) < newHeight*2 && logIdx < history.Length(); logIdx++ {
    line := history.Line(logIdx)
    wrapped := line.WrapToWidth(newWidth)

    if logIdx == topLogical {
        // Skip wraps before topWrap (they're above viewport)
        wrapped = wrapped[min(topWrap, len(wrapped)-1):]
    }

    db.rows = append(db.rows, wrapped...)
}

// Load some content above too
db.loadAbove(100)

// Set viewport position
db.viewportTop = db.RowsAbove()
```

**Step 3: Adjust cursor**

The cursor's logical position (`logicalX`) is unchanged. But its physical position needs recalculation:

```go
// Cursor is at logicalX in the current logical line
// Where is that in the display buffer?
physicalRow := logicalX / newWidth
physicalCol := logicalX % newWidth

// Find which display row corresponds to current logical line + physicalRow
cursorDisplayRow := db.FindDisplayRow(history.Length()-1, physicalRow)
v.cursorY = cursorDisplayRow - db.viewportTop
v.cursorX = physicalCol
```

### 9. Initial Load from Persisted History

When terminal opens with existing history file:

```go
func (v *VTerm) LoadHistory(history *ScrollbackHistory) {
    // Build display buffer from end of history
    v.display = NewDisplayBuffer(v.width, v.height)

    // Load last N logical lines, wrap them
    physicalRows := 0
    for i := history.Length() - 1; i >= 0 && physicalRows < v.height*2; i-- {
        line := history.Line(i)
        wrapped := line.WrapToWidth(v.width)
        v.display.PrependRows(wrapped)
        physicalRows += len(wrapped)
    }

    // Position at live edge (bottom)
    v.display.PositionAtLiveEdge()

    // Cursor at end of last line
    lastLine := history.CurrentLine()  // Might be empty
    v.logicalX = len(lastLine.Cells)
    v.cursorX = v.logicalX % v.width
    v.cursorY = v.height - 1  // Bottom of screen
}
```

### 10. Cursor Movement Without Output

Programs can move the cursor around without printing. This creates interesting cases:

**Case A: Cursor moves within current logical line**
```
"Hello" → cursor at col 5
CSI 2 D (move left 2) → cursor at col 3
"XX" → overwrites, line becomes "HelXXo"
```
This works fine - `logicalX` tracks position, `SetCell` overwrites.

**Case B: Cursor moves to previous physical row (same logical line)**
```
Width=10, "Hello World" (11 chars, wraps to 2 rows)
Cursor at row 1, col 1 (after 'd')
CSI A (move up) → cursor at row 0, col 1

logicalX should become: 1 (position in logical line)
```

We need to translate physical (row, col) back to logical position:
```go
func (v *VTerm) physicalToLogical(row, col int) int {
    // Find which logical line this row belongs to
    // Calculate: (wrapIndex * width) + col
    return wrapIndex * v.width + col
}
```

**Case C: Cursor moves to a different logical line**
```
Line 0: "First"
Line 1: "Second" ← cursor here
CSI A (move up) → cursor to Line 0
```

Now we're editing a **committed** logical line, not the current one!

**Options**:
1. **Disallow**: Cursor can't move above current logical line. (Wrong - breaks many programs)
2. **Re-open**: Moving cursor up "re-opens" that logical line for editing. (Complex)
3. **Display-only**: Edits to committed lines only affect display, not history. (Lossy)
4. **Sparse edits**: Track edits to committed lines separately. (Complex)

**Proposed solution: Option 2 with constraints**

When cursor moves to a committed logical line:
```go
func (v *VTerm) moveCursorToLogicalLine(lineIdx int) {
    if lineIdx < v.history.Length() {
        // Moving to a committed line
        // This line becomes the "active" line for edits
        v.activeLogicalLine = lineIdx
        // Note: history.current stays as-is (pending for when we return)
    }
}
```

But this is getting complicated. Let's simplify:

**Simpler approach**: The "current logical line" is always the one the cursor is on, regardless of whether it was previously "committed".

```go
type ScrollbackHistory struct {
    lines []LogicalLine

    // No separate "current" - any line can be edited
    // Lines are only truly "frozen" when they scroll off the top of the screen
}
```

The commitment point isn't LF - it's **scrolling off the visible screen**. As long as a line is visible (or recently visible), it can be edited.

This actually matches current behavior! The current vterm modifies history lines in-place via `setHistoryLine`.

### 11. Memory and Trimming

**Display buffer size limits**:
```go
const (
    maxOffscreenAbove = 200  // ~200 physical rows above viewport
    maxOffscreenBelow = 50   // Small, since we're usually at live edge
    trimThreshold     = 50   // Trim when exceeding max by this much
)
```

**When to trim**:
- After scroll operations that grow the buffer
- After loading from history

**Trimming above** (user scrolled down, old stuff at top):
```go
func (db *DisplayBuffer) TrimAbove() {
    excess := db.RowsAbove() - maxOffscreenAbove
    if excess > trimThreshold {
        db.rows = db.rows[excess:]
        db.viewportTop -= excess
        db.anchorRow -= excess
        if db.anchorRow < 0 {
            // Anchor was trimmed, need to recalculate
            db.recalculateAnchorFromTop()
        }
    }
}
```

**Trimming below** (user scrolled up, live edge content accumulated):
```go
func (db *DisplayBuffer) TrimBelow() {
    excess := db.RowsBelow() - maxOffscreenBelow
    if excess > trimThreshold {
        db.rows = db.rows[:len(db.rows)-excess]
    }
}
```

## Summary of Key Decisions

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| **Line storage** | Logical (unwrapped) lines | Width-independent, natural reflow |
| **Display buffer** | Separate from history, ephemeral | O(viewport) resize, clean separation |
| **Off-screen margins** | Variable size, ~200 rows max | Smooth scrolling without constant reloading |
| **Anchor tracking** | Single anchor + walk | Simple, display buffer is small |
| **Logical line boundary** | LF/CNL commits, wrap continues | Matches user expectation of "lines" |
| **Cursor tracking** | Dual: logicalX + (cursorX, cursorY) | Need both for proper editing |
| **Editing committed lines** | Allow in-place edits | Matches current behavior, needed for cursor-up |
| **Scroll regions** | Display-only operation | History stays clean, regions are visual |
| **Persistence format** | Store logical lines, no Wrapped flag | Simpler, width-independent |

## References

- Current implementation: `apps/texelterm/parser/vterm.go`
- History management: `apps/texelterm/parser/history.go`
- Storage format: `apps/texelterm/parser/history_store.go`
