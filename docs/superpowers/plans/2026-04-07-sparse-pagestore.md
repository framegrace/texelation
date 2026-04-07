# Sparse-Indexed PageStore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `PageStore` store lines keyed by an explicit global index, allowing gaps as first-class citizens. This eliminates the silent drift between `wal.nextGlobalIdx` and `pageStore.totalLineCount` that caused the "line index out of bounds" checkpoint failure and the stale-metadata-on-reload bug.

**Architecture:** `pageIndex` becomes a sorted slice of `(globalIdx, pageID, offsetInPage)` tuples. Within a page, lines stay dense (`globalIdx == page.FirstGlobalIdx + i`). Gaps force page boundaries, so the on-disk format is unchanged. `LineCount()` now returns logical end (max stored `globalIdx + 1`). `StoredLineCount()` is added for density assertions. The old positional `AppendLine` / `AppendLineWithTimestamp` API is removed — every caller moves to `AppendLineWithGlobalIdx(globalIdx, line, ts)`.

**Tech Stack:** Go 1.24.3, existing `apps/texelterm/parser` package, no new dependencies.

---

## File Structure

**Modified:**
- `apps/texelterm/parser/page_store.go` — core refactor
- `apps/texelterm/parser/write_ahead_log.go` — switch to new API in `recoverFromWAL` and `checkpointLocked`
- `apps/texelterm/parser/adaptive_persistence.go` — remove debug logging
- `apps/texelterm/parser/viewport_content_reader.go` — handle nils from `ReadLineRange`
- `apps/texelterm/parser/vterm_memory_buffer.go` — handle nils in backfill path
- `apps/texelterm/parser/vterm_memory_buffer_test.go` — update callers
- `apps/texelterm/parser/wal_atomicity_test.go` — update callers
- `apps/texelterm/parser/adaptive_persistence_recovery_test.go` — update callers
- `apps/texelterm/parser/burst_recovery_test.go` — update callers

**Created:**
- `apps/texelterm/parser/page_store_sparse_test.go` — new tests for gap behavior
- `apps/texelterm/parser/wal_sparse_test.go` — new tests for checkpoint-with-gap

---

## Task 1: Add `StoredLineCount` and redefine `LineCount` semantics (TDD baseline)

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`
- Create: `apps/texelterm/parser/page_store_sparse_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/parser/page_store_sparse_test.go`:

```go
// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestPageStore(t *testing.T) *PageStore {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultPageStoreConfig(filepath.Join(dir, "hist"), "test-terminal")
	ps, err := CreatePageStore(cfg)
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	t.Cleanup(func() { ps.Close() })
	return ps
}

func mkLine(s string) *LogicalLine {
	cells := make([]Cell, len(s))
	for i, r := range s {
		cells[i] = Cell{Rune: r}
	}
	return &LogicalLine{Cells: cells}
}

func TestPageStore_StoredLineCountVsLineCount(t *testing.T) {
	ps := newTestPageStore(t)

	if got := ps.LineCount(); got != 0 {
		t.Errorf("empty LineCount: got %d, want 0", got)
	}
	if got := ps.StoredLineCount(); got != 0 {
		t.Errorf("empty StoredLineCount: got %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_StoredLineCountVsLineCount -v`
Expected: FAIL with "undefined: StoredLineCount" (compile error).

- [ ] **Step 3: Add `StoredLineCount` method**

In `apps/texelterm/parser/page_store.go`, add below `LineCount`:

```go
// StoredLineCount returns the number of lines actually stored.
// This may be less than LineCount() when there are gaps in the
// global-index space (e.g., from LineFeed operations that advanced
// the live edge without dirtying intermediate lines).
func (ps *PageStore) StoredLineCount() int64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.totalLineCount
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_StoredLineCountVsLineCount -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_sparse_test.go
git commit -m "Add StoredLineCount to PageStore"
```

---

## Task 2: Add `globalIdx` to `pageIndexEntry` and populate on rebuild

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`
- Modify: `apps/texelterm/parser/page_store_sparse_test.go`

- [ ] **Step 1: Write the failing test**

Append to `page_store_sparse_test.go`:

```go
func TestPageStore_RebuildPopulatesGlobalIdx(t *testing.T) {
	// Create a store, append some lines via the old API (we'll replace
	// this in a later task, but for now it works because the data is dense).
	dir := t.TempDir()
	cfg := DefaultPageStoreConfig(filepath.Join(dir, "hist"), "rebuild-test")

	ps, err := CreatePageStore(cfg)
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := ps.AppendLineWithTimestamp(mkLine("line"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithTimestamp: %v", err)
		}
	}
	ps.Close()

	// Reopen: rebuildIndex must populate globalIdx on each pageIndexEntry.
	ps2, err := OpenPageStore(cfg)
	if err != nil {
		t.Fatalf("OpenPageStore: %v", err)
	}
	t.Cleanup(func() { ps2.Close() })

	if got := ps2.LineCount(); got != 5 {
		t.Errorf("LineCount after reopen: got %d, want 5", got)
	}
	for i := int64(0); i < 5; i++ {
		if ps2.pageIndex[i].globalIdx != i {
			t.Errorf("pageIndex[%d].globalIdx: got %d, want %d",
				i, ps2.pageIndex[i].globalIdx, i)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_RebuildPopulatesGlobalIdx -v`
Expected: FAIL with "pageIndex[0].globalIdx undefined" (compile error).

- [ ] **Step 3: Add `globalIdx` field to `pageIndexEntry`**

In `page_store.go`:

```go
// pageIndexEntry tracks which page contains each line, keyed by global index.
type pageIndexEntry struct {
	globalIdx    int64  // Global line index this entry represents
	pageID       uint64 // Page containing this line
	offsetInPage int    // Line's index within the page (0-based)
}
```

- [ ] **Step 4: Populate `globalIdx` in `rebuildIndex`**

In `rebuildIndex()`, replace the loop that appends entries with:

```go
// Read each page header to build index
for _, pi := range pages {
	page, err := ps.readPageHeader(pi.path)
	if err != nil {
		return fmt.Errorf("failed to read page %d header: %w", pi.id, err)
	}

	baseGlobal := int64(page.Header.FirstGlobalIdx)
	// Add index entries for each line in this page
	for i := uint32(0); i < page.Header.LineCount; i++ {
		ps.pageIndex = append(ps.pageIndex, pageIndexEntry{
			globalIdx:    baseGlobal + int64(i),
			pageID:       pi.id,
			offsetInPage: int(i),
		})
	}

	ps.totalLineCount += int64(page.Header.LineCount)

	// Track highest page ID
	if pi.id >= ps.nextPageID {
		ps.nextPageID = pi.id + 1
	}
}

// Set nextGlobalIdx to the logical end (highest stored globalIdx + 1).
if len(ps.pageIndex) > 0 {
	last := ps.pageIndex[len(ps.pageIndex)-1]
	ps.nextGlobalIdx = last.globalIdx + 1
} else {
	ps.nextGlobalIdx = 0
}
```

- [ ] **Step 5: Populate `globalIdx` in `AppendLineWithTimestamp` (temporary — will be removed in Task 5)**

In `AppendLineWithTimestamp`, replace the index append with:

```go
// Update index
ps.pageIndex = append(ps.pageIndex, pageIndexEntry{
	globalIdx:    ps.nextGlobalIdx,
	pageID:       ps.currentPage.Header.PageID,
	offsetInPage: int(ps.currentPage.Header.LineCount) - 1,
})
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_RebuildPopulatesGlobalIdx -v`
Expected: PASS

- [ ] **Step 7: Run full PageStore tests to verify no regression**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore -v`
Expected: PASS (all existing tests still green because data is still dense).

- [ ] **Step 8: Commit**

```bash
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_sparse_test.go
git commit -m "Add globalIdx field to pageIndexEntry"
```

---

## Task 3: Redefine `LineCount` to return logical end

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`
- Modify: `apps/texelterm/parser/page_store_sparse_test.go`

- [ ] **Step 1: Update the baseline test**

In `page_store_sparse_test.go`, add:

```go
func TestPageStore_LineCountIsLogicalEnd(t *testing.T) {
	ps := newTestPageStore(t)

	for i := 0; i < 10; i++ {
		if err := ps.AppendLineWithTimestamp(mkLine("x"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithTimestamp: %v", err)
		}
	}

	// With dense data, LineCount and StoredLineCount match.
	if got := ps.LineCount(); got != 10 {
		t.Errorf("LineCount: got %d, want 10", got)
	}
	if got := ps.StoredLineCount(); got != 10 {
		t.Errorf("StoredLineCount: got %d, want 10", got)
	}
}
```

- [ ] **Step 2: Run test to verify it passes (it should — nothing has diverged yet)**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_LineCountIsLogicalEnd -v`
Expected: PASS

- [ ] **Step 3: Change `LineCount` to return `nextGlobalIdx`**

In `page_store.go`:

```go
// LineCount returns the logical end of the global-index space:
// the highest stored global index plus one (zero if empty).
// Note: this may exceed the number of stored lines when gaps exist.
// Use StoredLineCount for the actual stored count.
func (ps *PageStore) LineCount() int64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.nextGlobalIdx
}
```

- [ ] **Step 4: Run test to verify it still passes**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore -v`
Expected: PASS (dense data → both metrics equal).

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_sparse_test.go
git commit -m "Redefine PageStore.LineCount as logical end"
```

---

## Task 4: Implement `AppendLineWithGlobalIdx`

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`
- Modify: `apps/texelterm/parser/page_store_sparse_test.go`

- [ ] **Step 1: Write the failing test**

Append to `page_store_sparse_test.go`:

```go
func TestPageStore_AppendWithGlobalIdx_Dense(t *testing.T) {
	ps := newTestPageStore(t)

	for i := int64(0); i < 5; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("line"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithGlobalIdx(%d): %v", i, err)
		}
	}

	if got := ps.LineCount(); got != 5 {
		t.Errorf("LineCount: got %d, want 5", got)
	}
	if got := ps.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount: got %d, want 5", got)
	}
}

func TestPageStore_AppendWithGlobalIdx_Gap(t *testing.T) {
	ps := newTestPageStore(t)

	// Append 0..2, then jump to 100..101.
	for i := int64(0); i < 3; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("early"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithGlobalIdx(%d): %v", i, err)
		}
	}
	for i := int64(100); i < 102; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("late"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithGlobalIdx(%d): %v", i, err)
		}
	}

	if got := ps.LineCount(); got != 102 {
		t.Errorf("LineCount: got %d, want 102", got)
	}
	if got := ps.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount: got %d, want 5", got)
	}

	// Verify a new page was created at the gap boundary.
	// We expect pageID 1 holds globalIdx 0..2 and pageID 2 holds 100..101
	// (or similar — exact pageIDs depend on startNewPage behavior).
	if len(ps.pageIndex) != 5 {
		t.Fatalf("pageIndex length: got %d, want 5", len(ps.pageIndex))
	}
	if ps.pageIndex[0].pageID == ps.pageIndex[3].pageID {
		t.Errorf("expected pageID split between idx=2 and idx=100, but both are on page %d",
			ps.pageIndex[0].pageID)
	}
	if ps.pageIndex[3].globalIdx != 100 {
		t.Errorf("pageIndex[3].globalIdx: got %d, want 100", ps.pageIndex[3].globalIdx)
	}
}

func TestPageStore_AppendWithGlobalIdx_MustIncrease(t *testing.T) {
	ps := newTestPageStore(t)

	if err := ps.AppendLineWithGlobalIdx(10, mkLine("a"), time.Now()); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// Appending an index <= max stored must fail.
	if err := ps.AppendLineWithGlobalIdx(10, mkLine("b"), time.Now()); err == nil {
		t.Errorf("expected error on duplicate globalIdx, got nil")
	}
	if err := ps.AppendLineWithGlobalIdx(5, mkLine("c"), time.Now()); err == nil {
		t.Errorf("expected error on decreasing globalIdx, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_AppendWithGlobalIdx -v`
Expected: FAIL (compile error: undefined `AppendLineWithGlobalIdx`).

- [ ] **Step 3: Implement `AppendLineWithGlobalIdx`**

Add to `page_store.go`:

```go
// AppendLineWithGlobalIdx writes a line at the specified global index.
// globalIdx must be strictly greater than every previously stored globalIdx.
// If globalIdx is not contiguous with the current page, the current page is
// flushed and a new page is started anchored at globalIdx.
func (ps *PageStore) AppendLineWithGlobalIdx(globalIdx int64, line *LogicalLine, timestamp time.Time) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if globalIdx < ps.nextGlobalIdx {
		return fmt.Errorf("globalIdx %d must be >= nextGlobalIdx %d", globalIdx, ps.nextGlobalIdx)
	}

	// Determine if we need a fresh page: no current page, or a gap, or we
	// simply need to start fresh because the current page was flushed.
	needNewPage := ps.currentPage == nil
	if !needNewPage {
		expectedNext := int64(ps.currentPage.Header.FirstGlobalIdx) + int64(ps.currentPage.Header.LineCount)
		if globalIdx != expectedNext {
			// Gap — flush and start a new page anchored at globalIdx.
			if err := ps.flushCurrentPage(); err != nil {
				return err
			}
			needNewPage = true
		}
	}

	if needNewPage {
		ps.currentPage = NewPage(ps.nextPageID, uint64(globalIdx))
		ps.nextPageID++
	}

	// Try to add line to current page.
	if !ps.currentPage.AddLine(line, timestamp, 0) {
		// Page is full — flush and start a new page anchored at globalIdx.
		if err := ps.flushCurrentPage(); err != nil {
			return err
		}
		ps.currentPage = NewPage(ps.nextPageID, uint64(globalIdx))
		ps.nextPageID++
		if !ps.currentPage.AddLine(line, timestamp, 0) {
			// Oversized line — add anyway (same behavior as the old path).
			ps.currentPage.AddLine(line, timestamp, 0)
		}
	}

	// Update index.
	ps.pageIndex = append(ps.pageIndex, pageIndexEntry{
		globalIdx:    globalIdx,
		pageID:       ps.currentPage.Header.PageID,
		offsetInPage: int(ps.currentPage.Header.LineCount) - 1,
	})

	ps.totalLineCount++
	ps.nextGlobalIdx = globalIdx + 1

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_AppendWithGlobalIdx -v`
Expected: PASS

- [ ] **Step 5: Run full parser tests**

Run: `go test ./apps/texelterm/parser/ -v`
Expected: PASS (existing tests unaffected).

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_sparse_test.go
git commit -m "Implement PageStore.AppendLineWithGlobalIdx with gap support"
```

---

## Task 5: Rewrite `ReadLine` / `UpdateLine` / `ReadLineWithTimestamp` to use binary search

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`
- Modify: `apps/texelterm/parser/page_store_sparse_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestPageStore_ReadWithGaps(t *testing.T) {
	ps := newTestPageStore(t)

	for i := int64(0); i < 3; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("early"), time.Now()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	for i := int64(100); i < 102; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("late"), time.Now()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Stored entries: readable.
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		line, err := ps.ReadLine(idx)
		if err != nil {
			t.Errorf("ReadLine(%d): unexpected error %v", idx, err)
		}
		if line == nil {
			t.Errorf("ReadLine(%d): got nil, want line", idx)
		}
	}

	// Gap entries: return (nil, nil).
	for _, idx := range []int64{3, 50, 99} {
		line, err := ps.ReadLine(idx)
		if err != nil {
			t.Errorf("ReadLine(%d) gap: unexpected error %v", idx, err)
		}
		if line != nil {
			t.Errorf("ReadLine(%d) gap: got line, want nil", idx)
		}
	}

	// Out of range: also (nil, nil).
	line, err := ps.ReadLine(102)
	if err != nil || line != nil {
		t.Errorf("ReadLine(102) OOR: got (%v, %v), want (nil, nil)", line, err)
	}
}

func TestPageStore_UpdateWithGaps(t *testing.T) {
	ps := newTestPageStore(t)

	for i := int64(0); i < 3; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("early"), time.Now()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	for i := int64(100); i < 102; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("late"), time.Now()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	// Update an existing line.
	if err := ps.UpdateLine(101, mkLine("updated"), time.Now()); err != nil {
		t.Errorf("UpdateLine(101) existing: %v", err)
	}
	line, _ := ps.ReadLine(101)
	if line == nil || string(runesFromCells(line.Cells)) != "updated" {
		t.Errorf("ReadLine(101) after update: got %q, want \"updated\"",
			string(runesFromCells(line.Cells)))
	}

	// Update a gap must fail.
	if err := ps.UpdateLine(50, mkLine("ghost"), time.Now()); err == nil {
		t.Errorf("UpdateLine(50) gap: expected error, got nil")
	}
}

func runesFromCells(cells []Cell) []rune {
	out := make([]rune, len(cells))
	for i, c := range cells {
		out[i] = c.Rune
	}
	return out
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./apps/texelterm/parser/ -run "TestPageStore_(Read|Update)WithGaps" -v`
Expected: FAIL (current UpdateLine uses positional index; current ReadLine uses positional index).

- [ ] **Step 3: Add binary-search helper**

Add to `page_store.go`:

```go
// findByGlobalIdx does a binary search on pageIndex for the entry matching
// globalIdx. Returns (entry, true) if found, (zero, false) otherwise.
// Caller must hold ps.mu (read or write).
func (ps *PageStore) findByGlobalIdx(globalIdx int64) (pageIndexEntry, bool) {
	n := len(ps.pageIndex)
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi) / 2
		if ps.pageIndex[mid].globalIdx < globalIdx {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < n && ps.pageIndex[lo].globalIdx == globalIdx {
		return ps.pageIndex[lo], true
	}
	return pageIndexEntry{}, false
}
```

- [ ] **Step 4: Rewrite `ReadLine`**

Replace the existing `ReadLine`:

```go
// ReadLine reads a single line by global index.
// Returns (nil, nil) if the global index is not stored (gap or out of range).
func (ps *PageStore) ReadLine(globalIdx int64) (*LogicalLine, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	entry, ok := ps.findByGlobalIdx(globalIdx)
	if !ok {
		return nil, nil
	}

	// Check if line is in current page (not yet flushed).
	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.GetLine(entry.offsetInPage), nil
	}

	// Load page from disk.
	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return nil, err
	}
	return page.GetLine(entry.offsetInPage), nil
}
```

- [ ] **Step 5: Rewrite `ReadLineWithTimestamp`**

```go
// ReadLineWithTimestamp reads a line and its timestamp by global index.
// Returns (nil, zero, nil) if the global index is not stored.
func (ps *PageStore) ReadLineWithTimestamp(globalIdx int64) (*LogicalLine, time.Time, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	entry, ok := ps.findByGlobalIdx(globalIdx)
	if !ok {
		return nil, time.Time{}, nil
	}

	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		line := ps.currentPage.GetLine(entry.offsetInPage)
		ts := ps.currentPage.GetTimestamp(entry.offsetInPage)
		return line, ts, nil
	}

	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return nil, time.Time{}, err
	}
	return page.GetLine(entry.offsetInPage), page.GetTimestamp(entry.offsetInPage), nil
}
```

- [ ] **Step 6: Rewrite `UpdateLine`**

```go
// UpdateLine updates an existing line by global index.
// Returns an error if the global index is not stored.
func (ps *PageStore) UpdateLine(globalIdx int64, line *LogicalLine, timestamp time.Time) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	entry, ok := ps.findByGlobalIdx(globalIdx)
	if !ok {
		return fmt.Errorf("line %d not present in PageStore", globalIdx)
	}

	// Check if line is in current (unflushed) page.
	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.UpdateLine(entry.offsetInPage, line, timestamp)
	}

	// Line is in a flushed page — reload, update, rewrite atomically.
	return ps.updateLineInFlushedPage(entry.pageID, entry.offsetInPage, line, timestamp)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/ -run "TestPageStore_(Read|Update)WithGaps" -v`
Expected: PASS

- [ ] **Step 8: Run all PageStore tests**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_sparse_test.go
git commit -m "Use binary search by globalIdx for PageStore Read/Update"
```

---

## Task 6: Rewrite `ReadLineRange` to return dense slice with nil gaps

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`
- Modify: `apps/texelterm/parser/page_store_sparse_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestPageStore_ReadLineRange_WithGaps(t *testing.T) {
	ps := newTestPageStore(t)

	for i := int64(0); i < 3; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("early"), time.Now()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	for i := int64(100); i < 102; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("late"), time.Now()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	lines, err := ps.ReadLineRange(0, 102)
	if err != nil {
		t.Fatalf("ReadLineRange: %v", err)
	}
	if len(lines) != 102 {
		t.Fatalf("ReadLineRange length: got %d, want 102", len(lines))
	}
	for i, line := range lines {
		switch {
		case i < 3 || i >= 100:
			if line == nil {
				t.Errorf("ReadLineRange[%d]: got nil, want line", i)
			}
		default:
			if line != nil {
				t.Errorf("ReadLineRange[%d]: got line, want nil (gap)", i)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_ReadLineRange_WithGaps -v`
Expected: FAIL (current implementation returns compact slice).

- [ ] **Step 3: Rewrite `ReadLineRange`**

Replace:

```go
// ReadLineRange reads a range of lines [start, end) by global index.
// Returns a slice of length (end - start), with nil entries for gaps
// or out-of-range indices. Caller can index directly as result[globalIdx - start].
func (ps *PageStore) ReadLineRange(start, end int64) ([]*LogicalLine, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if start < 0 {
		start = 0
	}
	if end <= start {
		return nil, nil
	}

	result := make([]*LogicalLine, end-start)

	// Find the first stored entry with globalIdx >= start.
	lo, hi := 0, len(ps.pageIndex)
	for lo < hi {
		mid := (lo + hi) / 2
		if ps.pageIndex[mid].globalIdx < start {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	// Walk entries in order, loading pages lazily and batching reads.
	var currentPageID uint64
	var currentPage *Page
	for i := lo; i < len(ps.pageIndex); i++ {
		entry := ps.pageIndex[i]
		if entry.globalIdx >= end {
			break
		}

		var line *LogicalLine
		if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
			line = ps.currentPage.GetLine(entry.offsetInPage)
		} else {
			if currentPage == nil || entry.pageID != currentPageID {
				p, err := ps.loadPage(entry.pageID)
				if err != nil {
					return nil, fmt.Errorf("failed to load page %d: %w", entry.pageID, err)
				}
				currentPage = p
				currentPageID = entry.pageID
			}
			line = currentPage.GetLine(entry.offsetInPage)
		}

		result[entry.globalIdx-start] = line
	}

	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_ReadLineRange_WithGaps -v`
Expected: PASS

- [ ] **Step 5: Run all parser tests to find any dense-assumption callers**

Run: `go test ./apps/texelterm/parser/ -v 2>&1 | tail -80`
Expected: Some tests may fail because they assume `ReadLineRange` returns a compact slice. Record any failures — they're fixed in Task 10.

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_sparse_test.go
git commit -m "ReadLineRange returns dense slice with nil for gaps"
```

---

## Task 7: Rewrite `FindLineAt` and `prepareForAppend` for sparse storage

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`

- [ ] **Step 1: Write the failing test**

Append to `page_store_sparse_test.go`:

```go
func TestPageStore_PrepareForAppend_GapAfterReopen(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultPageStoreConfig(filepath.Join(dir, "hist"), "prep-test")

	ps, err := CreatePageStore(cfg)
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	for i := int64(0); i < 3; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("a"), time.Now()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	ps.Close()

	ps2, err := OpenPageStore(cfg)
	if err != nil {
		t.Fatalf("OpenPageStore: %v", err)
	}
	t.Cleanup(func() { ps2.Close() })

	// Appending at a gap after reopen must succeed and start a fresh page.
	if err := ps2.AppendLineWithGlobalIdx(100, mkLine("z"), time.Now()); err != nil {
		t.Fatalf("AppendLineWithGlobalIdx(100) after reopen: %v", err)
	}
	if got := ps2.LineCount(); got != 101 {
		t.Errorf("LineCount: got %d, want 101", got)
	}
	if got := ps2.StoredLineCount(); got != 4 {
		t.Errorf("StoredLineCount: got %d, want 4", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore_PrepareForAppend_GapAfterReopen -v`
Expected: FAIL or PASS — investigate the result. If it passes, skip to Step 4. If it fails, continue.

- [ ] **Step 3: Fix `prepareForAppend` to handle reopened stores correctly**

The existing `prepareForAppend` adopts the last page as `currentPage` if it is not full. Under sparse semantics this is still correct — the next contiguous append extends it, and a gap append triggers the flush-and-new-page path in `AppendLineWithGlobalIdx`. No code change is needed for Step 3; the existing logic handles it. Verify by re-running the test.

- [ ] **Step 4: Rewrite `FindLineAt` to return a global index via binary search on pageIndex**

Replace `FindLineAt` and `getTimestampUnlocked`:

```go
// FindLineAt returns the global index of the stored line closest to (but not
// after) the given time. Returns -1 if no lines are stored.
func (ps *PageStore) FindLineAt(t time.Time) (int64, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	n := len(ps.pageIndex)
	if n == 0 {
		return -1, nil
	}

	targetNano := t.UnixNano()
	lo, hi := 0, n-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		ts, err := ps.getTimestampAtPosUnlocked(mid)
		if err != nil {
			return -1, err
		}
		if ts.UnixNano() <= targetNano {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return ps.pageIndex[lo].globalIdx, nil
}

// getTimestampAtPosUnlocked returns the timestamp for the pageIndex entry at
// position `pos` (not by globalIdx). Caller must hold lock.
func (ps *PageStore) getTimestampAtPosUnlocked(pos int) (time.Time, error) {
	if pos < 0 || pos >= len(ps.pageIndex) {
		return time.Time{}, nil
	}
	entry := ps.pageIndex[pos]

	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.GetTimestamp(entry.offsetInPage), nil
	}

	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return time.Time{}, err
	}
	return page.GetTimestamp(entry.offsetInPage), nil
}
```

Also update `GetTimestamp(index int64)`:

```go
// GetTimestamp returns the timestamp for the line at the given global index.
// Returns zero time if the index is not stored.
func (ps *PageStore) GetTimestamp(globalIdx int64) (time.Time, error) {
	_, ts, err := ps.ReadLineWithTimestamp(globalIdx)
	return ts, err
}
```

Remove the old `getTimestampUnlocked` helper if it's no longer referenced.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/ -run TestPageStore -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_sparse_test.go
git commit -m "Rewrite FindLineAt and prepareForAppend for sparse storage"
```

---

## Task 8: Remove old positional `AppendLine` / `AppendLineWithTimestamp`

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`
- Modify: `apps/texelterm/parser/write_ahead_log.go`
- Modify: all test files that call `AppendLine` / `AppendLineWithTimestamp`

- [ ] **Step 1: Update WAL callers first (so removal compiles)**

In `apps/texelterm/parser/write_ahead_log.go`, find the two call sites:

Around line 741 (`recoverFromWAL`), replace:
```go
if err := w.pageStore.AppendLineWithTimestamp(entry.Line, entry.Timestamp); err != nil {
```
with:
```go
if err := w.pageStore.AppendLineWithGlobalIdx(int64(entry.GlobalLineIdx), entry.Line, entry.Timestamp); err != nil {
```

Around line 896 (`checkpointLocked` Pass 1), replace:
```go
if err := w.pageStore.AppendLineWithTimestamp(entry.Line, entry.Timestamp); err != nil {
	return fmt.Errorf("failed to append line %d to PageStore: %w", entry.GlobalLineIdx, err)
}
```
with:
```go
if err := w.pageStore.AppendLineWithGlobalIdx(int64(entry.GlobalLineIdx), entry.Line, entry.Timestamp); err != nil {
	return fmt.Errorf("failed to append line %d to PageStore: %w", entry.GlobalLineIdx, err)
}
```

- [ ] **Step 2: Update test callers**

Find every call to `pageStore.AppendLine(` or `pageStore.AppendLineWithTimestamp(` in test files. For each:

```bash
grep -rn "AppendLine\b\|AppendLineWithTimestamp\b" apps/texelterm/parser/*_test.go
```

Replace each call `ps.AppendLine(line)` with:
```go
ps.AppendLineWithGlobalIdx(ps.LineCount(), line, time.Now())
```

and each `ps.AppendLineWithTimestamp(line, ts)` with:
```go
ps.AppendLineWithGlobalIdx(ps.LineCount(), line, ts)
```

Do the same inside `page_store_sparse_test.go` (the Task 2 `TestPageStore_RebuildPopulatesGlobalIdx` test currently uses the old API — update it to use `AppendLineWithGlobalIdx(i, ...)`).

Also search for non-test callers that may exist outside the parser package:
```bash
grep -rn "\.AppendLine(\|\.AppendLineWithTimestamp(" apps/ cmd/ internal/ texel/ 2>/dev/null
```

Update any found.

- [ ] **Step 3: Remove `AppendLine` and `AppendLineWithTimestamp` from `page_store.go`**

Delete both methods and their doc comments.

- [ ] **Step 4: Build and verify everything compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Run full parser test suite**

Run: `go test ./apps/texelterm/parser/ -v 2>&1 | tail -50`
Expected: PASS

- [ ] **Step 6: Run the full workspace test suite**

Run: `go test ./... 2>&1 | tail -50`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "Remove positional AppendLine API; all callers use globalIdx"
```

---

## Task 9: Update `viewport_content_reader.go` and `vterm_memory_buffer.go` to handle nils from `ReadLineRange`

**Files:**
- Modify: `apps/texelterm/parser/viewport_content_reader.go`
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go`

- [ ] **Step 1: Audit `viewport_content_reader.go` calls**

Open `apps/texelterm/parser/viewport_content_reader.go` and read the two `ReadLineRange` call sites (around lines 118 and 127). Trace how the returned slice is consumed. The returned slice is now dense with `nil` for gaps. Callers that iterate and dereference must substitute a blank line for nils.

Add a helper near the top of the file:

```go
// blankLine returns an empty LogicalLine suitable for rendering gap positions.
func blankLine() *LogicalLine {
	return &LogicalLine{Cells: nil}
}
```

At each call site, after the `ReadLineRange` call, replace any nil entry:

```go
lines, _ := r.pageStore.ReadLineRange(start, end)
for i, line := range lines {
	if line == nil {
		lines[i] = blankLine()
	}
}
```

- [ ] **Step 2: Audit `vterm_memory_buffer.go` backfill at line 358**

Open `apps/texelterm/parser/vterm_memory_buffer.go` and read the `ReadLineRange` call around line 358. This is the backfill path that pulls disk history into memBuf. The returned slice now has nils for gaps; the existing code likely iterates expecting every entry to be a valid line.

Replace the iteration to skip nils OR substitute blank lines depending on how the result is used. Read the surrounding code first to decide. If the loop writes into memBuf, skip nils so the memBuf position doesn't get a bogus entry. If the loop builds a display grid, substitute a blank line.

- [ ] **Step 3: Run all parser tests**

Run: `go test ./apps/texelterm/parser/ -v 2>&1 | tail -50`
Expected: PASS

- [ ] **Step 4: Build the full project**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "Handle nil entries from sparse ReadLineRange"
```

---

## Task 10: Add checkpoint-with-gap integration tests

**Files:**
- Create: `apps/texelterm/parser/wal_sparse_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/parser/wal_sparse_test.go`:

```go
// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestWAL(t *testing.T) *WriteAheadLog {
	t.Helper()
	dir := t.TempDir()
	cfg := WALConfig{
		BaseDir:           filepath.Join(dir, "hist"),
		TerminalID:        "sparse-wal-test",
		MaxSize:           1 << 20,
		CheckpointMaxSize: 1 << 30, // effectively disable auto-checkpoint
	}
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}
	t.Cleanup(func() { wal.Close() })
	return wal
}

func TestWAL_CheckpointWithGap(t *testing.T) {
	wal := newTestWAL(t)

	// Append entries at globalIdx 0,1,2 then jump to 100,101.
	now := time.Now()
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		if err := wal.Append(idx, mkLine("x"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}

	// Force checkpoint.
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	// PageStore state after checkpoint.
	if got := wal.pageStore.LineCount(); got != 102 {
		t.Errorf("LineCount after checkpoint: got %d, want 102", got)
	}
	if got := wal.pageStore.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount after checkpoint: got %d, want 5", got)
	}

	// Verify stored entries are readable and gaps return nil.
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		line, err := wal.pageStore.ReadLine(idx)
		if err != nil || line == nil {
			t.Errorf("ReadLine(%d): got (%v, %v), want line", idx, line, err)
		}
	}
	for _, idx := range []int64{3, 50, 99} {
		line, err := wal.pageStore.ReadLine(idx)
		if err != nil || line != nil {
			t.Errorf("ReadLine(%d) gap: got (%v, %v), want (nil, nil)", idx, line, err)
		}
	}
}

func TestWAL_CheckpointWithGap_ThenModify(t *testing.T) {
	wal := newTestWAL(t)

	now := time.Now()
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		if err := wal.Append(idx, mkLine("x"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	// Modify an existing line — goes through WAL as EntryTypeLineModify,
	// checkpoint Pass 2 calls UpdateLine(101, ...) which must succeed.
	if err := wal.Append(101, mkLine("modified"), now); err != nil {
		t.Fatalf("Append modify: %v", err)
	}
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint modify: %v", err)
	}

	line, err := wal.pageStore.ReadLine(101)
	if err != nil || line == nil {
		t.Fatalf("ReadLine(101): (%v, %v)", line, err)
	}
	if got := string(runesFromCells(line.Cells)); got != "modified" {
		t.Errorf("line 101 content: got %q, want \"modified\"", got)
	}
}

func TestWAL_RecoveryPreservesGap(t *testing.T) {
	dir := t.TempDir()
	cfg := WALConfig{
		BaseDir:           filepath.Join(dir, "hist"),
		TerminalID:        "sparse-recovery-test",
		MaxSize:           1 << 20,
		CheckpointMaxSize: 1 << 30,
	}

	wal1, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #1: %v", err)
	}
	now := time.Now()
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		if err := wal1.Append(idx, mkLine("x"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}
	if err := wal1.Close(); err != nil {
		t.Fatalf("wal1.Close: %v", err)
	}

	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #2: %v", err)
	}
	defer wal2.Close()

	if got := wal2.pageStore.LineCount(); got != 102 {
		t.Errorf("LineCount after recovery: got %d, want 102", got)
	}
	if got := wal2.pageStore.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount after recovery: got %d, want 5", got)
	}
	for _, idx := range []int64{50} {
		line, _ := wal2.pageStore.ReadLine(idx)
		if line != nil {
			t.Errorf("ReadLine(%d) after recovery: got line, want nil", idx)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/ -run "TestWAL_CheckpointWithGap|TestWAL_CheckpointWithGap_ThenModify|TestWAL_RecoveryPreservesGap" -v`
Expected: PASS

- [ ] **Step 3: Also verify the previously-added `TestVTermCoherence_StaleMetadataRecovery` still passes**

Run: `go test ./apps/texelterm/parser/ -run TestVTermCoherence_StaleMetadataRecovery -v`
Expected: PASS (now passing naturally — no clamping needed).

- [ ] **Step 4: Commit**

```bash
git add apps/texelterm/parser/wal_sparse_test.go
git commit -m "Add WAL checkpoint tests for gapped global indices"
```

---

## Task 11: Remove debug logging from `adaptive_persistence.go`

**Files:**
- Modify: `apps/texelterm/parser/adaptive_persistence.go`

- [ ] **Step 1: Remove META_NOTIFY debug block**

In `NotifyMetadataChange` (around line 343), remove the `log.Printf("[META_NOTIFY] ...")` block introduced during investigation. Keep the `ap.metrics.TotalWrites++`, `ap.pendingMetadata = state`, and `ap.lastActivity = ...` assignments.

- [ ] **Step 2: Restore original clamp logging**

In `flushPendingLocked` (around line 633), replace:

```go
log.Printf("[META_WRITE] pendingMeta.LiveEdgeBase=%d, walLineCount=%d, cursorY=%d",
	pendingMeta.LiveEdgeBase, walLineCount, pendingMeta.CursorY)
if pendingMeta.LiveEdgeBase > walLineCount {
	log.Printf("[META_WRITE] CLAMPING LiveEdgeBase %d -> %d",
		pendingMeta.LiveEdgeBase, walLineCount)
	pendingMeta.LiveEdgeBase = walLineCount
}
```

with:

```go
if pendingMeta.LiveEdgeBase > walLineCount {
	debuglog.Printf("[AdaptivePersistence] Clamped LiveEdgeBase %d -> %d",
		pendingMeta.LiveEdgeBase, walLineCount)
	pendingMeta.LiveEdgeBase = walLineCount
}
```

- [ ] **Step 3: Remove unused `log` import if no longer referenced**

Run: `go build ./apps/texelterm/parser/`
If the build fails with "imported and not used: log", remove the `"log"` line from the import block.

- [ ] **Step 4: Run all parser tests**

Run: `go test ./apps/texelterm/parser/ -v 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "Remove investigation debug logging from AdaptivePersistence"
```

---

## Task 12: Manual smoke test with real shell

**Files:** none modified — this is a validation step.

- [ ] **Step 1: Build the server**

Run: `make build`
Expected: success.

- [ ] **Step 2: Clean any stale history for the smoke test**

Run: `rm -rf ~/.local/share/texelation/history/terminals/*/pages ~/.local/share/texelation/history/terminals/*/wal.log 2>/dev/null || true`

- [ ] **Step 3: Run the server and reproduce the `ls -lR` burst**

In one terminal: `make server`
In another: launch the client, open a terminal pane, run `ls -lR /` until it stops.
Stop the server with Ctrl-C or the graceful shutdown path.

- [ ] **Step 4: Check the log for the old errors**

Run: `grep -E "out of bounds|stale metadata|Error closing memory buffer" ~/.texelation/server.log | tail -20`
Expected: no matches.

- [ ] **Step 5: Restart the server and verify the scrollback reloads at the correct position**

Restart: `make server`. Reconnect the client. The terminal pane should restore with the live edge matching where it was at shutdown — no blank lines above real content, no missing tail.

- [ ] **Step 6: Commit if any fixes were needed during smoke testing**

If issues were found, fix them and commit with a descriptive message. If the smoke test passes cleanly, no commit needed.

---

## Self-Review

**Spec coverage check:**

- ✅ `pageIndexEntry.globalIdx` field — Task 2
- ✅ `AppendLineWithGlobalIdx` with gap-forces-page-boundary — Task 4
- ✅ Strict error on duplicate/decreasing globalIdx — Task 4
- ✅ `LineCount()` returns logical end, `StoredLineCount()` returns stored count — Tasks 1, 3
- ✅ `UpdateLine` strict error on missing globalIdx — Task 5
- ✅ `ReadLine` / `ReadLineWithTimestamp` return (nil, nil) for gaps — Task 5
- ✅ `ReadLineRange` returns dense slice with nils — Task 6
- ✅ `FindLineAt` binary search over pageIndex — Task 7
- ✅ `prepareForAppend` handles reopened stores — Task 7
- ✅ Remove old `AppendLine` / `AppendLineWithTimestamp` — Task 8
- ✅ WAL `recoverFromWAL` and `checkpointLocked` Pass 1 use new API — Task 8
- ✅ `viewport_content_reader` handles nils — Task 9
- ✅ `vterm_memory_buffer` backfill handles nils — Task 9
- ✅ New tests for gap round-trip — Tasks 4, 5, 6, 10
- ✅ WAL checkpoint-with-gap test — Task 10
- ✅ WAL recovery-with-gap test — Task 10
- ✅ Remove `META_NOTIFY` / `META_WRITE` debug logging — Task 11
- ✅ Smoke test — Task 12

**Intentionally NOT in this plan** (per user direction):
- `liveEdgeRestored` defensive guard from PR #166 — user said "keep it near in case all this fails," so it is not re-added.
- Any backward-compat wrapper for `AppendLine` / `AppendLineWithTimestamp`.

**Type consistency:** `AppendLineWithGlobalIdx(int64, *LogicalLine, time.Time) error` is used consistently across all tasks and call sites. `LineCount()` and `StoredLineCount()` both return `int64`. `ReadLine` / `ReadLineWithTimestamp` return `(nil, nil)` / `(nil, zero, nil)` for missing indices (matching existing out-of-range behavior).

**Placeholder scan:** No TBDs, no "similar to task N," every code step shows full code.
