# Sparse-Indexed PageStore Design

## Problem

`PageStore.AppendLine` does not honor a global line index — it appends to the next storage slot. The WAL's `nextGlobalIdx` and `pageStore.totalLineCount` only stay in sync if every `LineWrite` covers a strictly contiguous range starting from the previous `pageStore.LineCount()`.

`AdaptivePersistence.flushPendingLocked` sorts `pendingLines` (a map keyed by `lineIdx`) and calls `wal.Append(idx, …)` for each. If the map has a gap — e.g. `{100, 102, 103}` because line 101 wasn't dirty in this flush window — every entry gets written as a `LineWrite` (each `lineIdx >= nextGlobalIdx` after the previous bumps it). Pass 1 of `checkpointLocked` then calls `pageStore.AppendLine` three times → `pageStore.totalLineCount = 3`, while `wal.nextGlobalIdx = 104`. The two diverge.

Once they diverge, a later real `LineModify` for an old line `lineIdx = 50` becomes `Append(50)` → `50 < 104` → `EntryTypeLineModify` → Pass 2 calls `pageStore.UpdateLine(50, …)` → out of bounds because PageStore only has 3 lines.

Observed in production logs:
```
Error closing memory buffer: failed to update line 97380 in PageStore: line index 97380 out of bounds (0-61579)
```

The drift also causes "stale metadata" on graceful close: `LiveEdgeBase = 97380` passes the clamp against `wal.NextGlobalIdx() = 97436`, but the actual `pageStore.totalLineCount = 61580`. On reload, `loadHistoryFromDisk` reads 61580 lines and accepts metadata pointing past them.

## Goal

Make global line index a first-class identifier in `PageStore`. Gaps are allowed and intentional. The WAL's view of "what global indices exist" matches PageStore's view exactly, with no implicit reindexing.

## Non-Goals

- No on-disk format change. Existing pages already carry `FirstGlobalIdx` in their header and are dense within a page; that remains true.
- No backward-compat wrappers for the old positional `AppendLine`/`AppendLineWithTimestamp` API. Remove them and update every caller.

## Architecture

### Invariant

> **Every line stored in PageStore is identified by its global line index. `LineCount()` returns the logical end (`max(stored globalIdx) + 1`), not the count of stored lines.**

Within a single page, lines remain dense: `globalIdx[i] = page.FirstGlobalIdx + i`. **A gap in the global index sequence forces a page boundary.** This keeps the on-disk format unchanged while letting the in-memory index represent sparse data.

### PageStore changes (`apps/texelterm/parser/page_store.go`)

**Index entry:**
```go
type pageIndexEntry struct {
    globalIdx    int64  // NEW: global line index this entry represents
    pageID       uint64
    offsetInPage int
}
```
The `pageIndex` slice stays sorted by `globalIdx`. Lookups use binary search.

**New API:**
```go
// AppendLineWithGlobalIdx writes a line at the specified global index.
// If globalIdx is not contiguous with the current page (or there is no current
// page), the current page is flushed and a new page is started anchored at globalIdx.
// globalIdx must be strictly greater than every previously stored globalIdx.
func (ps *PageStore) AppendLineWithGlobalIdx(globalIdx int64, line *LogicalLine, ts time.Time) error
```

Behavior:
- If `ps.currentPage == nil`: start a new page anchored at `globalIdx`.
- If `globalIdx == currentPage.FirstGlobalIdx + int64(currentPage.LineCount)`: append in place.
- Else (gap, or current page is full and must be flushed mid-call): flush current page, start a new page anchored at `globalIdx`, then add.
- After successful add, append `pageIndexEntry{globalIdx, pageID, offsetInPage}` to `ps.pageIndex`.
- Update `ps.nextGlobalIdx = globalIdx + 1` (or leave as is if equal).
- Increment `ps.totalLineCount` (now means "stored count", used internally for diagnostics).

**Removed API:** `AppendLine` and `AppendLineWithTimestamp` are deleted.

**Modified API semantics:**

- `LineCount() int64` — returns `ps.nextGlobalIdx` (logical end). When empty, returns 0.
- `StoredLineCount() int64` — NEW: returns `ps.totalLineCount` (number of stored lines). Used by diagnostics and tests asserting density.
- `UpdateLine(globalIdx int64, line, ts) error` — binary search `pageIndex` by `globalIdx`. If not found → error `"line %d not present in PageStore"`.
- `ReadLine(globalIdx int64) (*LogicalLine, error)` — binary search; returns `(nil, nil)` if not present (gap or out of range). Existing callers already treat nil as "not available".
- `ReadLineWithTimestamp(globalIdx int64)` — same.
- `ReadLineRange(start, end int64) ([]*LogicalLine, error)` — iterates `[start, end)` and returns one entry per global index in range; `nil` for gaps. Length of returned slice equals `end - start` (caller can index by `globalIdx - start`). This is a behavior change from "list of stored lines" to "dense slice possibly containing nils".
- `FindLineAt(t time.Time)` — binary search over the sorted `pageIndex`; result is a global index, not a positional index.
- `Flush()` — unchanged in spirit; preserves the next-page anchor.
- `Close()` — unchanged.

**`rebuildIndex()`:** for each page, populate `pageIndex` entries with `globalIdx = page.FirstGlobalIdx + int64(i)`. Initialize `ps.nextGlobalIdx = max(ps.nextGlobalIdx, page.FirstGlobalIdx + int64(page.LineCount))` over all pages. Existing on-disk pages (which are contiguous) load correctly: their pageIndex becomes `[0, 1, 2, …]` and `nextGlobalIdx == totalLineCount`.

**`prepareForAppend()`:** load the last page (highest pageID). Adopt it as the current page only if `lastEntry.globalIdx + 1 == ps.nextGlobalIdx` and the page is not full. Otherwise leave `currentPage = nil` and let the next `AppendLineWithGlobalIdx` start a fresh page.

### WAL changes (`apps/texelterm/parser/write_ahead_log.go`)

- Init at line 203: `w.nextGlobalIdx = w.pageStore.LineCount()` — unchanged (now both mean "logical end").
- `recoverFromWAL` (around line 741): replace `pageStore.AppendLineWithTimestamp(entry.Line, entry.Timestamp)` with `pageStore.AppendLineWithGlobalIdx(int64(entry.GlobalLineIdx), entry.Line, entry.Timestamp)`.
- `checkpointLocked` Pass 1 (line 894): same swap.
- The `LineModify` distinction in `Append()` (line 387) is unchanged. It already keys off `nextGlobalIdx`, which now exactly matches PageStore's logical end, so the only `LineModify` entries written will be for lines that are actually present.
- `RecoveredMetadata` and metadata clamping are unchanged in code but become unnecessary in practice — they remain as defense-in-depth.

### AdaptivePersistence changes (`apps/texelterm/parser/adaptive_persistence.go`)

- `flushPendingLocked` already uses `wal.NextGlobalIdx()` for its clamp, which now reflects logical end. No code change required for the clamp itself.
- Remove the `META_NOTIFY` and `META_WRITE` debug `log.Printf` calls (added during the investigation). Restore the original `debuglog.Printf` clamp message.

### vterm_memory_buffer.go changes

- Re-add the `liveEdgeRestored` defensive guard from PR #166: in `loadHistoryFromDisk`, only restore the cursor (`v.cursorY`, `v.cursorX`) if `LiveEdgeBase` was successfully restored from valid metadata. Belt-and-braces for ungraceful stops.

### viewport_content_reader.go changes

`ReadLineRange` now returns a slice that may contain `nil` for gaps. Update `getLogicalLineRange` (line 118 and 127) to substitute a blank `LogicalLine` (empty `Cells` slice) for any nil entry, so downstream rendering produces an empty row rather than panicking.

### Test/diagnostic call-site updates

Audit every caller of `pageStore.LineCount()`, `pageStore.ReadLine()`, `pageStore.ReadLineRange()`:

- `vterm_memory_buffer_test.go:1452,1601,1610,1616,1622` — most use `LineCount` to mean "logical end" (next available index). Audit and either keep as-is or switch to `StoredLineCount` if asserting on density. Tests that loop `for i := 0; i < LineCount; i++` reading every line must skip nils or use `StoredLineCount`.
- `wal_atomicity_test.go:61` — same audit.
- `adaptive_persistence_recovery_test.go:110,476,595,1027` — same audit.
- `burst_recovery_test.go:352` — same audit.
- `viewport_content_reader.go:92,118,127` — gap-tolerant.
- `vterm.go:395` — calls `ReadLine`; already nil-tolerant.
- `vterm_memory_buffer.go:166,192,358` — `LineCount` for boundary calc and `ReadLineRange` for backfill. Boundary calc wants logical end (correct under new semantics). The `ReadLineRange` call (line 358) backfills the memory buffer from disk; gaps must produce nil entries, which the existing path handles by skipping (verify and adjust).

## Data Flow

**Write path (no behavior change for callers):**

```
parser → memBuf.SetCell → ap.NotifyWrite(globalIdx)
                          → flushPendingLocked
                          → wal.Append(globalIdx, line, ts)
                              → if globalIdx >= nextGlobalIdx: LineWrite
                              → else: LineModify
                          → wal flushes; checkpoint eventually
                              → pageStore.AppendLineWithGlobalIdx(globalIdx, ...)  ← NEW
                                → page boundary on gap
```

**Read path:**

```
viewport → ReadLineRange(start, end) → returns []*LogicalLine length (end-start)
                                       → nil entries for gaps
                                       → caller substitutes blank line
```

## Testing

New tests in `page_store_test.go`:

1. **GapForcesPageBoundary:** Append globalIdx=0,1,2 then 100,101 → verify two pages, both readable, `LineCount()==102`, `StoredLineCount()==5`, `ReadLine(50)==(nil,nil)`.
2. **UpdateInGap:** After above, `UpdateLine(50, …)` returns "not present" error.
3. **UpdateExisting:** `UpdateLine(101, …)` succeeds and persists.
4. **ReadRangeWithGap:** `ReadLineRange(0, 102)` returns 102 entries with nils at 3..99.
5. **RebuildAfterClose:** Close, reopen, verify same logical/stored counts and reads.

New tests in `write_ahead_log_test.go`:

6. **CheckpointWithGap:** Append entries to WAL with non-contiguous globalIdx, force checkpoint, verify PageStore contains correct entries with correct global indices, no out-of-bounds.
7. **RecoveryWithGap:** Same setup, then close and reopen WAL → verify recovery preserves the gaps.

`TestVTermCoherence_StaleMetadataRecovery` (already exists from previous session): should still pass, and should now pass *naturally* because the drift that caused stale metadata is no longer possible.

## Migration / Compatibility

- **On-disk format unchanged.** Pages still use `FirstGlobalIdx + i` for per-line global index. Existing pages on disk are dense and load fine — they're a degenerate case of sparse with no gaps.
- **In-memory API breaking change:** `AppendLine` / `AppendLineWithTimestamp` removed. Every caller must move to `AppendLineWithGlobalIdx`. Tests updated.
- **WAL format unchanged.**

## Risk

- **Behavior change in `ReadLineRange`** (returns slice with nils). Audit every caller carefully; this is the most likely source of regression.
- **Test fan-out:** every test that creates pages directly via the old API needs updating. Mechanical but tedious.
- **Page-boundary explosion** if writes are extremely sparse (e.g., touching every 1000th line). Each gap forces a new page → many tiny pages on disk. Not a concern for normal terminal output (writes are mostly contiguous; gaps occur during scroll-region apps but are bounded). Acceptable.
