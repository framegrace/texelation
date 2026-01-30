# Disk Layer Architecture for Texelterm

## Executive Summary

This document describes a modern, future-proof disk storage layer for texelterm's scrollback history. The design supports:

- **Infinite history** (disk-limited only)
- **Full-text search** with command prioritization
- **Per-line timestamps** for time-based navigation
- **Zstandard compression** for archived pages
- **System keyring encryption** for sensitive content
- **WAL-style journaling** for crash recovery
- **Cross-terminal search** capability

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              MEMORY LAYER                                    │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                    MemoryBuffer (existing)                               ││
│  │         Ring buffer of ~50K LogicalLines with global indexing           ││
│  │         Dirty tracking, cursor management, content versioning            ││
│  └─────────────────────────────────────────────────────────────────────────┘│
│                                    ↓ writes                                  │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                     WriteAheadLog (NEW)                                  ││
│  │         Append-only WAL for crash recovery of live pages                ││
│  │         Checkpointed on clean shutdown or periodic flush                 ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
                                      ↓ checkpoint
┌─────────────────────────────────────────────────────────────────────────────┐
│                              PAGE LAYER                                      │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                      PageStore (NEW)                                     ││
│  │         64KB pages of LogicalLines + per-line timestamps                ││
│  │         Page states: LIVE → WARM → FROZEN                               ││
│  │         Directory-based storage: pages/<terminal-id>/<page-id>.page     ││
│  └─────────────────────────────────────────────────────────────────────────┘│
│                                    ↓ freeze (background)                     │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                      ArchiveManager (NEW)                                ││
│  │         Zstd-compressed frozen pages with optional encryption            ││
│  │         Read-only after freezing, content-addressable naming             ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
                                      ↓ index
┌─────────────────────────────────────────────────────────────────────────────┐
│                              INDEX LAYER                                     │
│  ┌──────────────────────────────┐  ┌──────────────────────────────────────┐ │
│  │    TerminalIndex (SQLite)    │  │     GlobalIndex (SQLite) [optional]  │ │
│  │    Per-terminal FTS5 index   │  │     Cross-terminal search index      │ │
│  │    Commands indexed sync     │  │     Federated queries                │ │
│  │    Output indexed async      │  │                                      │ │
│  └──────────────────────────────┘  └──────────────────────────────────────┘ │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                      TimeIndex (NEW)                                     ││
│  │         B-tree of timestamp → (page_id, line_offset) mappings           ││
│  │         Enables "jump to time" navigation                               ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Component Details

### 1. WriteAheadLog (WAL)

**Purpose**: Ensure no data loss on crash by journaling writes before they're committed to pages.

**File Format**: `<terminal-id>.wal`

```
┌─────────────────────────────────────────────────────────────────┐
│ WAL Header (32 bytes)                                           │
├─────────────────────────────────────────────────────────────────┤
│ Magic: "TXWAL001" (8 bytes)                                     │
│ Version: uint32 (4 bytes)                                       │
│ TerminalID: UUID (16 bytes)                                     │
│ LastCheckpoint: uint64 (8 bytes) - global line index            │
├─────────────────────────────────────────────────────────────────┤
│ WAL Entry (variable, repeated)                                  │
├─────────────────────────────────────────────────────────────────┤
│ EntryType: uint8 (1 byte)                                       │
│   - 0x01: LINE_WRITE                                            │
│   - 0x02: LINE_MODIFY                                           │
│   - 0x03: CHECKPOINT                                            │
│ GlobalLineIdx: uint64 (8 bytes)                                 │
│ Timestamp: int64 (8 bytes) - UnixNano                           │
│ DataLen: uint32 (4 bytes)                                       │
│ Data: [DataLen]byte - serialized LogicalLine                    │
│ CRC32: uint32 (4 bytes)                                         │
└─────────────────────────────────────────────────────────────────┘
```

**Operations**:
- `Append(lineIdx, line, timestamp)` - Journal a line write
- `Checkpoint()` - Mark current position as durable (pages flushed)
- `Recover()` - Replay entries after last checkpoint on startup
- `Truncate()` - Remove entries before checkpoint

**Integration with AdaptivePersistence**:
- WAL receives all `NotifyWrite()` calls
- Replaces direct `DiskHistory.AppendLine()` calls
- Checkpoints trigger page writes

---

### 2. PageStore

**Purpose**: Organize content into 64KB pages with metadata for efficient access and freezing.

**Directory Structure**:
```
~/.local/share/texelation/history/
├── terminals/
│   └── <terminal-uuid>/
│       ├── manifest.json          # Terminal metadata
│       ├── wal.log               # Write-ahead log
│       ├── pages/
│       │   ├── 00000001.page     # Live/warm pages
│       │   ├── 00000002.page
│       │   └── ...
│       ├── archive/
│       │   ├── 00000001.zst      # Frozen compressed pages
│       │   └── ...
│       └── index.db              # SQLite FTS5 index
└── global/
    └── search.db                 # Cross-terminal index (optional)
```

**Page Format** (64KB target, uncompressed):

```
┌─────────────────────────────────────────────────────────────────┐
│ Page Header (64 bytes)                                          │
├─────────────────────────────────────────────────────────────────┤
│ Magic: "TXPAGE01" (8 bytes)                                     │
│ Version: uint32 (4 bytes)                                       │
│ PageID: uint64 (8 bytes)                                        │
│ State: uint8 (1 byte) - LIVE=0, WARM=1, FROZEN=2               │
│ Flags: uint8 (1 byte) - ENCRYPTED=0x01, COMPRESSED=0x02        │
│ LineCount: uint32 (4 bytes)                                     │
│ FirstGlobalIdx: uint64 (8 bytes) - first line's global index   │
│ FirstTimestamp: int64 (8 bytes) - UnixNano                      │
│ LastTimestamp: int64 (8 bytes) - UnixNano                       │
│ UncompressedSize: uint32 (4 bytes)                              │
│ CompressedSize: uint32 (4 bytes) - 0 if not compressed         │
│ Reserved: [6]byte                                               │
├─────────────────────────────────────────────────────────────────┤
│ Line Index (LineCount * 16 bytes)                               │
├─────────────────────────────────────────────────────────────────┤
│ Per-line entry:                                                 │
│   Offset: uint32 (4 bytes) - offset into data section          │
│   Timestamp: int64 (8 bytes) - UnixNano                         │
│   Flags: uint16 (2 bytes) - IS_COMMAND=0x01, FIXED_WIDTH=0x02  │
│   Reserved: uint16 (2 bytes)                                    │
├─────────────────────────────────────────────────────────────────┤
│ Line Data (variable)                                            │
├─────────────────────────────────────────────────────────────────┤
│ Per-line:                                                       │
│   CellCount: uint32 (4 bytes)                                   │
│   FixedWidth: uint32 (4 bytes)                                  │
│   Cells: [CellCount * 16 bytes] - same as current format       │
└─────────────────────────────────────────────────────────────────┘
```

**Page States**:

| State | Description | Storage | Modifiable |
|-------|-------------|---------|------------|
| LIVE | Recently written, may still be modified | Uncompressed `.page` file | Yes |
| WARM | Off-screen but within scroll range | Uncompressed `.page` file | Yes (rare) |
| FROZEN | Far from scroll range, archived | Compressed `.zst` file | No |

**Page Lifecycle**:
```
                    WAL checkpoint
NEW LINE ─────────────────────────────► LIVE PAGE
                                           │
                    scroll away            │
                    (in scroll range)      ▼
                                        WARM PAGE
                                           │
                    background freeze      │
                    (far from scroll)      ▼
                                       FROZEN PAGE
                                       (compressed)
```

**Freeze Criteria**:
- Page is entirely off-screen
- No line accessed in last 30 seconds
- Page is at least 2 * viewport_height lines behind scroll position
- Background freezer has idle CPU

---

### 3. ArchiveManager

**Purpose**: Compress and optionally encrypt frozen pages.

**Compression**: Zstandard with dictionary

```go
type ArchiveManager struct {
    zstdEncoder *zstd.Encoder
    zstdDecoder *zstd.Decoder
    dictionary  []byte        // Trained on terminal content
    keyring     KeyringClient // System keyring integration
}
```

**Zstd Dictionary**:
- Pre-trained dictionary for terminal content (ANSI escapes, common commands, paths)
- Improves compression ratio by 20-40% for small pages
- Shipped with binary or generated from user's history

**Encryption Flow**:
```
┌──────────────┐     ┌─────────────────┐     ┌──────────────────┐
│  Plain Page  │ ──► │ Zstd Compress   │ ──► │ ChaCha20-Poly1305│ ──► .zst.enc
└──────────────┘     └─────────────────┘     └──────────────────┘
                                                     │
                                                     ▼
                                            ┌────────────────┐
                                            │ System Keyring │
                                            │ (key storage)  │
                                            └────────────────┘
```

**Keyring Integration**:
- Uses `go-keyring` library for cross-platform support
- Key stored as: service="texelation", user="history-<terminal-id>"
- Key generated on first terminal creation, 256-bit random
- If keyring unavailable, falls back to encrypted file with user passphrase

---

### 4. TerminalIndex (SQLite FTS5)

**Purpose**: Full-text search within a terminal's history.

**Schema**:
```sql
-- Main content table
CREATE TABLE lines (
    id INTEGER PRIMARY KEY,           -- Global line index
    page_id INTEGER NOT NULL,
    line_offset INTEGER NOT NULL,     -- Offset within page
    timestamp INTEGER NOT NULL,       -- UnixNano
    is_command INTEGER DEFAULT 0,     -- OSC 133 detected
    content TEXT NOT NULL             -- Plain text (cells→runes)
);

-- FTS5 virtual table for search
CREATE VIRTUAL TABLE lines_fts USING fts5(
    content,
    content='lines',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);

-- Triggers for FTS sync
CREATE TRIGGER lines_ai AFTER INSERT ON lines BEGIN
    INSERT INTO lines_fts(rowid, content) VALUES (new.id, new.content);
END;

-- Time index for "jump to time"
CREATE INDEX idx_lines_timestamp ON lines(timestamp);

-- Page lookup
CREATE INDEX idx_lines_page ON lines(page_id, line_offset);
```

**Indexing Strategy**:

| Content Type | Indexing Mode | Latency |
|--------------|---------------|---------|
| Commands (OSC 133) | Synchronous | Immediate |
| Regular output | Async batch | 1-5 seconds |
| Frozen pages | Background | When idle |

**Command Detection**:
- OSC 133 markers: `\x1b]133;A\x07` (prompt start), `\x1b]133;C\x07` (command start)
- Lines between `;A` and `;C` are prompts
- Lines after `;C` until next `;A` are command output

---

### 5. GlobalIndex (Cross-Terminal Search)

**Purpose**: Search across all terminals in a session.

**Schema**:
```sql
-- Terminal registry
CREATE TABLE terminals (
    id TEXT PRIMARY KEY,              -- UUID
    name TEXT,                        -- User-friendly name
    created_at INTEGER,
    last_active INTEGER,
    index_path TEXT                   -- Path to terminal's index.db
);

-- Federated search view (queries each terminal's index)
-- Actual implementation uses ATTACH DATABASE dynamically
```

**Query Strategy**:
1. Query each terminal's `index.db` in parallel
2. Merge results by relevance score
3. Return with terminal ID for navigation

---

### 6. TimeIndex

**Purpose**: Enable "jump to time" navigation.

**Implementation**: Uses SQLite index on `lines.timestamp`

**API**:
```go
type TimeIndex interface {
    // FindLineAt returns the line closest to the given time
    FindLineAt(t time.Time) (globalIdx int64, err error)

    // FindLinesInRange returns lines within a time range
    FindLinesInRange(start, end time.Time) ([]int64, error)

    // GetTimestamp returns when a line was written
    GetTimestamp(globalIdx int64) (time.Time, error)
}
```

---

## Data Flow

### Write Path

```
User types command
        │
        ▼
┌──────────────────┐
│   MemoryBuffer   │  Write cell at cursor position
│   (ring buffer)  │
└────────┬─────────┘
         │ NotifyWrite(lineIdx)
         ▼
┌──────────────────┐
│ AdaptivePersist  │  Rate monitoring, mode selection
│ (existing)       │
└────────┬─────────┘
         │ based on mode
         ▼
┌──────────────────┐
│  WriteAheadLog   │  Append entry with timestamp
│  (NEW)           │
└────────┬─────────┘
         │ periodic/threshold
         ▼
┌──────────────────┐
│   PageStore      │  Checkpoint WAL → pages
│   (NEW)          │
└────────┬─────────┘
         │ async
         ▼
┌──────────────────┐
│ TerminalIndex    │  Insert into FTS5
│ (SQLite)         │
└──────────────────┘
```

### Read Path (Normal Scroll)

```
User scrolls up
        │
        ▼
┌──────────────────┐
│ ViewportWindow   │  Check if lines in MemoryBuffer
└────────┬─────────┘
         │ if not in memory
         ▼
┌──────────────────┐
│   PageStore      │  Load page (decompress if frozen)
└────────┬─────────┘
         │ populate
         ▼
┌──────────────────┐
│  MemoryBuffer    │  Insert loaded lines
│  (evict old)     │
└──────────────────┘
```

### Search Path

```
User searches "docker run"
        │
        ▼
┌──────────────────┐
│ TerminalIndex    │  SELECT * FROM lines_fts WHERE lines_fts MATCH 'docker run'
│ (FTS5 query)     │
└────────┬─────────┘
         │ results: [(page_id, line_offset, timestamp), ...]
         ▼
┌──────────────────┐
│   PageStore      │  Load relevant pages
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ ViewportWindow   │  Jump to result, highlight match
└──────────────────┘
```

---

## File Layout

```
~/.local/share/texelation/
├── history/
│   ├── terminals/
│   │   └── a1b2c3d4-e5f6-7890-abcd-ef1234567890/
│   │       ├── manifest.json
│   │       │   {
│   │       │     "id": "a1b2c3d4-...",
│   │       │     "created": "2025-01-28T10:00:00Z",
│   │       │     "last_active": "2025-01-28T15:30:00Z",
│   │       │     "total_lines": 150000,
│   │       │     "page_count": 45,
│   │       │     "frozen_count": 40,
│   │       │     "encrypted": true
│   │       │   }
│   │       │
│   │       ├── wal.log              # ~1-10 MB, truncated on checkpoint
│   │       │
│   │       ├── pages/
│   │       │   ├── 00000043.page    # ~64KB, LIVE
│   │       │   ├── 00000044.page    # ~64KB, LIVE
│   │       │   └── 00000045.page    # ~64KB, LIVE (current)
│   │       │
│   │       ├── archive/
│   │       │   ├── 00000001.zst     # ~15-25KB compressed
│   │       │   ├── 00000002.zst
│   │       │   └── ...              # 40 frozen pages
│   │       │
│   │       └── index.db             # ~5-20 MB SQLite
│   │
│   ├── global/
│   │   └── search.db                # Cross-terminal index
│   │
│   └── config/
│       └── compression_dict.zdict   # Shared zstd dictionary
│
└── scrollback/                      # DEPRECATED: old format (ignored)
    └── <pane-id>.hist3              # Ignored, start fresh with new format
```

---

## Configuration

```go
type DiskLayerConfig struct {
    // Storage paths
    BaseDir string // Default: ~/.local/share/texelation/history

    // Page settings
    TargetPageSize   int           // Default: 64 * 1024 (64KB)
    MaxLivePages     int           // Default: 5 (pages before freezing)
    FreezeThreshold  int           // Default: 2 * viewport_height lines behind

    // WAL settings
    WALSyncMode      WALSyncMode   // SYNC_IMMEDIATE, SYNC_BATCHED, SYNC_ON_IDLE
    WALCheckpointInterval time.Duration // Default: 30s
    WALMaxSize       int64         // Default: 10MB, triggers checkpoint

    // Compression
    CompressionLevel int           // zstd level 1-22, default: 3
    UseDictionary    bool          // Default: true

    // Encryption
    EncryptionEnabled bool         // Default: true
    KeyringService    string       // Default: "texelation"
    FallbackToFile    bool         // Default: true (passphrase-encrypted file)

    // Indexing
    IndexCommands     bool         // Default: true (sync)
    IndexOutput       bool         // Default: true (async)
    CrossTerminalSearch bool       // Default: true

    // Background operations
    FreezeOnIdle      bool         // Default: true
    IdleThreshold     time.Duration // Default: 5s
    MaxConcurrentOps  int          // Default: 2
}
```

---

## Component Interfaces

```go
// WriteAheadLog provides crash recovery for live content
type WriteAheadLog interface {
    Append(lineIdx int64, line *LogicalLine, timestamp time.Time) error
    Checkpoint() error
    Recover() ([]WALEntry, error)
    Close() error
}

// PageStore manages page lifecycle and storage
type PageStore interface {
    // Write operations
    WriteLine(lineIdx int64, line *LogicalLine, timestamp time.Time) error
    Checkpoint() error

    // Read operations
    ReadLine(lineIdx int64) (*LogicalLine, time.Time, error)
    ReadPage(pageID uint64) (*Page, error)

    // Page management
    GetPageForLine(lineIdx int64) (pageID uint64, offset int, error)
    FreezePage(pageID uint64) error

    // Lifecycle
    Close() error
}

// ArchiveManager handles compression and encryption
type ArchiveManager interface {
    Compress(page *Page) ([]byte, error)
    Decompress(data []byte) (*Page, error)
    Encrypt(data []byte) ([]byte, error)
    Decrypt(data []byte) ([]byte, error)
}

// SearchIndex provides full-text search
type SearchIndex interface {
    // Indexing
    IndexLine(lineIdx int64, content string, isCommand bool, timestamp time.Time) error
    IndexBatch(entries []IndexEntry) error

    // Searching
    Search(query string, limit int) ([]SearchResult, error)
    SearchInRange(query string, start, end time.Time, limit int) ([]SearchResult, error)

    // Lifecycle
    Close() error
}

// TimeIndex enables time-based navigation
type TimeIndex interface {
    FindLineAt(t time.Time) (int64, error)
    GetTimestamp(lineIdx int64) (time.Time, error)
}
```

---

## Format Compatibility

**No migration from old format.** If an existing `.hist3` file exists with old format:
- Ignore it completely
- Start fresh with new format
- Old files can be manually deleted by user

This simplifies implementation and avoids edge cases with synthetic timestamps.

---

## History Navigator Card

A unified texelui-based overlay for navigating terminal history by **text search** and **time**.
Built using the texelui widget library (`github.com/framegrace/texelui`) following the launcher app pattern.

Two-line card combining search and time navigation in one interface.

### Visual Design

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           [Terminal Content]                                 │
│                                                                             │
│   ... scrollback history with highlighted match ...                         │
│                                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│ 🔍 Search: [docker run___________]  ◀ Prev | Next ▶  3/47                   │
│ 🕐 Time:   [2025-01-28 14:32_____]  ◀ -1h  | +1h ▶   [Jump]  ← current time │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Line 1 - Text Search:**
- Search input field with live results
- Prev/Next buttons to navigate matches
- Result counter (3/47)

**Line 2 - Time Navigation:**
- Time/date input (accepts: "yesterday 3pm", "2025-01-15", "-2h", etc.)
- Quick jump buttons: -1h, +1h (or configurable intervals)
- Jump button to navigate to entered time
- Current result's timestamp displayed on the right

### Implementation Pattern

Following the launcher app pattern, the history navigator uses:
- `adapter.UIApp` - Wraps UIManager as a texelcore.App
- `core.UIManager` - Manages widget tree and focus
- `widgets.Input` - Text inputs for search query and time
- `widgets.Label` - For icons, counter, current timestamp
- `widgets.Button` - For Prev/Next/Jump navigation
- `texelcore.ControlBus` - Signals events back to terminal (navigate, close)

### Structure

```go
package texelterm

import (
    "github.com/framegrace/texelui/adapter"
    "github.com/framegrace/texelui/core"
    "github.com/framegrace/texelui/widgets"
    texelcore "github.com/framegrace/texelui/core"
)

// HistoryNavigator provides search and time-based navigation of terminal history.
// Two-line card: search on line 1, time navigation on line 2.
// Implements texelcore.App via embedded adapter.UIApp.
type HistoryNavigator struct {
    *adapter.UIApp
    controlBus texelcore.ControlBus

    // Line 1: Search widgets
    searchIcon    *widgets.Label   // "🔍"
    searchInput   *widgets.Input   // Query text
    searchPrev    *widgets.Button  // ◀
    searchNext    *widgets.Button  // ▶
    searchCounter *widgets.Label   // "3/47"

    // Line 2: Time widgets
    timeIcon      *widgets.Label   // "🕐"
    timeInput     *widgets.Input   // Time/date entry
    timePrevHour  *widgets.Button  // -1h
    timeNextHour  *widgets.Button  // +1h
    timeJump      *widgets.Button  // [Jump]
    currentTime   *widgets.Label   // Shows timestamp of current position

    // State
    searchResults []SearchResult
    searchIndex   int              // Current search result index
    currentLine   int64            // Current line being viewed
    currentTs     time.Time        // Timestamp of current line

    // Callbacks
    searchFunc    func(query string) []SearchResult
    timeFunc      func(t time.Time) (int64, error)  // Find line at time
    getTimestamp  func(lineIdx int64) time.Time     // Get timestamp for line

    mu sync.RWMutex
}

type SearchResult struct {
    GlobalLineIdx int64
    PageID        uint64
    Timestamp     time.Time
    MatchStart    int
    MatchEnd      int
    LineContent   string
}

// HistoryNavigatorConfig holds callbacks for the navigator.
type HistoryNavigatorConfig struct {
    Search       func(query string) []SearchResult
    FindLineAt   func(t time.Time) (int64, error)
    GetTimestamp func(lineIdx int64) time.Time
}

// NewHistoryNavigator creates a combined search/time navigation card.
func NewHistoryNavigator(cfg HistoryNavigatorConfig) texelcore.App {
    h := &HistoryNavigator{
        controlBus:   texelcore.NewControlBus(),
        searchFunc:   cfg.Search,
        timeFunc:     cfg.FindLineAt,
        getTimestamp: cfg.GetTimestamp,
    }

    // Create UIManager and wrap as UIApp
    ui := core.NewUIManager()
    h.UIApp = adapter.NewUIApp("History", ui)

    // UI built on first Resize()
    return h
}

// ControlBus returns the card's control bus for event registration.
func (h *HistoryNavigator) ControlBus() texelcore.ControlBus {
    return h.controlBus
}
```

### Widget Layout

```go
func (h *HistoryNavigator) buildUI() {
    ui := h.UI()

    // === LINE 1: Search ===

    // Search icon
    h.searchIcon = widgets.NewLabel("🔍 Search:")
    h.searchIcon.SetPosition(1, 0)
    ui.AddWidget(h.searchIcon)

    // Search input field
    h.searchInput = widgets.NewInput()
    h.searchInput.Placeholder = "type to search..."
    h.searchInput.OnChange = h.onSearchChange  // Debounced search
    h.searchInput.OnSubmit = h.onSearchNext    // Enter = next result
    h.searchInput.SetPosition(12, 0)
    h.searchInput.Resize(25, 1)
    ui.AddWidget(h.searchInput)
    ui.Focus(h.searchInput)  // Start with search focused

    // Search Prev/Next buttons
    h.searchPrev = widgets.NewButton(39, 0, 3, 1, "◀")
    h.searchPrev.OnClick = h.onSearchPrev
    ui.AddWidget(h.searchPrev)

    h.searchNext = widgets.NewButton(43, 0, 3, 1, "▶")
    h.searchNext.OnClick = h.onSearchNext
    ui.AddWidget(h.searchNext)

    // Search counter
    h.searchCounter = widgets.NewLabel("0/0")
    h.searchCounter.SetPosition(48, 0)
    ui.AddWidget(h.searchCounter)

    // === LINE 2: Time Navigation ===

    // Time icon
    h.timeIcon = widgets.NewLabel("🕐 Time:")
    h.timeIcon.SetPosition(1, 1)
    ui.AddWidget(h.timeIcon)

    // Time input (accepts: "yesterday 3pm", "2025-01-15 14:00", "-2h")
    h.timeInput = widgets.NewInput()
    h.timeInput.Placeholder = "e.g., yesterday 3pm"
    h.timeInput.OnSubmit = h.onTimeJump
    h.timeInput.SetPosition(12, 1)
    h.timeInput.Resize(25, 1)
    ui.AddWidget(h.timeInput)

    // Quick time buttons
    h.timePrevHour = widgets.NewButton(39, 1, 4, 1, "-1h")
    h.timePrevHour.OnClick = h.onTimePrevHour
    ui.AddWidget(h.timePrevHour)

    h.timeNextHour = widgets.NewButton(44, 1, 4, 1, "+1h")
    h.timeNextHour.OnClick = h.onTimeNextHour
    ui.AddWidget(h.timeNextHour)

    h.timeJump = widgets.NewButton(49, 1, 6, 1, "Jump")
    h.timeJump.OnClick = h.onTimeJump
    ui.AddWidget(h.timeJump)

    // Current timestamp display (right side)
    h.currentTime = widgets.NewLabel("")
    h.currentTime.SetPosition(57, 1)
    ui.AddWidget(h.currentTime)
}
```

### Event Handling

```go
func (h *HistoryNavigator) HandleKey(ev *tcell.EventKey) {
    switch ev.Key() {
    case tcell.KeyEsc:
        h.controlBus.Trigger("history.close", nil)
        return

    case tcell.KeyTab:
        // Tab cycles focus between search and time inputs
        h.UIApp.HandleKey(ev)
        return
    }

    // Pass to UIManager for widget handling
    h.UIApp.HandleKey(ev)
}

// --- Search handlers ---

func (h *HistoryNavigator) onSearchChange(query string) {
    // Debounce 150ms, then execute search
    h.searchResults = h.searchFunc(query)
    h.searchIndex = 0
    h.updateSearchDisplay()
    if len(h.searchResults) > 0 {
        h.navigateToLine(h.searchResults[0].GlobalLineIdx)
    }
}

func (h *HistoryNavigator) onSearchNext() {
    if len(h.searchResults) == 0 {
        return
    }
    h.searchIndex = (h.searchIndex + 1) % len(h.searchResults)
    h.updateSearchDisplay()
    h.navigateToLine(h.searchResults[h.searchIndex].GlobalLineIdx)
}

func (h *HistoryNavigator) onSearchPrev() {
    if len(h.searchResults) == 0 {
        return
    }
    h.searchIndex--
    if h.searchIndex < 0 {
        h.searchIndex = len(h.searchResults) - 1
    }
    h.updateSearchDisplay()
    h.navigateToLine(h.searchResults[h.searchIndex].GlobalLineIdx)
}

// --- Time handlers ---

func (h *HistoryNavigator) onTimeJump() {
    // Parse time input (supports: "yesterday 3pm", "2025-01-15", "-2h", etc.)
    t, err := parseTimeInput(h.timeInput.Text, time.Now())
    if err != nil {
        return // Could show error in UI
    }
    lineIdx, err := h.timeFunc(t)
    if err != nil {
        return
    }
    h.navigateToLine(lineIdx)
}

func (h *HistoryNavigator) onTimePrevHour() {
    newTime := h.currentTs.Add(-1 * time.Hour)
    lineIdx, err := h.timeFunc(newTime)
    if err != nil {
        return
    }
    h.navigateToLine(lineIdx)
}

func (h *HistoryNavigator) onTimeNextHour() {
    newTime := h.currentTs.Add(1 * time.Hour)
    lineIdx, err := h.timeFunc(newTime)
    if err != nil {
        return
    }
    h.navigateToLine(lineIdx)
}

// --- Common navigation ---

func (h *HistoryNavigator) navigateToLine(lineIdx int64) {
    h.currentLine = lineIdx
    h.currentTs = h.getTimestamp(lineIdx)
    h.updateTimeDisplay()

    // Signal terminal to scroll
    h.controlBus.Trigger("history.navigate", lineIdx)
}

func (h *HistoryNavigator) updateSearchDisplay() {
    if len(h.searchResults) == 0 {
        h.searchCounter.Text = "0/0"
    } else {
        h.searchCounter.Text = fmt.Sprintf("%d/%d", h.searchIndex+1, len(h.searchResults))
    }
}

func (h *HistoryNavigator) updateTimeDisplay() {
    h.currentTime.Text = h.currentTs.Format("2006-01-02 15:04:05")
}
```

### Time Input Parsing

Supports flexible time formats:
- Absolute: `"2025-01-28 14:30"`, `"2025-01-28"`, `"14:30"`
- Relative: `"-2h"`, `"-30m"`, `"-1d"`, `"+1h"`
- Natural: `"yesterday"`, `"yesterday 3pm"`, `"last monday"`

```go
func parseTimeInput(input string, now time.Time) (time.Time, error) {
    input = strings.TrimSpace(strings.ToLower(input))

    // Relative times: -2h, +30m, -1d
    if strings.HasPrefix(input, "-") || strings.HasPrefix(input, "+") {
        return parseRelativeTime(input, now)
    }

    // Natural language: yesterday, last monday, etc.
    if t, ok := parseNaturalTime(input, now); ok {
        return t, nil
    }

    // Absolute formats
    formats := []string{
        "2006-01-02 15:04:05",
        "2006-01-02 15:04",
        "2006-01-02",
        "15:04:05",
        "15:04",
    }
    for _, fmt := range formats {
        if t, err := time.Parse(fmt, input); err == nil {
            // For time-only, use today's date
            if !strings.Contains(fmt, "2006") {
                t = time.Date(now.Year(), now.Month(), now.Day(),
                    t.Hour(), t.Minute(), t.Second(), 0, now.Location())
            }
            return t, nil
        }
    }

    return time.Time{}, fmt.Errorf("unrecognized time format: %s", input)
}
```

### Control Bus Events

| Event | Payload | Description |
|-------|---------|-------------|
| `history.navigate` | `int64` (lineIdx) | Terminal scrolls to this line |
| `history.close` | `nil` | Close navigator, return to live edge |

### Integration with Terminal

```go
// In apps/texelterm/term.go

func (t *TexelTerm) openHistoryNavigator() {
    // Create navigator with callbacks
    nav := NewHistoryNavigator(HistoryNavigatorConfig{
        Search: func(query string) []SearchResult {
            return t.searchIndex.Search(query, 1000)
        },
        FindLineAt: func(tm time.Time) (int64, error) {
            return t.timeIndex.FindLineAt(tm)
        },
        GetTimestamp: func(lineIdx int64) time.Time {
            return t.timeIndex.GetTimestamp(lineIdx)
        },
    })

    // Register control bus handlers
    bus := nav.(texelcore.ControlBusProvider).ControlBus()

    bus.On("history.navigate", func(payload interface{}) error {
        lineIdx := payload.(int64)
        t.vterm.ScrollToLine(lineIdx)
        // Highlight if from search (check if we have match info)
        return nil
    })

    bus.On("history.close", func(_ interface{}) error {
        t.closeHistoryNavigator()
        t.vterm.ClearHighlight()
        t.vterm.ScrollToLiveEdge()
        return nil
    })

    // Add to pipeline (renders 2-line overlay at bottom)
    t.pipeline.PushCard(cards.WrapApp(nav))
}
```

### Key Bindings

| Key | Context | Action |
|-----|---------|--------|
| Any printable | Focused input | Add to text |
| Backspace | Focused input | Delete character |
| Enter | Search input | Next search result |
| Enter | Time input | Jump to time |
| Tab | Any | Cycle focus (search ↔ time) |
| Escape | Any | Close navigator |
| ↑ / ↓ | Search focused | Navigate search results |

### Theming

Uses semantic colors from texelui theme:
- `bg.surface` - Card background
- `text.primary` - Input text, labels
- `text.muted` - Placeholder text
- `accent.primary` - Highlight current match, focused input
- `border.focus` - Input border when focused
```

---

## Performance Considerations

### Memory Usage

| Component | Typical Size | Notes |
|-----------|--------------|-------|
| MemoryBuffer | ~50MB | 50K lines * ~1KB avg |
| Live pages (5) | ~320KB | 5 * 64KB |
| WAL | 1-10MB | Truncated on checkpoint |
| SQLite cache | 2-8MB | Configurable |
| Zstd dictionary | ~100KB | Loaded once |

### Disk I/O

| Operation | Frequency | I/O Pattern |
|-----------|-----------|-------------|
| WAL append | Per line (batched) | Sequential write |
| Page write | Every ~1000 lines | Sequential write |
| Page freeze | Background | Read + write |
| Search | User-triggered | Random read (index) |
| Scroll to history | User-triggered | Random read (page) |

### Latency Targets

| Operation | Target | Fallback |
|-----------|--------|----------|
| Character echo | < 1ms | WAL batched |
| Search (10K lines) | < 100ms | FTS5 optimized |
| Jump to time | < 50ms | Index lookup |
| Load frozen page | < 20ms | Cached decompression |

---

## Security Considerations

### Encryption

- **At rest**: All frozen pages encrypted with ChaCha20-Poly1305
- **Key storage**: System keyring (GNOME/KDE/macOS)
- **Fallback**: Argon2-derived key from user passphrase
- **Key rotation**: Supported via re-encryption of frozen pages

### Sensitive Content

- Live pages and WAL are NOT encrypted (performance)
- Commands may contain passwords in arguments
- Search index contains plain text (for FTS5)
- Recommendation: Use encrypted home directory for full protection

---

## Future Enhancements

1. **Cloud sync**: Encrypted backup to user's cloud storage
2. **Semantic search**: Embed commands/output for semantic similarity
3. **Session replay**: Record timestamps + content for video-like playback
4. **Sharing**: Export encrypted, searchable history bundles
5. **Analytics**: Command frequency, error patterns, productivity metrics

---

## Implementation Phases

Each phase is designed to be completed in a single feature-dev session. Progress is tracked in:
**`docs/plans/DISK_LAYER_PROGRESS.md`**

---

### Phase 1: Page Format & Basic Storage

**Goal**: Replace DiskHistory with page-based storage, no compression yet.

**Files to create/modify**:
- `apps/texelterm/parser/page.go` - Page struct and serialization
- `apps/texelterm/parser/page_store.go` - PageStore implementation
- `apps/texelterm/parser/page_store_test.go` - Unit tests

**Deliverables**:
1. Page format with header (64 bytes) + line index + line data
2. PageStore that writes 64KB pages
3. Read/write pages to `pages/` directory
4. Per-line timestamp tracking in page index
5. Replace DiskHistory calls with PageStore in VTerm integration

**Success criteria**:
- `go test ./apps/texelterm/parser/... -run Page` passes
- Terminal can write and read pages
- Timestamps stored per-line

---

### Phase 2: Write-Ahead Log

**Goal**: Add WAL for crash recovery of live content.

**Files to create/modify**:
- `apps/texelterm/parser/wal.go` - WriteAheadLog implementation
- `apps/texelterm/parser/wal_test.go` - Unit tests
- `apps/texelterm/parser/vterm_memory_buffer.go` - Integration

**Deliverables**:
1. WAL format with entries: LINE_WRITE, LINE_MODIFY, CHECKPOINT
2. Append entries on NotifyWrite()
3. Checkpoint triggers page flush
4. Recovery: replay entries after last checkpoint on startup
5. WAL truncation after successful checkpoint

**Success criteria**:
- Kill terminal mid-write, restart, content recovered
- WAL size stays bounded (truncated on checkpoint)

---

### Phase 3: SQLite Search Index

**Goal**: Full-text search with FTS5.

**Files to create/modify**:
- `apps/texelterm/parser/search_index.go` - SearchIndex with SQLite
- `apps/texelterm/parser/search_index_test.go` - Unit tests
- `apps/texelterm/parser/text_extractor.go` - Cell[] → plain text

**Deliverables**:
1. SQLite database with `lines` table and `lines_fts` FTS5 virtual table
2. `IndexLine(lineIdx, content, isCommand, timestamp)` method
3. `Search(query, limit)` method returning results with timestamps
4. Async batch indexing for regular output
5. Sync indexing for commands (OSC 133 detection)

**Success criteria**:
- Search "docker" finds all lines containing "docker"
- Results include timestamp and line index
- Indexing doesn't block terminal output

---

### Phase 4: History Navigator Card

**Goal**: Combined search + time navigation in a 2-line card.

**Files to create/modify**:
- `apps/texelterm/history_navigator.go` - HistoryNavigator implementation
- `apps/texelterm/time_parser.go` - Time input parsing
- `apps/texelterm/term.go` - Key binding and card integration

**Deliverables**:
1. HistoryNavigator as texelui card (bottom of terminal, 2 rows)
2. Line 1: Search input + Prev/Next + counter
3. Line 2: Time input + -1h/+1h buttons + Jump + current timestamp
4. Real-time search (debounced 150ms)
5. Time parsing: relative (-2h), absolute (2025-01-28), natural (yesterday 3pm)
6. Tab cycles focus between search and time inputs
7. Terminal scrolls to selected result
8. Escape closes card, returns to live edge

**Success criteria**:
- Ctrl+Shift+F opens navigator
- Search: type query, see results, navigate with Enter
- Time: enter time, click Jump or -1h/+1h
- Current timestamp always displayed
- Tab switches between search and time
- Escape returns to normal

---

### Phase 5: Page Freezing & Compression

**Goal**: Compress old pages with zstd in background.

**Files to create/modify**:
- `apps/texelterm/parser/archive_manager.go` - Compression/decompression
- `apps/texelterm/parser/page_store.go` - Add freeze logic
- `apps/texelterm/parser/freezer.go` - Background freezer goroutine

**Deliverables**:
1. Zstd compression with dictionary
2. Page states: LIVE → WARM → FROZEN
3. Background freezer runs when idle
4. Freeze criteria: 2× viewport height behind, 30s no access
5. Frozen pages in `archive/` directory
6. Transparent decompression on read

**Success criteria**:
- Pages compress to ~25-40% of original size
- Scrolling to frozen pages works transparently
- Freeze happens in background, no UI stutter

---

### Phase 6: Encryption

**Goal**: Encrypt frozen pages with system keyring.

**Files to create/modify**:
- `apps/texelterm/parser/crypto.go` - Encryption/decryption
- `apps/texelterm/parser/keyring.go` - System keyring integration
- `apps/texelterm/parser/archive_manager.go` - Add encryption layer

**Deliverables**:
1. ChaCha20-Poly1305 encryption
2. System keyring integration (go-keyring)
3. Key generation on first terminal
4. Encrypt after compression, decrypt before decompression
5. Fallback to passphrase if keyring unavailable

**Success criteria**:
- Frozen pages encrypted on disk
- Reading works transparently with keyring
- Works on Linux (GNOME/KDE) and macOS

---

### Phase 7: Cross-Terminal Search

**Goal**: Search across all terminal sessions.

**Files to create/modify**:
- `apps/texelterm/parser/global_index.go` - GlobalIndex
- `apps/texelterm/history_navigator.go` - Add cross-terminal mode toggle

**Deliverables**:
1. Global SQLite index in `global/search.db`
2. Terminal registry table
3. Federated queries across terminal indexes
4. UI toggle in navigator: "This terminal" / "All terminals"
5. Results show terminal name/ID

**Success criteria**:
- Search finds results from other terminals
- Can navigate to result (opens/focuses that terminal or shows read-only)
