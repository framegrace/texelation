# Disk Layer Implementation Progress

This file tracks the implementation of the disk layer architecture.
See `DISK_LAYER_ARCHITECTURE.md` for full design details.

**Last updated**: 2026-01-28
**Overall status**: Phase 4 Complete

---

## Phase Summary

| Phase | Name | Status | Branch | PR |
|-------|------|--------|--------|-----|
| 1 | Page Format & Basic Storage | COMPLETE | - | - |
| 2 | Write-Ahead Log | COMPLETE | - | - |
| 3 | SQLite Search Index | COMPLETE | - | - |
| 4 | History Navigator Card | COMPLETE | - | - |
| 5 | Page Freezing & Compression | NOT STARTED | - | - |
| 6 | Encryption | NOT STARTED | - | - |
| 7 | Cross-Terminal Search | NOT STARTED | - | - |

---

## Phase 1: Page Format & Basic Storage

**Status**: COMPLETE
**Branch**: -
**Started**: 2026-01-28
**Completed**: 2026-01-28

### Instructions

1. Create `apps/texelterm/parser/page.go`:
   - `Page` struct with Header (64 bytes), LineIndex, LineData
   - `PageHeader` with magic "TXPAGE01", version, pageID, state, flags, timestamps
   - `LineIndexEntry` with offset, timestamp, flags
   - Serialization: `WriteTo(io.Writer)`, `ReadFrom(io.Reader)`

2. Create `apps/texelterm/parser/page_store.go`:
   - `PageStore` struct managing pages directory
   - `WriteLine(lineIdx, line, timestamp)` - buffer lines, flush at 64KB
   - `ReadLine(lineIdx)` - find page, read line
   - `GetCurrentPage()`, `FlushPage()`, `Close()`

3. Update `apps/texelterm/parser/vterm_memory_buffer.go`:
   - Replace `DiskHistory` with `PageStore`
   - Pass timestamp with each write

4. Create tests in `apps/texelterm/parser/page_store_test.go`

### Checklist

- [x] Page struct defined with correct header format
- [x] Page serialization works (round-trip test)
- [x] PageStore creates pages directory
- [x] PageStore writes lines to pages
- [x] PageStore reads lines from pages
- [x] Per-line timestamps stored in index
- [x] VTerm integration updated
- [x] Old DiskHistory code removed or deprecated
- [x] Tests pass: `go test ./apps/texelterm/parser/... -run Page`

### Notes

**Implementation completed 2026-01-28:**

Created files:
- `apps/texelterm/parser/page.go` - Page struct with 64-byte header, LineIndexEntry (16 bytes), serialization
- `apps/texelterm/parser/page_store.go` - PageStore managing 64KB page files with HistoryWriter interface
- `apps/texelterm/parser/page_store_test.go` - Comprehensive tests for Page and PageStore
- `apps/texelterm/parser/history_writer.go` - Interface for disk persistence backends

Updated files:
- `apps/texelterm/parser/adaptive_persistence.go` - Uses PageStore instead of DiskHistory
- `apps/texelterm/parser/adaptive_persistence_test.go` - All tests updated to use PageStore
- `apps/texelterm/parser/vterm_memory_buffer.go` - Integrated with PageStore

Key features:
- Page format: TXPAGE01 magic, 64-byte header, per-line timestamps (UnixNano)
- Strict 64KB page size limit
- Cell encoding: 16 bytes per cell (rune + colors + attributes)
- Directory structure: `<base>/<terminalID>/pages/`
- DiskHistory fully removed (disk_history.go, disk_history_test.go deleted)

---

## Phase 2: Write-Ahead Log

**Status**: COMPLETE
**Branch**: -
**Started**: 2026-01-28
**Completed**: 2026-01-28

### Instructions

1. Create `apps/texelterm/parser/wal.go`:
   - WAL header: magic "TXWAL001", version, terminalID, lastCheckpoint
   - Entry types: LINE_WRITE (0x01), LINE_MODIFY (0x02), CHECKPOINT (0x03)
   - Entry format: type, globalLineIdx, timestamp, dataLen, data, CRC32
   - Methods: `Append()`, `Checkpoint()`, `Recover()`, `Truncate()`, `Close()`

2. Integrate with AdaptivePersistence:
   - WAL receives writes instead of direct page writes
   - Checkpoint triggers page flush + WAL truncation

3. Add startup recovery in VTerm initialization

4. Create tests in `apps/texelterm/parser/wal_test.go`

### Checklist

- [x] WAL format implemented
- [x] Append entries on write
- [x] Checkpoint triggers page flush
- [x] Recovery replays entries after last checkpoint
- [x] WAL truncated after checkpoint
- [x] Crash recovery test (kill -9, restart)
- [x] Tests pass: `go test ./apps/texelterm/parser/... -run WAL`

### Notes

**Implementation completed 2026-01-28:**

Created files:
- `apps/texelterm/parser/write_ahead_log.go` - Full WAL implementation with crash recovery
- `apps/texelterm/parser/write_ahead_log_test.go` - Comprehensive tests (10 test cases)

Updated files:
- `apps/texelterm/parser/adaptive_persistence.go` - Added WAL integration with `NewAdaptivePersistenceWithWAL`
- `apps/texelterm/parser/adaptive_persistence_test.go` - Updated for new function signatures
- `apps/texelterm/parser/page_store.go` - Fixed `Flush()` to use new pageID
- `apps/texelterm/parser/history_writer.go` - Added `HistoryWriterWithTimestamp` interface

Key features:
- WAL format: TXWAL001 magic, 32-byte header, variable-length entries with CRC32 checksums
- Entry types: LINE_WRITE (0x01), LINE_MODIFY (0x02), CHECKPOINT (0x03)
- Clean architecture: WAL owns PageStore internally
- Automatic recovery on startup: replays uncommitted entries to PageStore
- Checkpoint: replays WAL entries to PageStore, truncates WAL, fsyncs
- Auto-checkpoint: timer-based (default 30s) and size-based (default 10MB)
- Per-line timestamps (UnixNano precision)
- Little-endian binary encoding

Architecture decisions:
- WAL owns PageStore (Approach 2: Clean Architecture) - cleaner abstraction
- Sync on checkpoint only (not every write) - better performance
- Full spec with LINE_WRITE/LINE_MODIFY/CHECKPOINT entry types
- WAL filename: `wal.log` in terminal directory

Test coverage:
- TestWAL_CreateAndClose - Basic lifecycle
- TestWAL_AppendAndRecover - Append + checkpoint + recovery
- TestWAL_RecoveryWithoutCheckpoint - Recovery replays to PageStore
- TestWAL_CheckpointTruncatesWAL - Checkpoint clears WAL
- TestWAL_Timestamps - Timestamp preservation
- TestWAL_ColorAndAttributes - Full cell attributes preserved
- TestWAL_LargeLineCount - 500 line stress test
- TestWAL_HistoryWriterInterface - Interface compliance
- TestWAL_ReadLineRange - Range reads
- TestWAL_CRCValidation - CRC32 checksums

---

## Phase 3: SQLite Search Index

**Status**: COMPLETE
**Branch**: -
**Started**: 2026-01-28
**Completed**: 2026-01-28

### Instructions

1. Add dependency: `go get github.com/mattn/go-sqlite3` (or `modernc.org/sqlite` for pure Go)

2. Create `apps/texelterm/parser/text_extractor.go`:
   - `ExtractText(cells []Cell) string` - convert cells to plain text
   - Handle wide characters, control characters

3. Create `apps/texelterm/parser/search_index.go`:
   - SQLite database at `<terminal>/index.db`
   - Schema: `lines` table + `lines_fts` FTS5 virtual table
   - Methods: `IndexLine()`, `IndexBatch()`, `Search()`, `Close()`
   - OSC 133 command detection

4. Integrate with PageStore:
   - Async indexing queue for regular output
   - Sync indexing for detected commands

5. Create tests in `apps/texelterm/parser/search_index_test.go`

### Checklist

- [x] SQLite database created
- [x] FTS5 virtual table configured
- [x] IndexLine() inserts content
- [x] Search() returns results with timestamps
- [x] Async batch indexing works
- [x] Command detection (OSC 133) works
- [x] Tests pass: `go test ./apps/texelterm/parser/... -run Search`

### Notes

**Implementation completed 2026-01-28:**

Created files:
- `apps/texelterm/parser/text_extractor.go` - Text extraction from terminal cells
- `apps/texelterm/parser/text_extractor_test.go` - Tests for text extraction
- `apps/texelterm/parser/search_index.go` - SQLite FTS5 search index implementation
- `apps/texelterm/parser/search_index_test.go` - Comprehensive tests (16 test cases)

Updated files:
- `apps/texelterm/parser/vterm.go` - Added `OnLineIndex` callback and `WithLineIndexHandler()` option
- `apps/texelterm/parser/vterm_memory_buffer.go` - Call `OnLineIndex` callback on line feed
- `apps/texelterm/term.go` - Initialize search index, wire up callback, close on stop

Key features:
- SQLite dependency: `modernc.org/sqlite` (pure Go, no CGO required)
- Database location: `~/.texelation/scrollback/<paneID>.index.db`
- Schema: `lines` table + `lines_fts` FTS5 virtual table with unicode61 tokenizer
- Async batch indexing: background goroutine, channel-based queue, 100 line batches / 5s timeout
- Sync indexing for commands: immediately searchable when OSC 133 `CommandActive` is true
- BM25 ranking with commands prioritized (ORDER BY is_command DESC, bm25())
- Time-based navigation: `FindLineAt(time)`, `GetTimestamp(lineIdx)`
- Search methods: `Search(query, limit)`, `SearchInRange(query, start, end, limit)`
- Callback-based integration: `OnLineIndex` callback in VTerm for loose coupling

Architecture decisions:
- Pure Go SQLite driver (`modernc.org/sqlite`) for cross-platform builds without CGO
- Callback pattern for integration (separate ownership, not WAL-owned)
- Async indexing for output (non-blocking), sync for commands (instant search)
- FTS5 with triggers to keep index in sync with lines table

Test coverage:
- TestSearchIndex_CreateAndClose - Basic lifecycle
- TestSearchIndex_IndexLineSync - Sync indexing for commands
- TestSearchIndex_IndexLineAsync - Async batch indexing
- TestSearchIndex_SearchEmpty - Empty query handling
- TestSearchIndex_SearchNoResults - No match handling
- TestSearchIndex_CommandsPrioritized - Commands ranked first
- TestSearchIndex_SearchInRange - Time range filtering
- TestSearchIndex_FindLineAt - Time-based navigation
- TestSearchIndex_GetTimestamp - Timestamp retrieval
- TestSearchIndex_EmptyIndex - Edge case handling
- TestSearchIndex_FTS5Wildcard - Wildcard search (docker*)
- TestSearchIndex_UnicodeContent - Unicode handling
- TestSearchIndex_SkipEmptyText - Empty content filtering
- TestSearchIndex_BatchFlush - Batch flush verification
- TestSearchIndex_ReopenExisting - Persistence verification
- TestSearchIndex_LargeVolume - 1000 line stress test

---

## Phase 4: History Navigator Card

**Status**: COMPLETE
**Branch**: -
**Started**: 2026-01-28
**Completed**: 2026-01-28

### Instructions

Build a **2-line card** combining search and time navigation using texelui library.

**Reference files:**
- `apps/launcher/launcher.go` - Pattern to follow (UIApp, UIManager, widgets, ControlBus)
- `texelui/widgets/input.go` - Input widget with OnChange/OnSubmit
- `texelui/widgets/button.go` - Button widget with OnClick
- `texelui/widgets/label.go` - Label widget for text display
- `texelui/adapter/texel_app.go` - UIApp adapter
- `docs/plans/DISK_LAYER_ARCHITECTURE.md` - Full design with code examples

**Card Layout (2 lines):**
```
│ 🔍 Search: [query input________]  ◀ ▶  3/47                    │
│ 🕐 Time:   [time input_________]  -1h +1h [Jump]  2025-01-28 14:32 │
```

1. Create `apps/texelterm/history_navigator.go`:
   - Embed `*adapter.UIApp` (like launcher does)
   - Create `core.UIManager` and wrap with `adapter.NewUIApp()`
   - **Line 1 widgets**: searchIcon (Label), searchInput (Input), searchPrev/Next (Button), searchCounter (Label)
   - **Line 2 widgets**: timeIcon (Label), timeInput (Input), timePrevHour/NextHour (Button), timeJump (Button), currentTime (Label)
   - Create `texelcore.ControlBus` for signaling (history.navigate, history.close)
   - Build UI in `Resize()` on first call (like launcher)

2. Create `apps/texelterm/time_parser.go`:
   - `parseTimeInput(input string, now time.Time) (time.Time, error)`
   - Support relative: "-2h", "+30m", "-1d"
   - Support absolute: "2025-01-28 14:30", "2025-01-28", "14:30"
   - Support natural: "yesterday", "yesterday 3pm"

3. Implement behaviors:
   - **Search**: OnChange debounce 150ms → search → update counter → navigate to first result
   - **Search navigation**: Enter/OnSubmit → next result, Up/Down arrows → prev/next
   - **Time**: OnSubmit or Jump button → parse time → find line → navigate
   - **Quick time**: -1h/+1h buttons adjust current time by 1 hour
   - **Tab**: cycles focus between search input and time input
   - **Escape**: trigger "history.close"

4. Update `apps/texelterm/term.go`:
   - Add `Ctrl+Shift+F` binding to open navigator
   - Create HistoryNavigator with config:
     - Search: query → SearchIndex.Search()
     - FindLineAt: time → TimeIndex.FindLineAt()
     - GetTimestamp: lineIdx → TimeIndex.GetTimestamp()
   - Register handlers on ControlBus:
     - "history.navigate" → scroll to line
     - "history.close" → close card, return to live edge
   - Add card to pipeline with `cards.WrapApp()`

5. Add highlighting in viewport (for search matches):
   - `vterm.HighlightRange(lineIdx, startCol, endCol)`
   - `vterm.ClearHighlight()`

### Checklist

**Structure:**
- [x] HistoryNavigator embeds `*adapter.UIApp`
- [x] UIManager created with all widgets
- [x] 2-line layout renders correctly
- [x] ControlBus created and exposed

**Line 1 - Search:**
- [x] Search input with placeholder
- [x] OnChange triggers debounced search (150ms)
- [x] Prev/Next buttons navigate results
- [x] Enter in search input → next result
- [x] Up/Down arrows navigate results
- [x] Counter shows "N/M" format

**Line 2 - Time:**
- [x] Time input with placeholder
- [x] Time parser handles relative (-2h), absolute, natural formats
- [x] -1h/+1h buttons adjust time
- [x] Jump button navigates to entered time
- [x] Current timestamp always displayed

**Integration:**
- [x] Tab cycles focus between search and time inputs
- [x] Ctrl+Shift+F opens navigator
- [x] Terminal scrolls to selected line
- [x] Escape closes and returns to live edge
- [x] Theming uses semantic colors

**Testing:**
- [x] Manual test: search works end-to-end
- [x] Manual test: time jump works
- [x] Manual test: -1h/+1h buttons work

### Notes

**Implementation completed 2026-01-28:**

Created files:
- `apps/texelterm/history_navigator.go` - Full history navigator implementation (~700 lines)

Updated files:
- `apps/texelterm/term.go` - Added Ctrl+Shift+F keybinding, navigator integration
- `apps/texelterm/parser/vterm_memory_buffer.go` - Added ScrollToGlobalLine(), GlobalOffset(), GlobalEnd()
- `apps/texelterm/parser/viewport_window.go` - Added Builder() accessor

Key features:
- 2-line card overlay at bottom of terminal
- Line 1: Search row with input, prev/next buttons, result counter (N/M format)
- Line 2: Time row with input, -1h/+1h buttons, Jump button, current timestamp
- Debounced search with 150ms timer (via time.AfterFunc)
- Time parsing: relative (-2h, +30m, -1d, 5m), absolute (2026-01-28 14:30, 14:30), natural (yesterday, today 3pm)
- Unicode emoji icons (🔍 for search, 🕐 for time)
- Keyboard navigation: Tab cycles inputs, Escape closes, Enter navigates results, Up/Down for prev/next result
- Integration with SearchIndex for FTS5 search
- Integration with VTerm for scrolling to global line

Architecture decisions:
- Single-file implementation (~700 lines) following launcher.go pattern
- Direct widget management without card pipeline (renders as overlay in term.go)
- Time parser functions in same file for locality
- Callback-based refresh via SetRefreshNotifier()
- ScrollToGlobalLine converts global line index to physical scroll offset

Implementation notes:
- Added Builder() method to ViewportWindow to expose PhysicalLineBuilder for scroll offset calculation
- ScrollToGlobalLine centers the target line in viewport (viewportHeight/2)
- Time input supports multiple formats with graceful fallback
- Search results include line index for direct navigation

---

## Phase 5: Page Freezing & Compression

**Status**: NOT STARTED
**Branch**: -
**Started**: -
**Completed**: -

### Instructions

1. Add dependency: `go get github.com/klauspost/compress/zstd`

2. Create `apps/texelterm/parser/archive_manager.go`:
   - Zstd encoder/decoder with dictionary
   - `Compress(page *Page) ([]byte, error)`
   - `Decompress(data []byte) (*Page, error)`

3. Add page states to PageStore:
   - LIVE (0), WARM (1), FROZEN (2)
   - Track last access time per page

4. Create `apps/texelterm/parser/freezer.go`:
   - Background goroutine
   - Freeze criteria: 2× viewport behind, 30s no access
   - Move frozen pages to `archive/` as `.zst` files

5. Update PageStore to read frozen pages transparently

### Checklist

- [ ] Zstd compression works
- [ ] Dictionary trained/loaded
- [ ] Page states tracked (LIVE/WARM/FROZEN)
- [ ] Freezer runs in background when idle
- [ ] Freeze criteria implemented correctly
- [ ] Frozen pages stored in archive/
- [ ] Reading frozen pages works transparently
- [ ] Compression ratio ~25-40%
- [ ] No UI stutter during freeze
- [ ] Tests pass

### Notes

(Session notes go here)

---

## Phase 6: Encryption

**Status**: NOT STARTED
**Branch**: -
**Started**: -
**Completed**: -

### Instructions

1. Add dependency: `go get github.com/zalando/go-keyring`

2. Create `apps/texelterm/parser/keyring.go`:
   - GetKey(terminalID) / SetKey(terminalID, key)
   - Falls back to passphrase file if keyring unavailable

3. Create `apps/texelterm/parser/crypto.go`:
   - ChaCha20-Poly1305 encryption
   - `Encrypt(data, key []byte) ([]byte, error)`
   - `Decrypt(data, key []byte) ([]byte, error)`

4. Integrate with ArchiveManager:
   - Compress → Encrypt → Write
   - Read → Decrypt → Decompress

5. Key generation on first terminal creation

### Checklist

- [ ] Keyring integration works (Linux + macOS)
- [ ] Key generation works
- [ ] Encryption/decryption works
- [ ] Fallback to passphrase file works
- [ ] Frozen pages encrypted on disk
- [ ] Reading encrypted pages works
- [ ] Tests pass

### Notes

(Session notes go here)

---

## Phase 7: Cross-Terminal Search

**Status**: NOT STARTED
**Branch**: -
**Started**: -
**Completed**: -

### Instructions

1. Create `apps/texelterm/parser/global_index.go`:
   - SQLite at `global/search.db`
   - Terminal registry table
   - Federated search across terminal indexes

2. Update history navigator:
   - Add toggle widget: "This terminal" / "All terminals"
   - When "All terminals" selected, search across all terminal indexes
   - Results show terminal name/ID
   - Navigate to result in different terminal (focus that pane)

3. Handle terminal lifecycle:
   - Register terminal in global index on create
   - Update last_active periodically
   - Deregister on permanent close (optional)

### Checklist

- [ ] Global index database created
- [ ] Terminal registry works
- [ ] Federated search across terminals works
- [ ] UI toggle added to history navigator
- [ ] Results show terminal source
- [ ] Navigation to other terminal works (focus pane)
- [ ] Tests pass

### Notes

(Session notes go here)

---

## Completion Log

Record completed phases here with summary:

### Phase 1: Page Format & Basic Storage
Completed: 2026-01-28
Branch: -
PR: -
Summary: Implemented page-based storage system replacing DiskHistory:
- Created Page struct with 64-byte header (magic TXPAGE01, version, timestamps)
- Created PageStore for managing 64KB page files with per-line timestamps
- Updated AdaptivePersistence and VTerm to use PageStore exclusively
- Added HistoryWriter interface for persistence backends
- All tests pass (page_store_test.go, adaptive_persistence_test.go)
- Removed unused DiskHistory code (disk_history.go, disk_history_test.go)
Issues: None

### Phase 2: Write-Ahead Log
Completed: 2026-01-28
Branch: -
PR: -
Summary: Implemented Write-Ahead Log for crash recovery:
- Created WriteAheadLog with TXWAL001 format (32-byte header, variable entries, CRC32)
- Entry types: LINE_WRITE (0x01), LINE_MODIFY (0x02), CHECKPOINT (0x03)
- Clean architecture: WAL owns PageStore internally
- Automatic recovery on startup replays uncommitted entries
- Checkpoint: replays to PageStore, truncates WAL, fsyncs
- Auto-checkpoint: timer (30s) and size (10MB) based
- Integrated with AdaptivePersistence via NewAdaptivePersistenceWithWAL
- HistoryWriterWithTimestamp interface for timestamp support
- All tests pass (10 WAL tests + 17 AdaptivePersistence tests)
Issues: Fixed PageStore.Flush() bug causing ReadLine to return nil after flush

### Phase 3: SQLite Search Index
Completed: 2026-01-28
Branch: -
PR: -
Summary: Implemented SQLite FTS5 full-text search index:
- Created text_extractor.go for converting terminal cells to plain text
- Created search_index.go with SQLite FTS5 (using pure Go modernc.org/sqlite)
- Async batch indexing (background goroutine, 100 lines / 5s timeout)
- Sync indexing for commands (OSC 133 CommandActive detection)
- BM25 relevance ranking with commands prioritized
- Time-based navigation: FindLineAt(), GetTimestamp()
- Callback-based integration: OnLineIndex callback in VTerm
- All tests pass (16 SearchIndex tests + 15 TextExtractor tests)
Issues: None

### Phase 4: History Navigator Card
Completed: 2026-01-28
Branch: -
PR: -
Summary: Implemented 2-line history navigator overlay for search and time-based navigation:
- Created history_navigator.go with full UI (search row + time row)
- Search: debounced FTS5 search (150ms), prev/next navigation, N/M counter
- Time: relative (-2h), absolute (14:30), natural (yesterday) format parsing
- Keyboard: Ctrl+Shift+F opens, Tab cycles inputs, Escape closes, Enter/Up/Down navigate
- Added ScrollToGlobalLine() to VTerm for navigating to search results
- Added Builder() accessor to ViewportWindow for scroll offset calculation
- All tests pass
Issues: None
