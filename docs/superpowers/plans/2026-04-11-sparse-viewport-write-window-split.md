# Sparse Viewport + Write-Window Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `VTerm`'s main-screen storage (MemoryBuffer + liveEdgeBase + pendingRollback heuristics) with a sparse, globalIdx-keyed cell store and a three-cursor model (contentEnd, cursor, writeTop) that decouples the TUI's write window from the user's view window, eliminating the "claude shrink pollutes scrollback" bug class.

**Architecture:** A new `apps/texelterm/parser/sparse/` package provides four composable types — `Store` (sparse cell storage), `WriteWindow` (TUI-facing cursor + resize rules), `ViewWindow` (user-facing scroll + autoFollow), and `Terminal` (thin composition exposing a `Grid()` projection). `VTerm` swaps its main-screen path from `MemoryBuffer` to `sparse.Terminal` in one cutover PR. Persistence reuses PR #167's `PageStore` via a new adapter; WAL metadata replaces `LiveEdgeBase` with `WriteTop + ContentEnd`.

**Tech Stack:** Go 1.24.3, existing `github.com/framegrace/texelation/apps/texelterm/parser` package, existing `PageStore` / `AdaptivePersistence`, standard library `sync` primitives.

**Design spec:** `docs/superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md` — read this before starting any task.

---

## Locked design choices (resolved from spec open questions)

1. **`sparse.Store` internal data structure:** `map[int64]*storeLine` where `storeLine` wraps `[]parser.Cell`. Lookup is O(1), gaps are naturally represented by missing keys, and concurrency is covered by a single `sync.RWMutex`. Chunk/page alignment to `PageStore`'s 64KB pages is **deferred** until profiling shows it's needed.
2. **WAL metadata wire format:** Bump `ViewportState` to a new struct `MainScreenState` with fields `{WriteTop, ContentEnd, CursorGlobalIdx, CursorCol, PromptStartLine, WorkingDir, SavedAt}`. JSON key-tagged. Old `ViewportState` records on disk are **discarded on load** — a warning is logged, and the terminal starts clean. (The design spec calls this "bump and break.")
3. **Concurrency boundaries:** `sparse.Store` holds one `sync.RWMutex`. `WriteWindow` holds one `sync.Mutex`. `ViewWindow` holds one `sync.Mutex`. `Terminal` does not add a lock — its methods acquire the underlying locks in strict order: `WriteWindow` → `ViewWindow` → `Store`. Deadlock-free because the graph is acyclic. Lazy-init is done eagerly in constructors, so no read methods need to upgrade their locks.
4. **Test migration strategy:** Integration PR (Task 6.x) audits `apps/texelterm/parser/*_test.go` and `apps/texelterm/testutil/*_test.go`. Tests that assert on `MemoryBuffer` internals, `pendingRollback*` fields, `liveEdgeBase`, or `PushViewportToScrollback` behavior are either flipped to the new semantics or deleted. Tests that assert on `VTerm.Grid()` output only (model-agnostic) stay as-is.
5. **Client-side viewport state:** Server-side. `sparse.ViewWindow` lives inside `VTerm` on the server. Multiple clients attached to one pane share one view. Per-client views are an optional future extension, not in scope.

---

## File structure

### New files (created in steps 1–5)

```
apps/texelterm/parser/sparse/
├── store.go                  # sparse.Store type + storeLine wrapper
├── store_test.go             # Store unit tests
├── write_window.go           # sparse.WriteWindow type (cursor, writeTop, resize rules)
├── write_window_test.go      # WriteWindow unit tests, Rule 5 in particular
├── view_window.go            # sparse.ViewWindow type (viewBottom, autoFollow, scroll)
├── view_window_test.go       # ViewWindow unit tests, Rule 6+7
├── terminal.go               # sparse.Terminal composition + Grid() projection
├── terminal_test.go          # Terminal integration unit tests
├── persistence.go            # Persistence adapter for PageStore + MainScreenState
├── persistence_test.go       # Persistence round-trip tests
└── doc.go                    # Package-level godoc
```

### Files modified in step 6 (integration PR)

```
apps/texelterm/parser/vterm.go                  # Replace memBuf state with sparse.Terminal
apps/texelterm/parser/vterm_memory_buffer.go    # Delete ~95% of this file, keep thin delegation
apps/texelterm/parser/memory_buffer.go          # DELETE entire file
apps/texelterm/parser/memory_buffer_test.go     # DELETE entire file (model-internal tests)
apps/texelterm/parser/adaptive_persistence.go   # Update metadata calls to use MainScreenState
apps/texelterm/parser/page_store.go             # Deprecate ViewportState, add MainScreenState
apps/texelterm/testutil/claude_code_shrink_test.go  # Update assertions to spec success criteria
```

### Files modified in step 7 (cleanup PR)

```
apps/texelterm/parser/viewport_window.go        # Rename to view_window.go, delegate to sparse
internal/runtime/server/desktop_publisher.go    # No functional change, just type rename
docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md       # Replace "Three-Level Architecture" section
CLAUDE.md                                       # Remove/update memory entries on old model
```

---

## Working branch strategy

Steps 1–5 are pure additions. Create one branch per step off `main`:

```
feat/sparse-store          # Step 1
feat/sparse-write-window   # Step 2 (branched from sparse-store once merged)
feat/sparse-view-window    # Step 3
feat/sparse-terminal       # Step 4
feat/sparse-persistence    # Step 5
feat/sparse-integration    # Step 6 (the cutover — waits for 1-5 to merge)
chore/sparse-cleanup       # Step 7
```

Each step gets its own PR. Do **not** work on step 6 until steps 1–5 are merged to `main` — the cutover depends on all five being live. The branch `fix/no-scrollback-from-partial-scroll-regions` is **not** merged; its 18+ fix commits are superseded.

---

# Step 1 — `parser/sparse/store.go`

**Goal:** A globalIdx-keyed sparse cell store with `Get`, `Set`, `GetLine`, `SetLine`, `ClearRange`, `Max`, `Width`. No viewport, no cursor, no resize — pure storage CRUD. Backed by `map[int64]*storeLine`, protected by one `sync.RWMutex`.

**Prereqs:** None. Pure addition.

### Task 1.1: Create package skeleton and storeLine type

**Files:**
- Create: `apps/texelterm/parser/sparse/doc.go`
- Create: `apps/texelterm/parser/sparse/store.go`
- Create: `apps/texelterm/parser/sparse/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
// apps/texelterm/parser/sparse/store_test.go
package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestStore_NewStore(t *testing.T) {
	s := NewStore(80)
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
	if got := s.Width(); got != 80 {
		t.Errorf("Width() = %d, want 80", got)
	}
	if got := s.Max(); got != -1 {
		t.Errorf("Max() of empty store = %d, want -1", got)
	}
	_ = parser.Cell{} // Keep the import; used in later tests
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestStore_NewStore -v`
Expected: FAIL with "undefined: NewStore" or similar.

- [ ] **Step 3: Write minimal implementation**

```go
// apps/texelterm/parser/sparse/doc.go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sparse provides a globalIdx-keyed sparse cell store and the
// three-cursor write/view model that replaces texelterm's dense MemoryBuffer.
//
// See docs/superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md.
package sparse
```

```go
// apps/texelterm/parser/sparse/store.go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"sync"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// storeLine is the wrapper around a row of cells in the sparse Store.
// A missing map entry represents "no content at this globalIdx" — reads of
// missing globalIdxs return blank cells.
type storeLine struct {
	cells []parser.Cell
}

// Store is a sparse, globalIdx-keyed cell storage.
//
// A cell at globalIdx X is just a cell at globalIdx X. There is no viewport
// concept, no cursor, no scrollback/viewport distinction. Reads of unwritten
// globalIdxs return blank cells. Writes at arbitrary globalIdxs are allowed.
//
// Store is safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	width    int
	lines    map[int64]*storeLine
	contentEnd int64 // highest globalIdx ever written; -1 means empty
}

// NewStore creates an empty Store for a terminal of the given column width.
func NewStore(width int) *Store {
	return &Store{
		width:      width,
		lines:      make(map[int64]*storeLine),
		contentEnd: -1,
	}
}

// Width returns the column width the Store was created with.
func (s *Store) Width() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.width
}

// Max returns the highest globalIdx ever written. Returns -1 if the Store
// has never been written to.
func (s *Store) Max() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contentEnd
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestStore_NewStore -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git checkout -b feat/sparse-store
git add apps/texelterm/parser/sparse/doc.go apps/texelterm/parser/sparse/store.go apps/texelterm/parser/sparse/store_test.go
git commit -m "feat(sparse): add Store skeleton with NewStore/Width/Max"
```

### Task 1.2: Implement Set and Get

**Files:**
- Modify: `apps/texelterm/parser/sparse/store.go`
- Modify: `apps/texelterm/parser/sparse/store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `store_test.go`:

```go
func TestStore_SetGetSingleCell(t *testing.T) {
	s := NewStore(10)
	cell := parser.Cell{Rune: 'A'}
	s.Set(5, 3, cell)

	got := s.Get(5, 3)
	if got.Rune != 'A' {
		t.Errorf("Get(5,3).Rune = %q, want %q", got.Rune, 'A')
	}
	if got := s.Max(); got != 5 {
		t.Errorf("Max() after Set(5,*) = %d, want 5", got)
	}
}

func TestStore_GetMissingReturnsBlank(t *testing.T) {
	s := NewStore(10)
	// Nothing written; every Get should return a zero-value Cell.
	got := s.Get(0, 0)
	if got.Rune != 0 {
		t.Errorf("Get on empty Store returned rune %q, want 0", got.Rune)
	}
	got = s.Get(999, 7)
	if got.Rune != 0 {
		t.Errorf("Get(999,7) on empty Store returned rune %q, want 0", got.Rune)
	}
}

func TestStore_SetExtendsBeyondExistingLine(t *testing.T) {
	s := NewStore(80)
	// Write at col 0, then col 40 on the same line.
	s.Set(0, 0, parser.Cell{Rune: 'X'})
	s.Set(0, 40, parser.Cell{Rune: 'Y'})
	if got := s.Get(0, 0).Rune; got != 'X' {
		t.Errorf("Get(0,0) = %q, want X", got)
	}
	if got := s.Get(0, 40).Rune; got != 'Y' {
		t.Errorf("Get(0,40) = %q, want Y", got)
	}
	// Cells in between should be blank.
	if got := s.Get(0, 20).Rune; got != 0 {
		t.Errorf("Get(0,20) = %q, want blank", got)
	}
}

func TestStore_MaxNeverDecreases(t *testing.T) {
	s := NewStore(10)
	s.Set(10, 0, parser.Cell{Rune: 'A'})
	s.Set(5, 0, parser.Cell{Rune: 'B'})
	if got := s.Max(); got != 10 {
		t.Errorf("Max() after writing higher then lower = %d, want 10", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL with "undefined: s.Set" / "undefined: s.Get".

- [ ] **Step 3: Implement Set and Get**

Append to `store.go`:

```go
// Get returns the Cell at (globalIdx, col). Returns a zero-value Cell if the
// globalIdx has never been written to or if col is outside the line's current
// length.
func (s *Store) Get(globalIdx int64, col int) parser.Cell {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		return parser.Cell{}
	}
	if col < 0 || col >= len(line.cells) {
		return parser.Cell{}
	}
	return line.cells[col]
}

// Set writes a single Cell at (globalIdx, col). The target line is
// automatically extended to cover col if it did not already.
func (s *Store) Set(globalIdx int64, col int, cell parser.Cell) {
	if col < 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		line = &storeLine{}
		s.lines[globalIdx] = line
	}
	if col >= len(line.cells) {
		// Extend the line with blank cells up to col.
		newCells := make([]parser.Cell, col+1)
		copy(newCells, line.cells)
		line.cells = newCells
	}
	line.cells[col] = cell
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS (all 4 tests in this task + the test from 1.1).

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/store.go apps/texelterm/parser/sparse/store_test.go
git commit -m "feat(sparse): add Store.Get/Set with auto-extend"
```

### Task 1.3: Implement GetLine and SetLine

**Files:**
- Modify: `apps/texelterm/parser/sparse/store.go`
- Modify: `apps/texelterm/parser/sparse/store_test.go`

- [ ] **Step 1: Write failing tests**

Append to `store_test.go`:

```go
func TestStore_SetLineGetLine(t *testing.T) {
	s := NewStore(10)
	line := []parser.Cell{
		{Rune: 'h'}, {Rune: 'i'}, {Rune: '!'},
	}
	s.SetLine(3, line)

	got := s.GetLine(3)
	if len(got) != 3 {
		t.Fatalf("GetLine(3) len = %d, want 3", len(got))
	}
	if got[0].Rune != 'h' || got[1].Rune != 'i' || got[2].Rune != '!' {
		t.Errorf("GetLine(3) runes = %q,%q,%q; want h,i,!",
			got[0].Rune, got[1].Rune, got[2].Rune)
	}
}

func TestStore_SetLineOverwritesExistingCells(t *testing.T) {
	s := NewStore(10)
	s.Set(0, 5, parser.Cell{Rune: 'X'}) // existing cell at col 5
	s.SetLine(0, []parser.Cell{{Rune: 'A'}, {Rune: 'B'}})

	line := s.GetLine(0)
	if len(line) != 2 {
		t.Fatalf("GetLine(0) len = %d, want 2 (SetLine replaces, not merges)", len(line))
	}
}

func TestStore_GetLineDoesNotAffectAdjacent(t *testing.T) {
	s := NewStore(10)
	s.SetLine(5, []parser.Cell{{Rune: 'X'}})
	// Line 4 and 6 are untouched.
	if got := s.GetLine(4); got != nil && len(got) != 0 {
		t.Errorf("GetLine(4) = %v, want empty/nil", got)
	}
	if got := s.GetLine(6); got != nil && len(got) != 0 {
		t.Errorf("GetLine(6) = %v, want empty/nil", got)
	}
}

func TestStore_GetLineReturnsCopy(t *testing.T) {
	s := NewStore(10)
	s.SetLine(0, []parser.Cell{{Rune: 'A'}})
	line := s.GetLine(0)
	line[0].Rune = 'Z' // mutate returned slice
	// The store must not be affected.
	if got := s.Get(0, 0).Rune; got != 'A' {
		t.Errorf("Store was mutated by caller: Get(0,0) = %q, want A", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL with "undefined: s.SetLine" / "undefined: s.GetLine".

- [ ] **Step 3: Implement SetLine and GetLine**

Append to `store.go`:

```go
// GetLine returns a copy of the cells at globalIdx. Returns nil if the
// globalIdx has never been written to. The returned slice is safe to mutate
// — it does not alias Store internal state.
func (s *Store) GetLine(globalIdx int64) []parser.Cell {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		return nil
	}
	out := make([]parser.Cell, len(line.cells))
	copy(out, line.cells)
	return out
}

// SetLine replaces the cells at globalIdx with a copy of cells. Any existing
// content at that globalIdx is overwritten in full. To preserve alignment
// with column 0, callers must pass cells starting at column 0.
func (s *Store) SetLine(globalIdx int64, cells []parser.Cell) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		line = &storeLine{}
		s.lines[globalIdx] = line
	}
	line.cells = make([]parser.Cell, len(cells))
	copy(line.cells, cells)
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/store.go apps/texelterm/parser/sparse/store_test.go
git commit -m "feat(sparse): add Store.GetLine/SetLine with defensive copy"
```

### Task 1.4: Implement ClearRange

**Files:**
- Modify: `apps/texelterm/parser/sparse/store.go`
- Modify: `apps/texelterm/parser/sparse/store_test.go`

- [ ] **Step 1: Write failing tests**

Append to `store_test.go`:

```go
func TestStore_ClearRangeRemovesOnlyTargets(t *testing.T) {
	s := NewStore(10)
	s.SetLine(0, []parser.Cell{{Rune: 'A'}})
	s.SetLine(5, []parser.Cell{{Rune: 'B'}})
	s.SetLine(10, []parser.Cell{{Rune: 'C'}})

	s.ClearRange(3, 7) // inclusive range

	if got := s.GetLine(0); got == nil || got[0].Rune != 'A' {
		t.Errorf("line 0 should be preserved, got %v", got)
	}
	if got := s.GetLine(5); got != nil && len(got) > 0 && got[0].Rune != 0 {
		t.Errorf("line 5 should be cleared, got %v", got)
	}
	if got := s.GetLine(10); got == nil || got[0].Rune != 'C' {
		t.Errorf("line 10 should be preserved, got %v", got)
	}
}

func TestStore_ClearRangeKeepsContentEnd(t *testing.T) {
	s := NewStore(10)
	s.SetLine(20, []parser.Cell{{Rune: 'X'}})
	s.ClearRange(20, 20)
	if got := s.Max(); got != 20 {
		t.Errorf("Max() after ClearRange = %d, want 20 (contentEnd never decreases)", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL with "undefined: s.ClearRange".

- [ ] **Step 3: Implement ClearRange**

Append to `store.go`:

```go
// ClearRange removes every line in the closed interval [lo, hi]. Lines
// outside the interval are untouched. contentEnd is not decreased — a
// cleared range still counts as "ever been written" for the high-water mark.
func (s *Store) ClearRange(lo, hi int64) {
	if lo > hi {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := lo; k <= hi; k++ {
		delete(s.lines, k)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/store.go apps/texelterm/parser/sparse/store_test.go
git commit -m "feat(sparse): add Store.ClearRange (contentEnd monotonic)"
```

### Task 1.5: Concurrency smoke test

**Files:**
- Modify: `apps/texelterm/parser/sparse/store_test.go`

- [ ] **Step 1: Write the race test**

Append to `store_test.go`:

```go
func TestStore_ConcurrentReadersWriter(t *testing.T) {
	s := NewStore(80)
	const N = 200
	done := make(chan struct{})

	// One writer filling in lines.
	go func() {
		for i := int64(0); i < N; i++ {
			s.SetLine(i, []parser.Cell{{Rune: 'x'}})
		}
		close(done)
	}()

	// Many readers hammering.
	for r := 0; r < 8; r++ {
		go func() {
			for i := int64(0); i < N; i++ {
				_ = s.Get(i, 0)
				_ = s.GetLine(i)
				_ = s.Max()
				_ = s.Width()
			}
		}()
	}

	<-done
}
```

- [ ] **Step 2: Run with race detector**

Run: `go test -race ./apps/texelterm/parser/sparse/ -run TestStore_ConcurrentReadersWriter -v`
Expected: PASS with no race detector warnings.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/sparse/store_test.go
git commit -m "test(sparse): add concurrent reader/writer smoke test"
```

### Task 1.6: Push branch and open PR

- [ ] **Step 1: Run full test suite for the new package**

Run: `go test -race ./apps/texelterm/parser/sparse/...`
Expected: PASS.

- [ ] **Step 2: Push branch**

```bash
git push -u origin feat/sparse-store
```

- [ ] **Step 3: Open PR**

Title: `feat(sparse): add Store — sparse globalIdx-keyed cell storage`
Body: Link to design spec + note that this is step 1 of 7.

Wait for review and merge before starting step 2.

---

# Step 2 — `parser/sparse/write_window.go`

**Goal:** `sparse.WriteWindow` owning `writeTop`, `height`, `width`, `cursor`. Implements Rule 5 (shrink cursor-minimum-advance, grow writeBottom-anchor) from the spec. All writes go through the underlying `Store`.

**Prereqs:** Step 1 merged.

### Task 2.1: Skeleton + WriteWindow constructor

**Files:**
- Create: `apps/texelterm/parser/sparse/write_window.go`
- Create: `apps/texelterm/parser/sparse/write_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
// apps/texelterm/parser/sparse/write_window_test.go
package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestWriteWindow_NewInitialState(t *testing.T) {
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 24)
	if got := ww.Width(); got != 80 {
		t.Errorf("Width() = %d, want 80", got)
	}
	if got := ww.Height(); got != 24 {
		t.Errorf("Height() = %d, want 24", got)
	}
	if got := ww.WriteTop(); got != 0 {
		t.Errorf("WriteTop() = %d, want 0 (fresh WriteWindow)", got)
	}
	if got := ww.WriteBottom(); got != 23 {
		t.Errorf("WriteBottom() = %d, want 23", got)
	}
	gi, col := ww.Cursor()
	if gi != 0 || col != 0 {
		t.Errorf("Cursor() = (%d,%d), want (0,0)", gi, col)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestWriteWindow_NewInitialState -v`
Expected: FAIL with "undefined: NewWriteWindow".

- [ ] **Step 3: Implement skeleton**

```go
// apps/texelterm/parser/sparse/write_window.go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"sync"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// WriteWindow is the TUI-facing portion of a sparse terminal. It owns the
// cursor and the writeTop anchor, and it forwards writes to an underlying
// Store.
//
// Applications issue cursor-relative writes: ESC[row;colH resolves to
// (writeTop + row - 1, col - 1). The addressable area is the closed range
// [writeTop, writeBottom], where writeBottom is derived as writeTop + height - 1.
//
// WriteWindow is safe for concurrent use. Callers that need to observe
// window-move events should consult WriteTop/WriteBottom after each call.
type WriteWindow struct {
	mu     sync.Mutex
	store  *Store
	width  int
	height int

	writeTop       int64
	cursorGlobalIdx int64
	cursorCol      int
}

// NewWriteWindow creates a WriteWindow anchored at globalIdx 0 with the given
// dimensions. The cursor starts at (writeTop, 0).
func NewWriteWindow(store *Store, width, height int) *WriteWindow {
	return &WriteWindow{
		store:  store,
		width:  width,
		height: height,
	}
}

// Width returns the current column width.
func (w *WriteWindow) Width() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.width
}

// Height returns the current row height.
func (w *WriteWindow) Height() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.height
}

// WriteTop returns the globalIdx of the top row of the write window.
func (w *WriteWindow) WriteTop() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeTop
}

// WriteBottom returns the globalIdx of the bottom row of the write window.
func (w *WriteWindow) WriteBottom() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeTop + int64(w.height) - 1
}

// Cursor returns the current cursor position as (globalIdx, col).
func (w *WriteWindow) Cursor() (globalIdx int64, col int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cursorGlobalIdx, w.cursorCol
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestWriteWindow_NewInitialState -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git checkout -b feat/sparse-write-window
git add apps/texelterm/parser/sparse/write_window.go apps/texelterm/parser/sparse/write_window_test.go
git commit -m "feat(sparse): add WriteWindow skeleton and getters"
```

### Task 2.2: WriteCell (no wrap yet)

**Files:**
- Modify: `apps/texelterm/parser/sparse/write_window.go`
- Modify: `apps/texelterm/parser/sparse/write_window_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestWriteWindow_WriteCellAdvancesCol(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.WriteCell(parser.Cell{Rune: 'h'})
	ww.WriteCell(parser.Cell{Rune: 'i'})

	gi, col := ww.Cursor()
	if gi != 0 || col != 2 {
		t.Errorf("Cursor() after 2 writes = (%d,%d), want (0,2)", gi, col)
	}
	if got := store.Get(0, 0).Rune; got != 'h' {
		t.Errorf("store[0][0] = %q, want h", got)
	}
	if got := store.Get(0, 1).Rune; got != 'i' {
		t.Errorf("store[0][1] = %q, want i", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestWriteWindow_WriteCellAdvancesCol -v`
Expected: FAIL.

- [ ] **Step 3: Implement WriteCell**

Append to `write_window.go`:

```go
// WriteCell writes one cell at the current cursor position and advances the
// cursor column by one. This method does NOT handle line wrap — the caller
// (typically the Parser layer) is responsible for wrap semantics.
func (w *WriteWindow) WriteCell(cell parser.Cell) {
	w.mu.Lock()
	gi := w.cursorGlobalIdx
	col := w.cursorCol
	w.cursorCol++
	w.mu.Unlock()

	w.store.Set(gi, col, cell)
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestWriteWindow_WriteCellAdvancesCol -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/write_window.go apps/texelterm/parser/sparse/write_window_test.go
git commit -m "feat(sparse): WriteWindow.WriteCell writes and advances col"
```

### Task 2.3: CarriageReturn and SetCursor

**Files:**
- Modify: `apps/texelterm/parser/sparse/write_window.go`
- Modify: `apps/texelterm/parser/sparse/write_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestWriteWindow_CarriageReturn(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.WriteCell(parser.Cell{Rune: 'h'})
	ww.WriteCell(parser.Cell{Rune: 'i'})
	ww.CarriageReturn()
	gi, col := ww.Cursor()
	if gi != 0 || col != 0 {
		t.Errorf("after CR, Cursor() = (%d,%d), want (0,0)", gi, col)
	}
}

func TestWriteWindow_SetCursorRelative(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 10)
	ww.SetCursor(3, 7) // row 3, col 7
	gi, col := ww.Cursor()
	if gi != 3 || col != 7 {
		t.Errorf("SetCursor(3,7): Cursor() = (%d,%d), want (3,7)", gi, col)
	}
	if got := ww.CursorRow(); got != 3 {
		t.Errorf("CursorRow() = %d, want 3", got)
	}
}

func TestWriteWindow_SetCursorClampsToWindow(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.SetCursor(100, 100) // way out of range
	gi, col := ww.Cursor()
	// Clamp row to [0, height-1] and col to [0, width-1].
	if gi != 4 {
		t.Errorf("row clamp: gi = %d, want 4", gi)
	}
	if col != 9 {
		t.Errorf("col clamp: col = %d, want 9", col)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL on the new tests.

- [ ] **Step 3: Implement**

Append to `write_window.go`:

```go
// CarriageReturn resets the cursor column to 0. The cursor globalIdx is
// unchanged.
func (w *WriteWindow) CarriageReturn() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cursorCol = 0
}

// SetCursor places the cursor at row, col relative to the write window.
// Rows are clamped to [0, height-1]; cols to [0, width-1].
func (w *WriteWindow) SetCursor(row, col int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if row < 0 {
		row = 0
	}
	if row >= w.height {
		row = w.height - 1
	}
	if col < 0 {
		col = 0
	}
	if col >= w.width {
		col = w.width - 1
	}
	w.cursorGlobalIdx = w.writeTop + int64(row)
	w.cursorCol = col
}

// CursorRow returns the cursor's row relative to the write window top.
func (w *WriteWindow) CursorRow() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return int(w.cursorGlobalIdx - w.writeTop)
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/write_window.go apps/texelterm/parser/sparse/write_window_test.go
git commit -m "feat(sparse): WriteWindow.CarriageReturn and SetCursor with clamping"
```

### Task 2.4: Newline with scroll-up at bottom

**Files:**
- Modify: `apps/texelterm/parser/sparse/write_window.go`
- Modify: `apps/texelterm/parser/sparse/write_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestWriteWindow_NewlineAdvancesCursor(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.WriteCell(parser.Cell{Rune: 'a'})
	ww.Newline()

	gi, col := ww.Cursor()
	if gi != 1 || col != 0 {
		t.Errorf("after Newline from row 0, Cursor() = (%d,%d), want (1,0)", gi, col)
	}
	if got := ww.WriteTop(); got != 0 {
		t.Errorf("WriteTop() should not move; got %d", got)
	}
}

func TestWriteWindow_NewlineAtBottomAdvancesWriteTop(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 3)
	// Park cursor at last row.
	ww.SetCursor(2, 0)
	ww.Newline()

	if got := ww.WriteTop(); got != 1 {
		t.Errorf("WriteTop() after LF at bottom = %d, want 1 (scrolled up)", got)
	}
	if got := ww.WriteBottom(); got != 3 {
		t.Errorf("WriteBottom() = %d, want 3", got)
	}
	gi, col := ww.Cursor()
	if gi != 3 || col != 0 {
		t.Errorf("Cursor() = (%d,%d), want (3,0)", gi, col)
	}
}

func TestWriteWindow_NewlinePreservesContent(t *testing.T) {
	// Content at oldWriteTop (row 0) must stay in the store even after the
	// window moves — that's the whole "scrollback is a windowing concept"
	// principle.
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 3)
	ww.WriteCell(parser.Cell{Rune: 'H'})  // row 0
	ww.SetCursor(2, 0)
	ww.Newline() // scrolls

	if got := store.Get(0, 0).Rune; got != 'H' {
		t.Errorf("after scroll-up, store[0][0] = %q, want H (content survives)", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL with "undefined: ww.Newline".

- [ ] **Step 3: Implement**

Append to `write_window.go`:

```go
// Newline advances the cursor to the next row. If the cursor is already at
// the bottom row of the write window, writeTop is advanced by 1 (classical
// scroll-up). Cells at the old writeTop remain in the Store — they are now
// "historical" simply because the window moved, not because they were copied.
// The cursor column is reset to 0 (combined CR+LF semantics of LF in most
// terminal modes; pure LF variants are handled by the parser, not here).
func (w *WriteWindow) Newline() {
	w.mu.Lock()
	defer w.mu.Unlock()
	writeBottom := w.writeTop + int64(w.height) - 1
	if w.cursorGlobalIdx >= writeBottom {
		// At or below bottom — scroll up.
		w.writeTop++
		w.cursorGlobalIdx = w.writeTop + int64(w.height) - 1
	} else {
		w.cursorGlobalIdx++
	}
	w.cursorCol = 0
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/write_window.go apps/texelterm/parser/sparse/write_window_test.go
git commit -m "feat(sparse): WriteWindow.Newline advances writeTop at bottom"
```

### Task 2.5: Resize — grow (writeBottom-anchor rule)

**Files:**
- Modify: `apps/texelterm/parser/sparse/write_window.go`
- Modify: `apps/texelterm/parser/sparse/write_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestWriteWindow_ResizeGrowRetreatsWriteTop(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	// Scroll down 10 times so writeTop is at 10.
	for i := 0; i < 10; i++ {
		ww.SetCursor(4, 0)
		ww.Newline()
	}
	if got := ww.WriteTop(); got != 10 {
		t.Fatalf("setup: WriteTop = %d, want 10", got)
	}

	// Grow from 5 to 8. writeTop should retreat by 3 to keep writeBottom pinned.
	ww.Resize(10, 8)
	if got := ww.WriteTop(); got != 7 {
		t.Errorf("after grow 5->8, WriteTop = %d, want 7", got)
	}
	if got := ww.WriteBottom(); got != 14 {
		t.Errorf("after grow, WriteBottom = %d, want 14 (unchanged)", got)
	}
	if got := ww.Height(); got != 8 {
		t.Errorf("Height = %d, want 8", got)
	}
}

func TestWriteWindow_ResizeGrowClampsAtZero(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	// writeTop = 0. Grow to 10 — shallow scrollback case.
	ww.Resize(10, 10)
	if got := ww.WriteTop(); got != 0 {
		t.Errorf("after grow from 0, WriteTop = %d, want 0 (clamped)", got)
	}
	if got := ww.WriteBottom(); got != 9 {
		t.Errorf("WriteBottom = %d, want 9 (extended past oldWriteBottom=4)", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL with "undefined: ww.Resize".

- [ ] **Step 3: Implement grow path (shrink comes next task)**

Append to `write_window.go`:

```go
// Resize applies Rule 5 from the design spec.
//
// Grow: writeTop retreats by the grow delta, clamped at 0. No cells are
// cleared. The new top rows of the window expose whatever is already stored
// there (old scrollback, or blank if none).
//
// Shrink: cursor-minimum-advance — writeTop advances by the minimum amount
// needed to keep the cursor inside the new write window. Cells below the
// new writeBottom are cleared. Cells in [oldWriteTop, newWriteTop) stay in
// the Store and become "above the window" (scrollback).
//
// Pure width changes (newHeight == height) apply only to width without
// touching writeTop.
func (w *WriteWindow) Resize(newWidth, newHeight int) {
	if newWidth <= 0 || newHeight <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if newHeight > w.height {
		w.resizeGrowLocked(newWidth, newHeight)
	} else if newHeight < w.height {
		w.resizeShrinkLocked(newWidth, newHeight)
	}
	w.width = newWidth
	w.height = newHeight

	// Keep cursor in bounds.
	if w.cursorGlobalIdx < w.writeTop {
		w.cursorGlobalIdx = w.writeTop
	}
	if bottom := w.writeTop + int64(w.height) - 1; w.cursorGlobalIdx > bottom {
		w.cursorGlobalIdx = bottom
	}
	if w.cursorCol >= w.width {
		w.cursorCol = w.width - 1
	}
}

// resizeGrowLocked assumes w.mu is held.
func (w *WriteWindow) resizeGrowLocked(newWidth, newHeight int) {
	delta := int64(newHeight - w.height)
	newTop := w.writeTop - delta
	if newTop < 0 {
		newTop = 0
	}
	w.writeTop = newTop
}

// resizeShrinkLocked assumes w.mu is held. Stub — real impl in Task 2.6.
func (w *WriteWindow) resizeShrinkLocked(newWidth, newHeight int) {
	// Placeholder; Task 2.6 replaces this with cursor-minimum-advance.
	_ = newWidth
	_ = newHeight
}
```

- [ ] **Step 4: Run grow tests to verify they pass**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestWriteWindow_ResizeGrow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/write_window.go apps/texelterm/parser/sparse/write_window_test.go
git commit -m "feat(sparse): WriteWindow.Resize grow path (writeBottom-anchor)"
```

### Task 2.6: Resize — shrink (cursor-minimum-advance rule)

**Files:**
- Modify: `apps/texelterm/parser/sparse/write_window.go`
- Modify: `apps/texelterm/parser/sparse/write_window_test.go`

- [ ] **Step 1: Write failing tests**

These tests encode the three cases from the spec's "Why this rule set" section.

```go
func TestWriteWindow_ResizeShrinkShellCase(t *testing.T) {
	// Shell case: cursor at bottom row. Shrink should advance writeTop by
	// exactly the shrink delta, keeping the cursor pinned at the new bottom.
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 40)
	// Fill some content and park cursor at row 39 (bottom).
	for i := 0; i < 40; i++ {
		store.SetLine(int64(i), []parser.Cell{{Rune: 'L'}}) // row marker
	}
	ww.SetCursor(39, 5)

	ww.Resize(80, 20)

	if got := ww.WriteTop(); got != 20 {
		t.Errorf("shell shrink 40->20: WriteTop = %d, want 20", got)
	}
	gi, col := ww.Cursor()
	if gi != 39 || col != 5 {
		t.Errorf("cursor moved: (%d,%d), want (39,5)", gi, col)
	}
	// Old top rows [0, 20) must still be in the store.
	if got := store.Get(0, 0).Rune; got != 'L' {
		t.Errorf("row 0 should survive in store: %q", got)
	}
	if got := store.Get(19, 0).Rune; got != 'L' {
		t.Errorf("row 19 should survive in store: %q", got)
	}
}

func TestWriteWindow_ResizeShrinkCursorNearTop(t *testing.T) {
	// Full-screen TUI case: cursor at row 2. Shrink from 40 to 20 — cursor
	// still fits. writeTop unchanged; bottom rows cleared.
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 40)
	for i := 0; i < 40; i++ {
		store.SetLine(int64(i), []parser.Cell{{Rune: 'L'}})
	}
	ww.SetCursor(2, 0)

	ww.Resize(80, 20)

	if got := ww.WriteTop(); got != 0 {
		t.Errorf("top-cursor shrink: WriteTop = %d, want 0 (no advance)", got)
	}
	// Cells [20, 39] should be cleared from the store.
	if got := store.GetLine(20); got != nil && len(got) > 0 && got[0].Rune != 0 {
		t.Errorf("row 20 should be cleared, got %v", got)
	}
	if got := store.GetLine(39); got != nil && len(got) > 0 && got[0].Rune != 0 {
		t.Errorf("row 39 should be cleared, got %v", got)
	}
	// Row 0 still there.
	if got := store.Get(0, 0).Rune; got != 'L' {
		t.Errorf("row 0 unchanged: %q", got)
	}
}

func TestWriteWindow_ResizeShrinkPartialAdvance(t *testing.T) {
	// Claude case: cursor at row 30 of h=40. Shrink to h=20 — cursor would
	// otherwise be at row 30 of a 20-row window, outside. Advance should
	// be exactly 11 (cursor.globalIdx=30 must fit in [newTop, newTop+19]).
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 40)
	ww.SetCursor(30, 0)

	ww.Resize(80, 20)

	if got := ww.WriteTop(); got != 11 {
		t.Errorf("partial-advance shrink: WriteTop = %d, want 11", got)
	}
	gi, _ := ww.Cursor()
	if gi != 30 {
		t.Errorf("cursor globalIdx moved: %d, want 30 (cursor is pinned)", gi)
	}
	// Cursor row within new window = 30 - 11 = 19 (bottom of new window).
	if got := ww.CursorRow(); got != 19 {
		t.Errorf("CursorRow after partial advance = %d, want 19", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL on the 3 new tests.

- [ ] **Step 3: Replace shrink stub with real implementation**

In `write_window.go`, replace `resizeShrinkLocked`:

```go
// resizeShrinkLocked implements Rule 5 shrink: cursor-minimum-advance.
// Preconditions: w.mu held, newHeight < w.height.
func (w *WriteWindow) resizeShrinkLocked(newWidth, newHeight int) {
	oldWriteBottom := w.writeTop + int64(w.height) - 1
	// Tentative newWriteBottom if writeTop didn't move.
	tentativeBottom := w.writeTop + int64(newHeight) - 1

	advance := int64(0)
	if w.cursorGlobalIdx > tentativeBottom {
		advance = w.cursorGlobalIdx - tentativeBottom
	}
	w.writeTop += advance
	newWriteBottom := w.writeTop + int64(newHeight) - 1

	// Cells [newWriteBottom+1, oldWriteBottom] are scratch space below the
	// new window. Clear them.
	if oldWriteBottom > newWriteBottom {
		w.store.ClearRange(newWriteBottom+1, oldWriteBottom)
	}

	// Cells in [oldWriteTop, writeTop) (only when advance > 0) stay in the
	// store. They are now "above the window" — scrollback. No action needed.
	_ = newWidth
}
```

- [ ] **Step 4: Run all WriteWindow tests**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS on all WriteWindow tests.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/write_window.go apps/texelterm/parser/sparse/write_window_test.go
git commit -m "feat(sparse): WriteWindow.Resize shrink with cursor-minimum-advance"
```

### Task 2.7: EraseInLine, EraseInDisplay, ScrollUp/Down on the window

**Files:**
- Modify: `apps/texelterm/parser/sparse/write_window.go`
- Modify: `apps/texelterm/parser/sparse/write_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestWriteWindow_EraseDisplayClearsWindow(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	// Fill store [0..9] with content, window covers [0..4].
	for i := int64(0); i < 10; i++ {
		store.SetLine(i, []parser.Cell{{Rune: 'X'}})
	}
	ww.EraseDisplay()
	// [0..4] cleared; [5..9] preserved.
	for i := int64(0); i <= 4; i++ {
		if got := store.GetLine(i); got != nil && len(got) > 0 && got[0].Rune != 0 {
			t.Errorf("row %d should be cleared, got %v", i, got)
		}
	}
	for i := int64(5); i <= 9; i++ {
		if got := store.Get(i, 0).Rune; got != 'X' {
			t.Errorf("row %d should be preserved, got %q", i, got)
		}
	}
}

func TestWriteWindow_EraseLineClearsCurrentRow(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	store.SetLine(2, []parser.Cell{{Rune: 'A'}, {Rune: 'B'}, {Rune: 'C'}})
	ww.SetCursor(2, 0)
	ww.EraseLine()
	if got := store.GetLine(2); got != nil && len(got) > 0 && got[0].Rune != 0 {
		t.Errorf("row 2 should be cleared, got %v", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `write_window.go`:

```go
// EraseDisplay clears every cell in the current write window [writeTop,
// writeBottom]. Cells outside the window are not touched.
func (w *WriteWindow) EraseDisplay() {
	w.mu.Lock()
	top := w.writeTop
	bottom := w.writeTop + int64(w.height) - 1
	w.mu.Unlock()
	w.store.ClearRange(top, bottom)
}

// EraseLine clears the line at the cursor's current globalIdx.
func (w *WriteWindow) EraseLine() {
	w.mu.Lock()
	gi := w.cursorGlobalIdx
	w.mu.Unlock()
	w.store.ClearRange(gi, gi)
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/write_window.go apps/texelterm/parser/sparse/write_window_test.go
git commit -m "feat(sparse): WriteWindow.EraseDisplay and EraseLine"
```

### Task 2.8: Push branch and open PR

- [ ] **Step 1: Full test run**

Run: `go test -race ./apps/texelterm/parser/sparse/...`
Expected: PASS.

- [ ] **Step 2: Push and open PR**

```bash
git push -u origin feat/sparse-write-window
```

Title: `feat(sparse): add WriteWindow with Rule 5 resize`
Body: Note the three shrink cases covered by tests (shell / cursor-near-top / partial advance). Link to design spec.

Wait for review and merge.

---

# Step 3 — `parser/sparse/view_window.go`

**Goal:** `sparse.ViewWindow` owning `viewBottom`, `height`, `width`, `autoFollow`. Implements Rule 6 (resize), Rule 7 (scroll), and the Rule 4 autoFollow observer callbacks.

**Prereqs:** Step 2 merged.

### Task 3.1: Skeleton + initial state

**Files:**
- Create: `apps/texelterm/parser/sparse/view_window.go`
- Create: `apps/texelterm/parser/sparse/view_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
// apps/texelterm/parser/sparse/view_window_test.go
package sparse

import "testing"

func TestViewWindow_NewFollowing(t *testing.T) {
	vw := NewViewWindow(80, 24)
	if !vw.IsFollowing() {
		t.Error("new ViewWindow should be in autoFollow mode")
	}
	if got := vw.Height(); got != 24 {
		t.Errorf("Height = %d, want 24", got)
	}
	if got := vw.Width(); got != 80 {
		t.Errorf("Width = %d, want 80", got)
	}
}

func TestViewWindow_VisibleRangeInitially(t *testing.T) {
	vw := NewViewWindow(80, 24)
	top, bottom := vw.VisibleRange()
	// Fresh ViewWindow pretends writeBottom is height-1 until told otherwise.
	if bottom != 23 {
		t.Errorf("fresh viewBottom = %d, want 23", bottom)
	}
	if top != 0 {
		t.Errorf("fresh viewTop = %d, want 0", top)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestViewWindow -v`
Expected: FAIL.

- [ ] **Step 3: Implement skeleton**

```go
// apps/texelterm/parser/sparse/view_window.go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import "sync"

// ViewWindow is the user-facing portion of a sparse terminal. It owns the
// viewBottom anchor and the autoFollow flag, and it responds to write-window
// events when following.
//
// ViewWindow does not read from the Store directly — it only tracks the
// coordinate pair (viewTop, viewBottom) for the caller to project.
// ViewWindow is safe for concurrent use.
type ViewWindow struct {
	mu         sync.Mutex
	width      int
	height     int
	viewBottom int64
	autoFollow bool
}

// NewViewWindow creates a ViewWindow in autoFollow mode. viewBottom starts
// at height-1 so a fresh terminal projects rows [0, height-1].
func NewViewWindow(width, height int) *ViewWindow {
	return &ViewWindow{
		width:      width,
		height:     height,
		viewBottom: int64(height - 1),
		autoFollow: true,
	}
}

// Width returns the current column width.
func (v *ViewWindow) Width() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.width
}

// Height returns the current row height.
func (v *ViewWindow) Height() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.height
}

// IsFollowing reports whether the view is tracking the write window.
func (v *ViewWindow) IsFollowing() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.autoFollow
}

// VisibleRange returns the (top, bottom) globalIdx pair that the caller
// should project from the Store.
func (v *ViewWindow) VisibleRange() (top, bottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.viewBottom - int64(v.height) + 1, v.viewBottom
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestViewWindow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git checkout -b feat/sparse-view-window
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/view_window_test.go
git commit -m "feat(sparse): add ViewWindow skeleton"
```

### Task 3.2: OnWriteBottomChanged / OnWriteTopChanged

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Modify: `apps/texelterm/parser/sparse/view_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestViewWindow_FollowsWriteBottom(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	_, bottom := vw.VisibleRange()
	if bottom != 100 {
		t.Errorf("autoFollow: viewBottom = %d, want 100", bottom)
	}
}

func TestViewWindow_DoesNotFollowWhenScrolledBack(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(10) // detaches from live edge
	if vw.IsFollowing() {
		t.Error("after ScrollUp, should not be following")
	}
	vw.OnWriteBottomChanged(200)
	_, bottom := vw.VisibleRange()
	if bottom != 90 {
		t.Errorf("frozen viewBottom = %d, want 90 (unchanged)", bottom)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `view_window.go`:

```go
// OnWriteBottomChanged is called by the WriteWindow observer wiring when the
// bottom of the write window moves. If autoFollow is true, viewBottom is
// updated to match.
func (v *ViewWindow) OnWriteBottomChanged(newBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.autoFollow {
		v.viewBottom = newBottom
	}
}

// OnWriteTopChanged is called when the WriteWindow retreats its top on grow.
// If autoFollow is true, viewBottom snaps to the new writeBottom (caller
// passes the new writeBottom directly, NOT writeTop).
func (v *ViewWindow) OnWriteTopChanged(newBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.autoFollow {
		v.viewBottom = newBottom
	}
}

// ScrollUp detaches from the live edge and moves viewBottom up by n lines.
// viewBottom is clamped to at least height-1 (can't show negative globalIdxs
// as the view bottom).
func (v *ViewWindow) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoFollow = false
	v.viewBottom -= int64(n)
	minBottom := int64(v.height - 1)
	if v.viewBottom < minBottom {
		v.viewBottom = minBottom
	}
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/view_window_test.go
git commit -m "feat(sparse): ViewWindow observer callbacks and ScrollUp"
```

### Task 3.3: ScrollDown, ScrollToBottom, OnInput

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Modify: `apps/texelterm/parser/sparse/view_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestViewWindow_ScrollDownClampedToWriteBottom(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(30)
	vw.ScrollDown(100, 100) // n, writeBottom
	if !vw.IsFollowing() {
		// ScrollDown does not auto-reattach, but reaching writeBottom does.
	}
	_, bottom := vw.VisibleRange()
	if bottom != 100 {
		t.Errorf("ScrollDown clamped at writeBottom: viewBottom = %d, want 100", bottom)
	}
}

func TestViewWindow_ScrollToBottomReattaches(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(50)
	vw.ScrollToBottom(100)

	if !vw.IsFollowing() {
		t.Error("ScrollToBottom should re-engage autoFollow")
	}
	_, bottom := vw.VisibleRange()
	if bottom != 100 {
		t.Errorf("viewBottom = %d, want 100", bottom)
	}
}

func TestViewWindow_OnInputReattaches(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(50)
	vw.OnInput(100)
	if !vw.IsFollowing() {
		t.Error("OnInput should re-engage autoFollow")
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `view_window.go`:

```go
// ScrollDown moves viewBottom down by n lines toward the live edge. writeBottom
// is the current WriteWindow bottom; ScrollDown will not move past it. If
// viewBottom reaches writeBottom, autoFollow is automatically re-engaged.
func (v *ViewWindow) ScrollDown(n int, writeBottom int64) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom += int64(n)
	if v.viewBottom >= writeBottom {
		v.viewBottom = writeBottom
		v.autoFollow = true
	}
}

// ScrollToBottom snaps viewBottom to writeBottom and re-engages autoFollow.
func (v *ViewWindow) ScrollToBottom(writeBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom = writeBottom
	v.autoFollow = true
}

// OnInput is called when the user types or clicks in the pane. Re-engages
// autoFollow at the current writeBottom.
func (v *ViewWindow) OnInput(writeBottom int64) {
	v.ScrollToBottom(writeBottom)
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/view_window_test.go
git commit -m "feat(sparse): ViewWindow ScrollDown/ScrollToBottom/OnInput"
```

### Task 3.4: Resize (Rule 6)

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Modify: `apps/texelterm/parser/sparse/view_window_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestViewWindow_ResizeWhileFollowing(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.Resize(80, 30, 115) // new size, new writeBottom (grew by 15)
	_, bottom := vw.VisibleRange()
	if bottom != 115 {
		t.Errorf("follow-resize: viewBottom = %d, want 115", bottom)
	}
	if got := vw.Height(); got != 30 {
		t.Errorf("Height = %d, want 30", got)
	}
}

func TestViewWindow_ResizeWhileScrolledBack(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(30) // viewBottom = 70, autoFollow off
	vw.Resize(80, 30, 100)  // grow height; writeBottom unchanged
	_, bottom := vw.VisibleRange()
	if bottom != 70 {
		t.Errorf("frozen view: viewBottom = %d, want 70 (anchored)", bottom)
	}
	if got := vw.Height(); got != 30 {
		t.Errorf("Height = %d, want 30", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `view_window.go`:

```go
// Resize applies Rule 6 from the design spec.
//
// If autoFollow is true, viewBottom is snapped to newWriteBottom so the view
// follows the (possibly moved) write window.
//
// If autoFollow is false, viewBottom is unchanged. viewTop is simply derived
// from the new height, which may reveal or hide rows above viewBottom.
func (v *ViewWindow) Resize(newWidth, newHeight int, newWriteBottom int64) {
	if newWidth <= 0 || newHeight <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.width = newWidth
	v.height = newHeight
	if v.autoFollow {
		v.viewBottom = newWriteBottom
	}
	// Enforce viewBottom >= height - 1.
	minBottom := int64(v.height - 1)
	if v.viewBottom < minBottom {
		v.viewBottom = minBottom
	}
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/view_window_test.go
git commit -m "feat(sparse): ViewWindow.Resize (Rule 6)"
```

### Task 3.5: Push branch and open PR

- [ ] **Step 1: Full test run with race**

Run: `go test -race ./apps/texelterm/parser/sparse/...`
Expected: PASS.

- [ ] **Step 2: Push**

```bash
git push -u origin feat/sparse-view-window
```

- [ ] **Step 3: Open PR**

Title: `feat(sparse): add ViewWindow with autoFollow and Rule 6/7`
Body: Note that ScrollDown clamps to writeBottom (passed in as a parameter so ViewWindow has no dependency on WriteWindow). Link to spec.

Wait for review and merge.

---

# Step 4 — `parser/sparse/terminal.go`

**Goal:** `sparse.Terminal` — thin composition of `Store`, `WriteWindow`, `ViewWindow`. Exposes the API that `VTerm`'s main-screen path will call: `Grid()` projection, `WriteCell`, `Newline`, `Resize`, `ScrollUp`/`ScrollDown`, cursor queries.

**Prereqs:** Step 3 merged.

### Task 4.1: Skeleton + construction

**Files:**
- Create: `apps/texelterm/parser/sparse/terminal.go`
- Create: `apps/texelterm/parser/sparse/terminal_test.go`

- [ ] **Step 1: Write failing tests**

```go
// apps/texelterm/parser/sparse/terminal_test.go
package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestTerminal_NewInitialState(t *testing.T) {
	tm := NewTerminal(80, 24)
	if got := tm.Width(); got != 80 {
		t.Errorf("Width = %d, want 80", got)
	}
	if got := tm.Height(); got != 24 {
		t.Errorf("Height = %d, want 24", got)
	}
	if !tm.IsFollowing() {
		t.Error("new Terminal should follow writeBottom")
	}
	if got := tm.ContentEnd(); got != -1 {
		t.Errorf("fresh ContentEnd = %d, want -1", got)
	}
	_ = parser.Cell{}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestTerminal -v`
Expected: FAIL.

- [ ] **Step 3: Implement skeleton**

```go
// apps/texelterm/parser/sparse/terminal.go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import "github.com/framegrace/texelation/apps/texelterm/parser"

// Terminal is a thin composition of Store, WriteWindow, and ViewWindow. It
// exposes the API that VTerm's main-screen path calls into.
//
// Construction is eager: all three underlying types are created up-front so
// that no method has to lazy-init anything. This keeps the locking strategy
// simple — reads never upgrade to writes.
type Terminal struct {
	store *Store
	write *WriteWindow
	view  *ViewWindow
}

// NewTerminal creates a Terminal with the given dimensions. ViewWindow starts
// in autoFollow mode with viewBottom = height - 1.
func NewTerminal(width, height int) *Terminal {
	store := NewStore(width)
	write := NewWriteWindow(store, width, height)
	view := NewViewWindow(width, height)
	return &Terminal{store: store, write: write, view: view}
}

// Width returns the terminal width.
func (t *Terminal) Width() int { return t.write.Width() }

// Height returns the terminal height.
func (t *Terminal) Height() int { return t.write.Height() }

// IsFollowing reports whether the view is auto-following the write window.
func (t *Terminal) IsFollowing() bool { return t.view.IsFollowing() }

// ContentEnd returns the highest globalIdx ever written, or -1 if nothing
// has been written yet.
func (t *Terminal) ContentEnd() int64 { return t.store.Max() }
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestTerminal -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git checkout -b feat/sparse-terminal
git add apps/texelterm/parser/sparse/terminal.go apps/texelterm/parser/sparse/terminal_test.go
git commit -m "feat(sparse): add Terminal skeleton composing Store/Write/View"
```

### Task 4.2: Write methods wire through WriteWindow and notify ViewWindow

**Files:**
- Modify: `apps/texelterm/parser/sparse/terminal.go`
- Modify: `apps/texelterm/parser/sparse/terminal_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestTerminal_WriteCellAdvancesFollowingView(t *testing.T) {
	tm := NewTerminal(10, 5)
	tm.WriteCell(parser.Cell{Rune: 'h'})
	tm.Newline()
	// Cursor should be on row 1 now.
	gi, col := tm.Cursor()
	if gi != 1 || col != 0 {
		t.Errorf("after Newline, Cursor = (%d,%d), want (1,0)", gi, col)
	}
	// Because we're following, viewBottom should track writeBottom.
	_, vbottom := tm.VisibleRange()
	if vbottom != 4 {
		t.Errorf("viewBottom = %d, want 4 (writeBottom)", vbottom)
	}
}

func TestTerminal_NewlineAtBottomScrollsAndViewFollows(t *testing.T) {
	tm := NewTerminal(10, 3)
	tm.SetCursor(2, 0)
	tm.Newline()
	// writeTop advanced, writeBottom = 3, following view snaps.
	_, vbottom := tm.VisibleRange()
	if vbottom != 3 {
		t.Errorf("viewBottom = %d, want 3", vbottom)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement delegation**

Append to `terminal.go`:

```go
// WriteCell writes one cell and notifies the ViewWindow of any writeBottom
// change so auto-follow stays coherent.
func (t *Terminal) WriteCell(cell parser.Cell) {
	t.write.WriteCell(cell)
	t.view.OnWriteBottomChanged(t.write.WriteBottom())
}

// Newline advances the cursor (scrolling at bottom) and notifies the view.
func (t *Terminal) Newline() {
	t.write.Newline()
	t.view.OnWriteBottomChanged(t.write.WriteBottom())
}

// CarriageReturn resets cursor column to 0.
func (t *Terminal) CarriageReturn() { t.write.CarriageReturn() }

// SetCursor places the cursor at row, col (viewport-relative to writeTop).
func (t *Terminal) SetCursor(row, col int) { t.write.SetCursor(row, col) }

// Cursor returns the cursor (globalIdx, col) pair.
func (t *Terminal) Cursor() (globalIdx int64, col int) { return t.write.Cursor() }

// CursorRow returns the cursor row relative to writeTop.
func (t *Terminal) CursorRow() int { return t.write.CursorRow() }

// WriteTop returns the top globalIdx of the write window.
func (t *Terminal) WriteTop() int64 { return t.write.WriteTop() }

// WriteBottom returns the bottom globalIdx of the write window.
func (t *Terminal) WriteBottom() int64 { return t.write.WriteBottom() }

// VisibleRange returns the (top, bottom) globalIdx pair of the current view.
func (t *Terminal) VisibleRange() (top, bottom int64) { return t.view.VisibleRange() }
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/terminal.go apps/texelterm/parser/sparse/terminal_test.go
git commit -m "feat(sparse): Terminal write methods with autoFollow wiring"
```

### Task 4.3: Resize delegates to both windows

**Files:**
- Modify: `apps/texelterm/parser/sparse/terminal.go`
- Modify: `apps/texelterm/parser/sparse/terminal_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestTerminal_ResizeShrinkShellCase(t *testing.T) {
	tm := NewTerminal(80, 40)
	// Fill 40 rows.
	for i := 0; i < 40; i++ {
		tm.WriteCell(parser.Cell{Rune: 'X'})
		tm.Newline()
	}
	// cursor is now at row 40 of a scrolled window.
	tm.SetCursor(39, 0)
	tm.Resize(80, 20)

	_, vbottom := tm.VisibleRange()
	_, writeBottom := tm.WriteTop(), tm.WriteBottom()
	if vbottom != writeBottom {
		t.Errorf("following view: viewBottom = %d, writeBottom = %d", vbottom, writeBottom)
	}
	if got := tm.Height(); got != 20 {
		t.Errorf("Height = %d, want 20", got)
	}
}

func TestTerminal_ResizeFrozenViewStaysPut(t *testing.T) {
	tm := NewTerminal(80, 40)
	for i := 0; i < 80; i++ {
		tm.WriteCell(parser.Cell{Rune: 'X'})
		tm.Newline()
	}
	// Scroll back 20 rows.
	tm.ScrollUp(20)
	_, beforeBottom := tm.VisibleRange()

	tm.Resize(80, 30) // grow

	_, afterBottom := tm.VisibleRange()
	if afterBottom != beforeBottom {
		t.Errorf("frozen view moved: %d -> %d", beforeBottom, afterBottom)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement Resize and ScrollUp**

Append to `terminal.go`:

```go
// Resize resizes both the write and view windows. WriteWindow applies
// Rule 5 first; ViewWindow then applies Rule 6 observing the (possibly
// moved) writeBottom.
func (t *Terminal) Resize(newWidth, newHeight int) {
	t.write.Resize(newWidth, newHeight)
	t.view.Resize(newWidth, newHeight, t.write.WriteBottom())
}

// ScrollUp scrolls the view back by n lines and disengages autoFollow.
func (t *Terminal) ScrollUp(n int) { t.view.ScrollUp(n) }

// ScrollDown scrolls the view forward by n lines toward the live edge.
func (t *Terminal) ScrollDown(n int) { t.view.ScrollDown(n, t.write.WriteBottom()) }

// ScrollToBottom snaps the view to the live edge and re-engages autoFollow.
func (t *Terminal) ScrollToBottom() { t.view.ScrollToBottom(t.write.WriteBottom()) }

// OnInput re-engages autoFollow after a user keystroke or click.
func (t *Terminal) OnInput() { t.view.OnInput(t.write.WriteBottom()) }
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/terminal.go apps/texelterm/parser/sparse/terminal_test.go
git commit -m "feat(sparse): Terminal.Resize and scroll delegation"
```

### Task 4.4: Grid() projection

**Files:**
- Modify: `apps/texelterm/parser/sparse/terminal.go`
- Modify: `apps/texelterm/parser/sparse/terminal_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestTerminal_GridReturnsHeightXWidth(t *testing.T) {
	tm := NewTerminal(10, 5)
	tm.WriteCell(parser.Cell{Rune: 'A'})
	tm.WriteCell(parser.Cell{Rune: 'B'})

	grid := tm.Grid()
	if len(grid) != 5 {
		t.Fatalf("grid rows = %d, want 5", len(grid))
	}
	for y, row := range grid {
		if len(row) != 10 {
			t.Errorf("row %d width = %d, want 10", y, len(row))
		}
	}
	if grid[0][0].Rune != 'A' {
		t.Errorf("grid[0][0] = %q, want A", grid[0][0].Rune)
	}
	if grid[0][1].Rune != 'B' {
		t.Errorf("grid[0][1] = %q, want B", grid[0][1].Rune)
	}
	// Unwritten cells are blank.
	if grid[0][5].Rune != 0 {
		t.Errorf("grid[0][5] = %q, want blank", grid[0][5].Rune)
	}
	if grid[4][0].Rune != 0 {
		t.Errorf("grid[4][0] = %q, want blank (unwritten row)", grid[4][0].Rune)
	}
}

func TestTerminal_GridReflectsScrollback(t *testing.T) {
	tm := NewTerminal(10, 3)
	// Fill rows 0,1,2 then scroll down — writeTop=1, writeBottom=3.
	tm.WriteCell(parser.Cell{Rune: 'A'})
	tm.Newline()
	tm.WriteCell(parser.Cell{Rune: 'B'})
	tm.Newline()
	tm.WriteCell(parser.Cell{Rune: 'C'})
	tm.Newline() // scrolls
	// Following view: viewBottom = 3, view covers [1,2,3]
	grid := tm.Grid()
	if grid[0][0].Rune != 'B' {
		t.Errorf("grid[0][0] = %q, want B (row 1)", grid[0][0].Rune)
	}
	if grid[1][0].Rune != 'C' {
		t.Errorf("grid[1][0] = %q, want C (row 2)", grid[1][0].Rune)
	}
	if grid[2][0].Rune != 0 {
		t.Errorf("grid[2][0] = %q, want blank (row 3, unwritten)", grid[2][0].Rune)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement Grid()**

Append to `terminal.go`:

```go
// Grid builds a dense height x width grid from the current view range by
// reading the Store. Unwritten cells and short lines are blank-padded.
//
// The returned slice is owned by the caller and safe to mutate.
func (t *Terminal) Grid() [][]parser.Cell {
	width := t.write.Width()
	height := t.write.Height()
	top, _ := t.view.VisibleRange()

	grid := make([][]parser.Cell, height)
	for y := 0; y < height; y++ {
		row := make([]parser.Cell, width)
		gi := top + int64(y)
		if gi >= 0 {
			line := t.store.GetLine(gi)
			for x := 0; x < width && x < len(line); x++ {
				row[x] = line[x]
			}
		}
		grid[y] = row
	}
	return grid
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/terminal.go apps/texelterm/parser/sparse/terminal_test.go
git commit -m "feat(sparse): Terminal.Grid() dense projection from view range"
```

### Task 4.5: End-to-end claude shrink smoke test (unit-level)

**Files:**
- Modify: `apps/texelterm/parser/sparse/terminal_test.go`

- [ ] **Step 1: Write the regression test**

This is the heart of the redesign — verify at the unit level that a simulated TUI drag does not duplicate textual content.

```go
// TestTerminal_ShrinkDragDoesNotDuplicateTextContent simulates claude-like
// behavior: each shrink step, "redraw" the UI at the new size with a text
// marker on row 1. Afterward, verify the text marker appears exactly once in
// the store.
func TestTerminal_ShrinkDragDoesNotDuplicateTextContent(t *testing.T) {
	tm := NewTerminal(80, 40)
	marker := "Claude Code"

	// Initial draw: border on row 0, marker on row 1, cursor parked at
	// row 37 (input box bottom).
	drawUI := func(h int) {
		// Border row 0.
		tm.SetCursor(0, 0)
		for _, r := range "┌──────────────┐" {
			tm.WriteCell(parser.Cell{Rune: r})
		}
		// Text row 1.
		tm.SetCursor(1, 0)
		for _, r := range marker {
			tm.WriteCell(parser.Cell{Rune: r})
		}
		// Cursor parked at last-row (input box).
		tm.SetCursor(h-2, 5)
	}

	drawUI(40)

	// Shrink-drag from 40 -> 20.
	for h := 39; h >= 20; h-- {
		tm.Resize(80, h)
		// Clear the old window and redraw at new size.
		// (In real life, the TUI does this via ESC[2J or scroll region.)
		top := tm.WriteTop()
		bottom := tm.WriteBottom()
		tm.EraseDisplay() // new helper — see below
		_ = top
		_ = bottom
		drawUI(h)
	}

	// Count occurrences of the marker across the entire store, from globalIdx 0
	// up to ContentEnd.
	count := 0
	end := tm.ContentEnd()
	for gi := int64(0); gi <= end; gi++ {
		line := tm.ReadLine(gi)
		if containsRunes(line, []rune(marker)) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("marker %q appears %d times in store; want 1", marker, count)
	}
}

// containsRunes reports whether row contains the full sequence needle as a
// contiguous run of Rune fields.
func containsRunes(row []parser.Cell, needle []rune) bool {
	if len(needle) == 0 || len(row) < len(needle) {
		return false
	}
	for start := 0; start+len(needle) <= len(row); start++ {
		match := true
		for j, r := range needle {
			if row[start+j].Rune != r {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run, verify fail (`EraseDisplay` and `ReadLine` don't exist on Terminal yet)**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestTerminal_ShrinkDragDoesNotDuplicate -v`
Expected: FAIL with "undefined: EraseDisplay" or similar.

- [ ] **Step 3: Add the three helper methods**

Append to `terminal.go`:

```go
// EraseDisplay clears every cell in the current write window. This is
// the sparse equivalent of ESC[2J on the main screen.
func (t *Terminal) EraseDisplay() {
	t.write.EraseDisplay()
}

// EraseLine clears the cells of the line at the cursor's current globalIdx.
// This is the sparse equivalent of ESC[2K.
func (t *Terminal) EraseLine() {
	t.write.EraseLine()
}

// ReadLine returns a copy of the cells at globalIdx. Returns nil for gaps.
func (t *Terminal) ReadLine(globalIdx int64) []parser.Cell {
	return t.store.GetLine(globalIdx)
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestTerminal_ShrinkDragDoesNotDuplicate -v`
Expected: PASS.

Note: the test proves that under the revised Rule 5, the cursor-minimum-advance leaves at most bounded "row 0 border" smearing in scrollback, and the text-content marker on row 1 appears exactly once. This is the unit-level analogue of the `TestClaudeCodeShrinkDragPollutesScrollback` success criterion.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/terminal.go apps/texelterm/parser/sparse/terminal_test.go
git commit -m "test(sparse): claude-like shrink drag does not duplicate text content"
```

### Task 4.6: Push branch and open PR

- [ ] **Step 1: Full test run**

Run: `go test -race ./apps/texelterm/parser/sparse/...`
Expected: PASS.

- [ ] **Step 2: Push**

```bash
git push -u origin feat/sparse-terminal
```

- [ ] **Step 3: Open PR**

Title: `feat(sparse): add Terminal composition + shrink-drag regression test`
Body: Link to design spec + call out the `TestTerminal_ShrinkDragDoesNotDuplicateTextContent` test as the unit-level proof that the model fixes the target bug class.

Wait for merge.

---

# Step 5 — `parser/sparse/persistence.go`

**Goal:** An adapter that flushes `sparse.Store` writes to the existing `PageStore` using matching globalIdxs, and saves/restores `(contentEnd, cursor, writeTop)` metadata via a new `MainScreenState` struct on the WAL. Replaces the old `LiveEdgeBase`-based `ViewportState`.

**Prereqs:** Step 4 merged.

### Task 5.1: Introduce MainScreenState alongside ViewportState

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`
- Create: `apps/texelterm/parser/page_store_main_screen_state_test.go`

- [ ] **Step 1: Write failing test**

```go
// apps/texelterm/parser/page_store_main_screen_state_test.go
package parser

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMainScreenState_JSONRoundtrip(t *testing.T) {
	s := MainScreenState{
		WriteTop:        100,
		ContentEnd:      150,
		CursorGlobalIdx: 145,
		CursorCol:       5,
		PromptStartLine: 140,
		WorkingDir:      "/home/user",
		SavedAt:         time.Unix(1700000000, 0).UTC(),
	}
	b, err := json.Marshal(&s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got MainScreenState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != s {
		t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v", got, s)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/ -run TestMainScreenState_JSONRoundtrip -v`
Expected: FAIL with "undefined: MainScreenState".

- [ ] **Step 3: Add MainScreenState type next to ViewportState**

In `apps/texelterm/parser/page_store.go`, append after the `ViewportState` struct (line 59):

```go
// MainScreenState stores the sparse-model main-screen state for session
// restoration. Replaces ViewportState when the sparse redesign is active.
// On load, if only a legacy ViewportState is present, it is discarded and
// the terminal starts fresh (there is no clean translation between the two
// models).
type MainScreenState struct {
	// WriteTop is the globalIdx of the top row of the write window.
	WriteTop int64 `json:"write_top"`

	// ContentEnd is the highest globalIdx ever written. -1 means empty.
	ContentEnd int64 `json:"content_end"`

	// CursorGlobalIdx is the absolute globalIdx where the cursor currently sits.
	CursorGlobalIdx int64 `json:"cursor_global_idx"`

	// CursorCol is the cursor column position (0-indexed).
	CursorCol int `json:"cursor_col"`

	// PromptStartLine is the global line index of the last shell prompt start.
	// -1 means unknown.
	PromptStartLine int64 `json:"prompt_start_line"`

	// WorkingDir is the last known working directory from OSC 7.
	WorkingDir string `json:"working_dir"`

	// SavedAt is when the state was saved.
	SavedAt time.Time `json:"saved_at"`
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/ -run TestMainScreenState -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git checkout -b feat/sparse-persistence
git add apps/texelterm/parser/page_store.go apps/texelterm/parser/page_store_main_screen_state_test.go
git commit -m "feat(parser): add MainScreenState struct alongside ViewportState"
```

### Task 5.2: Persistence adapter — write path

**Files:**
- Create: `apps/texelterm/parser/sparse/persistence.go`
- Create: `apps/texelterm/parser/sparse/persistence_test.go`

- [ ] **Step 1: Write failing test**

```go
// apps/texelterm/parser/sparse/persistence_test.go
package sparse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestPersistence_FlushLinesToPageStore(t *testing.T) {
	dir := t.TempDir()
	cfg := parser.DefaultPageStoreConfig(dir, "unit-test")
	ps, err := parser.NewPageStore(cfg)
	if err != nil {
		t.Fatalf("NewPageStore: %v", err)
	}
	defer ps.Close()

	adapter := NewPersistence(ps)

	// Write three lines to the sparse side.
	store := NewStore(10)
	store.SetLine(0, []parser.Cell{{Rune: 'a'}})
	store.SetLine(1, []parser.Cell{{Rune: 'b'}})
	store.SetLine(2, []parser.Cell{{Rune: 'c'}})

	if err := adapter.FlushLines(store, []int64{0, 1, 2}); err != nil {
		t.Fatalf("FlushLines: %v", err)
	}
	if err := ps.Flush(); err != nil {
		t.Fatalf("ps.Flush: %v", err)
	}

	// Read back through PageStore.
	line, err := ps.ReadLine(1)
	if err != nil {
		t.Fatalf("ReadLine(1): %v", err)
	}
	if len(line.Cells) == 0 || line.Cells[0].Rune != 'b' {
		t.Errorf("ReadLine(1) first rune = %q, want b", line.Cells[0].Rune)
	}

	// Ensure the temp dir was actually written to.
	if _, err := os.Stat(filepath.Join(dir, "terminals")); err != nil {
		t.Errorf("expected terminal dir under %s: %v", dir, err)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestPersistence -v`
Expected: FAIL with "undefined: NewPersistence" or similar.

- [ ] **Step 3: Implement adapter**

```go
// apps/texelterm/parser/sparse/persistence.go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// Persistence adapts a sparse.Store / sparse.Terminal to the existing
// PageStore on-disk layer. The same globalIdx is used on both sides, so a
// line in sparse.Store at globalIdx 42 is persisted at PageStore globalIdx 42.
//
// This is a thin forward-only adapter: it does not own lifecycle, it does not
// manage flush scheduling. Those concerns stay in AdaptivePersistence. The
// adapter only knows how to take a list of "dirty" globalIdxs and push them
// to PageStore, and how to save/load MainScreenState.
type Persistence struct {
	page *parser.PageStore
}

// NewPersistence creates a new adapter writing to the given PageStore.
func NewPersistence(ps *parser.PageStore) *Persistence {
	return &Persistence{page: ps}
}

// FlushLines forwards each listed globalIdx's current content in the Store
// to the PageStore. Missing lines (gaps) are skipped. Lines that already
// exist in PageStore are updated via UpdateLine; new lines are appended via
// AppendLineWithGlobalIdx.
func (p *Persistence) FlushLines(store *Store, globalIdxs []int64) error {
	now := time.Now()
	for _, gi := range globalIdxs {
		cells := store.GetLine(gi)
		if cells == nil {
			continue
		}
		line := &parser.LogicalLine{Cells: cells}
		if p.page.HasLine(gi) {
			if err := p.page.UpdateLine(gi, line, now); err != nil {
				return err
			}
		} else {
			if err := p.page.AppendLineWithGlobalIdx(gi, line, now); err != nil {
				return err
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestPersistence -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/persistence.go apps/texelterm/parser/sparse/persistence_test.go
git commit -m "feat(sparse): Persistence.FlushLines writes through to PageStore"
```

### Task 5.3: Persistence adapter — metadata save/load

**Files:**
- Modify: `apps/texelterm/parser/sparse/persistence.go`
- Modify: `apps/texelterm/parser/sparse/persistence_test.go`

- [ ] **Step 1: Write failing test**

Note: we do not yet have a place to put MainScreenState on disk — that comes in Task 5.4 when we wire it through the WAL. For this task, test the in-memory helpers that produce and consume a `MainScreenState` from a `sparse.Terminal`.

```go
func TestPersistence_SnapshotTerminal(t *testing.T) {
	tm := NewTerminal(80, 24)
	tm.WriteCell(parser.Cell{Rune: 'a'})
	tm.Newline()
	tm.WriteCell(parser.Cell{Rune: 'b'})

	state := SnapshotState(tm)
	if state.WriteTop != 0 {
		t.Errorf("WriteTop = %d, want 0", state.WriteTop)
	}
	if state.ContentEnd != 1 {
		t.Errorf("ContentEnd = %d, want 1 (two rows written)", state.ContentEnd)
	}
	if state.CursorGlobalIdx != 1 || state.CursorCol != 1 {
		t.Errorf("Cursor = (%d,%d), want (1,1)",
			state.CursorGlobalIdx, state.CursorCol)
	}
}

func TestPersistence_RestoreTerminal(t *testing.T) {
	state := parser.MainScreenState{
		WriteTop:        50,
		ContentEnd:      70,
		CursorGlobalIdx: 65,
		CursorCol:       3,
	}
	tm := NewTerminal(80, 24)
	RestoreState(tm, state)

	if got := tm.WriteTop(); got != 50 {
		t.Errorf("restored WriteTop = %d, want 50", got)
	}
	gi, col := tm.Cursor()
	if gi != 65 || col != 3 {
		t.Errorf("restored Cursor = (%d,%d), want (65,3)", gi, col)
	}
	if !tm.IsFollowing() {
		t.Error("restored Terminal should be in autoFollow mode by default")
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestPersistence_SnapshotTerminal -v`
Expected: FAIL.

- [ ] **Step 3: Implement snapshot/restore and add WriteWindow setter**

First, `WriteWindow` needs a `RestoreState` helper. Append to `write_window.go`:

```go
// RestoreState forcibly sets writeTop and cursor, used during session
// restore. Do not call during normal operation.
func (w *WriteWindow) RestoreState(writeTop, cursorGlobalIdx int64, cursorCol int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeTop = writeTop
	w.cursorGlobalIdx = cursorGlobalIdx
	w.cursorCol = cursorCol
}
```

Then `Terminal` needs a pass-through. Append to `terminal.go`:

```go
// RestoreWriteState forcibly sets the write window's cursor and anchor,
// used during session restore. The ViewWindow is re-snapped to the new
// writeBottom in follow mode.
func (t *Terminal) RestoreWriteState(writeTop, cursorGlobalIdx int64, cursorCol int) {
	t.write.RestoreState(writeTop, cursorGlobalIdx, cursorCol)
	t.view.ScrollToBottom(t.write.WriteBottom())
}
```

Finally the persistence helpers. Append to `persistence.go`:

```go
// SnapshotState captures the current Terminal state into a MainScreenState
// suitable for WAL persistence.
func SnapshotState(tm *Terminal) parser.MainScreenState {
	gi, col := tm.Cursor()
	return parser.MainScreenState{
		WriteTop:        tm.WriteTop(),
		ContentEnd:      tm.ContentEnd(),
		CursorGlobalIdx: gi,
		CursorCol:       col,
		PromptStartLine: -1,
		SavedAt:         time.Now(),
	}
}

// RestoreState applies a MainScreenState to an existing Terminal, overwriting
// cursor and writeTop. The ViewWindow is put into autoFollow mode snapped to
// the new writeBottom.
func RestoreState(tm *Terminal, state parser.MainScreenState) {
	tm.RestoreWriteState(state.WriteTop, state.CursorGlobalIdx, state.CursorCol)
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/persistence.go apps/texelterm/parser/sparse/persistence_test.go apps/texelterm/parser/sparse/write_window.go apps/texelterm/parser/sparse/terminal.go
git commit -m "feat(sparse): SnapshotState/RestoreState for session restore"
```

### Task 5.4: Round-trip save and reload through PageStore

**Files:**
- Modify: `apps/texelterm/parser/sparse/persistence.go`
- Modify: `apps/texelterm/parser/sparse/persistence_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestPersistence_RoundTripViaPageStore(t *testing.T) {
	dir := t.TempDir()
	cfg := parser.DefaultPageStoreConfig(dir, "unit-test")
	ps1, err := parser.NewPageStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	adapter1 := NewPersistence(ps1)

	// Build a terminal, write some content, flush all lines.
	tm := NewTerminal(10, 5)
	tm.WriteCell(parser.Cell{Rune: 'x'})
	tm.Newline()
	tm.WriteCell(parser.Cell{Rune: 'y'})
	tm.Newline()

	idxs := []int64{0, 1}
	if err := adapter1.FlushLines(getStore(tm), idxs); err != nil {
		t.Fatalf("FlushLines: %v", err)
	}
	if err := ps1.Flush(); err != nil {
		t.Fatal(err)
	}
	ps1.Close()

	// Reload into a fresh Terminal.
	ps2, err := parser.NewPageStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ps2.Close()

	tm2 := NewTerminal(10, 5)
	if err := LoadStore(getStore(tm2), ps2); err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	if got := getStore(tm2).Get(0, 0).Rune; got != 'x' {
		t.Errorf("reloaded store[0][0] = %q, want x", got)
	}
	if got := getStore(tm2).Get(1, 0).Rune; got != 'y' {
		t.Errorf("reloaded store[1][0] = %q, want y", got)
	}
}
```

This test requires a `getStore` test helper that exposes the internal `*Store` from a `Terminal` (since the field is lowercase). Add it to `terminal_test.go`:

```go
// getStore is a test-only accessor for the internal Store.
func getStore(t *Terminal) *Store { return t.store }
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestPersistence_RoundTrip -v`
Expected: FAIL with "undefined: LoadStore".

- [ ] **Step 3: Implement LoadStore**

Append to `persistence.go`:

```go
// LoadStore reads every line currently present in the PageStore into the
// given sparse.Store. Used on startup to rebuild the in-memory state from
// disk. Existing entries in the Store are overwritten when their globalIdx
// matches; unrelated entries are untouched.
func LoadStore(store *Store, ps *parser.PageStore) error {
	count := ps.LineCount()
	for gi := int64(0); gi < count; gi++ {
		if !ps.HasLine(gi) {
			continue
		}
		line, err := ps.ReadLine(gi)
		if err != nil {
			return err
		}
		if line == nil {
			continue
		}
		store.SetLine(gi, line.Cells)
	}
	return nil
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./apps/texelterm/parser/sparse/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/persistence.go apps/texelterm/parser/sparse/persistence_test.go apps/texelterm/parser/sparse/terminal_test.go
git commit -m "feat(sparse): LoadStore repopulates sparse.Store from PageStore"
```

### Task 5.5: Push branch and open PR

- [ ] **Step 1: Full test run**

Run: `go test -race ./apps/texelterm/parser/...`
Expected: PASS (both parser/ and parser/sparse/).

- [ ] **Step 2: Push**

```bash
git push -u origin feat/sparse-persistence
```

- [ ] **Step 3: Open PR**

Title: `feat(sparse): Persistence adapter + MainScreenState`
Body: Note that `MainScreenState` coexists with `ViewportState` until the integration PR; at integration time the old struct becomes dead code and is removed. Link to spec.

Wait for merge.

---

# Step 6 — Integration PR (the cutover)

**Goal:** Replace `VTerm`'s main-screen path with `sparse.Terminal`. Delete `MemoryBuffer`, all `pendingRollback*` fields, `suppressNextScrollbackPush`, `hasPartialScrollRegion`, `restoredFromDisk`. Update `claude_code_shrink_test.go` to the new assertions. The whole thing lands in one branch as one PR.

**Prereqs:** Steps 1–5 merged to main.

> ⚠️ This is the risk step. It touches `vterm.go`, `vterm_memory_buffer.go`, `adaptive_persistence.go`, many tests. Do each task in order — the tests in each task must pass before moving to the next.

### Task 6.1: Wire sparse.Terminal into VTerm alongside MemoryBuffer

**Files:**
- Modify: `apps/texelterm/parser/vterm.go`

Goal: add `mainScreen *sparse.Terminal` as a new field, initialized in parallel with `memBufState`. Both systems run side-by-side for this task — we do not switch yet. This establishes the compilation path and lets us move consumers one at a time.

- [ ] **Step 1: Add the field and initialization**

In `apps/texelterm/parser/vterm.go`, add to the `VTerm` struct (near line 33, after `memBufState`):

```go
// mainScreen is the sparse main-screen terminal. During the transition from
// the MemoryBuffer model, it runs in parallel and is gradually moved in
// front of memBufState. After integration, memBufState and everything
// related is deleted.
mainScreen *sparse.Terminal
```

Add the import at the top of the file:

```go
"github.com/framegrace/texelation/apps/texelterm/parser/sparse"
```

In `NewVTerm` (or wherever VTerm is constructed — find with Grep), add after the existing `memBufState` init:

```go
v.mainScreen = sparse.NewTerminal(width, height)
```

- [ ] **Step 2: Build and run existing test suite**

Run: `go build ./apps/texelterm/...`
Expected: builds cleanly (no behavioral change yet).

Run: `go test ./apps/texelterm/parser/...`
Expected: PASS (existing tests unaffected).

- [ ] **Step 3: Commit**

```bash
git checkout -b feat/sparse-integration
git add apps/texelterm/parser/vterm.go
git commit -m "refactor(vterm): add sparse.Terminal field alongside memBufState"
```

### Task 6.2: Dual-write path — writes land in both MemoryBuffer and sparse.Terminal

**Files:**
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go`

Goal: every time `memoryBufferPlaceChar`, `memoryBufferLineFeed`, `memoryBufferCarriageReturn`, `memoryBufferEraseScreen`, `memoryBufferEraseLine`, etc. runs, also invoke the corresponding `sparse.Terminal` method. This gives us an in-process consistency check.

- [ ] **Step 1: Find all the `memoryBuffer*` entry points**

Run: `grep -n "func (v \*VTerm) memoryBuffer" apps/texelterm/parser/vterm_memory_buffer.go`

Expected output: the list from the reference map (memoryBufferPlaceChar @ 700, memoryBufferLineFeed @ 760, etc.).

- [ ] **Step 2: Add shim calls to each write method**

For each write-type memory-buffer method, add a call to the sparse equivalent at the top:

```go
func (v *VTerm) memoryBufferPlaceChar(r rune) {
	if v.mainScreen != nil {
		cell := parser.Cell{
			Rune: r,
			FG:   v.currentFG,
			BG:   v.currentBG,
			Attr: v.currentAttr,
		}
		v.mainScreen.WriteCell(cell)
	}
	// ... existing body ...
}
```

Repeat for:
- `memoryBufferLineFeed` → `v.mainScreen.Newline()`
- `memoryBufferCarriageReturn` → `v.mainScreen.CarriageReturn()`
- `memoryBufferEraseScreen` → when `mode == 2`, `v.mainScreen.EraseDisplay()`
- `memoryBufferEraseLine` → `v.mainScreen.EraseLine()`
- `memoryBufferResize` → `v.mainScreen.Resize(width, height)`

Leave reads alone — the consumer is still `memoryBufferGrid`.

- [ ] **Step 3: Build and run full suite**

Run: `go build ./apps/texelterm/...`
Run: `go test ./apps/texelterm/parser/...`
Expected: PASS — dual-writing does not change visible behavior.

- [ ] **Step 4: Commit**

```bash
git add apps/texelterm/parser/vterm_memory_buffer.go
git commit -m "refactor(vterm): dual-write to sparse.Terminal for transition"
```

### Task 6.3: Parity test — MemoryBuffer grid == sparse.Terminal grid

**Files:**
- Create: `apps/texelterm/parser/vterm_sparse_parity_test.go`

- [ ] **Step 1: Write failing parity test**

```go
// apps/texelterm/parser/vterm_sparse_parity_test.go
package parser

import (
	"testing"
)

// TestVTerm_SparseParityOnBasicWrites verifies that during the integration
// window the legacy memoryBufferGrid() and the new sparse.Terminal.Grid()
// produce the same output for simple writes.
func TestVTerm_SparseParityOnBasicWrites(t *testing.T) {
	v := NewVTerm(20, 5)
	v.EnableMemoryBuffer()

	p := NewParser(v)
	for _, r := range "hello\nworld" {
		p.Parse(r)
	}

	legacyGrid := v.memoryBufferGrid()
	sparseGrid := v.mainScreen.Grid()

	if len(legacyGrid) != len(sparseGrid) {
		t.Fatalf("row count mismatch: legacy=%d sparse=%d",
			len(legacyGrid), len(sparseGrid))
	}
	for y := range legacyGrid {
		if len(legacyGrid[y]) != len(sparseGrid[y]) {
			t.Errorf("row %d width mismatch: legacy=%d sparse=%d",
				y, len(legacyGrid[y]), len(sparseGrid[y]))
			continue
		}
		for x := range legacyGrid[y] {
			if legacyGrid[y][x].Rune != sparseGrid[y][x].Rune {
				t.Errorf("cell (%d,%d): legacy=%q sparse=%q",
					x, y, legacyGrid[y][x].Rune, sparseGrid[y][x].Rune)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./apps/texelterm/parser/ -run TestVTerm_SparseParityOnBasicWrites -v`
Expected: May PASS or FAIL depending on whether the dual-write from Task 6.2 is complete. If it fails, the failure reveals which write paths still aren't wired up. Iterate on Task 6.2 until it passes.

- [ ] **Step 3: If it fails, fix the missing write-path shim in Task 6.2 and re-run**

Keep iterating until the parity test passes.

- [ ] **Step 4: Commit**

```bash
git add apps/texelterm/parser/vterm_sparse_parity_test.go
git commit -m "test(vterm): parity test between memBufGrid and sparse.Grid"
```

### Task 6.4: Flip `memoryBufferGrid` to return sparse grid

**Files:**
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go`

- [ ] **Step 1: Change the return path**

Locate `memoryBufferGrid` (line 883 of `vterm_memory_buffer.go`) and replace its body with:

```go
func (v *VTerm) memoryBufferGrid() [][]Cell {
	if v.mainScreen == nil {
		return v.memBufLegacyGrid() // keep legacy path available as fallback
	}
	return v.mainScreen.Grid()
}
```

Rename the old body to `memBufLegacyGrid()` so it stays available for debugging.

- [ ] **Step 2: Run the full suite**

Run: `go test ./apps/texelterm/parser/...`
Expected: PASS. If not, the dual-write path is missing a case — go fix it in Task 6.2.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/vterm_memory_buffer.go
git commit -m "refactor(vterm): memoryBufferGrid returns sparse.Terminal.Grid()"
```

### Task 6.5: Delete pendingRollback fields and their call sites

**Files:**
- Modify: `apps/texelterm/parser/vterm.go`
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go`

- [ ] **Step 1: Remove struct fields**

In `vterm.go` (lines 44-49, 56), delete:

```go
pendingRollbackActive        bool
pendingRollbackPreEdge       int64
pendingRollbackPreGlobalEnd  int64
pendingRollbackSavedBase     int64
pendingRollbackSavedLines    []*LogicalLine
suppressNextScrollbackPush   bool
hasPartialScrollRegion       bool
restoredFromDisk             bool
```

- [ ] **Step 2: Delete every reference**

Run `go build ./apps/texelterm/parser/` — the compiler will emit errors for every remaining reference. Delete the call sites, not work around them. Specifically, delete:

- `memoryBufferPushViewportToScrollback` (line 1826, entire function body)
- The rollback branches in `SetMargins` (grep for `pendingRollbackActive` in vterm.go)
- The cursor-clamp scrollback-advance in `clampCursorToHeight` (grep for `pendingRollback` in that function)
- The resize-time rollback block in `memoryBufferResize`
- The push-on-clear case in `memoryBufferEraseScreen` (`mode == 2` branch, the part that pushes viewport rows to scrollback — keep the clear itself)

- [ ] **Step 3: Build, then test**

Run: `go build ./apps/texelterm/parser/`
Expected: builds cleanly.

Run: `go test ./apps/texelterm/parser/...`
Expected: some tests may fail — specifically those that asserted on `pendingRollbackActive` or `memoryBufferPushViewportToScrollback`. Those tests are obsolete; delete them.

- [ ] **Step 4: Verify delete-list invariants**

Run: `grep -r pendingRollback apps/texelterm/parser/`
Expected: no matches.

Run: `grep -r suppressNextScrollback apps/texelterm/parser/`
Expected: no matches.

- [ ] **Step 5: Commit**

```bash
git add -u apps/texelterm/parser/
git commit -m "refactor(vterm): delete pendingRollback/suppressScrollback/partialScrollRegion fields"
```

### Task 6.6: Delete MemoryBuffer dense ring

**Files:**
- Delete: `apps/texelterm/parser/memory_buffer.go`
- Delete: `apps/texelterm/parser/memory_buffer_test.go`
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go`
- Modify: `apps/texelterm/parser/vterm.go`

- [ ] **Step 1: Confirm nothing outside parser still imports MemoryBuffer**

Run: `grep -rn "parser.MemoryBuffer\|parser\.NewMemoryBuffer" apps/ internal/ client/`
Expected: only ViewportWindow references, which we'll rename in step 7.

If the grep turns up unexpected call sites, update them to use `sparse.Terminal` or add to the step 7 cleanup list. **Do not proceed until the grep is empty except for ViewportWindow.**

- [ ] **Step 2: Delete memory_buffer.go and its test**

```bash
git rm apps/texelterm/parser/memory_buffer.go apps/texelterm/parser/memory_buffer_test.go
```

- [ ] **Step 3: Remove `memBufState` field and all references**

In `vterm.go`, delete:
```go
memBufState *memoryBufferState
```

Run `go build ./apps/texelterm/parser/` and fix every compile error by either:
- Deleting the call site if it was pendingRollback-adjacent
- Replacing `v.memBufState.X` with the equivalent `v.mainScreen.Y` call

In `vterm_memory_buffer.go`, the whole file should now be a thin delegation layer: each `memoryBuffer*` method either calls into `v.mainScreen` or is deleted.

- [ ] **Step 4: Run the suite**

Run: `go test ./apps/texelterm/parser/...`
Expected: PASS. Any failing tests are either (a) asserting on deleted internals — delete them — or (b) asserting on old-model semantics that the new model does differently — flip the assertion to match the new semantics, using the spec as the source of truth.

- [ ] **Step 5: Commit**

```bash
git add -u apps/texelterm/parser/
git commit -m "refactor(vterm): delete MemoryBuffer dense ring, route all main-screen through sparse.Terminal"
```

### Task 6.7: Update AdaptivePersistence to use MainScreenState

**Files:**
- Modify: `apps/texelterm/parser/adaptive_persistence.go`

- [ ] **Step 1: Replace `pendingMetadata *ViewportState` with `pendingMetadata *MainScreenState`**

Find the field in `adaptive_persistence.go` (around line 115) and change:

```go
pendingMetadata *MainScreenState
```

Update every call to `NotifyMetadataChange(state *ViewportState)` — rename the parameter type to `*MainScreenState`. Update the metadata write path (`flushPendingLocked` and its helpers) to JSON-encode `MainScreenState` instead.

- [ ] **Step 2: Update callers in VTerm**

VTerm currently calls `persistence.NotifyMetadataChange(&ViewportState{...})`. Find these call sites:

```bash
grep -n NotifyMetadataChange apps/texelterm/parser/vterm*.go
```

Replace each with a `MainScreenState` built via `sparse.SnapshotState(v.mainScreen)`.

- [ ] **Step 3: Drop the legacy `ViewportState` reader**

On load, `AdaptivePersistence` reads a `ViewportState` from the WAL and applies it. Replace that with reading `MainScreenState` and calling `sparse.RestoreState(v.mainScreen, state)`. Delete the legacy `ViewportState` read path.

- [ ] **Step 4: Build and test**

Run: `go build ./apps/texelterm/...`
Run: `go test ./apps/texelterm/parser/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -u apps/texelterm/parser/
git commit -m "refactor(persist): use MainScreenState for main-screen WAL metadata"
```

### Task 6.8: Delete ViewportState

**Files:**
- Modify: `apps/texelterm/parser/page_store.go`

- [ ] **Step 1: Delete the struct**

In `page_store.go` (lines 35-59), delete the `ViewportState` struct entirely.

- [ ] **Step 2: Build**

Run: `go build ./apps/texelterm/...`
Expected: PASS. Any remaining references to `ViewportState` are dead code — delete them.

- [ ] **Step 3: Commit**

```bash
git add -u apps/texelterm/parser/page_store.go
git commit -m "refactor(parser): delete ViewportState (superseded by MainScreenState)"
```

### Task 6.9: Update `TestClaudeCodeShrinkDragPollutesScrollback`

**Files:**
- Modify: `apps/texelterm/testutil/claude_code_shrink_test.go`

- [ ] **Step 1: Update assertions to spec success criteria**

Read the current test. The old assertions (`totalClaudeMarks > 1`, `finalSpan > viewportHeight+2`) used the legacy model's concepts (`GlobalEnd`, `LiveEdgeBase`). Replace them with the spec's success criterion: `"Claude Code"` text marker must appear exactly once across scrollback + viewport.

Replace the Heuristic 1 block (the marker count section) with:

```go
// Heuristic 1: the textual "Claude Code" marker should appear exactly once
// across scrollback + viewport, regardless of banner-border smearing under
// cursor-minimum-advance.
const claudeTextMarker = "Claude Code"
markerCount := 0
for _, line := range scrollbackLines {
	if strings.Contains(logicalLineToString(line), claudeTextMarker) {
		markerCount++
	}
}
for _, row := range finalLines {
	if strings.Contains(row, claudeTextMarker) {
		markerCount++
	}
}
if markerCount != 1 {
	t.Errorf("BUG REPRO: %q appears %d times across scrollback+viewport (expected 1)",
		claudeTextMarker, markerCount)
} else {
	t.Logf("pollution check OK: %q appears exactly once", claudeTextMarker)
}
```

Delete Heuristic 1b entirely (the `finalSpan > viewport + 2` check) — under the sparse model there is no equivalent; "span" is not a defined concept.

Keep Heuristic 2 (marker exists in final grid) and Heuristic 3 (prompt ❯ below marker) as-is — they are model-agnostic.

Replace `vt.GlobalEnd()`, `vt.LiveEdgeBase()`, `vt.GlobalOffset()`, `vt.MemoryBuffer()` with their sparse equivalents: `vt.ContentEnd()`, `vt.WriteTop()`, ... (VTerm should expose these as thin pass-throughs to `v.mainScreen`; add them if missing).

- [ ] **Step 2: Run the test**

Run: `go test -run TestClaudeCodeShrinkDragPollutesScrollback -timeout 120s -v ./apps/texelterm/testutil/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/testutil/claude_code_shrink_test.go
git commit -m "test(shrink): update claude-drag test to sparse-model assertions"
```

### Task 6.10: Audit remaining parser tests for old-model assumptions

**Files:** (varies)
- Audit: `apps/texelterm/parser/*_test.go`, `apps/texelterm/testutil/*_test.go`

- [ ] **Step 1: List files to audit**

Run: `grep -rln "pendingRollback\|suppressNextScrollbackPush\|GlobalEnd\|LiveEdgeBase\|PushViewportToScrollback\|hasPartialScrollRegion" apps/texelterm/`

- [ ] **Step 2: For each matching file, choose one of three actions**

1. **Delete the test** if it asserted on now-gone internals that had no user-facing meaning (e.g., "pendingRollbackActive is true after SetMargins partial region").
2. **Flip the assertion** if it encoded a behavior the old model produced that the new model correctly does differently (e.g., "push to scrollback on ESC[2J" — now the clear just clears, no push).
3. **Rename fields** if it just uses old API names that now need sparse equivalents (e.g., `GlobalEnd()` → `ContentEnd()`).

Keep a running list of files touched. Each file gets its own commit.

- [ ] **Step 3: Run the full suite after each file**

Run: `go test ./apps/texelterm/...` after each file change.
Expected: PASS.

- [ ] **Step 4: Verify delete-list invariants one more time**

Run: `grep -r pendingRollback apps/texelterm/`
Expected: no matches.

Run: `grep -r suppressNextScrollback apps/texelterm/`
Expected: no matches.

- [ ] **Step 5: Commit (one or more commits as you audit)**

```bash
git commit -m "test(parser): migrate <file> to sparse-model assertions"
```

### Task 6.11: Full regression suite

- [ ] **Step 1: Run the full parser + testutil test suite with race detector**

Run: `go test -race ./apps/texelterm/...`
Expected: PASS.

- [ ] **Step 2: Run the app-level suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 3: Manual smoke test**

```bash
make build
./bin/texelation start
# In another terminal:
./bin/texelation attach
# Open a pane, run claude, drag the pane border to shrink/grow
# Scroll back, verify content stability
./bin/texelation stop
```

Verify:
1. Claude doesn't jump to top on shrink
2. Scrollback isn't polluted with replicas
3. Scrolled-back view is stable while claude redraws

- [ ] **Step 4: Commit if manual testing required changes**

Any fix found during manual testing gets its own commit.

### Task 6.12: Push branch and open PR

- [ ] **Step 1: Push**

```bash
git push -u origin feat/sparse-integration
```

- [ ] **Step 2: Open PR**

Title: `feat(sparse): integrate sparse.Terminal into VTerm main-screen (cutover)`

Body:
```
Replaces VTerm's main-screen MemoryBuffer path with sparse.Terminal from the
new parser/sparse package. Deletes liveEdgeBase, pendingRollback*,
suppressNextScrollbackPush, hasPartialScrollRegion, restoredFromDisk, and
MemoryBuffer itself.

Supersedes the 18+ fix commits on fix/no-scrollback-from-partial-scroll-regions.

Verification:
- All parser + testutil unit tests pass
- TestClaudeCodeShrinkDragPollutesScrollback passes with marker count == 1
- Manual smoke test: claude shrink-drag is stable, scrolled-back view is stable
- grep -r pendingRollback apps/texelterm/ → no matches
- grep -r suppressNextScrollback apps/texelterm/ → no matches

Spec: docs/superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md
```

Wait for review and merge. After merge, the old `fix/no-scrollback-from-partial-scroll-regions` branch can be closed.

---

# Step 7 — Cleanup

**Goal:** Rename `ViewportWindow` → `ViewWindow` (client-side projection), update stale docs, update CLAUDE.md memory entries.

**Prereqs:** Step 6 merged.

### Task 7.1: Rename ViewportWindow to ViewWindow

**Files:**
- Modify: `apps/texelterm/parser/viewport_window.go` (rename contents)
- Rename: `apps/texelterm/parser/viewport_window.go` → `apps/texelterm/parser/view_window.go` (watch out — there's already a file with this name in `parser/sparse/`; that one is the internal type and this one is the client-side projection, so either keep the name but move, or choose a different name like `client_view_window.go`)
- Modify: `internal/runtime/server/desktop_publisher.go`
- Modify: any test using `ViewportWindow`

- [ ] **Step 1: Choose the target file name**

Since `parser/sparse/view_window.go` already exists (internal), rename the client-side projection to `apps/texelterm/parser/client_view_window.go` with the type name `ClientViewWindow`. This keeps the two concepts visibly distinct.

Actually — re-reading the spec (Section "Client / server touch points"), the spec says "rename `ViewportWindow` to `ViewWindow`". But since `parser/sparse` already has `ViewWindow`, that creates a conflict. Use `ClientViewport` as the new name to resolve.

- [ ] **Step 2: Rename file and type**

```bash
git mv apps/texelterm/parser/viewport_window.go apps/texelterm/parser/client_viewport.go
```

In the file, rename `type ViewportWindow struct` → `type ClientViewport struct` and update the constructor `NewViewportWindow` → `NewClientViewport`. Rename all methods' receiver from `vw *ViewportWindow` to `vw *ClientViewport`.

- [ ] **Step 3: Fix call sites**

Run: `grep -rn "ViewportWindow\|NewViewportWindow" apps/ internal/ client/`

For each match, update to `ClientViewport` / `NewClientViewport`.

- [ ] **Step 4: Build and test**

Run: `go build ./...`
Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git checkout -b chore/sparse-cleanup
git add -u apps/ internal/ client/
git commit -m "refactor: rename ViewportWindow to ClientViewport for clarity"
```

### Task 7.2: Update TERMINAL_PERSISTENCE_ARCHITECTURE.md

**Files:**
- Modify: `docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md`

- [ ] **Step 1: Replace the "Three-Level Architecture" section**

Read the current document. Replace the section describing MemoryBuffer → ScrollbackHistory → DisplayBuffer with a new section that describes the sparse model: `sparse.Store` + `sparse.WriteWindow` + `sparse.ViewWindow` + `sparse.Terminal` + `sparse.Persistence` flowing to `PageStore`.

Keep any sections not about main-screen storage (alt-screen, disk format internals, TXHIST02).

- [ ] **Step 2: Commit**

```bash
git add docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md
git commit -m "docs: update TERMINAL_PERSISTENCE_ARCHITECTURE for sparse model"
```

### Task 7.3: Update CLAUDE.md memory entries

**Files:**
- Modify: `/home/marc/.claude/projects/-home-marc-projects-texel/memory/MEMORY.md`
- Possibly create: new memory entries

- [ ] **Step 1: Remove stale entries**

Find and remove/update:
- "Scroll Region Scrollback Preservation (2026-02-07)" — this is now historical; the new model handles it differently
- "Memory Buffer Erase Color Bug (2026-02-07)" — still relevant for alt screen, but the main-screen path no longer has this code
- "Scroll Region Reload Corruption Bugs (2026-02-09)" — the WAL metadata concerns are still relevant but the specific fields are gone
- Any reference to `liveEdgeBase`, `memoryBufferPushViewportToScrollback`, `pendingRollback`

- [ ] **Step 2: Add one new memory entry**

Create `feedback_sparse_model.md` with the key rule: "Main screen uses sparse.Terminal (store + write window + view window). TUIs and shells issue the same escapes; intent is not inferred. Scroll is decoupled from writes via autoFollow."

- [ ] **Step 3: Commit**

```bash
git add <memory paths>
git commit -m "docs(memory): update entries for sparse main-screen model"
```

### Task 7.4: Push and open PR

- [ ] **Step 1: Push**

```bash
git push -u origin chore/sparse-cleanup
```

- [ ] **Step 2: Open PR**

Title: `chore(sparse): rename ViewportWindow to ClientViewport, update docs`

Wait for merge.

---

## Self-review notes

- **Spec coverage:** every rule (1–8) from the spec is covered by at least one task. Rules 1 (storage) → Task 1.x; Rules 2 (TUI addressing) + 5 (write window resize) → Task 2.x; Rule 3 (view rendering), Rule 4 (autoFollow), Rule 6 (view resize), Rule 7 (scroll) → Task 3.x; Rule 8 (alt-screen) → Tasks 6.5-6.6 (preserved by deleting only main-screen rollback code, not touching alt path).
- **Success criteria mapping:** the `TestClaudeCodeShrinkDragPollutesScrollback` criterion is covered by Task 6.9. The unit-level analogue is Task 4.5. The `grep -r pendingRollback` / `grep -r suppressNextScrollback` criteria are verified in Tasks 6.5 and 6.10. The "scroll-back while claude draws" criterion is verified by the manual smoke test in Task 6.11. The "reload restores writeTop/contentEnd/cursor" criterion is covered by Tasks 5.3 + 5.4.
- **Open questions resolved:** Store data structure → `map[int64]*storeLine` (Task 1.2); WAL wire format → new `MainScreenState` struct, bump and break (Task 5.1 + 6.7 + 6.8); concurrency boundaries → per-type locks, acyclic acquisition order, eager init (explicit in Store/WriteWindow/ViewWindow); test migration → explicit audit in Task 6.10; client-side viewport state → server-side per spec default, so `ClientViewport` remains client-side projection but the authoritative state lives in `VTerm.mainScreen` server-side.
- **Known risk:** Task 6.5 (delete pendingRollback) and Task 6.10 (test audit) are the high-risk tasks. Keeping dual-write (Task 6.2) and the parity test (Task 6.3) during the transition lets us catch any behavior divergence early. If parity fails unexpectedly, the WriteWindow write-path wiring is incomplete — fix the wiring, don't the assertions.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-11-sparse-viewport-write-window-split.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
