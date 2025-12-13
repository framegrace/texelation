# Scrollback Persistence Plan: Three-Level Architecture

## Status: Ready for Implementation

## Problem

The current implementation has a tangled dual-path where both legacy HistoryManager and DisplayBuffer run in parallel, causing:
- Content loss when ScrollbackHistory fills up
- Resize loses content because rewrap only sees in-memory history
- No true "infinite" scrollback backed by disk

## Solution: Three-Level SNES-Style Architecture

Apply the same sliding-window pattern used in DisplayBuffer, but add a level for disk backing:

```
┌─────────────────────────────────────────────────────────────────┐
│                    DISK (Indexed File)                          │
│              Unlimited logical lines on disk                    │
│              Random access via TXHIST02 index                   │
│              Written incrementally as lines commit              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Load/Unload on demand
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│              MEMORY HISTORY BUFFER (~5000 logical lines)        │
│              Width-independent logical lines                    │
│   ┌───────────────────────────────────────────────────────┐     │
│   │     Off-screen ABOVE (margin ~1000)                   │     │
│   ├───────────────────────────────────────────────────────┤     │
│   │     "VISIBLE" to DisplayBuffer                        │     │
│   ├───────────────────────────────────────────────────────┤     │
│   │     Off-screen BELOW (margin ~500)                    │     │
│   └───────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Wrap to current width
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│              DISPLAY BUFFER (~500 physical lines)               │
│              Physical lines at current terminal width           │
│   ┌───────────────────────────────────────────────────────┐     │
│   │     Off-screen ABOVE (~200)                           │     │
│   ├───────────────────────────────────────────────────────┤     │
│   │     VISIBLE VIEWPORT (terminal height)                │     │
│   ├───────────────────────────────────────────────────────┤     │
│   │     Off-screen BELOW (~50)                            │     │
│   └───────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────┘
```

## Key Concepts

### Level 1: Disk (IndexedHistoryFile)
- Stores ALL logical lines ever written
- Uses TXHIST02 format with line offset index for O(1) random access
- Written incrementally: each committed line appends to file
- Index updated periodically or on close
- Only this level persists across restarts

### Level 2: Memory History Buffer (enhanced ScrollbackHistory)
- Holds ~5000 logical lines in memory
- Has margins above/below the "visible" window (what DisplayBuffer sees)
- When DisplayBuffer scrolls past margins, Memory History loads from disk
- When Memory History scrolls past ITS margins, it loads/unloads from disk
- This is the SOURCE OF TRUTH for DisplayBuffer

### Level 3: Display Buffer (existing)
- Holds ~500 physical lines wrapped to current width
- Has margins above/below viewport
- Loads from Memory History (not disk directly)
- Handles reflow on width change

## Data Flow

### New Output (character written):
```
placeChar(r)
  → DisplayBuffer.SetCell() [updates currentLine physical]
  → (logical line tracks position)

LineFeed()
  → DisplayBuffer.CommitCurrentLine()
    → MemoryHistory.Append(logicalLine)
      → DiskFile.AppendLine(logicalLine)  [incremental write]
    → Add to physical lines array
```

### Scrolling Up (user presses Page Up):
```
Scroll(-n)
  → DisplayBuffer.ScrollUp(n)
    → If need more physical lines above:
      → MemoryHistory.GetLinesAbove(count)
        → If need more logical lines above:
          → DiskFile.ReadLineRange(start, end)
          → Prepend to memory buffer, trim below
        → Return logical lines
      → Wrap to physical, prepend to display buffer
```

### Resize (width change):
```
Resize(newWidth, newHeight)
  → DisplayBuffer.Rewrap()
    → Get logical lines from MemoryHistory (current window)
    → Rewrap all to new width
    → Rebuild physical lines array
    → (MemoryHistory unchanged - it's width-independent)
```

## Files to Create/Modify

### New: `apps/texelterm/parser/disk_history.go`
- `DiskHistory` struct with:
  - `file *IndexedHistoryFile` (for reading)
  - `writer *IndexedHistoryWriter` (for appending)
  - `AppendLine(line *LogicalLine)` - incremental write
  - `ReadLineRange(start, end int64) []*LogicalLine`
  - `LineCount() int64`
  - `Close()` - finalize index

### Modify: `apps/texelterm/parser/scrollback_history.go`
- Add `diskHistory *DiskHistory` field
- Add margins (marginAbove, marginBelow, windowTop)
- Add `LoadAbove(count int)` - load from disk when scrolling
- Add `LoadBelow(count int)` - load from disk when scrolling down
- Add `TrimAbove()` / `TrimBelow()` - unload to stay within memory limit
- Rename to `MemoryHistory` for clarity?

### Modify: `apps/texelterm/parser/display_buffer.go`
- Change `history *ScrollbackHistory` to use the enhanced version
- Remove `loader HistoryLoader` (Memory History handles disk loading)
- `loadAbove()` just asks MemoryHistory for more lines

### Remove: Legacy paths
- Remove `HistoryManager` integration from VTerm
- Remove `historyManagerLoader`
- Remove TXHIST01 format support
- Remove dual-write in `placeChar`/`lineFeed`

## Indexed File Format (TXHIST02) - Keep As Is

```
Header (32 bytes):
  Magic: "TXHIST02" (8 bytes)
  Flags: uint32
  LineCount: uint64
  IndexOffset: uint64
  Reserved: 4 bytes

Line Data (variable):
  CellCount: uint32
  Cells: CellCount * 16 bytes each
    Rune: int32
    FG: 5 bytes (mode + value + RGB)
    BG: 5 bytes
    Attr: uint16

Index (at end of file):
  Offset[0]: uint64
  Offset[1]: uint64
  ...
```

For incremental writes:
- Append line data immediately
- Keep offsets in memory
- Write index on Close() or periodically

## Implementation Order

1. **Create DiskHistory** - wraps IndexedHistoryFile for read + incremental write
2. **Enhance ScrollbackHistory** - add disk backing with margins
3. **Simplify DisplayBuffer** - remove loader, just use MemoryHistory
4. **Update VTerm** - remove legacy HistoryManager, use single path
5. **Test** - verify scrolling loads from disk, resize preserves content
6. **Cleanup** - remove old code, TXHIST01 support

## Memory Estimates

- Memory History: 5000 lines * ~200 bytes avg = ~1MB per terminal
- Display Buffer: 500 lines * ~100 bytes avg = ~50KB per terminal
- Disk: Unlimited (typical session might be 10-100MB)

## Open Questions

1. How often to flush index to disk? (On close? Every N lines? Timer?)
2. Should we compress older portions of the disk file?
3. Privacy mode - stop writing to disk temporarily?
