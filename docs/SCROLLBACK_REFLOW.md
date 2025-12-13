# Scrollback Reflow - Implementation Reference

**Status**: Complete (2025-12-12)
**Branch**: `feature/fix-scrollback-reflow`

## Overview

The scrollback system uses a three-level architecture inspired by SNES tile scrolling, separating storage from display for efficient reflow on resize.

## Architecture

```
┌─────────────────────────────────────────┐
│              DISK HISTORY               │
│   (TXHIST02 format - O(1) random access)│
│   Unlimited logical lines on disk       │
└─────────────────────────────────────────┘
                    │
                    │ Load/Unload on demand
                    ▼
┌─────────────────────────────────────────┐
│         SCROLLBACK HISTORY              │
│   (~5000 logical lines in memory)       │
│   Sliding window with global indices    │
└─────────────────────────────────────────┘
                    │
                    │ Wrap to current width
                    ▼
┌─────────────────────────────────────────┐
│            DISPLAY BUFFER               │
│   (Physical lines - current width)      │
│   ┌─────────────────────────────────┐   │
│   │     Off-screen ABOVE (~200)     │   │
│   ├─────────────────────────────────┤   │
│   │     VISIBLE VIEWPORT            │   │
│   ├─────────────────────────────────┤   │
│   │     Off-screen BELOW (~50)      │   │
│   └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

### Key Properties

| Level | Width-dependent | Resize cost | Size | Persistence |
|-------|-----------------|-------------|------|-------------|
| Disk History | No | None | Unlimited | Yes |
| Scrollback History | No | None | ~5000 lines | No (runtime) |
| Display Buffer | Yes | O(viewport) | ~500 lines | No |

## Data Structures

### LogicalLine (`parser/logical_line.go`)

Width-independent line storage:

```go
type LogicalLine struct {
    Cells []Cell  // Full unwrapped content, any length
}

func (l *LogicalLine) WrapToWidth(width int) []PhysicalLine
func (l *LogicalLine) PhysicalLineCount(width int) int
```

### ScrollbackHistory (`parser/scrollback_history.go`)

In-memory sliding window with disk backing:

```go
type ScrollbackHistory struct {
    lines       []*LogicalLine
    startIndex  int64  // Global index of first line in memory
    disk        *DiskHistory
}

func (h *ScrollbackHistory) Append(line *LogicalLine)
func (h *ScrollbackHistory) Line(globalIndex int64) *LogicalLine
func (h *ScrollbackHistory) LoadAbove(count int) int
func (h *ScrollbackHistory) LoadBelow(count int) int
```

### DisplayBuffer (`parser/display_buffer.go`)

Physical lines wrapped to current terminal width:

```go
type DisplayBuffer struct {
    lines              []*PhysicalLine
    currentLine        *LogicalLine
    currentLinePhysical []PhysicalLine
    width, height      int
    viewportOffset     int
    atLiveEdge         bool
}

func (db *DisplayBuffer) GetViewport() []PhysicalLine
func (db *DisplayBuffer) SetCell(logicalX int, cell Cell)
func (db *DisplayBuffer) RebuildCurrentLine()
func (db *DisplayBuffer) Resize(newWidth, newHeight int)
```

### DiskHistory (`parser/disk_history.go`)

TXHIST02 indexed format for O(1) random access:

```go
type DiskHistory struct {
    file    *os.File
    offsets []int64  // Line offset index
}

func (d *DiskHistory) ReadLine(index int64) (*LogicalLine, error)
func (d *DiskHistory) AppendLine(line *LogicalLine) error
func (d *DiskHistory) LineCount() int64
```

## VTerm Integration

### Enabling Display Buffer

```go
// Memory-only mode
v.EnableDisplayBuffer()

// Disk-backed mode
err := v.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
    MaxMemoryLines: 5000,
    MarginAbove:    200,
    MarginBelow:    50,
})
defer v.CloseDisplayBuffer()
```

### Key Integration Points

- `placeChar()` - Dual writes to both history and display buffer via `displayBufferPlaceChar()`
- `LineFeed()` - Commits logical line via `displayBufferLineFeed()`
- `Backspace()` - Syncs logical X via `displayBufferBackspace()`
- `CarriageReturn()` - Resets logical X via `displayBufferCarriageReturn()`
- `ClearLine()` - Updates display via `displayBufferEraseToEndOfLine()` etc.
- `DeleteCharacters()` - Updates display via `displayBufferDeleteCharacters()`
- `Grid()` - Returns viewport from `displayBufferGrid()` when enabled
- `Resize()` - Rebuilds display buffer at new width (O(viewport) not O(history))

## Files

### Core Implementation
- `apps/texelterm/parser/logical_line.go` - LogicalLine with wrapping
- `apps/texelterm/parser/scrollback_history.go` - Memory window with disk backing
- `apps/texelterm/parser/display_buffer.go` - Physical line viewport
- `apps/texelterm/parser/disk_history.go` - TXHIST02 indexed format
- `apps/texelterm/parser/vterm_display_buffer.go` - VTerm integration layer

### Modified Files
- `apps/texelterm/parser/vterm.go` - Grid() path, displayBuf state
- `apps/texelterm/parser/vterm_scroll.go` - LineFeed commits, scroll handling
- `apps/texelterm/parser/vterm_navigation.go` - CR/Backspace sync
- `apps/texelterm/parser/vterm_erase.go` - Erase operations
- `apps/texelterm/parser/vterm_edit_char.go` - DCH (delete character) support
- `apps/texelterm/term.go` - Config option `display_buffer_enabled`

## Configuration

Enable in `~/.config/texelation/theme.json`:

```json
{
  "texelterm": {
    "display_buffer_enabled": true
  }
}
```

## Performance

Benchmarks on AMD Ryzen 9 3950X:
- **PlaceChar**: ~563ns/op
- **Resize**: ~146µs/op with 1000 lines (O(viewport))
- **Scroll**: ~8.6ns/op, 0 allocations

## Backspace Visual Erase Fix (2025-12-13)

Bash uses BS + EL (Erase to End of Line) for backspace erase. Fixed by adding `RebuildCurrentLine()` calls to all display buffer erase functions to update the physical representation after modifying the logical line.
