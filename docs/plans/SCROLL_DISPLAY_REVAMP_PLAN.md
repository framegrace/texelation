# Scroll/Display System Revamp Plan

## Overview

Complete revamp of the terminal scroll/display system with three cleanly separated layers:

1. **MemoryBuffer** - Large in-memory storage for all terminal content
2. **AdaptivePersistence** - Dynamic disk persistence with rate-based debouncing
3. **ViewportWindow** - Pure view layer for rendering and scrolling

## Key Design Principles

- **No TUI vs non-TUI differentiation** - Terminal draws anywhere freely
- **Viewing window decoupled from update window** - Completely separate concerns
- **Per-line fixed-width flags** - Lines that shouldn't reflow are flagged individually
- **Adaptive persistence** - Write-through for normal usage, debounced/best-effort under load
- **Clean incremental implementation** - Each phase removes old code

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                         VTerm (unchanged)                            │
│  Terminal state machine, cursor, escape sequences, scroll regions   │
└─────────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     MemoryBuffer (NEW)                               │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │ lines []*LogicalLine     // All content, configurable size   │   │
│  │ dirtySet map[int64]bool  // Per-line dirty tracking          │   │
│  │ config MemoryBufferConfig // Size limits, eviction policy    │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  Write(lineIdx, cells)  // VTerm writes here                        │
│  SetLineFixed(lineIdx, width)  // Flag as non-reflowable            │
│  GetLine(lineIdx) *LogicalLine  // Read access                      │
│  GetDirtyLines() []int64  // For persistence layer                  │
│  ClearDirty(lineIdx)  // After persistence                          │
└─────────────────────────────────────────────────────────────────────┘
          │                                    │
          ▼                                    ▼
┌──────────────────────────┐    ┌─────────────────────────────────────┐
│   ViewportWindow (NEW)   │    │   AdaptivePersistence (NEW)         │
│                          │    │                                      │
│  memBuf *MemoryBuffer    │    │  memBuf *MemoryBuffer                │
│  scrollOffset int64      │    │  disk *DiskHistory                   │
│  width, height int       │    │  writeRateMonitor                    │
│                          │    │  currentMode PersistMode             │
│  GetVisibleGrid()[][]Cell│    │                                      │
│  ScrollUp/Down(n)        │    │  Modes:                              │
│  Resize(w, h)            │    │   - WriteThrough (immediate)         │
│                          │    │   - Debounced (100-500ms)            │
└──────────────────────────┘    │   - BestEffort (flush on idle)       │
                                └─────────────────────────────────────┘
```

---

## Phase 1: MemoryBuffer Core ✅ COMPLETED

> **Status**: Implemented on 2026-01-26
> **Files**: `apps/texelterm/parser/memory_buffer.go`, `apps/texelterm/parser/memory_buffer_test.go`

### Goal
Create the central storage layer that holds all terminal content.

### Implementation Summary

**Files Created:**
- `apps/texelterm/parser/memory_buffer.go` (~720 lines)
- `apps/texelterm/parser/memory_buffer_test.go` (~650 lines, 23 test functions)

**Architecture Chosen: Clean Architecture with Ring Buffer**

The implementation uses:
1. **Ring buffer** for O(1) append and eviction operations
2. **Extracted `DirtyTracker`** component for clean separation and testability
3. **`Cursor` struct** to encapsulate write position (GlobalLineIdx, Col)
4. **`contentVersion` counter** for cache invalidation (used by ViewportWindow in Phase 3)

### Implemented Types

```go
// DirtyTracker - Extracted component for per-line dirty state
type DirtyTracker struct {
    dirty map[int64]bool
}

// Cursor - Encapsulates write position
type Cursor struct {
    GlobalLineIdx int64
    Col           int
}

// MemoryBufferConfig - Configuration
type MemoryBufferConfig struct {
    MaxLines      int  // Default: 50000
    EvictionBatch int  // Default: 1000
}

// MemoryBuffer - Central storage with ring buffer
type MemoryBuffer struct {
    config         MemoryBufferConfig
    lines          []*LogicalLine  // Ring buffer
    ringHead       int             // Index of oldest line
    ringSize       int             // Current line count
    globalOffset   int64           // Global index of ringHead
    cursor         Cursor          // Write position
    termWidth      int             // Current terminal width
    contentVersion int64           // For cache invalidation
    dirtyTracker   *DirtyTracker
    mu             sync.RWMutex
}
```

### Implemented Methods

**Writing:**
- `Write(r rune, fg, bg Color, attr Attribute)` - Single character
- `WriteWide(r rune, fg, bg Color, attr Attribute, isWide bool) bool` - Wide character with validation
- `SetCell(lineIdx int64, col int, cell Cell)` - Direct cell access
- `NewLine()` - Advance cursor line (LF behavior)
- `CarriageReturn()` - Reset cursor column to 0

**Cursor:**
- `SetCursor(lineIdx int64, col int)`
- `GetCursor() (lineIdx int64, col int)`
- `CursorLine() int64`
- `CursorCol() int`
- `SetTermWidth(width int)`
- `TermWidth() int`

**Reading:**
- `GetLine(globalIdx int64) *LogicalLine`
- `GetLineRange(start, end int64) []*LogicalLine`
- `TotalLines() int64`
- `GlobalOffset() int64`
- `GlobalEnd() int64`
- `ContentVersion() int64` - For cache invalidation

**Fixed-Width Flags:**
- `SetLineFixed(globalIdx int64, width int)` - Validates width bounds (1-10000)
- `IsLineFixed(globalIdx int64) bool`

**Dirty Tracking:**
- `GetDirtyLines() []int64` - Returns sorted slice
- `ClearDirty(globalIdx int64)`
- `ClearAllDirty()`
- `MarkDirty(globalIdx int64)`
- `IsDirty(globalIdx int64) bool`

**Line Operations:**
- `EnsureLine(globalIdx int64) *LogicalLine` - Creates line and fills gaps
- `InsertLine(globalIdx int64)` - Insert blank, shift lines down
- `DeleteLine(globalIdx int64)` - Remove line, shift lines up

**Erase Operations:**
- `EraseLine(globalIdx int64)`
- `EraseToEndOfLine(globalIdx int64, col int)`
- `EraseFromStartOfLine(globalIdx int64, col int)`

**Eviction:**
- `Evict(count int) int` - Returns actual lines evicted

**Configuration:**
- `NewMemoryBuffer(config MemoryBufferConfig) *MemoryBuffer`
- `DefaultMemoryBufferConfig() MemoryBufferConfig`
- `Config() MemoryBufferConfig`

### Key Design Decisions Made

1. **Ring Buffer Storage**: O(1) append and eviction. The ring buffer uses `ringHead` and `ringSize` to track the window into the pre-allocated array.

2. **Global Indexing with Pre-check Eviction**: When `EnsureLine` would trigger eviction that moves `globalOffset` past the requested index, it returns `nil` early instead of creating an unreachable line. This provides deterministic behavior.

3. **DirtyTracker Extraction**: Separated dirty tracking into its own type for:
   - Independent testing
   - Clean interface for Phase 2 (AdaptivePersistence)
   - Single responsibility

4. **ContentVersion Counter**: Increments on every write/modify operation. Phase 3 ViewportWindow uses this to detect when to invalidate its cache.

5. **Cursor Struct**: Encapsulates `GlobalLineIdx` and `Col` for cleaner method signatures and state management.

6. **FixedWidth Validation**: Rejects width <= 0 or > 10000 to prevent invalid state.

### Tests Implemented (23 tests, all passing)

**DirtyTracker Tests:**
- `TestDirtyTracker_MarkAndClear`
- `TestDirtyTracker_GetDirty`
- `TestDirtyTracker_ClearAll`
- `TestDirtyTracker_RemoveBelow`

**MemoryBuffer Tests:**
- `TestMemoryBuffer_DefaultConfig`
- `TestMemoryBuffer_WriteAndRead`
- `TestMemoryBuffer_WriteWide`
- `TestMemoryBuffer_WriteWideAtEdge`
- `TestMemoryBuffer_CursorMovement`
- `TestMemoryBuffer_Eviction`
- `TestMemoryBuffer_DirtyTracking`
- `TestMemoryBuffer_FixedWidthFlags`
- `TestMemoryBuffer_GlobalIndexing`
- `TestMemoryBuffer_LineOperations`
- `TestMemoryBuffer_EraseOperations`
- `TestMemoryBuffer_GetLineRange`
- `TestMemoryBuffer_ContentVersion`
- `TestMemoryBuffer_RingBufferWrap`
- `TestMemoryBuffer_Concurrency` (with `-race` flag)
- `TestMemoryBuffer_EnsureLine_GapFill`
- `TestMemoryBuffer_SetCell`
- `TestMemoryBuffer_EvictWithDirty`
- `TestMemoryBuffer_TermWidth`

### Important Notes for Phase 2 (AdaptivePersistence)

1. **Dirty Tracking Integration**: Call `mb.GetDirtyLines()` to get lines needing persistence. After successful disk write, call `mb.ClearDirty(lineIdx)` for each line.

2. **Line Access for Persistence**: Use `mb.GetLine(globalIdx)` to retrieve line content for writing to disk. Returns `nil` if line was evicted - persistence should handle this gracefully.

3. **Rate Monitoring Hook**: When integrating, call `NotifyWrite(lineIdx)` after each `mb.Write()` or `mb.SetCell()`. The `lineIdx` is available from `mb.CursorLine()`.

4. **Eviction Awareness**: Lines may be evicted between `GetDirtyLines()` and `GetLine()`. Check for `nil` returns and remove from dirty set if line no longer exists.

5. **Thread Safety**: All MemoryBuffer methods are thread-safe. AdaptivePersistence can safely read while VTerm writes.

6. **ContentVersion**: Use `mb.ContentVersion()` to detect changes if implementing any caching in persistence layer.

### Files to Remove
None - this phase is additive.

---

## Phase 2: Adaptive Persistence Layer ✅ COMPLETED

> **Status**: Implemented on 2026-01-26
> **Files**:
> - `apps/texelterm/parser/persist_mode.go` (~35 lines)
> - `apps/texelterm/parser/rate_monitor.go` (~90 lines)
> - `apps/texelterm/parser/mode_controller.go` (~95 lines)
> - `apps/texelterm/parser/adaptive_persistence.go` (~470 lines)
> - `apps/texelterm/parser/adaptive_persistence_test.go` (~700 lines, 26 test functions)

### Goal
Dynamic debounce based on write rate, from immediate to best-effort.

### Implementation Summary

**Architecture Chosen: Clean Architecture with Extracted Components**

The implementation separates concerns into focused, testable components:
1. **PersistMode** enum - Type-safe mode representation
2. **RateMonitor** - Ring buffer for timestamp tracking and rate calculation
3. **ModeController** - Mode selection logic and adaptive debounce delay calculation
4. **AdaptivePersistence** - Main orchestrator coordinating all components

### Implemented Types

```go
// persist_mode.go - Mode enum
type PersistMode int
const (
    PersistWriteThrough PersistMode = iota  // < 10 writes/sec: immediate
    PersistDebounced                        // 10-100 writes/sec: batched with adaptive delay
    PersistBestEffort                       // > 100 writes/sec: flush on idle only
)

// rate_monitor.go - Timestamp tracking with ring buffer
type RateMonitor struct {
    timestamps []time.Time  // Ring buffer
    head       int          // Next write position
    size       int          // Current entries (0 to windowSize)
    windowSize int          // Maximum entries
}

// mode_controller.go - Mode selection and delay calculation
type ModeController struct {
    writeThroughMaxRate float64  // Threshold for WriteThrough mode
    debouncedMaxRate    float64  // Threshold for Debounced mode
}

// adaptive_persistence.go - Main orchestrator
type AdaptivePersistence struct {
    config       AdaptivePersistenceConfig
    memBuf       *MemoryBuffer
    disk         *DiskHistory
    nowFunc      func() time.Time  // Injected for testing
    rateMonitor  *RateMonitor
    modeCtrl     *ModeController
    currentMode  PersistMode
    pendingLines map[int64]bool
    lastActivity time.Time
    flushTimer   *time.Timer
    idleTicker   *time.Ticker
    stopCh       chan struct{}
    stopped      bool
    stopOnce     sync.Once  // Safe double-close protection
    metrics      PersistenceMetrics
    mu           sync.Mutex
}
```

### Implemented Methods

**RateMonitor:**
- `NewRateMonitor(windowSize int) *RateMonitor`
- `RecordWrite(t time.Time)` - Add timestamp to ring buffer
- `CalculateRate(now time.Time, window time.Duration) float64` - Sliding window rate
- `Reset()` - Clear all timestamps
- `Size() int` - Current timestamp count

**ModeController:**
- `NewModeController(writeThroughMax, debouncedMax float64) *ModeController`
- `DetermineMode(writeRate float64) PersistMode` - Select mode based on rate
- `CalculateDebounceDelay(writeRate float64, minDelay, maxDelay time.Duration) time.Duration` - Linear interpolation

**AdaptivePersistence:**
- `NewAdaptivePersistence(config, memBuf, disk) (*AdaptivePersistence, error)`
- `NotifyWrite(lineIdx int64)` - Called when line changes
- `NotifyWriteBatch(lineIndices []int64)` - Batch notification
- `Flush() error` - Force immediate flush
- `Close() error` - Flush pending and stop monitor
- `CurrentMode() PersistMode`
- `Metrics() PersistenceMetrics`
- `PendingCount() int`
- `String() string` - Debug info

### Key Design Decisions Made

1. **DiskHistory Injection**: Constructor accepts `*DiskHistory` instead of creating internally. This enables:
   - Mock implementations for testing
   - Separation of concerns
   - Reuse of existing disk format (TXHIST03)

2. **Time Function Injection**: Internal constructor accepts `nowFunc` for deterministic testing without real timers.

3. **sync.Once for Double-Close Safety**: The `stopIdleMonitor()` method uses `sync.Once` to prevent panic on `close(stopCh)` if called multiple times.

4. **Extracted Helper Methods**: `updateRateAndModeLocked()` and `handleWriteLocked()` eliminate code duplication between `NotifyWrite` and `NotifyWriteBatch`.

5. **Adaptive Debouncing**: Delay scales linearly from `DebounceMinDelay` (50ms) to `DebounceMaxDelay` (500ms) based on write rate within the debounced range. Faster writes get longer delays to reduce I/O overhead.

6. **Background Idle Monitor**: Ticker-based goroutine checks for idle periods and flushes pending writes when the terminal becomes idle (default: 1 second).

7. **Error Handling**: Log and continue strategy - failed disk writes increment `FailedWrites` metric but don't stop processing. Lines remain pending for next flush attempt.

### Tests Implemented (26 tests, all passing including -race)

**RateMonitor Tests:**
- `TestRateMonitor_RecordAndCalculate`
- `TestRateMonitor_SlidingWindow`
- `TestRateMonitor_RingBufferWrap`
- `TestRateMonitor_Reset`
- `TestRateMonitor_EmptyBuffer`

**ModeController Tests:**
- `TestModeController_DetermineMode`
- `TestModeController_CalculateDebounceDelay`
- `TestModeController_LinearInterpolation`

**AdaptivePersistence Tests:**
- `TestAdaptivePersistence_WriteThroughMode`
- `TestAdaptivePersistence_DebouncedMode`
- `TestAdaptivePersistence_BestEffortMode`
- `TestAdaptivePersistence_ModeTransitions`
- `TestAdaptivePersistence_IdleFlush`
- `TestAdaptivePersistence_FlushOnClose`
- `TestAdaptivePersistence_EvictedLine`
- `TestAdaptivePersistence_NilMemBuf`
- `TestAdaptivePersistence_NilDisk`
- `TestAdaptivePersistence_Metrics`
- `TestAdaptivePersistence_String`
- `TestAdaptivePersistence_Concurrency`
- `TestAdaptivePersistence_DoubleClose`
- `TestAdaptivePersistence_NotifyAfterClose`
- `TestAdaptivePersistence_BatchNotify`
- `TestAdaptivePersistence_EmptyBatchNotify`
- `TestAdaptivePersistence_DefaultConfig`
- `TestPersistMode_String`

### Important Notes for Phase 4 (VTerm Integration)

1. **Integration Pattern**: After each write to MemoryBuffer, call:
   ```go
   persistence.NotifyWrite(memBuf.CursorLine())
   ```

2. **Initialization**: Create with injected DiskHistory:
   ```go
   disk, _ := parser.NewDiskHistory(path)
   persistence, _ := parser.NewAdaptivePersistence(config, memBuf, disk)
   defer persistence.Close()
   ```

3. **Shutdown**: Always call `Close()` to flush pending writes and stop the idle monitor.

4. **Thread Safety**: All AdaptivePersistence methods are thread-safe via mutex.

5. **Metrics Access**: Use `Metrics()` to monitor performance and mode changes.

### Files to Remove
None - this phase is additive.

---

### Original Design (Reference)

The following was the original plan specification. The implementation above follows this design with minor refinements for testability and thread safety.

### Original Types

```go
// PersistMode represents the current persistence strategy.
type PersistMode int

const (
    // PersistWriteThrough writes immediately (normal shell usage)
    PersistWriteThrough PersistMode = iota

    // PersistDebounced batches writes with a time window
    PersistDebounced

    // PersistBestEffort only flushes on idle or explicit request
    PersistBestEffort
)

// AdaptivePersistenceConfig holds configuration.
type AdaptivePersistenceConfig struct {
    // DiskPath for the history file
    DiskPath string

    // Rate thresholds for mode switching (writes per second)
    WriteThroughMaxRate  float64 // Below this: write-through (default: 10)
    DebouncedMaxRate     float64 // Below this: debounced (default: 100)
    // Above DebouncedMaxRate: best-effort

    // Debounce timing
    DebounceMinDelay time.Duration // Minimum delay (default: 50ms)
    DebounceMaxDelay time.Duration // Maximum delay (default: 500ms)

    // Idle detection for best-effort flush
    IdleThreshold time.Duration // Flush after this much idle time (default: 1s)

    // Ring buffer size for rate calculation
    RateWindowSize int // Number of timestamps to track (default: 1000)
}

// AdaptivePersistence manages disk writes with dynamic rate adjustment.
type AdaptivePersistence struct {
    config AdaptivePersistenceConfig
    memBuf *MemoryBuffer
    disk   *DiskHistory

    // Rate monitoring - ring buffer of recent write times
    writeTimestamps []time.Time
    timestampIdx    int
    timestampCount  int

    currentMode PersistMode

    // Debounce state
    pendingLines  map[int64]bool // Line indices pending flush
    flushTimer    *time.Timer
    lastActivity  time.Time

    // Idle monitor
    idleTicker *time.Ticker
    stopCh     chan struct{}

    // Metrics
    metrics PersistenceMetrics

    mu sync.Mutex
}

// PersistenceMetrics tracks performance data.
type PersistenceMetrics struct {
    TotalWrites      int64
    TotalFlushes     int64
    LinesWritten     int64
    ModeChanges      int64
    CurrentMode      PersistMode
    CurrentWriteRate float64
}
```

### Methods to Implement

```go
// Construction
func NewAdaptivePersistence(config AdaptivePersistenceConfig, memBuf *MemoryBuffer) (*AdaptivePersistence, error)
func DefaultAdaptivePersistenceConfig(diskPath string) AdaptivePersistenceConfig

// Core operations
func (ap *AdaptivePersistence) NotifyWrite(lineIdx int64) // Called when line changes
func (ap *AdaptivePersistence) NotifyWriteBatch(lineIndices []int64) // Batch notification
func (ap *AdaptivePersistence) Flush() error // Force immediate flush
func (ap *AdaptivePersistence) Close() error // Flush and close

// Rate monitoring
func (ap *AdaptivePersistence) calculateWriteRate() float64
func (ap *AdaptivePersistence) recordWriteTimestamp()
func (ap *AdaptivePersistence) adjustMode()

// Internal flush operations
func (ap *AdaptivePersistence) flushPending() error
func (ap *AdaptivePersistence) flushLine(lineIdx int64) error
func (ap *AdaptivePersistence) scheduleFlush(delay time.Duration)
func (ap *AdaptivePersistence) cancelScheduledFlush()

// Idle monitoring
func (ap *AdaptivePersistence) startIdleMonitor()
func (ap *AdaptivePersistence) stopIdleMonitor()
func (ap *AdaptivePersistence) onIdle()

// Status
func (ap *AdaptivePersistence) CurrentMode() PersistMode
func (ap *AdaptivePersistence) Metrics() PersistenceMetrics
func (ap *AdaptivePersistence) String() string // For debugging
```

### Rate Monitoring Algorithm

```go
func (ap *AdaptivePersistence) calculateWriteRate() float64 {
    now := time.Now()
    window := time.Second // 1-second sliding window
    cutoff := now.Add(-window)

    count := 0
    for i := 0; i < ap.timestampCount; i++ {
        idx := (ap.timestampIdx - 1 - i + len(ap.writeTimestamps)) % len(ap.writeTimestamps)
        if ap.writeTimestamps[idx].After(cutoff) {
            count++
        } else {
            break // Timestamps are in order, can stop early
        }
    }

    return float64(count)
}

func (ap *AdaptivePersistence) adjustMode() {
    rate := ap.calculateWriteRate()
    oldMode := ap.currentMode

    switch {
    case rate < ap.config.WriteThroughMaxRate:
        ap.currentMode = PersistWriteThrough
    case rate < ap.config.DebouncedMaxRate:
        ap.currentMode = PersistDebounced
    default:
        ap.currentMode = PersistBestEffort
    }

    if oldMode != ap.currentMode {
        ap.metrics.ModeChanges++
    }
    ap.metrics.CurrentMode = ap.currentMode
    ap.metrics.CurrentWriteRate = rate
}
```

### Debounce Logic

```go
func (ap *AdaptivePersistence) NotifyWrite(lineIdx int64) {
    ap.mu.Lock()
    defer ap.mu.Unlock()

    ap.metrics.TotalWrites++
    ap.recordWriteTimestamp()
    ap.adjustMode()

    switch ap.currentMode {
    case PersistWriteThrough:
        ap.flushLine(lineIdx)

    case PersistDebounced:
        ap.pendingLines[lineIdx] = true
        delay := ap.calculateDebounceDelay()
        ap.scheduleFlush(delay)

    case PersistBestEffort:
        ap.pendingLines[lineIdx] = true
        // Flush handled by idle monitor
    }

    ap.lastActivity = time.Now()
}

func (ap *AdaptivePersistence) calculateDebounceDelay() time.Duration {
    // Scale delay based on write rate
    rate := ap.metrics.CurrentWriteRate
    if rate <= ap.config.WriteThroughMaxRate {
        return ap.config.DebounceMinDelay
    }

    // Linear interpolation between min and max delay
    ratio := (rate - ap.config.WriteThroughMaxRate) /
             (ap.config.DebouncedMaxRate - ap.config.WriteThroughMaxRate)
    if ratio > 1 {
        ratio = 1
    }

    delay := ap.config.DebounceMinDelay +
             time.Duration(float64(ap.config.DebounceMaxDelay-ap.config.DebounceMinDelay)*ratio)
    return delay
}
```

### Disk Format

Keep TXHIST03 format - it already supports FixedWidth per line. No changes needed unless we find limitations.

### Tests to Write

- `TestAdaptivePersistence_WriteThroughMode`
- `TestAdaptivePersistence_DebouncedMode`
- `TestAdaptivePersistence_BestEffortMode`
- `TestAdaptivePersistence_ModeTransitions`
- `TestAdaptivePersistence_RateCalculation`
- `TestAdaptivePersistence_IdleFlush`
- `TestAdaptivePersistence_FlushOnClose`

### Files to Remove
None - this phase is additive.

---

## Phase 3: ViewportWindow (Pure View)

### Goal
Read-only view layer that renders memory buffer to terminal-sized grid.

### New File
`apps/texelterm/parser/viewport_window.go`

### Types

```go
// physicalLine represents a single rendered line in the viewport.
type physicalLine struct {
    cells      []Cell
    lineIdx    int64 // Global line index in MemoryBuffer
    charOffset int   // Character offset within logical line
}

// ViewportWindow provides a terminal-sized view into the MemoryBuffer.
// It handles scrolling, wrapping, and fixed-width line rendering.
// This is a pure view layer - no write operations.
type ViewportWindow struct {
    memBuf *MemoryBuffer

    // Viewport dimensions
    width, height int

    // Scroll position: how many physical lines from the bottom
    // 0 = live edge (showing most recent content)
    scrollOffset int64

    // Cache for the current view
    cachedGrid      [][]Cell
    cachedPhysical  []physicalLine // For coordinate conversion
    cacheValid      bool
    cacheScrollPos  int64
    cacheWidth      int
    cacheContentVer int64 // Version counter for content changes

    // Content version from MemoryBuffer (for cache invalidation)
    lastContentVer int64

    mu sync.RWMutex
}
```

### Methods to Implement

```go
// Construction
func NewViewportWindow(memBuf *MemoryBuffer, width, height int) *ViewportWindow

// Main render method
func (vw *ViewportWindow) GetVisibleGrid() [][]Cell

// Scrolling
func (vw *ViewportWindow) ScrollUp(lines int) int    // Returns actual scrolled
func (vw *ViewportWindow) ScrollDown(lines int) int  // Returns actual scrolled
func (vw *ViewportWindow) ScrollToBottom()           // Return to live edge
func (vw *ViewportWindow) ScrollToTop()              // Scroll to oldest content

// Resize
func (vw *ViewportWindow) Resize(width, height int)
func (vw *ViewportWindow) Width() int
func (vw *ViewportWindow) Height() int

// Status
func (vw *ViewportWindow) IsAtLiveEdge() bool
func (vw *ViewportWindow) CanScrollUp() bool
func (vw *ViewportWindow) CanScrollDown() bool
func (vw *ViewportWindow) ScrollOffset() int64
func (vw *ViewportWindow) TotalPhysicalLines() int // At current width

// Coordinate conversion (for selection, mouse clicks)
func (vw *ViewportWindow) ViewportToContent(y, x int) (lineIdx int64, charOffset int, ok bool)
func (vw *ViewportWindow) ContentToViewport(lineIdx int64, charOffset int) (y, x int, visible bool)

// Cache management
func (vw *ViewportWindow) InvalidateCache()
func (vw *ViewportWindow) isCacheValid() bool

// Internal
func (vw *ViewportWindow) buildPhysicalLines() []physicalLine
func (vw *ViewportWindow) wrapLine(line *LogicalLine, lineIdx int64) []physicalLine
func (vw *ViewportWindow) calculateMaxScroll() int64
```

### Wrapping Logic

```go
func (vw *ViewportWindow) wrapLine(line *LogicalLine, lineIdx int64) []physicalLine {
    if line == nil {
        return []physicalLine{{
            cells:      makeEmptyRow(vw.width),
            lineIdx:    lineIdx,
            charOffset: 0,
        }}
    }

    // Fixed-width lines: truncate/pad, don't wrap
    if line.FixedWidth > 0 {
        clipped := line.ClipOrPadToWidth(vw.width)
        return []physicalLine{{
            cells:      clipped.Cells,
            lineIdx:    lineIdx,
            charOffset: 0,
        }}
    }

    // Empty line
    if len(line.Cells) == 0 {
        return []physicalLine{{
            cells:      make([]Cell, 0),
            lineIdx:    lineIdx,
            charOffset: 0,
        }}
    }

    // Normal lines: wrap to viewport width
    wrapped := line.WrapToWidth(vw.width)
    result := make([]physicalLine, len(wrapped))
    for i, pl := range wrapped {
        result[i] = physicalLine{
            cells:      pl.Cells,
            lineIdx:    lineIdx,
            charOffset: pl.Offset,
        }
    }
    return result
}
```

### Grid Building

```go
func (vw *ViewportWindow) GetVisibleGrid() [][]Cell {
    vw.mu.Lock()
    defer vw.mu.Unlock()

    if vw.isCacheValid() {
        return vw.cachedGrid
    }

    // Build all physical lines
    physicalLines := vw.buildPhysicalLines()
    totalPhysical := len(physicalLines)

    // Allocate grid
    grid := make([][]Cell, vw.height)
    for y := range grid {
        grid[y] = makeEmptyRow(vw.width)
    }

    // Calculate visible window
    // scrollOffset is lines from bottom, 0 = live edge
    endIdx := totalPhysical - int(vw.scrollOffset)
    startIdx := endIdx - vw.height
    if startIdx < 0 {
        startIdx = 0
    }
    if endIdx < 0 {
        endIdx = 0
    }

    // Copy visible lines to grid
    gridY := 0
    for i := startIdx; i < endIdx && gridY < vw.height; i++ {
        if i >= 0 && i < totalPhysical {
            pl := physicalLines[i]
            for x := 0; x < vw.width && x < len(pl.cells); x++ {
                grid[gridY][x] = pl.cells[x]
            }
        }
        gridY++
    }

    // Cache results
    vw.cachedGrid = grid
    vw.cachedPhysical = physicalLines
    vw.cacheValid = true
    vw.cacheScrollPos = vw.scrollOffset
    vw.cacheWidth = vw.width

    return grid
}
```

### Tests to Write

- `TestViewportWindow_BasicRendering`
- `TestViewportWindow_Scrolling`
- `TestViewportWindow_Resize`
- `TestViewportWindow_FixedWidthLines`
- `TestViewportWindow_CoordinateConversion`
- `TestViewportWindow_CacheInvalidation`
- `TestViewportWindow_EmptyBuffer`
- `TestViewportWindow_WrapUnwrap`

### Files to Remove
None - this phase is additive.

---

## Phase 4: VTerm Integration ✅ COMPLETED

> **Status**: Implemented on 2026-01-27
> **Files**:
> - `apps/texelterm/parser/vterm_memory_buffer.go` (~580 lines) - Main integration bridge
> - `apps/texelterm/parser/vterm_memory_buffer_test.go` (~388 lines, 13 test functions)
> - Modified: `vterm.go`, `vterm_scroll.go`, `vterm_erase.go`, `term.go`

### Goal
Wire VTerm to write to MemoryBuffer instead of ViewportState.

### Implementation Summary

**Architecture Chosen: Integration Bridge Pattern**

The implementation creates a `memoryBufferState` struct that sits alongside the existing `displayBufferState`, allowing both systems to coexist. The new system is opt-in via config flag `use_memory_buffer`.

**Key Design Decisions:**

1. **Clean Coexistence**: New `memBufState *memoryBufferState` field in VTerm alongside existing `displayBuf *displayBufferState`. All operations check `IsMemoryBufferEnabled()` first.

2. **Config-Based Opt-In**: Enabled via `texelterm.use_memory_buffer = true` in config. This allows gradual migration and easy rollback.

3. **Stub for Phase 5**: `FixedWidthDetector` stub included but not wired - will be completed in Phase 5.

4. **DiskHistory Integration**: Uses `CreateDiskHistory(config)` with proper config struct, not simple constructor.

5. **Global Line Index Mapping**: `memoryBufferState` tracks `viewportTopLine` to convert between viewport Y coordinates and global line indices.

**Files Created:**

- `apps/texelterm/parser/vterm_memory_buffer.go` (~580 lines)
  - `memoryBufferState` struct holding MemoryBuffer, ViewportWindow, AdaptivePersistence
  - `MemoryBufferOptions` configuration struct
  - All integration methods: `initMemoryBuffer`, `EnableMemoryBuffer`, `EnableMemoryBufferWithDisk`
  - Character writing: `memoryBufferPlaceCharWide`, `memoryBufferWriteCharWithWrapping`
  - Grid rendering: `memoryBufferGrid`
  - Scrolling: `memoryBufferScroll`, `memoryBufferScrollToBottom`, `memoryBufferLineFeed`
  - Erase operations: `memoryBufferEraseScreen`, `memoryBufferEraseToEndOfLine`, etc.
  - Scroll region: `memoryBufferScrollRegion`
  - Resize: `memoryBufferResize`
  - Cleanup: `CloseMemoryBuffer`

**Files Modified:**

- `apps/texelterm/parser/vterm.go`
  - Added `memBufState *memoryBufferState` field to VTerm struct
  - Added constructor options: `WithMemoryBuffer()`, `WithMemoryBufferDisk()`, `WithMemoryBufferOptions()`
  - Modified `Grid()` to check `IsMemoryBufferEnabled()` first
  - Modified `Resize()` to call `memoryBufferResize()` when enabled
  - Modified `writeCharWithWrapping()` to use new system

- `apps/texelterm/parser/vterm_scroll.go`
  - `lineFeedInternal()` now has three paths: altScreen, memoryBuffer, displayBuffer
  - `scrollRegion()` calls `memoryBufferScrollRegion()` when enabled
  - `Scroll()` calls `memoryBufferScroll()` when enabled

- `apps/texelterm/parser/vterm_erase.go`
  - `ClearScreenMode()` calls `memoryBufferEraseScreen()` when enabled
  - `ClearLine()` calls memory buffer erase methods when enabled
  - `EraseCharacters()` calls `memoryBufferEraseCharacters()` when enabled

- `apps/texelterm/term.go`
  - Added `initializeMemoryBufferLocked()` function
  - Modified `initializeVTermFirstRun()` to check `use_memory_buffer` config flag

- `apps/texelterm/parser/vterm_display_buffer.go`
  - Added deprecation notice in header comment

### Tests Implemented (13 tests, all passing)

- `TestVTerm_MemoryBufferBasicWrite` - Character writing to buffer
- `TestVTerm_MemoryBufferLineFeed` - Line feed behavior
- `TestVTerm_MemoryBufferScrollRegion` - Scroll region operations
- `TestVTerm_MemoryBufferEraseLine` - Line erase (EL)
- `TestVTerm_MemoryBufferEraseScreen` - Screen erase (ED)
- `TestVTerm_MemoryBufferUserScroll` - User scrollback navigation
- `TestVTerm_MemoryBufferResize` - Terminal resize handling
- `TestVTerm_MemoryBufferCursorTracking` - Cursor position tracking
- `TestVTerm_MemoryBufferWideCharacter` - Wide character handling
- `TestVTerm_MemoryBufferWithDisk` - Disk persistence integration
- `TestVTerm_MemoryBufferNotEnabledByDefault` - Opt-in verification
- `TestVTerm_MemoryBufferGridDimensions` - Grid dimension verification
- `TestVTerm_MemoryBufferTotalLines` - Line counting

### Important Notes for Phase 5 (FixedWidthDetector)

1. **Stub Ready**: `FixedWidthDetector` stub exists in `vterm_memory_buffer.go` but is not wired.

2. **Integration Points**: When implementing Phase 5, wire detector in:
   - `memoryBufferPlaceCharWide()` - Call `detector.OnWrite()`
   - `memoryBufferScrollRegion()` - Call `detector.OnScrollRegionSet()`
   - Cursor movement methods - Call `detector.OnCursorMove()`

3. **Configuration**: Add detector config to `MemoryBufferOptions` struct.

### Files to Remove After Phase 6
- Begin deprecating `vterm_display_buffer.go` (marked as deprecated)

### Original Design (Reference)

The following was the original plan specification. The implementation follows this design with adjustments for clean coexistence with the old system.

### Modified Files
- `apps/texelterm/parser/vterm.go`
- `apps/texelterm/parser/vterm_display_buffer.go`
- `apps/texelterm/term.go`

### Changes to VTerm

```go
type VTerm struct {
    // ... existing fields ...

    // NEW: memory buffer for content storage
    memBuf *MemoryBuffer

    // NEW: viewport window for rendering
    viewportWin *ViewportWindow

    // NEW: adaptive persistence
    persistence *AdaptivePersistence

    // NEW: fixed-width detector
    fixedDetector *FixedWidthDetector

    // DEPRECATED: old display buffer (remove after migration)
    // displayBuffer *DisplayBuffer
}
```

### Key Method Changes

```go
// Character writing now goes to MemoryBuffer
func (v *VTerm) writeChar(r rune) {
    if v.memBuf != nil {
        v.memBuf.Write(r, v.currentFG, v.currentBG, v.currentAttr)

        // Notify persistence
        if v.persistence != nil {
            v.persistence.NotifyWrite(v.memBuf.CursorLine())
        }

        // Notify fixed-width detector
        if v.fixedDetector != nil {
            v.fixedDetector.OnWrite(v.memBuf.CursorLine(), v.width)
        }
    }

    // Mark dirty for rendering
    v.MarkDirty(v.cursorY)
}

// Grid() returns from ViewportWindow
func (v *VTerm) Grid() [][]Cell {
    if v.viewportWin != nil {
        return v.viewportWin.GetVisibleGrid()
    }
    // Fallback removed - viewportWin is required
    return nil
}

// Cursor movement updates MemoryBuffer cursor
func (v *VTerm) setCursorPos(x, y int) {
    v.cursorX = x
    v.cursorY = y

    if v.memBuf != nil {
        // Convert viewport position to global line index
        lineIdx := v.viewportYToGlobalLine(y)
        v.memBuf.SetCursor(lineIdx, x)
    }

    // Notify fixed-width detector
    if v.fixedDetector != nil {
        v.fixedDetector.OnCursorMove(y)
    }
}

// Scroll region handling
func (v *VTerm) setScrollRegion(top, bottom int) {
    v.scrollTop = top
    v.scrollBottom = bottom

    if v.fixedDetector != nil {
        v.fixedDetector.OnScrollRegionSet(top, bottom, v.height)
    }
}

// Scrollback navigation
func (v *VTerm) ScrollViewUp(lines int) int {
    if v.viewportWin != nil {
        return v.viewportWin.ScrollUp(lines)
    }
    return 0
}

func (v *VTerm) ScrollViewDown(lines int) int {
    if v.viewportWin != nil {
        return v.viewportWin.ScrollDown(lines)
    }
    return 0
}
```

### Initialization Changes (term.go)

```go
func (t *TexelTerm) initVTerm() {
    // Create memory buffer
    mbConfig := parser.DefaultMemoryBufferConfig()
    mbConfig.MaxLines = t.config.MaxScrollbackLines // From config
    t.memBuf = parser.NewMemoryBuffer(mbConfig)

    // Create viewport window
    t.viewportWin = parser.NewViewportWindow(t.memBuf, t.width, t.height)

    // Create adaptive persistence
    if t.config.HistoryPath != "" {
        apConfig := parser.DefaultAdaptivePersistenceConfig(t.config.HistoryPath)
        var err error
        t.persistence, err = parser.NewAdaptivePersistence(apConfig, t.memBuf)
        if err != nil {
            log.Printf("Warning: could not create persistence: %v", err)
        }
    }

    // Create fixed-width detector
    t.fixedDetector = parser.NewFixedWidthDetector(t.memBuf)

    // Create VTerm with new components
    t.vterm = parser.NewVTerm(t.width, t.height,
        parser.WithMemoryBuffer(t.memBuf),
        parser.WithViewportWindow(t.viewportWin),
        parser.WithPersistence(t.persistence),
        parser.WithFixedWidthDetector(t.fixedDetector),
    )
}
```

### Tests to Write

- `TestVTerm_WritesToMemoryBuffer`
- `TestVTerm_GridFromViewportWindow`
- `TestVTerm_ScrollNavigation`
- `TestVTerm_CursorTracking`
- `TestVTerm_PersistenceNotification`

### Files to Remove After Phase 4
- Begin deprecating `vterm_display_buffer.go` (mark methods as deprecated)

---

## Phase 5: Fixed-Width Detection

### Goal
Automatically detect and flag lines that shouldn't reflow.

### New File
`apps/texelterm/parser/fixed_width_detector.go`

### Types

```go
// FixedWidthDetector tracks TUI patterns and flags lines as fixed-width.
type FixedWidthDetector struct {
    memBuf *MemoryBuffer

    // Scroll region state
    inScrollRegion bool
    scrollTop      int
    scrollBottom   int
    termHeight     int

    // Cursor jump tracking
    lastCursorY      int
    consecutiveJumps int
    jumpThreshold    int // How many jumps to trigger TUI detection (default: 2)

    // Mode state
    cursorHidden bool

    // Configuration
    config FixedWidthDetectorConfig
}

// FixedWidthDetectorConfig holds configuration.
type FixedWidthDetectorConfig struct {
    // JumpThreshold: consecutive cursor jumps before flagging as TUI
    JumpThreshold int // Default: 2

    // MinJumpDistance: minimum rows to count as a "jump"
    MinJumpDistance int // Default: 2

    // EnableBoxDrawingDetection: flag lines with box-drawing chars
    EnableBoxDrawingDetection bool // Default: false (too many false positives)
}
```

### Methods to Implement

```go
// Construction
func NewFixedWidthDetector(memBuf *MemoryBuffer) *FixedWidthDetector
func NewFixedWidthDetectorWithConfig(memBuf *MemoryBuffer, config FixedWidthDetectorConfig) *FixedWidthDetector
func DefaultFixedWidthDetectorConfig() FixedWidthDetectorConfig

// Event handlers (called by VTerm)
func (d *FixedWidthDetector) OnCursorMove(newY int)
func (d *FixedWidthDetector) OnWrite(lineIdx int64, width int)
func (d *FixedWidthDetector) OnScrollRegionSet(top, bottom, height int)
func (d *FixedWidthDetector) OnScrollRegionClear()
func (d *FixedWidthDetector) OnCursorVisibilityChange(hidden bool)
func (d *FixedWidthDetector) OnResize(width, height int)

// Manual control
func (d *FixedWidthDetector) ForceFixedWidth(lineIdx int64, width int)
func (d *FixedWidthDetector) ClearFixedWidth(lineIdx int64)

// Status
func (d *FixedWidthDetector) IsInTUIMode() bool
func (d *FixedWidthDetector) String() string // Debug info
```

### Detection Logic

```go
func (d *FixedWidthDetector) OnCursorMove(newY int) {
    jump := abs(newY - d.lastCursorY)

    if jump >= d.config.MinJumpDistance {
        d.consecutiveJumps++
        if d.consecutiveJumps >= d.config.JumpThreshold {
            // Likely TUI mode - flag current line
            if d.memBuf != nil {
                lineIdx := d.memBuf.CursorLine()
                width := d.memBuf.TermWidth()
                d.memBuf.SetLineFixed(lineIdx, width)
            }
        }
    } else if jump == 1 || jump == 0 {
        // Normal movement (including LF) - reset jump counter
        d.consecutiveJumps = 0
    }

    d.lastCursorY = newY
}

func (d *FixedWidthDetector) OnScrollRegionSet(top, bottom, height int) {
    // Full-screen scroll region is normal (not TUI)
    if top == 0 && bottom == height-1 {
        d.inScrollRegion = false
        return
    }

    // Non-full-screen scroll region = TUI mode
    d.inScrollRegion = true
    d.scrollTop = top
    d.scrollBottom = bottom
    d.termHeight = height
}

func (d *FixedWidthDetector) OnWrite(lineIdx int64, width int) {
    if d.inScrollRegion {
        // Any write during scroll region is TUI content
        d.memBuf.SetLineFixed(lineIdx, width)
    }
}

func (d *FixedWidthDetector) OnCursorVisibilityChange(hidden bool) {
    d.cursorHidden = hidden
    // Cursor hiding often indicates TUI, but not always
    // Use as supporting signal, not primary trigger
}
```

### Tests to Write

- `TestFixedWidthDetector_ScrollRegion`
- `TestFixedWidthDetector_CursorJumps`
- `TestFixedWidthDetector_NormalUsage`
- `TestFixedWidthDetector_MixedContent`
- `TestFixedWidthDetector_Resize`

### Files to Remove After Phase 5
- `apps/texelterm/parser/tui_viewport_manager.go`
- `apps/texelterm/parser/tui_mode.go` (if exists)
- `apps/texelterm/parser/freeze_tracker.go` (if exists)
- Any `vterm_tui_*.go` files that are obsolete

---

## Phase 6: Cleanup and Final Integration

### Goal
Remove all old code, ensure clean architecture.

### Files to Remove

1. **viewport_state.go** - Replaced by ViewportWindow + MemoryBuffer
2. **display_buffer.go** - Replaced by MemoryBuffer + ViewportWindow
3. **tui_viewport_manager.go** - Replaced by per-line flags
4. **vterm_display_buffer.go** - Integration code for old system
5. **scrollback_history.go** - Functionality absorbed into MemoryBuffer + AdaptivePersistence

### Files to Modify

1. **vterm.go** - Remove all old viewport/display buffer references
2. **term.go** - Update initialization to only use new system
3. **vterm_scroll.go** - Update to use MemoryBuffer for scroll operations
4. **vterm_erase.go** - Update erase operations to use MemoryBuffer

### Final File Structure

```
apps/texelterm/parser/
├── memory_buffer.go          # Central storage (NEW)
├── adaptive_persistence.go   # Dynamic disk writes (NEW)
├── viewport_window.go        # Pure view layer (NEW)
├── fixed_width_detector.go   # TUI detection (NEW)
├── disk_history.go           # Disk format (unchanged)
├── logical_line.go           # Line representation (unchanged)
├── cell.go                   # Cell representation (unchanged)
├── color.go                  # Color types (unchanged)
├── vterm.go                  # Terminal state machine (modified)
├── vterm_cursor.go           # Cursor operations (modified)
├── vterm_scroll.go           # Scroll operations (modified)
├── vterm_erase.go            # Erase operations (modified)
├── vterm_input.go            # Input handling (unchanged)
├── vterm_osc.go              # OSC sequences (unchanged)
├── vterm_appearance.go       # Dirty tracking (modified)
└── ... other vterm_*.go files
```

### Cleanup Checklist

- [ ] Remove all `DisplayBuffer` references from VTerm
- [ ] Remove all `ViewportState` references from VTerm
- [ ] Remove all `ScrollbackHistory` direct usage (use MemoryBuffer)
- [ ] Remove TUI mode debouncing (replaced by per-line flags)
- [ ] Remove frozen lines tracking (replaced by FixedWidth flag)
- [ ] Update all tests to use new architecture
- [ ] Remove dead code and unused imports
- [ ] Run `gofmt` and `go vet`
- [ ] Verify all existing functionality works

### Final Integration Tests

- Full terminal session simulation
- TUI app (vim, htop) rendering and scrollback
- Shell output with resize and reflow
- High-throughput write scenario (video player)
- Session persistence and restore
- Long-running session with eviction

---

## Performance Targets

| Operation | Target | Notes |
|-----------|--------|-------|
| Write single char | < 1μs | Hot path |
| Get visible grid | < 1ms | Called 60fps |
| Resize reflow | < 10ms | Visible lines only |
| Scroll by 1 line | < 100μs | Cache invalidation |
| Disk flush (batch) | < 10ms | Background, debounced |

---

## Configuration Options

### MemoryBuffer

```go
MemoryBufferConfig{
    MaxLines:      50000,  // Configurable via settings
    EvictionBatch: 1000,   // Lines to evict at once
}
```

### AdaptivePersistence

```go
AdaptivePersistenceConfig{
    DiskPath:             "/path/to/history",
    WriteThroughMaxRate:  10,    // writes/sec
    DebouncedMaxRate:     100,   // writes/sec
    DebounceMinDelay:     50 * time.Millisecond,
    DebounceMaxDelay:     500 * time.Millisecond,
    IdleThreshold:        1 * time.Second,
    RateWindowSize:       1000,
}
```

### FixedWidthDetector

```go
FixedWidthDetectorConfig{
    JumpThreshold:             2,
    MinJumpDistance:           2,
    EnableBoxDrawingDetection: false,
}
```

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Performance regression | Benchmark before/after each phase |
| Visual bugs | Use testutil reference comparison |
| Data loss | Test persistence thoroughly, add recovery |
| Memory leaks | Profile with pprof, test long sessions |
| Race conditions | Use -race flag in tests |

---

## Success Criteria

1. All existing tests pass
2. No visual regressions (verified with reference comparisons)
3. Performance meets targets
4. Code is cleaner (fewer lines, simpler abstractions)
5. TUI content preserved correctly in scrollback
6. Adaptive persistence works under various loads
