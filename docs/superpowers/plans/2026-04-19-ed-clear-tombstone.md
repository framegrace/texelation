# ED-Clear Tombstone Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Propagate sparse `ClearRange` ops (ED 0/1/2 + single-line invalidate) to disk via WAL tombstones + PageStore range deletes, so persisted history never re-emits cells the live session has already cleared.

**Architecture:** Add WAL op `EntryTypeLineDelete = 0x06`, bump WAL version 1→2. Add `PageStore.DeleteRange`. Refactor `AdaptivePersistence` pending state from a write-only map to a FIFO op list (writes + deletes) with sweep-on-delete so call order is preserved across a flush. Add `sparse.Terminal.ClearRangePersistent` + `ClearNotifier` callback. Switch 7 VTerm ED/invalidate call sites; `WriteWindow` scroll/newline clears stay in-memory.

**Tech Stack:** Go 1.24.3, existing `apps/texelterm/parser/` sparse stack. No new deps.

---

## File Structure

### Files to modify

- `apps/texelterm/parser/write_ahead_log.go` — bump version constant, add `EntryTypeLineDelete`, add `WALEntry.DeleteHi` field, add `AppendDelete`, extend `readEntry` + `recover`.
- `apps/texelterm/parser/page_store.go` — add `DeleteRange`.
- `apps/texelterm/parser/adaptive_persistence.go` — refactor `pendingLines` to `pendingOps` FIFO list; add `NotifyClearRange`; update `flushPendingLocked`.
- `apps/texelterm/parser/sparse/terminal.go` — add `ClearNotifier` interface, `SetClearNotifier`, `ClearRangePersistent`.
- `apps/texelterm/parser/main_screen.go` — add `ClearRangePersistent` to `MainScreen` interface.
- `apps/texelterm/parser/vterm_main_screen.go` — wire persistence as `ClearNotifier` on mainScreen; migrate 6 ED/invalidate call sites.
- `apps/texelterm/parser/vterm_erase.go` — migrate 1 ED call site.

### Files to create

- `apps/texelterm/parser/write_ahead_log_delete_test.go` — WAL delete round-trip + CRC corruption tests.
- `apps/texelterm/parser/page_store_delete_range_test.go` — `DeleteRange` edge-case tests.
- `apps/texelterm/parser/adaptive_persistence_clearrange_test.go` — FIFO ordering tests.
- `apps/texelterm/parser/sparse/terminal_clear_persistent_test.go` — notifier fan-out test.
- `apps/texelterm/parser/sparse/persistence_clear_roundtrip_test.go` — reload-skips-cleared test.
- `apps/texelterm/parser/osc133_anchor_clear_persistence_test.go` — end-to-end regression test.

---

## Task 1: Bump WAL version constant to 2

**Files:**
- Modify: `apps/texelterm/parser/write_ahead_log.go:40-54`
- Test: reuses existing `write_ahead_log_test.go`

- [ ] **Step 1: Write the failing test**

Add to `apps/texelterm/parser/write_ahead_log_test.go`:

```go
func TestWAL_HeaderVersionIs2(t *testing.T) {
	if WALVersion != 2 {
		t.Errorf("WALVersion = %d, want 2", WALVersion)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /home/marc/projects/texel/texelation-sparse && go test ./apps/texelterm/parser/ -run TestWAL_HeaderVersionIs2 -count=1`
Expected: FAIL (`WALVersion = 1, want 2`).

- [ ] **Step 3: Bump the version constant and add delete op code**

Edit `apps/texelterm/parser/write_ahead_log.go:40-54`:

```go
// WAL format constants
const (
	WALMagic      = "TXWAL001"
	WALVersion    = uint32(2)
	WALHeaderSize = 32
	WALEntryBase  = 1 + 8 + 8 + 4 + 4 // type + lineIdx + timestamp + dataLen + crc32 (no data)
)

// WAL entry types
const (
	EntryTypeLineWrite       uint8 = 0x01
	EntryTypeLineModify      uint8 = 0x02
	EntryTypeCheckpoint      uint8 = 0x03
	EntryTypeMetadata        uint8 = 0x04 // Legacy ViewportState
	EntryTypeMainScreenState uint8 = 0x05 // Sparse MainScreenState
	EntryTypeLineDelete      uint8 = 0x06 // Range tombstone: [lo, hi] inclusive
)
```

Also update the header doc-comment at `write_ahead_log.go:7-20` to list `LINE_DELETE=0x06` and bump `Version: uint32 (4 bytes) - value 2`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestWAL_HeaderVersionIs2 -count=1`
Expected: PASS.

- [ ] **Step 5: Run the full WAL test suite to catch version regressions**

Run: `go test ./apps/texelterm/parser/ -run TestWAL -count=1`
Expected: PASS. Any test that writes-then-reopens a WAL must still work because we upgraded both the writer and reader in the same change; tests that embed a hard-coded v1 magic will need adjustment — fix them to use `WALVersion` rather than literal `1`.

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/write_ahead_log.go apps/texelterm/parser/write_ahead_log_test.go
git commit -m "wal: bump version to 2 and reserve EntryTypeLineDelete"
```

---

## Task 2: `WriteAheadLog.AppendDelete` — encode+append one tombstone entry

**Files:**
- Modify: `apps/texelterm/parser/write_ahead_log.go` (add method near `Append` at line 390; add `DeleteHi` field on `WALEntry` at line 96)
- Test: `apps/texelterm/parser/write_ahead_log_delete_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/parser/write_ahead_log_delete_test.go`:

```go
package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestWAL(t *testing.T) (*WriteAheadLog, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultWALConfig(dir, "test-term")
	cfg.CheckpointInterval = 0
	wal, err := NewWriteAheadLog(cfg, time.Now)
	if err != nil {
		t.Fatalf("NewWriteAheadLog: %v", err)
	}
	return wal, filepath.Join(cfg.WALDir, "wal.log")
}

func TestWAL_AppendDeleteRoundTrip(t *testing.T) {
	wal, walPath := newTestWAL(t)
	ts := time.Unix(1700000000, 0)
	if err := wal.AppendDelete(5, 10, ts); err != nil {
		t.Fatalf("AppendDelete: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and walk entries using readEntryStandalone semantics via recover.
	// We use the low-level reader to verify the bytes on disk.
	f, err := os.Open(walPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	if _, err := f.Seek(int64(WALHeaderSize), 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	entry, _, err := (&WriteAheadLog{}).readEntry(bufioNewReaderForTest(f))
	if err != nil {
		t.Fatalf("readEntry: %v", err)
	}
	if entry.Type != EntryTypeLineDelete {
		t.Errorf("Type = %#x, want %#x", entry.Type, EntryTypeLineDelete)
	}
	if entry.GlobalLineIdx != 5 {
		t.Errorf("lo = %d, want 5", entry.GlobalLineIdx)
	}
	if entry.DeleteHi != 10 {
		t.Errorf("hi = %d, want 10", entry.DeleteHi)
	}
}
```

Also add a tiny helper near the top of the file (same file is fine; keep local to test package):

```go
import "bufio"

func bufioNewReaderForTest(f *os.File) *bufio.Reader { return bufio.NewReader(f) }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestWAL_AppendDeleteRoundTrip -count=1`
Expected: FAIL — `AppendDelete` / `DeleteHi` undefined.

- [ ] **Step 3: Add `DeleteHi` field on `WALEntry`**

Edit `apps/texelterm/parser/write_ahead_log.go:95-103`:

```go
// WALEntry represents a single entry in the WAL.
type WALEntry struct {
	Type            uint8
	GlobalLineIdx   uint64
	Timestamp       time.Time
	Line            *LogicalLine     // nil for CHECKPOINT / METADATA / DELETE entries
	Metadata        *ViewportState   // nil for non-METADATA entries
	MainScreenState *MainScreenState // nil for non-MAIN_SCREEN_STATE entries
	DeleteHi        int64            // valid only for EntryTypeLineDelete (inclusive upper bound)
}
```

- [ ] **Step 4: Add `AppendDelete` method**

Add to `apps/texelterm/parser/write_ahead_log.go` (place it directly after `Append` at line 430):

```go
// AppendDelete writes a range-delete tombstone [lo, hi] to the WAL. Emits one
// entry, not one per line. Caller (PageStore.DeleteRange) applies the delete
// to the in-memory page index after this returns successfully.
func (w *WriteAheadLog) AppendDelete(lo, hi int64, timestamp time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return fmt.Errorf("WAL is closed")
	}
	if lo < 0 || hi < lo {
		return fmt.Errorf("invalid delete range [%d, %d]", lo, hi)
	}

	entryData, err := w.encodeDeleteEntry(uint64(lo), uint64(hi), timestamp)
	if err != nil {
		return fmt.Errorf("failed to encode delete entry: %w", err)
	}
	if _, err := w.walFile.Write(entryData); err != nil {
		return fmt.Errorf("failed to write delete entry: %w", err)
	}
	w.walSize += int64(len(entryData))
	w.entriesWritten++
	return nil
}

// encodeDeleteEntry builds a 33-byte WAL entry for a range tombstone:
// type(1) + lo(8, GlobalLineIdx) + timestamp(8) + dataLen(4, =8) + hi(8) + crc32(4).
func (w *WriteAheadLog) encodeDeleteEntry(lo, hi uint64, timestamp time.Time) ([]byte, error) {
	const dataLen = 8
	totalSize := WALEntryBase + dataLen
	buf := make([]byte, totalSize)
	buf[0] = EntryTypeLineDelete
	binary.LittleEndian.PutUint64(buf[1:9], lo)
	binary.LittleEndian.PutUint64(buf[9:17], uint64(timestamp.UnixNano()))
	binary.LittleEndian.PutUint32(buf[17:21], dataLen)
	binary.LittleEndian.PutUint64(buf[21:29], hi)
	crc := crc32.ChecksumIEEE(buf[:totalSize-4])
	binary.LittleEndian.PutUint32(buf[totalSize-4:], crc)
	return buf, nil
}
```

- [ ] **Step 5: Extend `readEntry` to decode delete entries**

Edit `apps/texelterm/parser/write_ahead_log.go:963-987` — add a new case inside the `switch entryType` block (right after `case EntryTypeCheckpoint:`):

```go
	case EntryTypeLineDelete:
		if dataLen != 8 {
			return WALEntry{}, totalSize, fmt.Errorf("delete entry dataLen = %d, want 8", dataLen)
		}
		hi := int64(binary.LittleEndian.Uint64(lineData))
		return WALEntry{
			Type:          entryType,
			GlobalLineIdx: lineIdx,
			Timestamp:     timestamp,
			DeleteHi:      hi,
		}, totalSize, nil
```

(The existing final `return WALEntry{...}` at line 989-996 stays intact for the non-delete path.)

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestWAL_AppendDeleteRoundTrip -count=1`
Expected: PASS.

- [ ] **Step 7: Add a CRC-corruption regression test**

Append to `write_ahead_log_delete_test.go`:

```go
func TestWAL_CorruptDeleteEntryTruncates(t *testing.T) {
	wal, walPath := newTestWAL(t)
	ts := time.Unix(1700000000, 0)
	if err := wal.AppendDelete(5, 10, ts); err != nil {
		t.Fatalf("AppendDelete: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Corrupt the CRC (last 4 bytes of the file)
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	f, err := os.OpenFile(walPath, os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF}, info.Size()-4); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	f.Close()

	// Reopen — recover should truncate and succeed.
	cfg := DefaultWALConfig(filepath.Dir(filepath.Dir(walPath)), "test-term")
	cfg.CheckpointInterval = 0
	wal2, err := NewWriteAheadLog(cfg, time.Now)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer wal2.Close()
	info2, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("Stat after recover: %v", err)
	}
	if info2.Size() != int64(WALHeaderSize) {
		t.Errorf("WAL size after corrupted-entry recover = %d, want %d (header only)", info2.Size(), WALHeaderSize)
	}
}
```

Run: `go test ./apps/texelterm/parser/ -run TestWAL_CorruptDeleteEntryTruncates -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add apps/texelterm/parser/write_ahead_log.go apps/texelterm/parser/write_ahead_log_delete_test.go
git commit -m "wal: add AppendDelete for range tombstones"
```

---

## Task 3: WAL `recover` applies delete entries to PageStore

**Files:**
- Modify: `apps/texelterm/parser/write_ahead_log.go:825-888` (recover loop)
- Test: `write_ahead_log_delete_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `apps/texelterm/parser/write_ahead_log_delete_test.go`:

```go
func TestWAL_RecoverAppliesDelete(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultWALConfig(dir, "test-term")
	cfg.CheckpointInterval = 0
	wal, err := NewWriteAheadLog(cfg, time.Now)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	// Write 5 lines, then tombstone lines 1..3.
	ll := func(text string) *LogicalLine {
		cells := make([]Cell, len(text))
		for i, r := range text {
			cells[i] = Cell{Rune: r}
		}
		return &LogicalLine{Cells: cells}
	}
	for i := 0; i < 5; i++ {
		if err := wal.Append(int64(i), ll("line"), time.Now()); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := wal.AppendDelete(1, 3, time.Now()); err != nil {
		t.Fatalf("AppendDelete: %v", err)
	}
	// Do NOT checkpoint; close so the WAL keeps its entries.
	if err := wal.walFile.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	wal.walFile.Close()

	// Reopen; recover should replay both writes and the delete.
	wal2, err := NewWriteAheadLog(cfg, time.Now)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer wal2.Close()
	ps := wal2.PageStore()
	if got := ps.StoredLineCount(); got != 2 {
		t.Errorf("StoredLineCount = %d, want 2 (lines 0 and 4)", got)
	}
	if gi := ps.GlobalIdxAtStoredPosition(0); gi != 0 {
		t.Errorf("first stored globalIdx = %d, want 0", gi)
	}
	if gi := ps.GlobalIdxAtStoredPosition(1); gi != 4 {
		t.Errorf("second stored globalIdx = %d, want 4", gi)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestWAL_RecoverAppliesDelete -count=1`
Expected: FAIL — deletes not replayed, `StoredLineCount` returns 5.

- [ ] **Step 3: Make `recover` apply delete entries**

Edit `apps/texelterm/parser/write_ahead_log.go:826-853`. Replace the entry-classification loop so delete entries are kept in the replay list. Change:

```go
		} else if entry.Type == EntryTypeMetadata {
			lastMetadata = entry.Metadata
		} else if entry.Type == EntryTypeMainScreenState {
			lastMainScreenState = entry.MainScreenState
		} else {
			entries = append(entries, entry)
		}
```

to:

```go
		} else if entry.Type == EntryTypeMetadata {
			lastMetadata = entry.Metadata
		} else if entry.Type == EntryTypeMainScreenState {
			lastMainScreenState = entry.MainScreenState
		} else if entry.Type == EntryTypeLineDelete {
			entries = append(entries, entry)
		} else {
			entries = append(entries, entry)
		}
```

(Two branches kept separate for clarity; a linter merge is fine.)

Now edit the replay loop at `write_ahead_log.go:877-888`. Replace:

```go
	for _, entry := range entries {
		if entry.Line == nil {
			continue
		}
		if entry.Type != EntryTypeLineWrite && entry.Type != EntryTypeLineModify {
			continue
		}
		lineIdx := int64(entry.GlobalLineIdx)
		if err := w.pageStore.AppendLineWithGlobalIdx(lineIdx, entry.Line, entry.Timestamp); err != nil {
			return fmt.Errorf("failed to replay line %d: %w", entry.GlobalLineIdx, err)
		}
	}
```

with:

```go
	for _, entry := range entries {
		switch entry.Type {
		case EntryTypeLineWrite, EntryTypeLineModify:
			if entry.Line == nil {
				continue
			}
			lineIdx := int64(entry.GlobalLineIdx)
			if err := w.pageStore.AppendLineWithGlobalIdx(lineIdx, entry.Line, entry.Timestamp); err != nil {
				return fmt.Errorf("failed to replay line %d: %w", entry.GlobalLineIdx, err)
			}
		case EntryTypeLineDelete:
			lo := int64(entry.GlobalLineIdx)
			hi := entry.DeleteHi
			if err := w.pageStore.deleteRangeNoWAL(lo, hi); err != nil {
				return fmt.Errorf("failed to replay delete [%d, %d]: %w", lo, hi, err)
			}
		}
	}
```

(`deleteRangeNoWAL` is the bypass helper we add in Task 4. Replay must NOT re-emit a WAL entry.)

- [ ] **Step 4: Run the test to verify it still fails with a clear signal**

Run: `go test ./apps/texelterm/parser/ -run TestWAL_RecoverAppliesDelete -count=1`
Expected: FAIL because `deleteRangeNoWAL` doesn't exist yet. That's the correct next-step shape — we wire replay before the target method to drive the Task 4 contract.

- [ ] **Step 5: Commit the replay wiring**

Leave the test failing until Task 4 lands. Commit only the WAL changes:

```bash
git add apps/texelterm/parser/write_ahead_log.go
git commit -m "wal: replay EntryTypeLineDelete during recover"
```

---

## Task 4: `PageStore.DeleteRange` — remove pageIndex entries in [lo, hi]

**Files:**
- Modify: `apps/texelterm/parser/page_store.go` (add near `AppendLineWithGlobalIdx` around line 548)
- Test: `apps/texelterm/parser/page_store_delete_range_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `apps/texelterm/parser/page_store_delete_range_test.go`:

```go
package parser

import (
	"testing"
	"time"
)

func seedLines(t *testing.T, ps *PageStore, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		ll := &LogicalLine{Cells: []Cell{{Rune: 'x'}}}
		if err := ps.AppendLineWithGlobalIdx(int64(i), ll, time.Now()); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if err := ps.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestPageStore_DeleteRangeMiddle(t *testing.T) {
	dir := t.TempDir()
	ps, err := NewPageStore(DefaultPageStoreConfig(dir, "term-x"))
	if err != nil {
		t.Fatalf("NewPageStore: %v", err)
	}
	defer ps.Close()
	seedLines(t, ps, 10)
	if err := ps.DeleteRange(3, 7); err != nil {
		t.Fatalf("DeleteRange: %v", err)
	}
	if got := ps.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount = %d, want 5", got)
	}
	// Remaining globalIdxs, in stored order: 0, 1, 2, 8, 9.
	want := []int64{0, 1, 2, 8, 9}
	for i, w := range want {
		if got := ps.GlobalIdxAtStoredPosition(i); got != w {
			t.Errorf("pos %d: got %d want %d", i, got, w)
		}
	}
}

func TestPageStore_DeleteRangeEmpty(t *testing.T) {
	dir := t.TempDir()
	ps, _ := NewPageStore(DefaultPageStoreConfig(dir, "term-x"))
	defer ps.Close()
	seedLines(t, ps, 3)
	// Range contains no entries.
	if err := ps.DeleteRange(100, 200); err != nil {
		t.Fatalf("DeleteRange empty: %v", err)
	}
	if got := ps.StoredLineCount(); got != 3 {
		t.Errorf("StoredLineCount = %d, want 3", got)
	}
}

func TestPageStore_DeleteRangeWholeStore(t *testing.T) {
	dir := t.TempDir()
	ps, _ := NewPageStore(DefaultPageStoreConfig(dir, "term-x"))
	defer ps.Close()
	seedLines(t, ps, 4)
	if err := ps.DeleteRange(0, 3); err != nil {
		t.Fatalf("DeleteRange: %v", err)
	}
	if got := ps.StoredLineCount(); got != 0 {
		t.Errorf("StoredLineCount = %d, want 0", got)
	}
}

func TestPageStore_DeleteRangeInvalid(t *testing.T) {
	dir := t.TempDir()
	ps, _ := NewPageStore(DefaultPageStoreConfig(dir, "term-x"))
	defer ps.Close()
	if err := ps.DeleteRange(5, 3); err == nil {
		t.Error("DeleteRange(5, 3) = nil, want error")
	}
	if err := ps.DeleteRange(-1, 3); err == nil {
		t.Error("DeleteRange(-1, 3) = nil, want error")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_DeleteRange -count=1`
Expected: FAIL — `DeleteRange` undefined.

- [ ] **Step 3: Implement `DeleteRange` and the no-WAL internal helper**

Add to `apps/texelterm/parser/page_store.go` (directly after `AppendLineWithGlobalIdx`):

```go
// DeleteRange removes all pageIndex entries whose globalIdx falls in the closed
// interval [lo, hi], emits one WAL tombstone (EntryTypeLineDelete), and
// decrements totalLineCount. Page-file bytes are NOT reclaimed; correctness
// depends on pageIndex absence. Safe no-op for empty ranges — the WAL entry is
// still emitted so recovery ordering is deterministic.
func (ps *PageStore) DeleteRange(lo, hi int64) error {
	if lo < 0 || hi < lo {
		return fmt.Errorf("invalid delete range [%d, %d]", lo, hi)
	}
	if ps.wal != nil {
		if err := ps.wal.AppendDelete(lo, hi, time.Now()); err != nil {
			return fmt.Errorf("wal append delete: %w", err)
		}
	}
	return ps.deleteRangeNoWAL(lo, hi)
}

// deleteRangeNoWAL mutates pageIndex + totalLineCount without emitting a WAL
// entry. Used by DeleteRange after it emits, and by WAL.recover() replay (the
// on-disk tombstone already exists).
func (ps *PageStore) deleteRangeNoWAL(lo, hi int64) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Binary search for first entry with globalIdx >= lo.
	start := sort.Search(len(ps.pageIndex), func(i int) bool {
		return ps.pageIndex[i].globalIdx >= lo
	})
	// Scan forward while globalIdx <= hi.
	end := start
	for end < len(ps.pageIndex) && ps.pageIndex[end].globalIdx <= hi {
		end++
	}
	if end > start {
		ps.pageIndex = append(ps.pageIndex[:start], ps.pageIndex[end:]...)
		ps.totalLineCount -= int64(end - start)
		if ps.totalLineCount < 0 {
			ps.totalLineCount = 0
		}
	}
	// Drop the current unflushed page if its range falls entirely inside [lo, hi].
	if ps.currentPage != nil && len(ps.currentPage.entries) > 0 {
		first := ps.currentPage.entries[0].globalIdx
		last := ps.currentPage.entries[len(ps.currentPage.entries)-1].globalIdx
		if first >= lo && last <= hi {
			ps.currentPage = nil
		}
	}
	return nil
}
```

Imports: make sure `"sort"` and `"time"` are present in `page_store.go`'s import block — they likely already are. `fmt` is already imported.

**Note on `ps.wal` field:** `PageStore` currently does not hold a `*WriteAheadLog` reference (the WAL owns the PageStore, not the other way around). This would force `DeleteRange` into the WAL itself. **Verify before implementing:** read `page_store.go`'s struct definition (around line 192) to confirm. If `ps.wal` does not exist, instead:

- Move `DeleteRange` onto `WriteAheadLog` as `DeleteRange(lo, hi int64) error`, which internally does `w.AppendDelete(lo, hi, time.Now())` followed by `w.pageStore.deleteRangeNoWAL(lo, hi)`.
- Callers (AdaptivePersistence flush path) use `wal.DeleteRange`.
- The test above changes to call `wal.DeleteRange` instead of `ps.DeleteRange`.

Pick the option that matches the existing ownership direction after reading the struct. Keep `deleteRangeNoWAL` as a `PageStore` method either way — `WAL.recover` calls it directly.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_DeleteRange -count=1`
Expected: PASS.

- [ ] **Step 5: Re-run the Task 3 recover test**

Run: `go test ./apps/texelterm/parser/ -run TestWAL_RecoverAppliesDelete -count=1`
Expected: PASS. This closes the loop opened in Task 3.

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_delete_range_test.go
git commit -m "page_store: add DeleteRange + deleteRangeNoWAL"
```

---

## Task 5: `AdaptivePersistence` FIFO ops list refactor (no behaviour change yet)

**Files:**
- Modify: `apps/texelterm/parser/adaptive_persistence.go:109-200` (struct), `324-410` (NotifyWrite), `606-680` (flushPendingLocked)
- Test: extend `apps/texelterm/parser/adaptive_persistence_test.go` with a strict ordering test that still passes after the refactor.

- [ ] **Step 1: Write the behaviour-preserving test**

Add to `adaptive_persistence_test.go`:

```go
func TestAdaptivePersistence_FlushesInCallOrder(t *testing.T) {
	ap, store := newTestAdaptivePersistenceWriteThrough(t) // existing helper; reuse or adapt
	ap.NotifyWrite(3)
	ap.NotifyWrite(7)
	ap.NotifyWrite(5)
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got := store.AppendedOrder() // existing test-helper; returns []int64
	want := []int64{3, 7, 5}
	if !equalInt64(got, want) {
		t.Errorf("append order = %v, want %v", got, want)
	}
}
```

If `newTestAdaptivePersistenceWriteThrough` / `AppendedOrder` / `equalInt64` do not yet exist, add them in this same test file; base them on the existing test scaffolding at the top of `adaptive_persistence_test.go`.

- [ ] **Step 2: Run the test to verify it passes on current implementation**

Run: `go test ./apps/texelterm/parser/ -run TestAdaptivePersistence_FlushesInCallOrder -count=1`
Expected: PASS (current code sorts by lineIdx at flush — fortunately 3<5<7, but `3,7,5` will surface the order mismatch). **If it passes on current code**, the test is not strict enough; the ordering check above should FAIL on the current `sort.Slice`-based flush. That failure is the signal to refactor.

- [ ] **Step 3: Refactor pending state to an ops list**

Edit `adaptive_persistence.go` struct definition (around line 109-200). Replace the `pendingLines map[int64]*pendingLineInfo` field with:

```go
type opKind uint8

const (
	opWrite  opKind = 1
	opDelete opKind = 2
)

type pendingOp struct {
	kind    opKind
	lo, hi  int64     // for writes: lo == hi == lineIdx
	ts      time.Time
	isCmd   bool      // writes only
	dropped bool      // superseded by a later write or swallowed by a delete
}

type AdaptivePersistence struct {
	// ... existing fields up through timers/config ...
	pendingOps []pendingOp
	pendingSet map[int64]int // lineIdx -> index in pendingOps for the most recent write
	// ... existing fields below ...
}
```

Update the constructor (`newAdaptivePersistenceWithWAL` / `NewAdaptivePersistence`) to initialise `pendingSet: make(map[int64]int)` instead of `pendingLines: make(map[int64]*pendingLineInfo)`. Remove the `pendingLineInfo` type declaration if nothing else consumes it.

- [ ] **Step 4: Rewrite `NotifyWrite` / `NotifyWriteWithMeta`**

Current code (around line 324-380) updates the map. Replace with an append-plus-supersede pattern:

```go
func (ap *AdaptivePersistence) NotifyWrite(lineIdx int64) {
	ap.NotifyWriteWithMeta(lineIdx, false)
}

func (ap *AdaptivePersistence) NotifyWriteWithMeta(lineIdx int64, isCmd bool) {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	if prev, ok := ap.pendingSet[lineIdx]; ok {
		ap.pendingOps[prev].dropped = true
	}
	ap.pendingOps = append(ap.pendingOps, pendingOp{
		kind:  opWrite,
		lo:    lineIdx,
		hi:    lineIdx,
		ts:    ap.nowFunc(),
		isCmd: isCmd,
	})
	ap.pendingSet[lineIdx] = len(ap.pendingOps) - 1

	ap.scheduleFlushLocked() // existing helper or inlined timer arm
}
```

- [ ] **Step 5: Rewrite `flushPendingLocked`**

Replace the body (around line 606-680) with:

```go
func (ap *AdaptivePersistence) flushPendingLocked() error {
	if len(ap.pendingOps) == 0 {
		return nil
	}
	ops := ap.pendingOps
	ap.pendingOps = nil
	ap.pendingSet = make(map[int64]int)

	for _, op := range ops {
		if op.dropped {
			continue
		}
		switch op.kind {
		case opWrite:
			cells := ap.store.GetLine(op.lo) // LineStore interface
			if cells == nil {
				continue
			}
			clone := cloneCells(cells) // existing helper
			ll := &LogicalLine{Cells: clone}
			if err := ap.wal.Append(op.lo, ll, op.ts); err != nil {
				return err
			}
		case opDelete:
			if err := ap.wal.DeleteRange(op.lo, op.hi); err != nil {
				return err
			}
		}
	}
	return nil
}
```

(`ap.wal.DeleteRange` was added in Task 4's fallback option. If you went with `ps.DeleteRange`, substitute `ap.wal.pageStore.DeleteRange(...)` or expose a matching method.)

- [ ] **Step 6: Run the full AdaptivePersistence test suite**

Run: `go test ./apps/texelterm/parser/ -run TestAdaptivePersistence -count=1`
Expected: PASS. The `TestAdaptivePersistence_FlushesInCallOrder` added in Step 1 is the canary for ordering.

- [ ] **Step 7: Commit**

```bash
git add apps/texelterm/parser/adaptive_persistence.go apps/texelterm/parser/adaptive_persistence_test.go
git commit -m "adaptive_persistence: refactor pending state to FIFO ops list"
```

---

## Task 6: `AdaptivePersistence.NotifyClearRange` + sweep-on-delete

**Files:**
- Modify: `apps/texelterm/parser/adaptive_persistence.go` (add method)
- Test: `apps/texelterm/parser/adaptive_persistence_clearrange_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `apps/texelterm/parser/adaptive_persistence_clearrange_test.go`:

```go
package parser

import (
	"testing"
)

func TestAdaptivePersistence_NotifyClearRangeDropsQueuedWrites(t *testing.T) {
	ap, store := newTestAdaptivePersistenceWriteThrough(t)
	ap.NotifyWrite(5)
	ap.NotifyWrite(6)
	ap.NotifyClearRange(0, 10)
	ap.NotifyWrite(6) // new write post-clear should survive
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	opOrder := store.RecordedOps() // []string like {"W5","W6","D0-10","W6"}
	want := []string{"W5", "W6", "D0-10", "W6"}
	if !equalStrings(opOrder, want) {
		t.Errorf("ops = %v, want %v", opOrder, want)
	}
}

func TestAdaptivePersistence_NotifyClearRangeSweepsSupersededWrite(t *testing.T) {
	// Writes inside the cleared range that were queued but not yet flushed
	// must be marked dropped so they don't flush at all.
	ap, store := newTestAdaptivePersistenceDebounced(t) // writes don't flush immediately
	ap.NotifyWrite(5)
	ap.NotifyClearRange(0, 10)
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	opOrder := store.RecordedOps()
	want := []string{"D0-10"} // W5 swept
	if !equalStrings(opOrder, want) {
		t.Errorf("ops = %v, want %v", opOrder, want)
	}
}
```

Add test helpers (`RecordedOps`, `newTestAdaptivePersistenceDebounced`, `equalStrings`) to whatever test-scaffolding file already exists for adaptive_persistence.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./apps/texelterm/parser/ -run TestAdaptivePersistence_NotifyClearRange -count=1`
Expected: FAIL — `NotifyClearRange` undefined.

- [ ] **Step 3: Implement `NotifyClearRange`**

Add to `adaptive_persistence.go`:

```go
// NotifyClearRange records a [lo, hi] tombstone. Sweeps pendingSet for any
// queued writes inside the range (marks them dropped and removes them from
// pendingSet), then enqueues an opDelete. Flush semantics match the current
// mode: WriteThrough flushes now; Debounced / BestEffort queue.
func (ap *AdaptivePersistence) NotifyClearRange(lo, hi int64) {
	if lo < 0 || hi < lo {
		return
	}
	ap.mu.Lock()
	defer ap.mu.Unlock()

	for gi, opIdx := range ap.pendingSet {
		if gi >= lo && gi <= hi {
			ap.pendingOps[opIdx].dropped = true
			delete(ap.pendingSet, gi)
		}
	}
	ap.pendingOps = append(ap.pendingOps, pendingOp{
		kind: opDelete,
		lo:   lo,
		hi:   hi,
		ts:   ap.nowFunc(),
	})

	ap.scheduleFlushLocked()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./apps/texelterm/parser/ -run TestAdaptivePersistence_NotifyClearRange -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/adaptive_persistence.go apps/texelterm/parser/adaptive_persistence_clearrange_test.go
git commit -m "adaptive_persistence: add NotifyClearRange with sweep-on-delete"
```

---

## Task 7: `sparse.Terminal.ClearRangePersistent` + `ClearNotifier` interface

**Files:**
- Modify: `apps/texelterm/parser/sparse/terminal.go` (add interface, field, setter, method)
- Test: `apps/texelterm/parser/sparse/terminal_clear_persistent_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/parser/sparse/terminal_clear_persistent_test.go`:

```go
package sparse

import "testing"

type stubNotifier struct {
	ranges [][2]int64
}

func (s *stubNotifier) NotifyClearRange(lo, hi int64) {
	s.ranges = append(s.ranges, [2]int64{lo, hi})
}

func TestTerminal_ClearRangePersistent_ClearsAndNotifies(t *testing.T) {
	term := NewTerminal(80, 24)
	// Seed a line at gi=5 so we can assert in-memory removal.
	term.SetLine(5, makeTestRow(80, 'x'))
	n := &stubNotifier{}
	term.SetClearNotifier(n)
	term.ClearRangePersistent(3, 7)
	if got := term.ReadLine(5); got != nil {
		t.Errorf("in-memory line 5 still present after clear: %v", got)
	}
	if len(n.ranges) != 1 || n.ranges[0] != [2]int64{3, 7} {
		t.Errorf("notifier ranges = %v, want [[3 7]]", n.ranges)
	}
}

func TestTerminal_ClearRangePersistent_NoNotifierIsFine(t *testing.T) {
	term := NewTerminal(80, 24)
	term.SetLine(5, makeTestRow(80, 'x'))
	// No SetClearNotifier call — ClearRangePersistent must not panic.
	term.ClearRangePersistent(3, 7)
	if got := term.ReadLine(5); got != nil {
		t.Error("in-memory line 5 still present")
	}
}
```

`makeTestRow` already exists in the sparse test utilities; if not, mirror the helper used by `store_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestTerminal_ClearRangePersistent -count=1`
Expected: FAIL — `SetClearNotifier` / `ClearRangePersistent` undefined.

- [ ] **Step 3: Add the interface, field, setter, and method**

Edit `apps/texelterm/parser/sparse/terminal.go`. Add near the top (under the existing `type Terminal struct`):

```go
// ClearNotifier is the minimal interface the sparse Terminal needs to propagate
// range clears to the persistence layer. AdaptivePersistence in the parser
// package satisfies it. Defined here so Terminal doesn't import parser
// (it already does, but keeping this interface-based keeps seams explicit).
type ClearNotifier interface {
	NotifyClearRange(lo, hi int64)
}
```

Add a field to the struct (line 14-18):

```go
type Terminal struct {
	store    *Store
	write    *WriteWindow
	view     *ViewWindow
	notifier ClearNotifier
}
```

Add the setter and method (near the existing `ClearRange` at line 187):

```go
// SetClearNotifier wires a persistence-layer callback for range clears.
// Passing nil disables notifications. Thread-safety: callers must not race
// with ClearRangePersistent; in practice this is set once during VTerm
// EnableMemoryBuffer.
func (t *Terminal) SetClearNotifier(n ClearNotifier) {
	t.notifier = n
}

// ClearRangePersistent removes lines [lo, hi] from the in-memory store AND
// notifies the persistence layer so the range is tombstoned on disk. Used by
// VTerm ED 0/1/2 and single-line invalidate. WriteWindow scroll / newline /
// scroll-region clears keep calling ClearRange (in-memory only).
func (t *Terminal) ClearRangePersistent(lo, hi int64) {
	t.store.ClearRange(lo, hi)
	if t.notifier != nil {
		t.notifier.NotifyClearRange(lo, hi)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestTerminal_ClearRangePersistent -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/terminal.go apps/texelterm/parser/sparse/terminal_clear_persistent_test.go
git commit -m "sparse: add Terminal.ClearRangePersistent with ClearNotifier"
```

---

## Task 8: Extend `MainScreen` interface with `ClearRangePersistent`

**Files:**
- Modify: `apps/texelterm/parser/main_screen.go` (interface line 10+)

- [ ] **Step 1: Add the method to the interface**

Edit `apps/texelterm/parser/main_screen.go`. Locate the `MainScreen` interface (line 10+). Under the existing `ClearRange(lo, hi int64)` line, add:

```go
	ClearRangePersistent(lo, hi int64)
	SetClearNotifier(n interface {
		NotifyClearRange(lo, hi int64)
	})
```

(Second method uses an inline interface to avoid a new named type in `parser`. Alternatively: define `type ClearNotifier interface { NotifyClearRange(lo, hi int64) }` in `main_screen.go` and reuse.)

- [ ] **Step 2: Verify the package still compiles**

Run: `go build ./apps/texelterm/parser/...`
Expected: PASS. `sparse.Terminal` already satisfies the new methods from Task 7.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/main_screen.go
git commit -m "parser: extend MainScreen interface with ClearRangePersistent"
```

---

## Task 9: Wire `mainScreenPersistence` as the `ClearNotifier` on `mainScreen`

**Files:**
- Modify: `apps/texelterm/parser/vterm_main_screen.go:118-126` (persistence setup)

- [ ] **Step 1: Write a focused wiring test**

Add to an appropriate existing test file (e.g. `vterm_main_screen_persistence_test.go`; create if missing):

```go
func TestEnableMemoryBuffer_WiresClearNotifier(t *testing.T) {
	v := NewVTerm(80, 24)
	dir := t.TempDir()
	if err := v.EnableMemoryBufferWithDisk(dir, MemoryBufferOptions{
		TerminalID: "wiring-test",
	}); err != nil {
		t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
	}
	defer v.CloseMemoryBuffer()

	// Trigger a clear via the public API; if the notifier is wired, the
	// AdaptivePersistence layer should observe a pending delete op.
	v.mainScreen.ClearRangePersistent(0, 5)
	if n := v.mainScreenPersistence.PendingOpCount(); n == 0 {
		t.Error("no pending op after ClearRangePersistent; notifier not wired")
	}
}
```

Add a small test-only introspection helper on `AdaptivePersistence`:

```go
// PendingOpCount returns the number of queued ops. Test-only.
func (ap *AdaptivePersistence) PendingOpCount() int {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return len(ap.pendingOps)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestEnableMemoryBuffer_WiresClearNotifier -count=1`
Expected: FAIL — notifier not wired; `PendingOpCount` == 0.

- [ ] **Step 3: Wire the notifier**

Edit `apps/texelterm/parser/vterm_main_screen.go:118-126`. After `v.mainScreenPersistence = persistence`, add:

```go
	v.mainScreen.SetClearNotifier(persistence)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestEnableMemoryBuffer_WiresClearNotifier -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/vterm_main_screen.go apps/texelterm/parser/adaptive_persistence.go apps/texelterm/parser/vterm_main_screen_persistence_test.go
git commit -m "vterm: wire AdaptivePersistence as ClearNotifier on mainScreen"
```

---

## Task 10: Migrate VTerm call sites from `ClearRange` to `ClearRangePersistent`

**Files:**
- Modify: `apps/texelterm/parser/vterm_main_screen.go:351, 355, 394, 410, 416, 507`
- Modify: `apps/texelterm/parser/vterm_erase.go:68`

Seven call sites total. Each change is a rename.

- [ ] **Step 1: Migrate `vterm_main_screen.go:351` (ED 0 cursor → screen bottom)**

Edit — change `v.mainScreen.ClearRange(` to `v.mainScreen.ClearRangePersistent(` at line 351.

- [ ] **Step 2: Migrate `vterm_main_screen.go:355` (ED 1 screen top → cursor)**

Same rename at line 355.

- [ ] **Step 3: Migrate `vterm_main_screen.go:394` (ED 2 anchor-rewind)**

Same rename at line 394.

- [ ] **Step 4: Migrate `vterm_main_screen.go:410` (ED 2 overflow-past-viewport)**

Same rename at line 410.

- [ ] **Step 5: Migrate `vterm_main_screen.go:416` (ED 2 no-scrollback)**

Same rename at line 416.

- [ ] **Step 6: Migrate `vterm_main_screen.go:507` (`MainScreenInvalidateLine`)**

Same rename at line 507.

- [ ] **Step 7: Migrate `vterm_erase.go:68` (alt-screen ED 2)**

Same rename at line 68.

- [ ] **Step 8: Run the full parser test suite**

Run: `go test ./apps/texelterm/parser/... -count=1`
Expected: PASS. Any test relying on pre-migration behaviour (ED clears leaving on-disk data) should now fail — treat that as evidence the migration is working. Update assertions in those tests to match the new behaviour.

- [ ] **Step 9: Commit**

```bash
git add apps/texelterm/parser/vterm_main_screen.go apps/texelterm/parser/vterm_erase.go
git commit -m "vterm: route ED + invalidate through ClearRangePersistent"
```

---

## Task 11: Sparse reload round-trip test

**Files:**
- Create: `apps/texelterm/parser/sparse/persistence_clear_roundtrip_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/parser/sparse/persistence_clear_roundtrip_test.go`:

```go
package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestLoadStore_SkipsTombstonedRange(t *testing.T) {
	dir := t.TempDir()
	ps1, err := parser.NewPageStore(parser.DefaultPageStoreConfig(dir, "rt-term"))
	if err != nil {
		t.Fatalf("NewPageStore: %v", err)
	}
	// Seed 20 lines.
	for i := 0; i < 20; i++ {
		ll := &parser.LogicalLine{Cells: []parser.Cell{{Rune: 'x'}}}
		if err := ps1.AppendLineWithGlobalIdx(int64(i), ll, timeNowForTest()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := ps1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	// Tombstone [5, 10].
	if err := ps1.DeleteRange(5, 10); err != nil {
		t.Fatalf("DeleteRange: %v", err)
	}
	if err := ps1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and LoadStore.
	ps2, err := parser.NewPageStore(parser.DefaultPageStoreConfig(dir, "rt-term"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer ps2.Close()
	store := NewStore(80)
	if err := LoadStore(store, ps2); err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	for gi := int64(5); gi <= 10; gi++ {
		if got := store.GetLine(gi); got != nil {
			t.Errorf("gi %d loaded after tombstone: %v", gi, got)
		}
	}
	for _, gi := range []int64{0, 4, 11, 19} {
		if got := store.GetLine(gi); got == nil {
			t.Errorf("gi %d missing after reload", gi)
		}
	}
}

func timeNowForTest() time.Time { return time.Unix(1700000000, 0) }
```

Imports: add `"time"` and `"github.com/framegrace/texelation/apps/texelterm/parser"`.

- [ ] **Step 2: Run the test to verify it passes immediately**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestLoadStore_SkipsTombstonedRange -count=1`
Expected: PASS on first try (no code changes needed — `LoadStore` reads from `pageIndex`, which `DeleteRange` already mutates).

If it fails, investigate `LoadStore` at `sparse/persistence.go:80` — the likely cause is iterating a stale index.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/sparse/persistence_clear_roundtrip_test.go
git commit -m "sparse: test LoadStore skips tombstoned lines on reload"
```

---

## Task 12: End-to-end OSC 133 + ED 2 regression test

**Files:**
- Create: `apps/texelterm/parser/osc133_anchor_clear_persistence_test.go`

This is the direct regression for the phantom-Claude-text bug that motivated the whole feature.

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/parser/osc133_anchor_clear_persistence_test.go`:

```go
package parser

import (
	"bytes"
	"strings"
	"testing"
)

// Simulates: prompt rendered → Claude writes output → ED 2 (TUI reset) →
// close → reopen → verify rewound range has no stale cells.
func TestOSC133_ED2_TombstonesPersistToDisk(t *testing.T) {
	dir := t.TempDir()

	v := NewVTerm(80, 24)
	if err := v.EnableMemoryBufferWithDisk(dir, MemoryBufferOptions{
		TerminalID: "osc133-tomb-test",
	}); err != nil {
		t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
	}

	// Emit OSC 133;A (prompt start) + prompt text + OSC 133;B (prompt end) +
	// user input + OSC 133;C (command start) + command output + ED 2.
	input := bytes.NewBuffer(nil)
	input.WriteString("\x1b]133;A\x07$ \x1b]133;B\x07ls\r\n\x1b]133;C\x07")
	for i := 0; i < 10; i++ {
		input.WriteString("phantom output line ")
		input.WriteByte('0' + byte(i))
		input.WriteString("\r\n")
	}
	// TUI resets with ED 2.
	input.WriteString("\x1b[2J")
	if _, err := v.Write(input.Bytes()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := v.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// Reopen.
	v2 := NewVTerm(80, 24)
	if err := v2.EnableMemoryBufferWithDisk(dir, MemoryBufferOptions{
		TerminalID: "osc133-tomb-test",
	}); err != nil {
		t.Fatalf("re-EnableMemoryBufferWithDisk: %v", err)
	}
	defer v2.CloseMemoryBuffer()

	// Scan every loaded row for "phantom output"; none should remain.
	hwm := v2.mainScreen.WriteBottomHWM()
	for gi := int64(0); gi <= hwm; gi++ {
		cells := v2.mainScreen.ReadLine(gi)
		if cells == nil {
			continue
		}
		var sb strings.Builder
		for _, c := range cells {
			if c.Rune != 0 {
				sb.WriteRune(c.Rune)
			}
		}
		if strings.Contains(sb.String(), "phantom output") {
			t.Errorf("stale phantom text at gi=%d: %q", gi, sb.String())
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./apps/texelterm/parser/ -run TestOSC133_ED2_TombstonesPersistToDisk -count=1`
Expected: PASS. If it fails, the most common causes are:
- The VTerm isn't routing ED 2 through the anchor-rewind path for this particular prompt layout. Inspect `vterm_main_screen.go:387-416` and confirm the anchor is set.
- The persistence layer is still in `BestEffort` mode and never flushes. Force a `Flush()` on close (`CloseMemoryBuffer` already does this — verify).

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/osc133_anchor_clear_persistence_test.go
git commit -m "test: OSC133 + ED2 tombstone persistence regression"
```

---

## Final verification

- [ ] **Run the full test suite**

```bash
cd /home/marc/projects/texel/texelation-sparse
go test ./apps/texelterm/parser/... -count=1
```
Expected: PASS.

- [ ] **Build all binaries**

```bash
make build
```
Expected: success.

- [ ] **Manual smoke test (optional)**

With a pre-existing v1 WAL file in place, launch `./bin/texelation`. The terminal should log `unsupported WAL version: 1`, start with empty scrollback, and create a fresh v2 WAL. This confirms the downgrade-safety contract from the spec.

- [ ] **Document the WAL-version bump in the PR description**

First launch after merge reports "unsupported WAL version" for pre-existing per-terminal WAL files; history resets. Call this out explicitly in the PR body so the expected empty-scrollback on first launch isn't mistaken for data loss.

---

## Notes on task ordering and model choice

- **Tasks 1–4** are mostly mechanical (constants, encoding, binary search). Fast model is fine.
- **Task 5** (FIFO refactor) touches hot persistence code paths — use a standard model and review carefully.
- **Task 6** (sweep-on-delete) has a subtle invariant (superseded writes must be dropped before the delete); standard model.
- **Tasks 7–10** (sparse Terminal + call-site migration) are mechanical — fast model.
- **Tasks 11–12** (reload + E2E regression) are the quality gates — run under the same model as implementation and verify by hand if anything is murky.
