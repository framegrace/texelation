# Sparse Resize-Reflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore reflow-on-resize for the sparse terminal: widening rejoins wrapped lines, shrinking splits them, for both live shell output and scrollback history. DECSTBM/structured content is protected via a per-row NoWrap flag.

**Architecture:** View-side reflow. `sparse.Store` gets a per-row `NoWrap` bit. `VTerm` sets it when DECSTBM is active at write time. `ViewWindow` walks `Wrapped` chains and reflows at current viewWidth (or renders 1:1 for NoWrap chains or when the user toggles reflow off). Cursor position is derived via forward/inverse mappings through the chain. Store layout is unchanged; resize is O(1) in content size.

**Tech Stack:** Go 1.24.3, sparse viewport package (`apps/texelterm/parser/sparse`), VTerm parser, `.lhist` persistence, WAL. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-04-16-sparse-resize-reflow-design.md`

---

## File Structure

**New files:**
- `apps/texelterm/parser/sparse/view_reflow.go` — chain walker + reflow helpers (keeps `view_window.go` focused)
- `apps/texelterm/parser/sparse/view_reflow_test.go` — unit tests for chain walking / reflow
- `apps/texelterm/parser/sparse/view_window_reflow_test.go` — Render/CursorToView/ViewToCursor tests
- `apps/texelterm/parser/sparse/store_nowrap_test.go` — NoWrap storage unit tests

**Modified files:**
- `apps/texelterm/parser/sparse/store.go` — add `nowrap` bit on `storeLine`, `SetRowNoWrap`, `RowNoWrap`, extend `SetLine`
- `apps/texelterm/parser/sparse/view_window.go` — add `viewAnchor`, `viewAnchorOffset`, `globalReflowOff`, `autoJumpOnInput`; new `Render`, `CursorToView`, `ViewToCursor`, `ScrollBy`; replace `VisibleRange`-based callers where needed
- `apps/texelterm/parser/sparse/write_window.go` — carry NoWrap through `SetLine`-equivalent paths for IL/DL/scroll shifts
- `apps/texelterm/parser/sparse/terminal.go` — wire decstbm propagation hook, expose toggle getters
- `apps/texelterm/parser/vterm.go` — track `decstbmActive`, call `SetRowNoWrap` on cell writes
- `apps/texelterm/parser/logical_line.go` — add `NoWrap bool` field
- `apps/texelterm/parser/logical_line_persistence.go` — serialize NoWrap as optional trailing field (backward compatible)
- `apps/texelterm/parser/write_ahead_log.go` — extend row-write WAL entry with NoWrap (optional trailing, old entries decode with false)
- `apps/texelterm/parser/vterm_main_screen.go` — thread NoWrap through `sparseLineStoreAdapter` at load/save
- `CLAUDE.md` — replace "No reflow on resize" note with view-side-reflow description

---

## Task 1: Store — NoWrap flag storage

**Files:**
- Modify: `apps/texelterm/parser/sparse/store.go`
- Create: `apps/texelterm/parser/sparse/store_nowrap_test.go`

- [ ] **Step 1: Write failing test for default NoWrap = false**

```go
package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestStore_RowNoWrap_DefaultFalse(t *testing.T) {
	s := NewStore(80)
	s.Set(5, 0, parser.Cell{Rune: 'x'})
	if s.RowNoWrap(5) {
		t.Errorf("new row should default NoWrap=false")
	}
	if s.RowNoWrap(99) {
		t.Errorf("missing row should report NoWrap=false")
	}
}

func TestStore_SetRowNoWrap_StickyOR(t *testing.T) {
	s := NewStore(80)
	s.Set(5, 0, parser.Cell{Rune: 'x'})
	s.SetRowNoWrap(5, true)
	if !s.RowNoWrap(5) {
		t.Errorf("after SetRowNoWrap(true), expected true")
	}
	// Sticky: setting false does not clear
	s.SetRowNoWrap(5, false)
	if !s.RowNoWrap(5) {
		t.Errorf("SetRowNoWrap(false) must not clear sticky flag")
	}
}

func TestStore_SetRowNoWrap_AutoCreateRow(t *testing.T) {
	s := NewStore(80)
	s.SetRowNoWrap(7, true)
	if !s.RowNoWrap(7) {
		t.Errorf("SetRowNoWrap on missing row should create row + set flag")
	}
	// Row should now exist as empty (GetLine returns zero-length slice, not nil)
	got := s.GetLine(7)
	if got == nil {
		t.Errorf("row should be created by SetRowNoWrap")
	}
}

func TestStore_SetLine_CarriesNoWrap(t *testing.T) {
	s := NewStore(80)
	s.Set(5, 0, parser.Cell{Rune: 'a'})
	s.SetRowNoWrap(5, true)
	// Rewriting the row via SetLine (without explicit flag) must preserve NoWrap
	s.SetLine(5, []parser.Cell{{Rune: 'b'}})
	if !s.RowNoWrap(5) {
		t.Errorf("SetLine must preserve NoWrap flag")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/sparse -run TestStore_RowNoWrap -v`
Expected: FAIL with "undefined: RowNoWrap" / "undefined: SetRowNoWrap"

- [ ] **Step 3: Add NoWrap to storeLine + accessors**

In `store.go`, extend `storeLine`:

```go
type storeLine struct {
	cells  []parser.Cell
	nowrap bool
}
```

Add methods (place after `SetLine`):

```go
// RowNoWrap reports whether the row at globalIdx is marked NoWrap.
// Missing rows return false.
func (s *Store) RowNoWrap(globalIdx int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		return false
	}
	return line.nowrap
}

// SetRowNoWrap sets the NoWrap flag on the row at globalIdx. The flag is
// sticky: passing false does NOT clear it. Callers that need to clear must
// replace the row (e.g., via SetLine after row reuse).
//
// If the row does not yet exist, it is created empty so the flag sticks.
func (s *Store) SetRowNoWrap(globalIdx int64, nowrap bool) {
	if !nowrap {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		line = &storeLine{}
		s.lines[globalIdx] = line
	}
	line.nowrap = true
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}
```

Modify `SetLine` to preserve nowrap across re-writes — change the body so that when a line already exists, its `nowrap` is retained:

```go
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
	// nowrap is preserved across SetLine (sticky). Callers that shift rows
	// (IL/DL) must use SetLineWithNoWrap to move the flag explicitly.
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}

// SetLineWithNoWrap replaces both cells and the NoWrap flag at globalIdx.
// Used by IL/DL/scroll shifts that move a row (and its NoWrap semantics)
// from one globalIdx to another.
func (s *Store) SetLineWithNoWrap(globalIdx int64, cells []parser.Cell, nowrap bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		line = &storeLine{}
		s.lines[globalIdx] = line
	}
	line.cells = make([]parser.Cell, len(cells))
	copy(line.cells, cells)
	line.nowrap = nowrap
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}
```

Also update `ClearRange` to drop nowrap when the row is deleted — already satisfied (map delete).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/sparse -run TestStore_ -v`
Expected: PASS (all four tests)

- [ ] **Step 5: Verify existing sparse tests still pass**

Run: `go test ./apps/texelterm/parser/sparse -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/sparse/store.go apps/texelterm/parser/sparse/store_nowrap_test.go
git commit -m "sparse: add per-row NoWrap flag on Store"
```

---

## Task 2: VTerm — DECSTBM tracking and write-time propagation

**Files:**
- Modify: `apps/texelterm/parser/vterm.go`
- Test: `apps/texelterm/parser/vterm_decstbm_nowrap_test.go`

- [ ] **Step 1: Locate existing DECSTBM handler**

Run: `grep -n "scrollTop\|scrollBottom\|DECSTBM\|\\[r" apps/texelterm/parser/vterm.go | head -20`

Read the hits. Identify (a) where scrollTop/scrollBottom are stored, (b) where `CSI r` (DECSTBM) is parsed. Record line numbers here:

- DECSTBM parse site: `vterm.go:<LINE>`
- scrollTop/scrollBottom fields: `vterm.go:<LINE>`

(Fill in during execution. If scrollTop==0 and scrollBottom==height-1 at all times, that's "default margins" per the spec.)

- [ ] **Step 2: Write failing test for decstbmActive propagation**

Create `apps/texelterm/parser/vterm_decstbm_nowrap_test.go`:

```go
package parser

import (
	"testing"
)

// After DECSTBM [2;5r sets non-default margins, any cell written should
// propagate NoWrap=true to the store row.
func TestVTerm_DECSTBM_MarksRowNoWrap(t *testing.T) {
	v := NewVTerm(80, 24)
	// Default margins: nothing marked NoWrap
	v.Write([]byte("hello"))
	cursorGI, _ := v.CursorGlobalIdx()
	if v.mainScreen.RowNoWrap(cursorGI) {
		t.Fatalf("with default margins, row should not be NoWrap")
	}

	// Set non-default scroll region
	v.Write([]byte("\x1b[2;5r"))
	// Cursor home within region, write a char
	v.Write([]byte("\x1b[HX"))

	cursorGI, _ = v.CursorGlobalIdx()
	if !v.mainScreen.RowNoWrap(cursorGI) {
		t.Errorf("after DECSTBM [2;5r, written row should be NoWrap")
	}
}

func TestVTerm_DECSTBM_Reset_ClearsActive(t *testing.T) {
	v := NewVTerm(80, 24)
	v.Write([]byte("\x1b[2;5r"))    // non-default
	v.Write([]byte("\x1b[r"))        // reset to full
	v.Write([]byte("\x1b[HY"))       // write at home
	cursorGI, _ := v.CursorGlobalIdx()
	if v.mainScreen.RowNoWrap(cursorGI) {
		t.Errorf("after DECSTBM reset, subsequent writes should not be NoWrap")
	}
}
```

Note: if `v.mainScreen.RowNoWrap` / `v.CursorGlobalIdx` are not the exact exported surface, adjust to the equivalent test accessors. If they don't exist, add thin accessors in `vterm_main_screen.go` first.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser -run TestVTerm_DECSTBM -v`
Expected: FAIL — the NoWrap propagation is not wired yet.

- [ ] **Step 4: Add decstbmActive and propagation**

In `vterm.go`, add to the VTerm struct (near other scroll-region fields):

```go
// decstbmActive reports whether DECSTBM has set non-default margins,
// i.e., anything other than [0, height-1]. When true, new cell writes
// mark the target row as NoWrap in the sparse store, protecting
// structured content from reflow on resize.
decstbmActive bool
```

In the DECSTBM parse site, after updating scrollTop/scrollBottom:

```go
v.decstbmActive = !(v.scrollTop == 0 && v.scrollBottom == v.height-1)
```

In the cell-write path (wherever `store.Set(cursorGlobalIdx, cursorX, cell)` is called — likely in `sparseLineStoreAdapter` or the VTerm cell-emit path):

```go
store.Set(globalIdx, col, cell)
if decstbmActive {
    store.SetRowNoWrap(globalIdx, true)
}
```

(If `decstbmActive` lives on VTerm but the adapter writes into the store, thread it through. The simplest path is a bool arg on the adapter's write entrypoint; if that's noisy, stash it on the adapter struct and have VTerm update it whenever `decstbmActive` changes.)

- [ ] **Step 5: Also propagate on height resize**

When height changes, `scrollBottom` may be auto-adjusted to `newHeight-1`. Recompute `decstbmActive` after any height change in `Resize`:

```go
v.decstbmActive = !(v.scrollTop == 0 && v.scrollBottom == v.height-1)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser -run TestVTerm_DECSTBM -v`
Expected: PASS

- [ ] **Step 7: Run full parser tests for regressions**

Run: `go test ./apps/texelterm/parser/...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/vterm_main_screen.go apps/texelterm/parser/vterm_decstbm_nowrap_test.go
git commit -m "vterm: track DECSTBM active, propagate NoWrap on writes"
```

---

## Task 3: Store — row-shift helpers for IL/DL

**Files:**
- Modify: `apps/texelterm/parser/sparse/write_window.go` (or wherever IL/DL/scroll shifts live)
- Test: `apps/texelterm/parser/sparse/store_nowrap_shift_test.go`

- [ ] **Step 1: Locate IL/DL/NewlineInRegion implementations**

Run: `grep -rn "RequestLineInsert\|RequestLineDelete\|NewlineInRegion\|InsertLines\|DeleteLines" apps/texelterm/parser/sparse/`

Record the call sites that move cells between globalIdx rows — these are the places that must carry NoWrap with the cells.

- [ ] **Step 2: Write failing test for NoWrap movement under IL**

Create `apps/texelterm/parser/sparse/store_nowrap_shift_test.go`:

```go
package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// When a row is shifted within a scroll region (via IL/DL/NewlineInRegion),
// its NoWrap flag must travel with its cells, not stay on the old globalIdx.
func TestStore_RowShift_NoWrapFollows(t *testing.T) {
	s := NewStore(80)
	s.Set(10, 0, parser.Cell{Rune: 'A'})
	s.SetRowNoWrap(10, true)

	// Simulate "move row at 10 to 11" via SetLineWithNoWrap + clear
	cells := s.GetLine(10)
	nw := s.RowNoWrap(10)
	s.SetLineWithNoWrap(11, cells, nw)
	s.ClearRange(10, 10)

	if s.RowNoWrap(11) != true {
		t.Errorf("NoWrap must follow cells to new row")
	}
	if s.RowNoWrap(10) != false {
		t.Errorf("old row should be cleared (NoWrap=false)")
	}
}
```

- [ ] **Step 3: Run test to verify it passes**

If `SetLineWithNoWrap` was added in Task 1, this test is already satisfied. Run:

Run: `go test ./apps/texelterm/parser/sparse -run TestStore_RowShift -v`
Expected: PASS (sanity check)

- [ ] **Step 4: Audit IL/DL/scroll-region code to use the shift helper**

Review the sites found in Step 1. For each row-move operation, replace `store.SetLine(dst, cells)` with:

```go
nw := store.RowNoWrap(src)
store.SetLineWithNoWrap(dst, cells, nw)
```

For rows being cleared (blanked by the shift), use `ClearRange(idx, idx)` so NoWrap is dropped.

- [ ] **Step 5: Run all sparse + parser tests**

Run: `go test ./apps/texelterm/parser/... ./apps/texelterm/parser/sparse/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/sparse/
git commit -m "sparse: preserve NoWrap flag across IL/DL row shifts"
```

---

## Task 4: ViewWindow — chain walker (reflow primitive)

**Files:**
- Create: `apps/texelterm/parser/sparse/view_reflow.go`
- Create: `apps/texelterm/parser/sparse/view_reflow_test.go`

- [ ] **Step 1: Write failing tests for chain walking**

Create `apps/texelterm/parser/sparse/view_reflow_test.go`:

```go
package sparse

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func fillRow(s *Store, gi int64, text string, wrapped bool) {
	cells := make([]parser.Cell, len(text))
	for i, r := range text {
		cells[i] = parser.Cell{Rune: r}
	}
	if wrapped && len(cells) > 0 {
		cells[len(cells)-1].Wrapped = true
	}
	s.SetLine(gi, cells)
}

func TestChainWalk_SingleRowNoWrap(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 0, "hello", false)
	end, nowrap := walkChain(s, 0, 4*24)
	if end != 0 {
		t.Errorf("single non-wrapped row: end=%d want 0", end)
	}
	if nowrap {
		t.Errorf("row without NoWrap flag: expected nowrap=false")
	}
}

func TestChainWalk_TwoRowChain(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 5, "0123456789", true)
	fillRow(s, 6, "abc", false)
	end, _ := walkChain(s, 5, 4*24)
	if end != 6 {
		t.Errorf("chain end=%d want 6", end)
	}
}

func TestChainWalk_NoWrapPropagation(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 5, "0123456789", true)
	fillRow(s, 6, "abc", false)
	s.SetRowNoWrap(6, true) // middle/last row NoWrap → whole chain NoWrap
	_, nowrap := walkChain(s, 5, 4*24)
	if !nowrap {
		t.Errorf("any NoWrap in chain should propagate")
	}
}

func TestChainWalk_MalformedChainStopsAtGap(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 5, "0123456789", true) // says "wrapped" but row 6 missing
	end, _ := walkChain(s, 5, 4*24)
	if end != 5 {
		t.Errorf("malformed chain should stop at gap; end=%d want 5", end)
	}
}

func TestChainWalk_CapOnUnboundedChain(t *testing.T) {
	s := NewStore(10)
	// 100 wrapped rows in a row
	for gi := int64(0); gi < 100; gi++ {
		fillRow(s, gi, "0123456789", true) // all wrapped
	}
	// Cap = 20
	end, _ := walkChain(s, 0, 20)
	if end-0+1 > 20 {
		t.Errorf("chain walk exceeded cap: end=%d", end)
	}
}

func TestReflowChain_SingleLogical(t *testing.T) {
	s := NewStore(10)
	fillRow(s, 0, "0123456789", true)
	fillRow(s, 1, "abcde", false)
	// Chain at width 5 → expect 3 rows: "01234", "56789", "abcde"
	rows := reflowChain(s, 0, 1, 5)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	got := strings.TrimRight(cellsToString(rows[0]), " ")
	if got != "01234" {
		t.Errorf("row 0 = %q want %q", got, "01234")
	}
	got = strings.TrimRight(cellsToString(rows[2]), " ")
	if got != "abcde" {
		t.Errorf("row 2 = %q want %q", got, "abcde")
	}
}

// Test helper
func cellsToString(cells []parser.Cell) string {
	b := strings.Builder{}
	for _, c := range cells {
		if c.Rune == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/sparse -run TestChainWalk -v`
Expected: FAIL with "undefined: walkChain" / "undefined: reflowChain"

- [ ] **Step 3: Implement chain walker + reflow**

Create `apps/texelterm/parser/sparse/view_reflow.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import "github.com/framegrace/texelation/apps/texelterm/parser"

// walkChain returns the end globalIdx of the Wrapped chain starting at
// startGI, plus whether any row in the chain is marked NoWrap (chain
// propagation). Walks at most cap rows to bound pathological inputs.
//
// A chain is defined as a sequence of globalIdxs where every row except
// the last has its final cell Wrapped=true and the next row exists in
// the store. A missing row terminates the chain at the current idx.
func walkChain(s *Store, startGI int64, cap int) (end int64, nowrap bool) {
	end = startGI
	nowrap = s.RowNoWrap(startGI)
	for steps := 0; steps < cap; steps++ {
		cells := s.GetLine(end)
		if len(cells) == 0 || !cells[len(cells)-1].Wrapped {
			return end, nowrap
		}
		next := end + 1
		nextCells := s.GetLine(next)
		if nextCells == nil {
			return end, nowrap
		}
		end = next
		if s.RowNoWrap(end) {
			nowrap = true
		}
	}
	return end, nowrap
}

// reflowChain returns the reflowed physical rows of the chain [startGI, endGI]
// at viewWidth. Each returned slice has length ≤ viewWidth. Trailing empty
// rows are NOT padded — the caller pads the viewport as needed.
//
// Concatenates all cells in the chain, then slices at viewWidth.
func reflowChain(s *Store, startGI, endGI int64, viewWidth int) [][]parser.Cell {
	if viewWidth <= 0 {
		return nil
	}
	// Concatenate
	var logical []parser.Cell
	for gi := startGI; gi <= endGI; gi++ {
		logical = append(logical, s.GetLine(gi)...)
	}
	if len(logical) == 0 {
		return [][]parser.Cell{nil}
	}
	var rows [][]parser.Cell
	for off := 0; off < len(logical); off += viewWidth {
		end := off + viewWidth
		if end > len(logical) {
			end = len(logical)
		}
		row := make([]parser.Cell, end-off)
		copy(row, logical[off:end])
		rows = append(rows, row)
	}
	return rows
}

// clipRow returns cells truncated or padded to viewWidth. Used for NoWrap
// rows that render 1:1.
func clipRow(cells []parser.Cell, viewWidth int) []parser.Cell {
	out := make([]parser.Cell, viewWidth)
	for i := 0; i < viewWidth; i++ {
		if i < len(cells) {
			out[i] = cells[i]
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/sparse -run "TestChainWalk|TestReflowChain" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_reflow.go apps/texelterm/parser/sparse/view_reflow_test.go
git commit -m "sparse: add chain walker and reflow primitives"
```

---

## Task 5: ViewWindow — view anchor fields and Render()

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Create: `apps/texelterm/parser/sparse/view_window_reflow_test.go`

- [ ] **Step 1: Write failing test for Render at width 40**

Create `apps/texelterm/parser/sparse/view_window_reflow_test.go`:

```go
package sparse

import (
	"strings"
	"testing"
)

// A chain that wraps at width 80 should reflow down to 2 rows at width 40.
func TestViewWindow_Render_ReflowsOnNarrow(t *testing.T) {
	s := NewStore(80)
	// Logical "0..79 80..89" — single chain of 2 physical rows at width 80
	text80 := strings.Repeat("0123456789", 8)
	fillRow(s, 0, text80, true)
	fillRow(s, 1, "abcde", false)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)
	out := vw.Render(s)

	// Expect 3 rows: 2 halves of 80-char line + "abcde" (reflowed into width 40).
	if len(out) != 5 {
		t.Fatalf("Render should return viewHeight=5 rows, got %d", len(out))
	}
	if !strings.HasPrefix(cellsToString(out[0]), "01234") {
		t.Errorf("row 0 unexpected: %q", cellsToString(out[0]))
	}
	if !strings.HasPrefix(cellsToString(out[2]), "abcde") {
		t.Errorf("row 2 unexpected: %q", cellsToString(out[2]))
	}
}

func TestViewWindow_Render_NoWrapChainStays1to1(t *testing.T) {
	s := NewStore(80)
	text80 := strings.Repeat("0123456789", 8)
	fillRow(s, 0, text80, true)
	fillRow(s, 1, "abcde", false)
	s.SetRowNoWrap(0, true) // chain becomes NoWrap

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)
	out := vw.Render(s)

	// NoWrap: row 0 clips to 40, row 1 clips to 40. Only 2 content rows.
	if !strings.HasPrefix(cellsToString(out[0]), "01234567890123456789") {
		t.Errorf("NoWrap row 0 should be clipped 1:1, got %q", cellsToString(out[0]))
	}
	if !strings.HasPrefix(cellsToString(out[1]), "abcde") {
		t.Errorf("NoWrap row 1 = %q", cellsToString(out[1]))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/sparse -run TestViewWindow_Render -v`
Expected: FAIL — `SetViewAnchor` / `Render` undefined.

- [ ] **Step 3: Add anchor fields + Render**

In `view_window.go`, extend struct:

```go
type ViewWindow struct {
	mu         sync.Mutex
	width      int
	height     int
	viewBottom int64
	autoFollow bool

	// Reflow state (2026-04-16 resize-reflow)
	viewAnchor       int64 // globalIdx of first chain at view top
	viewAnchorOffset int   // view-row offset within that chain (when reflowed)
	globalReflowOff  bool  // user toggle: force all reflowable rows to 1:1
	autoJumpOnInput  bool  // default true: user input snaps to live edge
}
```

In `NewViewWindow`, initialize:

```go
return &ViewWindow{
	width:           width,
	height:          height,
	viewBottom:      int64(height - 1),
	autoFollow:      true,
	viewAnchor:      0,
	viewAnchorOffset: 0,
	autoJumpOnInput: true,
}
```

Add methods:

```go
// SetViewAnchor pins the view to start at (globalIdx, offset-within-chain).
// Used by tests and by scroll operations.
func (v *ViewWindow) SetViewAnchor(globalIdx int64, offset int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewAnchor = globalIdx
	v.viewAnchorOffset = offset
}

// SetGlobalReflowOff flips the user toggle. NoWrap rows are unaffected
// (NoWrap is always 1:1). When true, reflowable chains are rendered 1:1.
func (v *ViewWindow) SetGlobalReflowOff(off bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.globalReflowOff = off
}

// Render produces a []row of exactly v.height rows from the store, starting
// at viewAnchor/viewAnchorOffset. Empty rows are zero-valued cell slices
// of length v.width. Callers overlay the cursor separately.
func (v *ViewWindow) Render(s *Store) [][]parser.Cell {
	v.mu.Lock()
	height, width := v.height, v.width
	anchor, offset := v.viewAnchor, v.viewAnchorOffset
	reflowOff := v.globalReflowOff
	v.mu.Unlock()

	out := make([][]parser.Cell, height)
	emitted := 0
	gi := anchor
	cap := 4 * height
	for emitted < height {
		if s.GetLine(gi) == nil && !s.RowNoWrap(gi) {
			// past content — pad with blanks
			break
		}
		end, nowrap := walkChain(s, gi, cap)
		if reflowOff {
			nowrap = true
		}
		var rows [][]parser.Cell
		if nowrap {
			for r := gi; r <= end; r++ {
				rows = append(rows, clipRow(s.GetLine(r), width))
			}
		} else {
			rows = reflowChain(s, gi, end, width)
			for i, row := range rows {
				rows[i] = clipRow(row, width)
			}
		}
		// Skip viewAnchorOffset on the first chain only
		startAt := 0
		if gi == anchor {
			startAt = offset
			if startAt > len(rows) {
				startAt = len(rows)
			}
		}
		for i := startAt; i < len(rows) && emitted < height; i++ {
			out[emitted] = rows[i]
			emitted++
		}
		gi = end + 1
	}
	// Pad remaining rows with blanks of width w
	for emitted < height {
		out[emitted] = make([]parser.Cell, width)
		emitted++
	}
	return out
}
```

Add the `parser` import if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/sparse -run TestViewWindow_Render -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/view_window_reflow_test.go
git commit -m "sparse: add ViewWindow.Render with chain-based reflow"
```

---

## Task 6: Cursor mapping — CursorToView and ViewToCursor

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Extend: `apps/texelterm/parser/sparse/view_window_reflow_test.go`

- [ ] **Step 1: Write failing test for round-trip cursor mapping**

Append to `view_window_reflow_test.go`:

```go
func TestCursor_RoundTrip_ReflowedChain(t *testing.T) {
	s := NewStore(80)
	text80 := strings.Repeat("0123456789", 8)
	fillRow(s, 0, text80, true)
	fillRow(s, 1, "abcde", false)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)

	// Cursor at (globalIdx=0, col=45) — logically the 45th char of chain.
	// At width 40 that's view row 1, col 5.
	vr, vc, ok := vw.CursorToView(s, 0, 45)
	if !ok || vr != 1 || vc != 5 {
		t.Errorf("CursorToView(0,45)=(%d,%d,%v) want (1,5,true)", vr, vc, ok)
	}
	gi, col, ok := vw.ViewToCursor(s, 1, 5)
	if !ok || gi != 0 || col != 45 {
		t.Errorf("ViewToCursor(1,5)=(%d,%d,%v) want (0,45,true)", gi, col, ok)
	}
}

func TestCursor_RoundTrip_NoWrapChain(t *testing.T) {
	s := NewStore(80)
	fillRow(s, 0, "hello", false)
	s.SetRowNoWrap(0, true)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)

	vr, vc, ok := vw.CursorToView(s, 0, 3)
	if !ok || vr != 0 || vc != 3 {
		t.Errorf("NoWrap CursorToView(0,3)=(%d,%d,%v) want (0,3,true)", vr, vc, ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/sparse -run TestCursor_RoundTrip -v`
Expected: FAIL (undefined)

- [ ] **Step 3: Implement CursorToView and ViewToCursor**

Add to `view_window.go`:

```go
// CursorToView maps a store (globalIdx, col) to (viewRow, viewCol) within
// the current view. Returns ok=false if the cursor is not inside the visible
// chain walk.
func (v *ViewWindow) CursorToView(s *Store, cursorGI int64, cursorCol int) (viewRow, viewCol int, ok bool) {
	v.mu.Lock()
	height, width := v.height, v.width
	anchor, offset := v.viewAnchor, v.viewAnchorOffset
	reflowOff := v.globalReflowOff
	v.mu.Unlock()

	emitted := 0
	gi := anchor
	cap := 4 * height
	for emitted < height {
		end, nowrap := walkChain(s, gi, cap)
		if reflowOff {
			nowrap = true
		}
		if cursorGI >= gi && cursorGI <= end {
			// In this chain.
			if nowrap {
				rowInChain := int(cursorGI - gi)
				startAt := 0
				if gi == anchor {
					startAt = offset
				}
				if rowInChain < startAt {
					return 0, 0, false
				}
				vr := emitted + (rowInChain - startAt)
				if vr >= height {
					return 0, 0, false
				}
				vc := cursorCol
				if vc >= width {
					vc = width - 1
				}
				return vr, vc, true
			}
			// Reflowed: compute logical column.
			logicalCol := 0
			for r := gi; r < cursorGI; r++ {
				logicalCol += len(s.GetLine(r))
			}
			logicalCol += cursorCol
			rowInChain := logicalCol / width
			colInRow := logicalCol % width
			startAt := 0
			if gi == anchor {
				startAt = offset
			}
			if rowInChain < startAt {
				return 0, 0, false
			}
			vr := emitted + (rowInChain - startAt)
			if vr >= height {
				return 0, 0, false
			}
			return vr, colInRow, true
		}
		// Advance past chain.
		chainRows := int(end - gi + 1)
		if !nowrap {
			// count reflowed rows
			chainRows = 0
			for r := gi; r <= end; r++ {
				chainRows += len(s.GetLine(r))
			}
			if chainRows == 0 {
				chainRows = 1
			} else {
				chainRows = (chainRows + width - 1) / width
			}
		}
		startAt := 0
		if gi == anchor {
			startAt = offset
		}
		emitted += chainRows - startAt
		gi = end + 1
		if s.GetLine(gi) == nil {
			break
		}
	}
	return 0, 0, false
}

// ViewToCursor maps (viewRow, viewCol) to (globalIdx, col) in the store.
// If viewRow is past content end, returns a fabricated "blank area" result
// (globalIdx beyond writeTop, col=viewCol).
func (v *ViewWindow) ViewToCursor(s *Store, viewRow, viewCol int) (globalIdx int64, col int, ok bool) {
	v.mu.Lock()
	height, width := v.height, v.width
	anchor, offset := v.viewAnchor, v.viewAnchorOffset
	reflowOff := v.globalReflowOff
	v.mu.Unlock()

	if viewRow < 0 || viewRow >= height {
		return 0, 0, false
	}

	emitted := 0
	gi := anchor
	cap := 4 * height
	for emitted < height {
		if s.GetLine(gi) == nil {
			break
		}
		end, nowrap := walkChain(s, gi, cap)
		if reflowOff {
			nowrap = true
		}
		var chainRows int
		if nowrap {
			chainRows = int(end - gi + 1)
		} else {
			total := 0
			for r := gi; r <= end; r++ {
				total += len(s.GetLine(r))
			}
			if total == 0 {
				chainRows = 1
			} else {
				chainRows = (total + width - 1) / width
			}
		}
		startAt := 0
		if gi == anchor {
			startAt = offset
		}
		rowsFromThisChain := chainRows - startAt
		if viewRow < emitted+rowsFromThisChain {
			rowInChain := (viewRow - emitted) + startAt
			if nowrap {
				return gi + int64(rowInChain), viewCol, true
			}
			// Walk cells to find (gi, col)
			logicalCol := rowInChain*width + viewCol
			for r := gi; r <= end; r++ {
				rowLen := len(s.GetLine(r))
				if logicalCol < rowLen {
					return r, logicalCol, true
				}
				logicalCol -= rowLen
			}
			// viewCol past end of logical — return at end of chain
			return end, len(s.GetLine(end)), true
		}
		emitted += rowsFromThisChain
		gi = end + 1
	}
	// Past content.
	return gi + int64(viewRow-emitted), viewCol, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./apps/texelterm/parser/sparse -run TestCursor_RoundTrip -v`
Expected: PASS

- [ ] **Step 5: Run all sparse tests**

Run: `go test ./apps/texelterm/parser/sparse`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/view_window_reflow_test.go
git commit -m "sparse: add CursorToView / ViewToCursor mapping"
```

---

## Task 7: Live-mode anchor recompute + integrate into render pipeline

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Modify: `apps/texelterm/parser/sparse/terminal.go`

- [ ] **Step 1: Write failing test for live-mode anchor tracks writeBottom**

Append to `view_window_reflow_test.go`:

```go
func TestViewWindow_LiveMode_AnchorTracksCursor(t *testing.T) {
	s := NewStore(80)
	// Fill 10 short lines
	for gi := int64(0); gi < 10; gi++ {
		fillRow(s, gi, "x", false)
	}

	vw := NewViewWindow(80, 3) // height=3
	vw.RecomputeLiveAnchor(s, /*cursorGI=*/9, /*cursorCol=*/0)
	vr, vc, ok := vw.CursorToView(s, 9, 0)
	if !ok || vr != 2 {
		t.Errorf("live anchor: cursor should be on bottom row; got (%d,%d,%v)", vr, vc, ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/sparse -run TestViewWindow_LiveMode -v`
Expected: FAIL (undefined: RecomputeLiveAnchor)

- [ ] **Step 3: Implement live-mode recompute**

Add to `view_window.go`:

```go
// RecomputeLiveAnchor sets viewAnchor so that (cursorGI, cursorCol) is
// visible on the last view row when autoFollow is active. Called from
// Render-time in live mode; cheap.
//
// Algorithm: walk chains backward from the cursor's chain, accumulating
// view rows, until we've gathered v.height rows or run out of content.
func (v *ViewWindow) RecomputeLiveAnchor(s *Store, cursorGI int64, cursorCol int) {
	v.mu.Lock()
	height, width := v.height, v.width
	reflowOff := v.globalReflowOff
	if !v.autoFollow {
		v.mu.Unlock()
		return
	}
	v.mu.Unlock()

	// Find the chain containing the cursor.
	chainStart := cursorGI
	for chainStart > 0 {
		prev := s.GetLine(chainStart - 1)
		if len(prev) == 0 || !prev[len(prev)-1].Wrapped {
			break
		}
		chainStart--
	}
	// Walk backward, counting reflowed rows per chain.
	target := height
	gi := chainStart
	offset := 0
	for target > 0 && gi >= 0 {
		end, nowrap := walkChain(s, gi, 4*height)
		if reflowOff {
			nowrap = true
		}
		var chainRows int
		if nowrap {
			chainRows = int(end - gi + 1)
		} else {
			total := 0
			for r := gi; r <= end; r++ {
				total += len(s.GetLine(r))
			}
			if total == 0 {
				chainRows = 1
			} else {
				chainRows = (total + width - 1) / width
			}
		}
		if chainRows >= target {
			offset = chainRows - target
			v.mu.Lock()
			v.viewAnchor = gi
			v.viewAnchorOffset = offset
			v.mu.Unlock()
			return
		}
		target -= chainRows
		if gi == 0 {
			break
		}
		// Walk back to the previous chain's start.
		gi--
		for gi > 0 {
			prev := s.GetLine(gi - 1)
			if len(prev) == 0 || !prev[len(prev)-1].Wrapped {
				break
			}
			gi--
		}
	}
	// Not enough content; anchor at top.
	v.mu.Lock()
	v.viewAnchor = 0
	v.viewAnchorOffset = 0
	v.mu.Unlock()
}
```

- [ ] **Step 4: Wire into Terminal.Render (or equivalent)**

In `terminal.go`, find the render entrypoint. Before it produces a grid, call `ViewWindow.RecomputeLiveAnchor` using the current cursor globalIdx/col. Produce the grid via `ViewWindow.Render(store)`. If a legacy `VisibleRange`/projection path exists, gate it behind a fallback flag for now.

- [ ] **Step 5: Run all sparse tests**

Run: `go test ./apps/texelterm/parser/sparse`
Expected: PASS

- [ ] **Step 6: Run full texelterm tests**

Run: `go test ./apps/texelterm/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/terminal.go apps/texelterm/parser/sparse/view_window_reflow_test.go
git commit -m "sparse: live-mode anchor recompute on render"
```

---

## Task 8: VTerm resize uses new Render path + cursor mapping

**Files:**
- Modify: `apps/texelterm/parser/vterm.go` (Grid, Resize, cursor accessors)
- Modify: `apps/texelterm/parser/vterm_main_screen.go`

- [ ] **Step 1: Write failing end-to-end reflow test**

Create `apps/texelterm/parser/vterm_reflow_test.go`:

```go
package parser

import (
	"strings"
	"testing"
)

// Fill a line longer than 80, resize narrower, verify it reflows.
func TestVTerm_Reflow_WidenAndNarrow(t *testing.T) {
	v := NewVTerm(80, 24)
	long := strings.Repeat("abcdefghij", 12) // 120 chars
	v.Write([]byte(long))

	// At width 80, line wraps into 2 physical rows.
	grid := v.Grid()
	row0 := cellsToStringParser(grid[0])
	if !strings.HasPrefix(row0, "abcdefghij") {
		t.Fatalf("row 0 at width 80: %q", row0)
	}

	// Narrow to 40 — same logical line should span 3 rows.
	v.Resize(40, 24)
	grid = v.Grid()
	// First three rows should contain "abcdef...".
	joined := cellsToStringParser(grid[0]) + cellsToStringParser(grid[1]) + cellsToStringParser(grid[2])
	if !strings.Contains(joined, long) {
		t.Errorf("after narrow resize, content did not reflow; joined=%q", joined)
	}

	// Widen to 120 — single row holds it all.
	v.Resize(120, 24)
	grid = v.Grid()
	row0 = cellsToStringParser(grid[0])
	if !strings.HasPrefix(row0, long) {
		t.Errorf("after widen, single row should hold full line; got %q", row0)
	}
}

func TestVTerm_Reflow_NoWrapRowsSurviveResize(t *testing.T) {
	v := NewVTerm(80, 24)
	v.Write([]byte("\x1b[2;5r"))       // non-default DECSTBM
	v.Write([]byte("\x1b[HABCDE"))      // write "ABCDE" at home within region
	v.Write([]byte("\x1b[r"))            // reset margins
	// Resize narrower; NoWrap row must NOT reflow.
	v.Resize(3, 24)
	grid := v.Grid()
	if !strings.HasPrefix(cellsToStringParser(grid[0]), "ABC") {
		t.Errorf("NoWrap row should clip at width 3, got %q", cellsToStringParser(grid[0]))
	}
}

func cellsToStringParser(cells []Cell) string {
	b := strings.Builder{}
	for _, c := range cells {
		if c.Rune == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}
```

- [ ] **Step 2: Run test**

Run: `go test ./apps/texelterm/parser -run TestVTerm_Reflow -v`

Either PASS (if Task 7 already wired through), or FAIL with a specific grid mismatch — in which case, find the Grid accessor and route it through `ViewWindow.Render` instead of the legacy projection.

- [ ] **Step 3: Route Grid() through ViewWindow.Render**

In `vterm.go` / `vterm_main_screen.go`, locate `Grid()` — if it reads from the sparse store via `VisibleRange` + `GetLine`, replace with:

```go
v.mainScreen.ViewWindow().RecomputeLiveAnchor(v.mainScreen.Store(), cursorGI, cursorCol)
grid := v.mainScreen.ViewWindow().Render(v.mainScreen.Store())
```

(Exact wrapper depends on the current MainScreen interface — adjust.)

- [ ] **Step 4: Route cursor position reads through ViewWindow**

In whatever place VTerm reports cursor X/Y to the renderer, replace store-coordinate math with `ViewWindow.CursorToView(store, cursorGI, cursorCol)`.

In the cursor-addressing path (CSI H / CSI f / CSI A etc.), replace "view row → store row" math with `ViewToCursor`. Specifically, the parser should take `(viewRow, viewCol)` and set `cursorGlobalIdx, cursorCol` via `ViewToCursor`.

- [ ] **Step 5: Run tests**

Run: `go test ./apps/texelterm/parser -run TestVTerm_Reflow -v`
Expected: PASS

- [ ] **Step 6: Run full texelterm test suite**

Run: `go test ./apps/texelterm/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/vterm_main_screen.go apps/texelterm/parser/vterm_reflow_test.go
git commit -m "vterm: route Grid and cursor mapping through ViewWindow"
```

---

## Task 9: Persistence — LogicalLine.NoWrap field

**Files:**
- Modify: `apps/texelterm/parser/logical_line.go`
- Modify: `apps/texelterm/parser/logical_line_persistence.go`
- Modify: `apps/texelterm/parser/vterm_main_screen.go` (sparseLineStoreAdapter)

- [ ] **Step 1: Write failing test for .lhist round-trip of NoWrap**

Run: `grep -n "TestLhist\|TestLogicalLine" apps/texelterm/parser/logical_line_persistence*.go` to find existing serialization tests.

Add to the persistence test file (or create `logical_line_nowrap_test.go`):

```go
func TestLogicalLine_NoWrap_RoundTrip(t *testing.T) {
	ll := NewLogicalLineFromCells([]Cell{{Rune: 'a'}})
	ll.NoWrap = true
	buf := encodeLogicalLine(ll) // adjust to the real encoder name
	got, err := decodeLogicalLine(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NoWrap {
		t.Errorf("NoWrap flag did not survive round-trip")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser -run TestLogicalLine_NoWrap -v`
Expected: FAIL

- [ ] **Step 3: Add NoWrap to LogicalLine**

In `logical_line.go`, add field:

```go
type LogicalLine struct {
	Cells        []Cell
	FixedWidth   int
	Overlay      []Cell
	OverlayWidth int
	Synthetic    bool

	// NoWrap: line should render 1:1 (clip/pad, not reflow). Derived from
	// per-row NoWrap in the sparse store via chain propagation at save time.
	NoWrap bool
}
```

Update `Clone()` to carry NoWrap.

- [ ] **Step 4: Extend serialization format with optional trailing field**

In `logical_line_persistence.go`, locate the encode/decode pair. Add NoWrap as an optional trailing byte:

```go
// Encode: after existing fields, append one more byte: 0 (reflowable) or 1 (NoWrap).
// Decode: if trailing byte is present, read it; if absent (old file), default false.
```

Exact serialization layout depends on the current scheme. If it uses length-prefixed fields or a version byte, prefer appending at the end guarded by a remaining-bytes check:

```go
// after existing Decode steps:
if buf.Len() > 0 {
    nw, err := buf.ReadByte()
    if err == nil && nw != 0 {
        ll.NoWrap = true
    }
}
```

- [ ] **Step 5: Update save and load bridging (sparseLineStoreAdapter)**

At save time: for each logical line being built from a chain, propagate NoWrap:

```go
ll.NoWrap = chainIsNoWrap // computed via walkChain in Task 4
```

At load time: when restoring rows into the store, call `store.SetRowNoWrap(gi, true)` on the first row of the chain (propagation ensures the whole chain looks NoWrap to the renderer).

- [ ] **Step 6: Run tests**

Run: `go test ./apps/texelterm/parser -run TestLogicalLine -v`
Expected: PASS

- [ ] **Step 7: Run full parser tests**

Run: `go test ./apps/texelterm/...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add apps/texelterm/parser/logical_line.go apps/texelterm/parser/logical_line_persistence.go apps/texelterm/parser/vterm_main_screen.go apps/texelterm/parser/logical_line_nowrap_test.go
git commit -m "parser: persist NoWrap on LogicalLine (backward-compatible)"
```

---

## Task 10: WAL — row-write entry carries NoWrap

**Files:**
- Modify: `apps/texelterm/parser/write_ahead_log.go`
- Test: `apps/texelterm/parser/write_ahead_log_nowrap_test.go`

- [ ] **Step 1: Locate the row-write WAL entry type**

Run: `grep -n "type.*Entry\|row\|Row" apps/texelterm/parser/write_ahead_log.go | head -30`

Identify the struct that records "write row cells at globalIdx". Record the name here: `<EntryStruct>`.

- [ ] **Step 2: Write failing test for WAL NoWrap round-trip**

Create `apps/texelterm/parser/write_ahead_log_nowrap_test.go`:

```go
func TestWAL_RowWrite_NoWrapRoundTrip(t *testing.T) {
	// Construct entry with NoWrap=true, encode, decode, assert.
	// Also: decode an old-format entry (without trailing byte), assert NoWrap=false.
	// (Fill in with real WAL constructor + encoder names.)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser -run TestWAL_RowWrite_NoWrap -v`
Expected: FAIL

- [ ] **Step 4: Extend WAL row-write entry**

Add `NoWrap bool` to the entry struct. In the encoder, append 1 byte after the existing fields. In the decoder, if bytes remain after existing fields, read it; otherwise default to false.

- [ ] **Step 5: Update the write path**

Wherever the WAL records a row write, pass in `store.RowNoWrap(gi)`:

```go
wal.AppendRowWrite(gi, cells, store.RowNoWrap(gi))
```

- [ ] **Step 6: Update replay**

When replaying WAL entries on crash recovery, call `store.SetRowNoWrap(gi, entry.NoWrap)` after `store.SetLine`.

- [ ] **Step 7: Run WAL tests**

Run: `go test ./apps/texelterm/parser -run "TestWAL|TestRecovery" -v`
Expected: PASS

- [ ] **Step 8: Run full texelterm tests**

Run: `go test ./apps/texelterm/...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add apps/texelterm/parser/write_ahead_log.go apps/texelterm/parser/write_ahead_log_nowrap_test.go
git commit -m "wal: carry NoWrap through row-write entries (backward-compatible)"
```

---

## Task 11: Scroll operations use new ViewWindow API

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Modify: callers of `ScrollUp`/`ScrollDown`/`ScrollToBottom` (search the tree)

- [ ] **Step 1: Audit scroll callers**

Run: `grep -rn "ScrollUp\|ScrollDown\|ScrollToBottom\|OnWriteBottomChanged" apps/texelterm/ client/ internal/`

Record the user-facing scroll entrypoints. Usually they translate user keys (PageUp etc.) into `ScrollUp(n)` calls on the ViewWindow.

- [ ] **Step 2: Write failing test for ScrollBy adjusting viewAnchor**

```go
func TestViewWindow_ScrollBy_MovesAnchor(t *testing.T) {
	s := NewStore(80)
	for gi := int64(0); gi < 20; gi++ {
		fillRow(s, gi, "x", false)
	}
	vw := NewViewWindow(80, 5)
	vw.SetViewAnchor(15, 0)
	vw.ScrollBy(s, -3) // scroll up 3
	gi, off := vw.Anchor()
	if gi != 12 || off != 0 {
		t.Errorf("ScrollBy(-3) anchor=(%d,%d) want (12,0)", gi, off)
	}
}
```

- [ ] **Step 3: Implement ScrollBy + Anchor accessor**

Add to `view_window.go`:

```go
// Anchor returns the current (globalIdx, offset-in-chain) anchor.
func (v *ViewWindow) Anchor() (int64, int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.viewAnchor, v.viewAnchorOffset
}

// ScrollBy moves the view anchor by dRows (negative = up into history).
// Detaches from autoFollow. Clamps to [0, writeTop].
func (v *ViewWindow) ScrollBy(s *Store, dRows int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoFollow = false
	// Simple implementation: treat dRows as chain-row deltas for now.
	// (For width-accurate scroll, a future revision can walk reflowed rows.)
	v.viewAnchor += int64(dRows)
	if v.viewAnchor < 0 {
		v.viewAnchor = 0
	}
	v.viewAnchorOffset = 0
}
```

Keep existing `ScrollUp`/`ScrollDown` as compatibility shims that call `ScrollBy`.

- [ ] **Step 4: Run test**

Run: `go test ./apps/texelterm/parser/sparse -run TestViewWindow_ScrollBy -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go
git commit -m "sparse: add ScrollBy / Anchor for reflow-aware scrolling"
```

---

## Task 12: User toggles — autoJumpOnInput and globalReflowOff

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Modify: `apps/texelterm/parser/sparse/terminal.go`

- [ ] **Step 1: Write failing test for autoJumpOnInput configurability**

```go
func TestViewWindow_AutoJumpOnInput_ConfigurableOff(t *testing.T) {
	vw := NewViewWindow(80, 5)
	vw.SetAutoJumpOnInput(false)
	vw.SetViewAnchor(3, 0) // scrolled back
	vw.OnInput(99)          // simulate user input at writeBottom=99
	gi, _ := vw.Anchor()
	if gi != 3 {
		t.Errorf("autoJumpOnInput=false: anchor should stay at 3, got %d", gi)
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `go test ./apps/texelterm/parser/sparse -run TestViewWindow_AutoJumpOnInput -v`
Expected: FAIL

- [ ] **Step 3: Implement SetAutoJumpOnInput and gate OnInput**

In `view_window.go`:

```go
func (v *ViewWindow) SetAutoJumpOnInput(enabled bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoJumpOnInput = enabled
}

// OnInput is called when the user types. If autoJumpOnInput is true,
// snaps to live edge; otherwise does nothing (user stays scrolled back).
func (v *ViewWindow) OnInput(writeBottom int64) {
	v.mu.Lock()
	if !v.autoJumpOnInput {
		v.mu.Unlock()
		return
	}
	v.mu.Unlock()
	v.ScrollToBottom(writeBottom)
}
```

- [ ] **Step 4: Run test**

Run: `go test ./apps/texelterm/parser/sparse -run TestViewWindow_AutoJumpOnInput -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go
git commit -m "sparse: make autoJumpOnInput configurable"
```

---

## Task 13: Manual verification checklist

**No code changes.** After Tasks 1–12, run the following by hand. If any fail, stop and file a follow-up task before proceeding to Task 14.

- [ ] **Start texelation, run `ls -la` in a wide terminal, resize narrower mid-output.** Wrapped rows should rejoin after widening and split correctly after narrowing. No duplicated or lost characters.

- [ ] **Scroll back through history after reflow.** Historical content should also reflow at current width.

- [ ] **Run a DECSTBM app (`progress` or similar).** Resize the terminal during its run. The structured content should NOT reflow — it should clip/pad at the new width.

- [ ] **Toggle reflow off (add a temporary test hook / debug command if needed).** Reflowable chains should snap to 1:1; NoWrap chains are unaffected.

- [ ] **Start a session, write some content, close texelation, reopen.** Check that `.lhist` is loaded, reflow still works, and NoWrap rows (from DECSTBM apps) still clip on resize.

- [ ] **Kill the server mid-session.** On restart, WAL replay should restore NoWrap flags — DECSTBM-written content should still clip.

- [ ] **Edge cases:** resize to width 1 briefly — should be slow but correct, not crash. `yes | head -c 1000000` then resize — the `4 × height` cap should keep the UI responsive.

- [ ] **TUI app (Claude) alt-screen + resize.** Launch `claude` inside a texelterm pane, interact, then resize the outer terminal. Entering and exiting the alt screen must preserve main-screen scrollback intact. Resize during alt-screen must not corrupt the main-screen view on exit. Scroll regions set by the TUI should be NoWrap-marked and clip 1:1 on resize. Note: TUI-side duplicated lines on resize are acceptable (ghostty has the same behavior) and not a blocker.

- [ ] **Keyboard and mouse scroll.** In a long history, `<alt-up>`/`<alt-down>` and mouse-wheel should scroll incrementally (single row per tick), not jump to top/bottom. Page-up/page-down should move by viewport height.

If all pass, commit any test/debug hooks removal:

```bash
git status
# (should be clean or only debug removals)
git commit -m "chore: clean up reflow debug hooks" --allow-empty
```

---

## Task 14: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (root of `texelation-sparse`)

- [ ] **Step 1: Locate the "Scrollback Persistence" section**

Current text (around the bottom): "**Notes**: No reflow on resize — the store is width-set-at-construction..."

- [ ] **Step 2: Replace with updated description**

New text:

```markdown
**Notes**: Reflow on resize is view-side. The store stays width-independent in structure (cells at `(globalIdx, col)`, `Wrapped` flag on wrap boundaries). `ViewWindow` walks `Wrapped` chains at render time and reflows at current viewport width; widening rejoins, narrowing splits. Per-row `NoWrap` flag (set when DECSTBM has non-default margins at write time) opts structured content out of reflow — that content clips/pads 1:1 instead. A global `globalReflowOff` toggle forces all reflowable rows to 1:1 without affecting NoWrap rows. See `docs/superpowers/specs/2026-04-16-sparse-resize-reflow-design.md`.
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: CLAUDE.md reflects view-side resize-reflow"
```

---

## Task 15: Final regression + create PR

- [ ] **Step 1: Run everything**

Run: `cd /home/marc/projects/texel/texelation-sparse && make test`
Expected: PASS

Run: `go test -tags=integration ./internal/runtime/server/...`
Expected: PASS

- [ ] **Step 2: Push branch and open PR**

```bash
git push -u origin <branch>
gh pr create --title "Sparse: view-side reflow on resize" --body "$(cat <<'EOF'
## Summary
- Restore reflow-on-resize for the sparse terminal: widening rejoins wrapped lines, shrinking splits them, for both live shell output and scrollback history.
- DECSTBM-protected rows carry a NoWrap flag and render 1:1 instead of reflowing.
- User toggle `globalReflowOff` forces all reflowable rows to 1:1 (NoWrap rows unaffected).

Spec: `docs/superpowers/specs/2026-04-16-sparse-resize-reflow-design.md`

## Test plan
- [x] Unit tests: store NoWrap, chain walker, reflow, cursor mapping round-trip
- [x] Integration tests: VTerm reflow widen/narrow, NoWrap resize survival
- [x] Manual: ls resize, DECSTBM resize, scrollback reflow, width=1, yes-stress cap
- [x] Persistence: `.lhist` NoWrap round-trip, WAL replay carries NoWrap

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Record PR URL**

Return PR URL to the user.
