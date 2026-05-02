# Wrap-aware selection mapping (issue #224) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Selection highlights track the cells they cover when a logical line wraps across multiple visual rows, even after a resize that shifts the wrap boundary.

**Architecture:** Thread a per-row `[]RowOrigin` slice (cell-bearing `(gid, col)` of each row's first cell) from `reflowChain` through `view.Render` up to `VTerm`. `ContentToViewport` and `ViewportToContent` consult the cached slice instead of computing `y = gid - visibleTop`. Selection state stays as `(gid, col)`; capture path and wire protocol are unchanged.

**Tech Stack:** Go 1.24.3. Existing testing patterns under `apps/texelterm/parser/sparse/` (table-driven `fillRow` helper, `cellsToStringSparse` for comparisons). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-02-issue-224-wrap-highlight-design.md`

---

## File Structure

| File | Responsibility | Status |
|---|---|---|
| `apps/texelterm/parser/sparse/view_reflow.go` | `reflowChain` consolidated to emit origin alongside rows. New `RowOrigin` type. | Modify |
| `apps/texelterm/parser/sparse/view_reflow_test.go` | New origin-emission tests + update existing reflowChain callers to discard the second return. | Modify |
| `apps/texelterm/parser/sparse/view_window.go` | `Render` returns three slices: rows, rowGI, rowOrigin. | Modify |
| `apps/texelterm/parser/sparse/terminal.go` | New `RenderReflowFull` returning all three; existing methods become discard-shims. | Modify |
| `apps/texelterm/parser/main_screen.go` | Interface adds `RenderReflowFull`. New `RowOrigin` type alias. | Modify |
| `apps/texelterm/parser/vterm.go` | Cache `mainScreenRowOrigin`. Rewrite `ContentToViewport` / `ViewportToContent` using new helpers. | Modify |
| `apps/texelterm/parser/vterm_main_screen.go` | `mainScreenGridFull` captures all three returns; old name becomes shim. | Modify |
| `apps/texelterm/parser/viewport_mapping_test.go` | New: round-trip + agreement tests on the new mappers. | Create |
| `apps/texelterm/selection_wrap_resize_test.go` | New: integration test — selection survives resize that re-wraps. | Create |

---

## Phase 1: `RowOrigin` type and reflowChain origin emission (non-wrapped)

### Task 1: Define `RowOrigin` type and update reflowChain signature with non-wrapped origin

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_reflow.go`
- Modify: `apps/texelterm/parser/sparse/view_reflow_test.go`
- Modify: `apps/texelterm/parser/sparse/view_window.go:157`

- [ ] **Step 1: Write the failing test**

Add to `apps/texelterm/parser/sparse/view_reflow_test.go` (append at end of file):

```go
func TestReflowChain_OriginNonWrapped(t *testing.T) {
	s := NewStore(80)
	fillRow(s, 5, "hello world", false) // single non-wrapped gid

	rows, origin := reflowChain(s, 5, 5, 80)
	if len(rows) != 1 {
		t.Fatalf("rows=%d, want 1", len(rows))
	}
	if len(origin) != 1 {
		t.Fatalf("origin len=%d, want 1", len(origin))
	}
	if origin[0] != (RowOrigin{Gid: 5, Col: 0}) {
		t.Errorf("origin[0]=%+v, want {Gid:5, Col:0}", origin[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test -run TestReflowChain_OriginNonWrapped ./apps/texelterm/parser/sparse/
```

Expected: FAIL — `reflowChain` returns one value, not two; `RowOrigin` undefined.

- [ ] **Step 3: Define `RowOrigin` and update reflowChain signature for the non-wrapped case**

In `apps/texelterm/parser/sparse/view_reflow.go`, near the top (after imports):

```go
// RowOrigin is the (gid, col) of the FIRST cell of a reflowed visual row.
// Used by the selection mapping to project content positions to viewport
// coordinates without re-deriving the renderer's chain walk.
//
// Sentinel: Gid == -1 means the row has no real content (blank row inside
// a chain-walk gap, or bottom padding when the viewport is taller than
// the rendered content). Selection callers fall back to naive math on
// such rows.
type RowOrigin struct {
	Gid int64
	Col int
}
```

Then change the signature of `reflowChain` and the body to also build the origin slice:

```go
func reflowChain(s *Store, startGI, endGI int64, viewWidth int) (rows [][]parser.Cell, origin []RowOrigin) {
	if viewWidth <= 0 {
		return nil, nil
	}
	if startGI == endGI && rowHasPositionalGap(s, startGI) {
		return [][]parser.Cell{s.GetLine(startGI)}, []RowOrigin{{Gid: startGI, Col: 0}}
	}
	var logical []parser.Cell
	var cellOrigin []RowOrigin
	for gi := startGI; gi <= endGI; gi++ {
		line := s.GetLine(gi)
		for col := range line {
			logical = append(logical, line[col])
			cellOrigin = append(cellOrigin, RowOrigin{Gid: gi, Col: col})
		}
	}
	// trimTrailingPadding shortens logical; cellOrigin must shrink in lockstep
	// so origin[i] still matches logical[i] after trimming.
	trimmedLen := len(trimTrailingPadding(logical))
	logical = logical[:trimmedLen]
	cellOrigin = cellOrigin[:trimmedLen]

	trailing := trailingEmptyRows(s, startGI, endGI)
	if len(logical) == 0 && trailing == 0 {
		return [][]parser.Cell{nil}, []RowOrigin{{Gid: -1}}
	}
	for off := 0; off < len(logical); off += viewWidth {
		end := off + viewWidth
		if end > len(logical) {
			end = len(logical)
		}
		row := make([]parser.Cell, end-off)
		copy(row, logical[off:end])
		rows = append(rows, row)
		origin = append(origin, cellOrigin[off])
	}
	for i := 0; i < trailing; i++ {
		rows = append(rows, nil)
		origin = append(origin, RowOrigin{Gid: endGI, Col: len(s.GetLine(endGI))})
	}
	return rows, origin
}
```

- [ ] **Step 4: Update existing reflowChain callers to discard the new return**

In `apps/texelterm/parser/sparse/view_window.go` line 157:

```go
reflowed, _ := reflowChain(s, gi, end, width)
```

In `apps/texelterm/parser/sparse/view_reflow_test.go`, update each existing `rows := reflowChain(...)` call to `rows, _ := reflowChain(...)`. There are six such lines (79, 149, 164, 178, 198, 214 in current file).

- [ ] **Step 5: Run all sparse-package tests**

```
go test ./apps/texelterm/parser/sparse/
```

Expected: PASS — including the new `TestReflowChain_OriginNonWrapped` and all pre-existing tests that just discarded the new return.

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/sparse/view_reflow.go \
        apps/texelterm/parser/sparse/view_reflow_test.go \
        apps/texelterm/parser/sparse/view_window.go
git commit -m "Add RowOrigin to reflowChain (non-wrapped baseline)

reflowChain now returns (rows, origin) — origin[i] tracks the (gid, col)
of the first cell on output row i. Existing callers updated to discard
the new return so behaviour is unchanged at this point.

Issue #224 plan, Task 1."
```

---

### Task 2: Origin emission for wrapped chain (single gid, multi-row)

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_reflow_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/texelterm/parser/sparse/view_reflow_test.go`:

```go
func TestReflowChain_OriginWrappedSingleGid(t *testing.T) {
	s := NewStore(80)
	// 100-char line in one gid, last cell Wrapped (chain head before tail).
	long := strings.Repeat("x", 80)
	fillRow(s, 5, long, true)
	// Continuation gid with the remaining 20 chars, last cell not wrapped.
	fillRow(s, 6, strings.Repeat("y", 20), false)

	// At width 80: row 0 is gid 5's 80 chars; row 1 is gid 6's 20 chars.
	rows, origin := reflowChain(s, 5, 6, 80)
	if len(rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(rows))
	}
	if len(origin) != 2 {
		t.Fatalf("origin len=%d, want 2", len(origin))
	}
	if origin[0] != (RowOrigin{Gid: 5, Col: 0}) {
		t.Errorf("origin[0]=%+v, want {Gid:5, Col:0}", origin[0])
	}
	if origin[1] != (RowOrigin{Gid: 6, Col: 0}) {
		t.Errorf("origin[1]=%+v, want {Gid:6, Col:0}", origin[1])
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```
go test -run TestReflowChain_OriginWrappedSingleGid ./apps/texelterm/parser/sparse/
```

Expected: PASS — Task 1's implementation already handles this case (origin tracks each cell's origin during concat).

If it fails, revisit Task 1's concat loop.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/sparse/view_reflow_test.go
git commit -m "Test: reflowChain origin emission for wrapped chain at gid boundary

Issue #224 plan, Task 2."
```

---

### Task 3: Origin emission for wrapped chain (cross-gid mid-row)

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_reflow_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestReflowChain_OriginWrappedCrossingGidMidRow(t *testing.T) {
	s := NewStore(80)
	// Same setup as the previous test: 80 chars in gid 5 (Wrapped) +
	// 20 chars in gid 6. But reflow at narrower width 50 — the wrap
	// boundary now falls mid-gid.
	fillRow(s, 5, strings.Repeat("x", 80), true)
	fillRow(s, 6, strings.Repeat("y", 20), false)

	// At width 50: row 0 = gid 5 cols 0..49 (50 cells), row 1 = gid 5
	// cols 50..79 + gid 6 cols 0..19 (30+20=50 cells).
	rows, origin := reflowChain(s, 5, 6, 50)
	if len(rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(rows))
	}
	if origin[0] != (RowOrigin{Gid: 5, Col: 0}) {
		t.Errorf("origin[0]=%+v, want {Gid:5, Col:0}", origin[0])
	}
	if origin[1] != (RowOrigin{Gid: 5, Col: 50}) {
		t.Errorf("origin[1]=%+v, want {Gid:5, Col:50}", origin[1])
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```
go test -run TestReflowChain_OriginWrappedCrossingGidMidRow ./apps/texelterm/parser/sparse/
```

Expected: PASS — `cellOrigin[off]` for `off=50` is `(gid 5, col 50)` because we walked 50 cells of gid 5 during concat.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/sparse/view_reflow_test.go
git commit -m "Test: reflowChain origin tracks cross-gid mid-row boundary

Issue #224 plan, Task 3."
```

---

### Task 4: Origin emission for trailing empty rows in chain

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_reflow_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestReflowChain_OriginTrailingEmptyRows(t *testing.T) {
	s := NewStore(80)
	// Chain with content in head, blank continuation rows that count
	// via trailingEmptyRows.
	fillRow(s, 5, "hello", true)
	// Force trailing empties: gid 6 exists but has no cells.
	s.SetLine(6, nil)
	// Walk the chain to confirm shape (sanity).
	end, _ := walkChain(s, 5, 100)
	if end < 5 {
		t.Skip("chain walk did not reach trailing empties; pattern unsupported")
	}

	rows, origin := reflowChain(s, 5, end, 80)
	if len(rows) != len(origin) {
		t.Fatalf("rows/origin length mismatch: %d vs %d", len(rows), len(origin))
	}
	if len(rows) < 1 {
		t.Fatalf("expected at least 1 row")
	}
	// The trailing empty rows (any rows past the head's content) must
	// have origin (endGI, len(endGI's cells)).
	for i := 1; i < len(rows); i++ {
		want := RowOrigin{Gid: end, Col: len(s.GetLine(end))}
		if origin[i] != want {
			t.Errorf("trailing row %d origin=%+v, want %+v", i, origin[i], want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```
go test -run TestReflowChain_OriginTrailingEmptyRows ./apps/texelterm/parser/sparse/
```

Expected: PASS — Task 1's trailing-empty-rows loop already emits `RowOrigin{Gid: endGI, Col: len(s.GetLine(endGI))}`.

If it fails, the trailing-empty-rows handling needs adjustment.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/sparse/view_reflow_test.go
git commit -m "Test: reflowChain origin for trailing empty wrap-continuation rows

Issue #224 plan, Task 4."
```

---

## Phase 2: `view.Render` exposes the origin slice

### Task 5: `view.Render` returns three slices

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Modify: `apps/texelterm/parser/sparse/terminal.go`

- [ ] **Step 1: Verify the current `view.Render` contract**

Read `apps/texelterm/parser/sparse/view_window.go` lines 105–195. Confirm:
- Return type is `([][]parser.Cell, []int64)`.
- `rowGI` is appended in three branches: blank rows (line 137 sets `-1`), nowrap branch (line 149 appends `r`), wrapped branch (line 160 appends `gi`).

- [ ] **Step 2: Modify `view.Render` to return a third slice**

Change the signature:

```go
func (v *ViewWindow) Render(s *Store) ([][]parser.Cell, []int64, []RowOrigin) {
```

Inside, immediately after `rowGI := make([]int64, 0, height)`, add the parallel output slice:

```go
rowOrigin := make([]RowOrigin, 0, height)
```

In the per-chain loop, alongside the existing `var rows [][]parser.Cell` and `var rowsGI []int64`, add:

```go
var rowsOrigin []RowOrigin
```

In the blank-row branch (current line 134–139), where the code does `rowGI = append(rowGI, -1)`, also append to `rowOrigin`:

```go
rowGI = append(rowGI, -1)
rowOrigin = append(rowOrigin, RowOrigin{Gid: -1})
```

In the nowrap / reflowOff branch, append `(r, 0)` per row:

```go
if reflowOff || nowrap {
    for r := gi; r <= end; r++ {
        rows = append(rows, clipRow(s.GetLine(r), width))
        rowsGI = append(rowsGI, r)
        rowsOrigin = append(rowsOrigin, RowOrigin{Gid: r, Col: 0})
    }
}
```

In the wrapped branch, capture both returns of `reflowChain`:

```go
} else {
    reflowed, originSlice := reflowChain(s, gi, end, width)
    for i, row := range reflowed {
        rows = append(rows, clipRow(row, width))
        rowsGI = append(rowsGI, gi)
        rowsOrigin = append(rowsOrigin, originSlice[i])
    }
}
```

- For the nowrap / reflowOff branch:

```go
if reflowOff || nowrap {
    for r := gi; r <= end; r++ {
        rows = append(rows, clipRow(s.GetLine(r), width))
        rowsGI = append(rowsGI, r)
        rowsOrigin = append(rowsOrigin, RowOrigin{Gid: r, Col: 0})
    }
}
```

- The skip / first-chain trim must apply to all three slices in lockstep:

```go
if first {
    first = false
    if skip < len(rows) {
        rows = rows[skip:]
        rowsGI = rowsGI[skip:]
        rowsOrigin = rowsOrigin[skip:]
    } else {
        rows = nil
        rowsGI = nil
        rowsOrigin = nil
    }
}
```

- The append-to-output loop appends to all three:

```go
for i, row := range rows {
    if len(out) >= height {
        break
    }
    out = append(out, row)
    rowGI = append(rowGI, rowsGI[i])
    rowOrigin = append(rowOrigin, rowsOrigin[i])
}
```

- Bottom padding:

```go
for len(out) < height {
    out = append(out, make([]parser.Cell, width))
    rowGI = append(rowGI, -1)
    rowOrigin = append(rowOrigin, RowOrigin{Gid: -1})
}
```

- Trim:

```go
if len(rowGI) > height {
    rowGI = rowGI[:height]
}
if len(rowOrigin) > height {
    rowOrigin = rowOrigin[:height]
}
```

- Return: `return out, rowGI, rowOrigin`.

- [ ] **Step 3: Update `Terminal.RenderReflowWithRowIdx` to drop the third return**

In `apps/texelterm/parser/sparse/terminal.go` line ~314:

```go
func (t *Terminal) RenderReflowWithRowIdx() ([][]parser.Cell, []int64) {
    cursorGI, cursorCol := t.write.Cursor()
    t.view.RecomputeLiveAnchor(t.store, cursorGI, cursorCol, t.write.WriteTop())
    rows, gids, _ := t.view.Render(t.store)
    return rows, gids
}
```

- [ ] **Step 4: Run sparse-package tests to confirm no regression**

```
go test ./apps/texelterm/parser/sparse/
```

Expected: PASS — no behaviour change for callers that discard the third return.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/terminal.go
git commit -m "view.Render returns rowOrigin slice alongside rowGI

The new slice carries cell-bearing (gid, col) per row. Existing
RenderReflowWithRowIdx wraps the new return and drops the slice;
chain-head clipping in the publisher path stays unchanged.

Issue #224 plan, Task 5."
```

---

### Task 6: `Terminal.RenderReflowFull` exposing all three slices

**Files:**
- Modify: `apps/texelterm/parser/sparse/terminal.go`

- [ ] **Step 1: Add `RenderReflowFull` next to the existing render methods**

In `apps/texelterm/parser/sparse/terminal.go`, near the existing `RenderReflowWithRowIdx`:

```go
// RenderReflowFull is the full render output: rows, per-row chain-head
// gid (for publisher clipping), and per-row cell-bearing origin (for
// selection mapping). Internal callers that only need rows or rows+gids
// use the existing shims; selection consults the origin slice via
// VTerm's cache.
func (t *Terminal) RenderReflowFull() ([][]parser.Cell, []int64, []RowOrigin) {
    cursorGI, cursorCol := t.write.Cursor()
    t.view.RecomputeLiveAnchor(t.store, cursorGI, cursorCol, t.write.WriteTop())
    return t.view.Render(t.store)
}
```

Also collapse `RenderReflowWithRowIdx` and `RenderReflow` to thin shims that go through `RenderReflowFull` (avoids two RecomputeLiveAnchor calls if both methods get invoked in one frame):

```go
func (t *Terminal) RenderReflowWithRowIdx() ([][]parser.Cell, []int64) {
    rows, gids, _ := t.RenderReflowFull()
    return rows, gids
}

func (t *Terminal) RenderReflow() [][]parser.Cell {
    rows, _, _ := t.RenderReflowFull()
    return rows
}
```

- [ ] **Step 2: Build to verify compilation**

```
go build ./apps/texelterm/...
```

Expected: clean build.

- [ ] **Step 3: Run tests**

```
go test ./apps/texelterm/parser/sparse/
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/texelterm/parser/sparse/terminal.go
git commit -m "Terminal.RenderReflowFull: single source of truth for render output

Existing render methods become thin shims that delegate to RenderReflowFull
and drop the slices they don't need. Issue #224 plan, Task 6."
```

---

## Phase 3: `MainScreen` interface + parser-package alias

### Task 7: Add `RenderReflowFull` to `MainScreen` interface and `RowOrigin` alias

**Files:**
- Modify: `apps/texelterm/parser/main_screen.go`

- [ ] **Step 1: Add the `RowOrigin` type alias and interface method**

In `apps/texelterm/parser/main_screen.go`, near the existing types (top of file):

```go
// RowOrigin is re-exported from the sparse package so callers of the
// MainScreen interface don't have to import parser/sparse internals
// directly. The values are produced at render time by the sparse view's
// chain walk (see sparse.RowOrigin for semantics).
type RowOrigin = sparse.RowOrigin
```

(If `sparse` isn't already imported in this file, add the import.)

In the `MainScreen` interface, near `RenderReflowWithRowIdx`:

```go
// RenderReflowFull returns the rendered grid plus parallel slices for
// per-row chain-head gid (publisher clipping) and per-row cell-bearing
// origin (selection mapping). RenderReflow / RenderReflowWithRowIdx
// stay on the interface as discard-shims for callers that only need
// some of the output.
RenderReflowFull() ([][]Cell, []int64, []RowOrigin)
```

- [ ] **Step 2: Build to confirm sparse.Terminal already satisfies the interface**

```
go build ./apps/texelterm/parser/...
```

Expected: clean build (Task 6's `Terminal.RenderReflowFull` matches the new interface entry).

- [ ] **Step 3: Run tests**

```
go test ./apps/texelterm/parser/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/texelterm/parser/main_screen.go
git commit -m "MainScreen interface adds RenderReflowFull + RowOrigin alias

Issue #224 plan, Task 7."
```

---

## Phase 4: `VTerm` caches `mainScreenRowOrigin`

### Task 8: Cache origin slice on every render

**Files:**
- Modify: `apps/texelterm/parser/vterm.go`
- Modify: `apps/texelterm/parser/vterm_main_screen.go`

- [ ] **Step 1: Read the existing cache layout**

Read `apps/texelterm/parser/vterm.go` to find the `lastRowGlobalIdx` field on `*VTerm` (or the equivalent stash if named differently). Confirm where the grid is built (`mainScreenGridWithRowIdx`).

- [ ] **Step 2: Add the new cache field**

In `apps/texelterm/parser/vterm.go`, near where the cached row-idx slice lives, add:

```go
// mainScreenRowOrigin caches the per-row cell-bearing (gid, col) emitted
// by the sparse view's last render. Length matches the rendered grid;
// Gid == -1 sentinel for blank rows / unwritten gaps. Read by
// ContentToViewport / ViewportToContent under the same lock that gates
// the existing grid cache. Issue #224.
mainScreenRowOrigin []RowOrigin
```

- [ ] **Step 3: Capture origin in `mainScreenGridWithRowIdx`**

In `apps/texelterm/parser/vterm_main_screen.go`, locate `mainScreenGridWithRowIdx` (~line 499). Change the implementation to call `RenderReflowFull` and stash all three results, then return the existing two:

```go
func (v *VTerm) mainScreenGridWithRowIdx() ([][]Cell, []int64) {
    if v.mainScreen == nil {
        return nil, nil
    }
    grid, rowIdx, rowOrigin := v.mainScreen.RenderReflowFull()
    v.mainScreenRowOrigin = rowOrigin
    return grid, rowIdx
}
```

(Keep the existing function name — its callers don't change. The third value is captured into the cache as a side effect.)

- [ ] **Step 4: Run tests**

```
go test ./apps/texelterm/parser/
```

Expected: PASS — no behaviour change for any caller of `mainScreenGridWithRowIdx`; the cache field is populated but not yet consumed.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/vterm_main_screen.go
git commit -m "VTerm caches mainScreenRowOrigin on every render

The cache is populated but not yet consumed; ContentToViewport /
ViewportToContent will read from it next. Issue #224 plan, Task 8."
```

---

## Phase 5: Helper functions for origin-based mapping

### Task 9: `advanceCells` helper

**Files:**
- Modify: `apps/texelterm/parser/vterm.go`
- Create: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/parser/viewport_mapping_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Tests for the origin-based viewport <-> content mapping.

package parser

import (
	"testing"
)

// fillRowVTerm helper: write text to gid via the underlying main screen.
// Existing tests use parser-level helpers; copy the pattern when needed.
func TestAdvanceCells_StaysInGid(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	// Pre-fill gid 5 with 30 chars by writing through the main screen.
	cells := make([]Cell, 30)
	for i := range cells {
		cells[i] = Cell{Rune: rune('a' + (i % 26))}
	}
	v.mainScreen.SetLine(5, cells)

	gid, col := v.advanceCells(5, 0, 10)
	if gid != 5 || col != 10 {
		t.Errorf("advanceCells(5,0,10) = (%d,%d), want (5,10)", gid, col)
	}
}

func TestAdvanceCells_CrossesGidBoundary(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	// gid 5 has 80 cells (last Wrapped); gid 6 has 60.
	left := make([]Cell, 80)
	for i := range left {
		left[i] = Cell{Rune: 'a'}
	}
	left[79].Wrapped = true
	v.mainScreen.SetLine(5, left)
	right := make([]Cell, 60)
	for i := range right {
		right[i] = Cell{Rune: 'b'}
	}
	v.mainScreen.SetLine(6, right)

	// From (5, 50), advance 40 cells: walks 30 cells of gid 5 (cols 50..79),
	// then 10 cells of gid 6 (cols 0..9). Result: (6, 10).
	gid, col := v.advanceCells(5, 50, 40)
	if gid != 6 || col != 10 {
		t.Errorf("advanceCells(5,50,40) = (%d,%d), want (6,10)", gid, col)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test -run TestAdvanceCells ./apps/texelterm/parser/
```

Expected: FAIL — `advanceCells` undefined.

- [ ] **Step 3: Implement `advanceCells`**

Add to `apps/texelterm/parser/vterm.go` (in the same section as `ContentToViewport` / `ViewportToContent`):

```go
// advanceCells walks `n` cells forward from (originGid, originCol) through
// the store, crossing gid boundaries when a row's cells are exhausted.
// Returns the resulting (gid, col). Used by ViewportToContent to resolve
// a viewport (y, x) given the row's origin.
//
// The walk is bounded by the caller (typically viewport width). Stops
// at the first row beyond the store's content if `n` would take us past;
// returns whatever (gid, col) the walk produced — past-content positions
// are valid (selection there resolves to a zero-width selection).
func (v *VTerm) advanceCells(originGid int64, originCol int, n int) (int64, int) {
    if v.mainScreen == nil {
        return originGid, originCol
    }
    gid := originGid
    col := originCol
    remaining := n
    for remaining > 0 {
        cells := v.mainScreen.ReadLine(gid)
        rowLen := len(cells)
        available := rowLen - col
        if remaining < available {
            return gid, col + remaining
        }
        // Consume the rest of this gid; advance to the next.
        if available < 0 {
            available = 0
        }
        remaining -= available
        gid++
        col = 0
    }
    return gid, col
}
```

- [ ] **Step 4: Run tests**

```
go test -run TestAdvanceCells ./apps/texelterm/parser/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/viewport_mapping_test.go
git commit -m "advanceCells: walk N cells from an origin, crossing gid boundaries

Issue #224 plan, Task 9."
```

---

### Task 10: `cellsBetween` helper

**Files:**
- Modify: `apps/texelterm/parser/vterm.go`
- Modify: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/texelterm/parser/viewport_mapping_test.go`:

```go
func TestCellsBetween_StaysInGid(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	cells := make([]Cell, 30)
	for i := range cells {
		cells[i] = Cell{Rune: 'a'}
	}
	v.mainScreen.SetLine(5, cells)

	steps, ok := v.cellsBetween(5, 0, 5, 10, 80)
	if !ok || steps != 10 {
		t.Errorf("cellsBetween(5,0 -> 5,10) = (%d,%v), want (10,true)", steps, ok)
	}
}

func TestCellsBetween_CrossesGidBoundary(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	left := make([]Cell, 80)
	for i := range left {
		left[i] = Cell{Rune: 'a'}
	}
	left[79].Wrapped = true
	v.mainScreen.SetLine(5, left)
	right := make([]Cell, 60)
	for i := range right {
		right[i] = Cell{Rune: 'b'}
	}
	v.mainScreen.SetLine(6, right)

	// Origin (5, 50), target (6, 10), max 80 cells. Steps: 30 (rest of gid 5) + 10 = 40.
	steps, ok := v.cellsBetween(5, 50, 6, 10, 80)
	if !ok || steps != 40 {
		t.Errorf("cellsBetween(5,50 -> 6,10) = (%d,%v), want (40,true)", steps, ok)
	}
}

func TestCellsBetween_TargetOutOfRange(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	cells := make([]Cell, 10)
	v.mainScreen.SetLine(5, cells)

	// Target too far; max 50 cells. Should return (_, false).
	_, ok := v.cellsBetween(5, 0, 100, 0, 50)
	if ok {
		t.Errorf("cellsBetween(5,0 -> 100,0) returned ok=true; expected false")
	}
}

func TestCellsBetween_TargetBeforeOrigin(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	cells := make([]Cell, 30)
	v.mainScreen.SetLine(5, cells)

	// Target is before origin in store order — not in this row.
	_, ok := v.cellsBetween(5, 20, 5, 5, 80)
	if ok {
		t.Errorf("cellsBetween(5,20 -> 5,5) returned ok=true; expected false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test -run TestCellsBetween ./apps/texelterm/parser/
```

Expected: FAIL — `cellsBetween` undefined.

- [ ] **Step 3: Implement `cellsBetween`**

Add to `apps/texelterm/parser/vterm.go`:

```go
// cellsBetween counts cells from (originGid, originCol) forward to
// (targetGid, targetCol), crossing gid boundaries. Returns
// (stepsTaken, true) if the target is reached within maxCells steps,
// or (0, false) if the walk runs past maxCells without finding the
// target. Used by ContentToViewport to compute the visual x within a
// row whose origin is known.
//
// "Before origin" cases (target is reachable only by going backward)
// return (0, false) — the caller continues scanning to the next row.
//
// Safety: the loop is bounded both by step count AND by iteration
// count. Walking through many consecutive empty gids contributes 0 to
// `steps` per iteration; without an iteration bound the loop could
// scan an arbitrarily distant target gid forever. Bound at
// `maxCells*2` iterations — comfortably above the worst case of a
// viewport-width walk through fully-populated rows (where each
// iteration advances `steps` by at least 1 once we leave the origin
// row).
func (v *VTerm) cellsBetween(originGid int64, originCol int, targetGid int64, targetCol, maxCells int) (int, bool) {
    if v.mainScreen == nil {
        return 0, false
    }
    if targetGid < originGid || (targetGid == originGid && targetCol < originCol) {
        return 0, false
    }
    gid := originGid
    col := originCol
    steps := 0
    iterCap := maxCells*2 + 1
    for i := 0; i < iterCap; i++ {
        if gid == targetGid {
            if targetCol < col {
                return 0, false
            }
            delta := targetCol - col
            if steps+delta > maxCells {
                return 0, false
            }
            return steps + delta, true
        }
        cells := v.mainScreen.ReadLine(gid)
        available := len(cells) - col
        if available < 0 {
            available = 0
        }
        steps += available
        if steps > maxCells {
            return 0, false
        }
        gid++
        col = 0
    }
    return 0, false
}
```

- [ ] **Step 4: Run tests**

```
go test -run TestCellsBetween ./apps/texelterm/parser/
```

Expected: PASS for all four sub-tests.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/viewport_mapping_test.go
git commit -m "cellsBetween: count cells from origin to target across gid boundaries

Bounded by maxCells (typically viewport width). Returns (steps, false)
when target is unreachable within the budget or before origin in
store order. Issue #224 plan, Task 10."
```

---

## Phase 6: Rewrite `ViewportToContent` and `ContentToViewport`

### Task 11: Rewrite `ViewportToContent` to use `mainScreenRowOrigin` + `advanceCells`

**Files:**
- Modify: `apps/texelterm/parser/vterm.go`
- Modify: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestViewportToContent_NonWrappedRow(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// Type a few non-wrapping lines.
	p := NewParser(v)
	for i := 0; i < 5; i++ {
		for _, r := range "hello" {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	// Force a render so mainScreenRowOrigin is populated.
	_, _ = v.GridWithRowIdx()

	// Click at row 2, col 3. Origin should map straight through.
	gid, col, _, ok := v.ViewportToContent(2, 3)
	if !ok {
		t.Fatalf("ViewportToContent(2, 3) ok=false")
	}
	// Validate against the rendered rowOrigin slice directly.
	if int(gid) != int(v.mainScreenRowOrigin[2].Gid) {
		t.Errorf("returned gid=%d, expected origin gid=%d", gid, v.mainScreenRowOrigin[2].Gid)
	}
	if col != v.mainScreenRowOrigin[2].Col+3 {
		t.Errorf("returned col=%d, expected origin col + 3 = %d", col, v.mainScreenRowOrigin[2].Col+3)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test -run TestViewportToContent_NonWrappedRow ./apps/texelterm/parser/
```

Expected: FAIL — current `ViewportToContent` uses naive math, not `mainScreenRowOrigin`. The check against `v.mainScreenRowOrigin[2]` will diverge for non-trivial cases (or compile-fail if that field isn't accessed yet).

If naive math happens to match (gid 0 starts at row 0, etc.), the failure may be subtle; the wrapped tests in Task 12 will surface real mismatches.

- [ ] **Step 3: Rewrite `ViewportToContent`**

Replace the existing main-screen path:

```go
func (v *VTerm) ViewportToContent(y, x int) (logicalLine int64, charOffset int, isCurrentLine bool, ok bool) {
    if v.inAltScreen {
        // Alt screen: treat as current line equivalent.
        charOffset = y*v.width + x
        return -1, charOffset, true, true
    }
    if v.mainScreen == nil {
        return 0, 0, false, false
    }
    cursorLine, _ := v.mainScreen.Cursor()

    // Prefer the cached origin slice produced by the last render.
    if y >= 0 && y < len(v.mainScreenRowOrigin) {
        o := v.mainScreenRowOrigin[y]
        if o.Gid != -1 {
            gid, col := v.advanceCells(o.Gid, o.Col, x)
            return gid, col, gid == cursorLine, true
        }
    }

    // Fallback for rows outside the cached window or sentinel rows: use
    // the naive (visibleTop + y, x) math, preserving pre-reflow behaviour
    // for blank gaps and bottom padding.
    visibleTop, _ := v.mainScreen.VisibleRange()
    logicalLine = visibleTop + int64(y)
    charOffset = x
    isCurrentLine = logicalLine == cursorLine
    ok = true
    return
}
```

- [ ] **Step 4: Run tests**

```
go test -run TestViewportToContent ./apps/texelterm/parser/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/viewport_mapping_test.go
git commit -m "ViewportToContent uses cached row origins

Origin lookup + advanceCells walk gives correct (gid, col) even when
a logical line wraps across multiple visual rows. Sentinel / out-of-
window rows fall back to naive math. Issue #224 plan, Task 11."
```

---

### Task 12: Rewrite `ContentToViewport` to use `mainScreenRowOrigin` + `cellsBetween`

**Files:**
- Modify: `apps/texelterm/parser/vterm.go`
- Modify: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestContentToViewport_RoundTripNonWrapped(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	p := NewParser(v)
	for i := 0; i < 5; i++ {
		for _, r := range "hello" {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	_, _ = v.GridWithRowIdx()

	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			gid, col, _, ok := v.ViewportToContent(y, x)
			if !ok {
				t.Fatalf("ViewportToContent(%d,%d) ok=false", y, x)
			}
			ry, rx, vis := v.ContentToViewport(gid, col)
			if !vis {
				t.Errorf("(%d,%d) → (%d,%d) → not visible", y, x, gid, col)
				continue
			}
			if ry != y || rx != x {
				t.Errorf("round-trip (%d,%d) → (gid=%d,col=%d) → (%d,%d)",
					y, x, gid, col, ry, rx)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test -run TestContentToViewport_RoundTripNonWrapped ./apps/texelterm/parser/
```

Expected: FAIL — ContentToViewport still uses naive math; round-trip won't match origin-based ViewportToContent for non-trivial cases.

- [ ] **Step 3: Rewrite `ContentToViewport`**

Replace the existing main-screen path:

```go
func (v *VTerm) ContentToViewport(logicalLine int64, charOffset int) (y, x int, visible bool) {
    if v.inAltScreen {
        if v.width <= 0 {
            return 0, 0, false
        }
        y = charOffset / v.width
        x = charOffset % v.width
        visible = y >= 0 && y < v.height
        return
    }
    if v.mainScreen == nil {
        return 0, 0, false
    }

    // Scan the cached origin slice. For each row whose origin is set
    // (Gid != -1), check whether (logicalLine, charOffset) falls within
    // [origin, origin + viewportWidth) cell-walked through the chain.
    for ry, o := range v.mainScreenRowOrigin {
        if o.Gid == -1 {
            continue
        }
        steps, ok := v.cellsBetween(o.Gid, o.Col, logicalLine, charOffset, v.width)
        if !ok {
            continue
        }
        if steps >= v.width {
            // Target is past this row's visible cells; let a later row
            // claim it (some other row's origin starts there).
            continue
        }
        return ry, steps, true
    }
    return 0, 0, false
}
```

- [ ] **Step 4: Run tests**

```
go test -run TestContentToViewport_RoundTripNonWrapped ./apps/texelterm/parser/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/viewport_mapping_test.go
git commit -m "ContentToViewport uses cached row origins

Linear scan over rowOrigin + cellsBetween bounded by viewport width.
Round-trip with ViewportToContent now consistent. Issue #224 plan,
Task 12."
```

---

## Phase 7: Wrapped-content tests + agreement against renderer

### Task 13: Wrapped-content round-trip test

**Files:**
- Modify: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestContentToViewport_RoundTripWrappedChain(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// Single logical line of 100 chars, autowrap at 80.
	p := NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars total
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	_, _ = v.GridWithRowIdx()

	// Round-trip every (y, x) on the first two rows (the wrap chain).
	for _, y := range []int{0, 1} {
		for _, x := range []int{0, 5, 40, 79} {
			gid, col, _, ok := v.ViewportToContent(y, x)
			if !ok {
				t.Fatalf("ViewportToContent(%d,%d) failed", y, x)
			}
			ry, rx, vis := v.ContentToViewport(gid, col)
			if !vis {
				t.Errorf("(%d,%d) → (%d,%d) → not visible", y, x, gid, col)
				continue
			}
			if ry != y || rx != x {
				t.Errorf("round-trip (%d,%d) → (gid=%d,col=%d) → (%d,%d)",
					y, x, gid, col, ry, rx)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test**

```
go test -run TestContentToViewport_RoundTripWrappedChain ./apps/texelterm/parser/
```

Expected: PASS — Tasks 11 + 12 already handle wrapped chains via `mainScreenRowOrigin`.

If it fails, the bug is in the origin slice or the helpers; debug there.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/viewport_mapping_test.go
git commit -m "Test: round-trip mapping for wrapped chain rows

Issue #224 plan, Task 13."
```

---

### Task 14: Agreement test (mapper matches renderer)

**Files:**
- Modify: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

This is the test that surfaced the bug in the reverted attempt. It confirms `ViewportToContent(y, 0)` returns the same `(gid, col)` that the renderer's `mainScreenRowOrigin[y]` reports for that row.

```go
func TestViewportToContent_AgreesWithRenderedRowOrigin(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// Mix of short non-wrapping lines, a long wrapped line, and more shorts.
	p := NewParser(v)
	for i := 0; i < 5; i++ {
		for _, r := range "short" {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	for i := 0; i < 5; i++ {
		for _, r := range "after" {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	_, _ = v.GridWithRowIdx()

	for y, o := range v.mainScreenRowOrigin {
		if o.Gid == -1 {
			continue
		}
		gid, col, _, ok := v.ViewportToContent(y, 0)
		if !ok {
			t.Errorf("row %d: ViewportToContent(%d, 0) failed", y, y)
			continue
		}
		if gid != o.Gid || col != o.Col {
			t.Errorf("row %d: ViewportToContent returned (%d,%d), origin says (%d,%d)",
				y, gid, col, o.Gid, o.Col)
		}
	}
}
```

- [ ] **Step 2: Run the test**

```
go test -run TestViewportToContent_AgreesWithRenderedRowOrigin ./apps/texelterm/parser/
```

Expected: PASS — by definition `ViewportToContent(y, 0)` reads `mainScreenRowOrigin[y]` and applies a 0-cell `advanceCells`.

If it fails, the path through `advanceCells(o.Gid, o.Col, 0)` isn't returning `(o.Gid, o.Col)` for some reason; debug `advanceCells`.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/viewport_mapping_test.go
git commit -m "Test: ViewportToContent agrees with rendered row origin

This is the invariant the reverted attempt violated — the mapper's
chain walk must match what the renderer emitted. Issue #224 plan,
Task 14."
```

---

### Task 15: Sentinel / off-viewport handling tests

**Files:**
- Modify: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestViewportToContent_FallsBackToNaiveOnSentinel(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// Type one short line, leaving rows 1..23 as bottom padding (-1 sentinel).
	p := NewParser(v)
	for _, r := range "only-line" {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	_, _ = v.GridWithRowIdx()

	// Find a sentinel row.
	var sentY = -1
	for y, o := range v.mainScreenRowOrigin {
		if o.Gid == -1 {
			sentY = y
			break
		}
	}
	if sentY < 0 {
		t.Skip("no sentinel row to exercise")
	}

	// ViewportToContent on the sentinel row falls back to naive math
	// — visibleTop + y, x — and reports ok=true so existing callers keep
	// working.
	gid, col, _, ok := v.ViewportToContent(sentY, 5)
	if !ok {
		t.Fatalf("ViewportToContent on sentinel row returned ok=false")
	}
	visibleTop, _ := v.mainScreen.VisibleRange()
	wantGid := visibleTop + int64(sentY)
	if gid != wantGid || col != 5 {
		t.Errorf("sentinel fallback = (%d,%d), want (%d,5)", gid, col, wantGid)
	}
}

func TestContentToViewport_OffViewportReturnsFalse(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	p := NewParser(v)
	for _, r := range "anchor-line" {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	_, _ = v.GridWithRowIdx()

	// (gid 9999, col 0) is past any rendered row; the scan finds no
	// match and returns visible=false.
	_, _, vis := v.ContentToViewport(9999, 0)
	if vis {
		t.Errorf("ContentToViewport(9999, 0) returned visible=true; expected false")
	}
}
```

- [ ] **Step 2: Run tests**

```
go test -run "TestViewportToContent_FallsBackToNaiveOnSentinel|TestContentToViewport_OffViewportReturnsFalse" ./apps/texelterm/parser/
```

Expected: PASS — the sentinel/fallback paths in `ViewportToContent` and the no-match path in `ContentToViewport` are both already implemented from Task 11/12.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/viewport_mapping_test.go
git commit -m "Test: sentinel fallback + off-viewport visibility

Issue #224 plan, Task 15."
```

---

## Phase 8: Integration — selection survives resize

### Task 16: Resize integration test

**Files:**
- Create: `apps/texelterm/selection_wrap_resize_test.go`

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/selection_wrap_resize_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Issue #224: selection highlight tracks the right cells across resize
// that re-wraps a logical line. Test exercises the full path:
// double-click → selection state in (gid, col) → resize → highlight
// resolved via the new mapping.

package texelterm

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestSelection_HighlightTracksWrappedContentAfterResize(t *testing.T) {
	app := New("test")
	app.Resize(80, 24)

	v := app.vterm
	v.EnableMemoryBuffer()
	app.SetClipboardService(nil) // standalone-mode plumbing not needed

	// Type a 100-char line that fits in one row at width 80 + 1 wrap row.
	p := parser.NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	_, _ = v.GridWithRowIdx()

	// Pick a (y, x) on the second wrapped row — col 5 → logical char 85.
	gid, col, _, ok := v.ViewportToContent(1, 5)
	if !ok {
		t.Fatalf("could not resolve (1, 5)")
	}

	// Sanity: confirm mapping back at width 80.
	yPre, xPre, vis := v.ContentToViewport(gid, col)
	if !vis || yPre != 1 || xPre != 5 {
		t.Fatalf("pre-resize round-trip: got (%d,%d, vis=%v), want (1,5,true)", yPre, xPre, vis)
	}

	// Resize narrower so the same line wraps to more rows.
	app.Resize(40, 24)
	_, _ = v.GridWithRowIdx()

	// The (gid, col) we captured pre-resize must still map to a visible
	// row at the new width — somewhere in the multi-row reflow of the
	// chain. We don't pin the exact row (that depends on chain layout),
	// but it must be visible AND the round-trip must be consistent.
	yPost, xPost, visPost := v.ContentToViewport(gid, col)
	if !visPost {
		t.Fatalf("post-resize: (%d,%d) not visible", gid, col)
	}
	gidR, colR, _, okR := v.ViewportToContent(yPost, xPost)
	if !okR || gidR != gid || colR != col {
		t.Errorf("post-resize round-trip: (gid=%d,col=%d) → (%d,%d) → (gid=%d,col=%d)",
			gid, col, yPost, xPost, gidR, colR)
	}
}
```

(Adjust constructor / field access patterns to match the codebase — the test may need to use `NewTexelTerm()` or similar; see existing tests in `apps/texelterm/term_test.go` for the established pattern.)

- [ ] **Step 2: Run the test**

```
go test -run TestSelection_HighlightTracksWrappedContentAfterResize ./apps/texelterm/
```

Expected: PASS.

If it fails, the resize path may not be re-rendering the origin slice; check that `app.Resize` triggers a render or that `_, _ = v.GridWithRowIdx()` is being called.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/selection_wrap_resize_test.go
git commit -m "Test: selection (gid,col) survives resize that re-wraps the line

Issue #224 plan, Task 16."
```

---

## Phase 9: Final verification

### Task 17: Full test suite + race detector

**Files:**
- None (verification only).

- [ ] **Step 1: Run the full texelterm parser suite**

```
go test ./apps/texelterm/parser/...
```

Expected: PASS.

- [ ] **Step 2: Run the texelterm app suite**

```
go test ./apps/texelterm/
```

Expected: PASS.

- [ ] **Step 3: Run with race detector**

```
go test -race -count=1 ./apps/texelterm/parser/... ./apps/texelterm/
```

Expected: PASS, no data races.

- [ ] **Step 4: Run the full repo test suite**

```
go test ./...
```

Expected: PASS — no consumer outside texelterm should be affected by the changes (publisher uses the discard-shim).

- [ ] **Step 5: Confirm no commit needed**

If everything passes, this verification phase needs no commit. If a regression surfaced, fix it inline and commit, then re-run.

---

## Summary

| Phase | Tasks | Outcome |
|---|---|---|
| 1 | 1–4 | `reflowChain` consolidated to emit `(rows, origin)` with full case coverage. |
| 2 | 5–6 | `view.Render` and `Terminal.RenderReflowFull` expose origin slice. |
| 3 | 7 | `MainScreen` interface + parser-package `RowOrigin` alias. |
| 4 | 8 | `VTerm` caches `mainScreenRowOrigin` on every render. |
| 5 | 9–10 | `advanceCells` and `cellsBetween` helpers. |
| 6 | 11–12 | `ViewportToContent` / `ContentToViewport` rewritten. |
| 7 | 13–15 | Wrapped round-trip + agreement-with-renderer + sentinel tests. |
| 8 | 16 | End-to-end resize integration test. |
| 9 | 17 | Full-suite verification. |

**Branch:** Continue on `feature/issue-224-wrap-highlight` (already created with the spec commit).
