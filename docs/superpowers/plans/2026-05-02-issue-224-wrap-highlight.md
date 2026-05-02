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

Then change the signature of `reflowChain` and the body to also build the origin slice. Critical: `logical` and `cellOrigin` must be trimmed in lockstep — call `trimTrailingPadding` once and use its return for both:

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
	// Trim logical AND cellOrigin from the same length so origin[i] stays
	// aligned with logical[i]. trimTrailingPadding returns a possibly-
	// shorter slice (it does cells[:n]); reassign logical to it and shrink
	// cellOrigin to match.
	logical = trimTrailingPadding(logical)
	cellOrigin = cellOrigin[:len(logical)]

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

Trailing empty rows in a chain are produced by `trailingEmptyRows` — that helper counts gids strictly after the start whose `len(s.GetLine(gid)) == 0`, **as long as `walkChain` reached them via `Wrapped=true` cells**. Setup that satisfies both: chain head has Wrapped=true, continuation gid is empty AND set with NoWrap=false (default), and `walkChain`'s exit condition `len(cells) == 0` returns early at the empty gid — so the trailing row count comes from chains where a continuation has cells but ends with Wrapped=true and the NEXT gid is empty. Use `SetLine` to write a fully-filled wrapped row at gid 6 (Wrapped=true) so `walkChain` advances to gid 6, then leave gid 7 empty so `trailingEmptyRows` counts it.

Actually `walkChain` exits when the current row's last cell is not Wrapped, so the chain must end with a cell that has Wrapped=true. The cleanest setup that produces a trailing empty: gid 5 is filled with Wrapped=true, gid 6 also filled+Wrapped=true, gid 7 is in store but empty. `walkChain` loops: gid 5 (cells, last Wrapped → continue), gid 6 (cells, last Wrapped → check next), gid 7 (`s.GetLine(7) == nil` exits returning end=6). Result: `trailingEmptyRows(s, 5, 6)` returns 0 (only checks gid 6 which has cells). Hmm — that doesn't produce trailing empties either.

Reading `trailingEmptyRows` again: it walks from `end` down to `start+1` counting empty rows. To get a non-zero count, an interior gid must be empty. That happens when a chain has been written, then the cursor advanced past the last Wrapped row by emitting a newline that creates a blank gid in the chain extension. Reproducing this without driving a parser is fragile.

**Pragmatic test:** force the shape directly with `SetLine(gid, nil)` followed by `SetLine(gid+1, ...)` such that walkChain will visit both. Test the origin emission by stubbing the input directly:

```go
func TestReflowChain_OriginTrailingEmptyRows(t *testing.T) {
	s := NewStore(80)
	// Two-gid chain head; both gids carry content + Wrapped=true. After
	// reflow, with a trailing-empty path simulated by a chain whose
	// trailingEmptyRows is non-zero, we'd want a setup where end > start
	// and an interior gid in [start+1, end] is empty.
	//
	// Direct setup: gid 5 has 80 wrapped chars, gid 6 has 80 wrapped chars,
	// gid 7 has empty cells but is in the store. walkChain will return
	// end=6 (it bails when next is nil), so trailingEmptyRows returns 0.
	// Fall back to confirming the absence-of-trailing-empties path
	// emits all origins from the cell walk.
	fillRow(s, 5, strings.Repeat("a", 80), true)
	fillRow(s, 6, "bb", false)

	rows, origin := reflowChain(s, 5, 6, 80)
	if len(rows) != len(origin) {
		t.Fatalf("rows/origin length mismatch: %d vs %d", len(rows), len(origin))
	}
	// Two rows: one for gid 5 (80 a's), one for "bb".
	if len(rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(rows))
	}
	if origin[0] != (RowOrigin{Gid: 5, Col: 0}) {
		t.Errorf("origin[0]=%+v, want {Gid:5, Col:0}", origin[0])
	}
	if origin[1] != (RowOrigin{Gid: 6, Col: 0}) {
		t.Errorf("origin[1]=%+v, want {Gid:6, Col:0}", origin[1])
	}
}
```

A second test directly verifies the trailing-empty origin sentinel via a synthetic setup that reaches the trailing-empty branch:

```go
func TestReflowChain_OriginTrailingEmptyBranch(t *testing.T) {
	// Verify the trailing-empty branch by reflecting on the function
	// directly. trailingEmptyRows's contract: counts rows in (start, end]
	// whose len == 0. Pick a chain where end > start and the in-between
	// gids are empty; this only happens when SetLine pre-creates an empty
	// row in the middle of a chain — fragile but representative.
	s := NewStore(80)
	// Chain head fully filled, Wrapped=true.
	cells := make([]parser.Cell, 80)
	for i := range cells {
		cells[i] = parser.Cell{Rune: 'a'}
	}
	cells[79].Wrapped = true
	s.SetLine(5, cells)
	// gid 6: pre-create an empty row. walkChain will see gid 6 has no
	// cells via len(cells) == 0 and return end=5. So trailingEmptyRows
	// runs over [5, 5] and returns 0. This means trailing branch isn't
	// reachable from public APIs in a unit-testable way without a parser
	// driving cursor moves. Skip the assert path; the trailing-empty
	// branch is structurally tested via the integration tests in Phase 8.
	end, _ := walkChain(s, 5, 100)
	if end != 5 {
		t.Fatalf("unexpected walkChain end=%d", end)
	}
	// Confirm the chain-head-only path still emits origins correctly.
	rows, origin := reflowChain(s, 5, end, 80)
	if len(rows) != 1 || len(origin) != 1 {
		t.Fatalf("rows=%d origin=%d, want 1 each", len(rows), len(origin))
	}
}
```

The trailing-empty branch is genuinely hard to drive from a unit test without a parser; the integration test in Task 16 covers it via realistic parser input. Document this limitation rather than ship a `t.Skip` that asserts nothing.

- [ ] **Step 2: Run tests to verify both pass**

```
go test -run "TestReflowChain_OriginTrailingEmptyRows|TestReflowChain_OriginTrailingEmptyBranch" ./apps/texelterm/parser/sparse/
```

Expected: PASS for both.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/sparse/view_reflow_test.go
git commit -m "Test: reflowChain origin for trailing wrap-continuation rows

Two tests: round-trip through the wrapped two-gid path, plus a
documented limitation note that the trailing-empty-branch is
covered structurally by Phase 8 integration. Issue #224 plan,
Task 4."
```

---

### Task 4a: Origin lockstep with trimTrailingPadding

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_reflow_test.go`

- [ ] **Step 1: Write the failing test**

This test directly exercises the lockstep bug class — `trimTrailingPadding` shortens `logical` and `cellOrigin` must shrink to match.

```go
func TestReflowChain_OriginWithTrailingPaddingTrimmed(t *testing.T) {
	s := NewStore(80)
	// 60 chars of content + 20 trailing padding cells (default-bg space).
	// Chain head Wrapped=false so the chain is single-gid.
	cells := make([]parser.Cell, 80)
	for i := 0; i < 60; i++ {
		cells[i] = parser.Cell{Rune: 'x'}
	}
	for i := 60; i < 80; i++ {
		cells[i] = parser.Cell{Rune: ' '}
	}
	s.SetLine(5, cells)

	rows, origin := reflowChain(s, 5, 5, 80)
	if len(rows) != 1 {
		t.Fatalf("rows=%d, want 1", len(rows))
	}
	// After trim, logical has 60 cells; the row is 60 cells wide.
	if len(rows[0]) != 60 {
		t.Errorf("row width=%d, want 60 (trailing 20 padding cells trimmed)", len(rows[0]))
	}
	// origin[0] must point at gid 5 col 0 — trim is symmetric.
	if origin[0] != (RowOrigin{Gid: 5, Col: 0}) {
		t.Errorf("origin[0]=%+v, want {Gid:5, Col:0}", origin[0])
	}
	if len(origin) != 1 {
		t.Errorf("origin len=%d, want 1", len(origin))
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```
go test -run TestReflowChain_OriginWithTrailingPaddingTrimmed ./apps/texelterm/parser/sparse/
```

Expected: PASS — Task 1's lockstep trim emits aligned `logical` and `cellOrigin`.

If it fails, `cellOrigin` was not trimmed in lockstep with `logical`; revisit Task 1's trim block.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/sparse/view_reflow_test.go
git commit -m "Test: reflowChain trim-padding lockstep on origin

Issue #224 plan, Task 4a."
```

---

### Task 4b: Origin emission for positional-gap one-row chain

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_reflow_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestReflowChain_OriginPositionalGap(t *testing.T) {
	s := NewStore(80)
	// Powerline-style row: write at col 89 directly via WriteCell so the
	// row carries unwritten cells before the last-written col.
	for col := 0; col < 90; col++ {
		if col == 89 {
			s.WriteCell(5, col, parser.Cell{Rune: 'x'})
		}
	}
	if !rowHasPositionalGap(s, 5) {
		t.Fatalf("row 5 has no positional gap; setup invalid")
	}

	rows, origin := reflowChain(s, 5, 5, 80)
	if len(rows) != 1 {
		t.Fatalf("rows=%d, want 1 (positional-gap path returns single row)", len(rows))
	}
	if origin[0] != (RowOrigin{Gid: 5, Col: 0}) {
		t.Errorf("origin[0]=%+v, want {Gid:5, Col:0}", origin[0])
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```
go test -run TestReflowChain_OriginPositionalGap ./apps/texelterm/parser/sparse/
```

Expected: PASS — Task 1's `if startGI == endGI && rowHasPositionalGap` branch returns `[]RowOrigin{{Gid: startGI, Col: 0}}`.

(If `WriteCell` isn't the right helper to populate without an autowrap, check the sparse package for the equivalent — `s.SetCell(gid, col, cell)` or similar. The test asserts `rowHasPositionalGap` upfront so a setup mismatch surfaces clearly.)

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/sparse/view_reflow_test.go
git commit -m "Test: reflowChain origin for positional-gap one-row chain

Issue #224 plan, Task 4b."
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

`mainScreenGridWithRowIdx` is called from `Grid()` and `GridWithRowIdx()`, both of which hold `v.mu.RLock()`. Mutating `v.mainScreenRowOrigin` from inside that path would race with concurrent `RLock`-held readers — even if no production callers exist today, the race detector in Task 17 will flag it. The fix: take an exclusive lock around the cache mutation, or write to a slice-typed field via atomic-pointer swap.

Simplest approach matching the existing codebase: drop the RLock for the duration of the render call (the caller's RLock is reacquired afterward), or — cleaner — change the field assignment to use a small dedicated mutex that's separate from `v.mu`. Pick the approach that matches existing patterns; if `lastRowGlobalIdx` (the parallel cache for `rowIdx`) is already protected somehow, follow that lead.

In `apps/texelterm/parser/vterm_main_screen.go`, locate `mainScreenGridWithRowIdx` (~line 499). Change the implementation to call `RenderReflowFull` and stash all three results:

```go
func (v *VTerm) mainScreenGridWithRowIdx() ([][]Cell, []int64) {
    if v.mainScreen == nil {
        return nil, nil
    }
    grid, rowIdx, rowOrigin := v.mainScreen.RenderReflowFull()
    // mainScreenRowOrigin is published under v.mu; readers in
    // ContentToViewport / ViewportToContent must hold an RLock on
    // v.mu while reading it.
    //
    // Lock-upgrade note: callers of mainScreenGridWithRowIdx hold an
    // RLock today; switching here without releasing first would
    // deadlock. The caller of Grid() / GridWithRowIdx() must promote
    // to a write lock (or this function must be called only from
    // contexts that already hold the write lock). See the calling
    // path before changing this.
    //
    // Pragmatic option: store rowOrigin via a sync.RWMutex-protected
    // field separate from v.mu, so this method can take its own write
    // lock independently:
    //
    //   v.rowOriginMu.Lock()
    //   v.mainScreenRowOrigin = rowOrigin
    //   v.rowOriginMu.Unlock()
    //
    // and have the mappers RLock rowOriginMu when reading. Pick
    // whichever pattern matches the codebase's existing rowIdx cache
    // protection.
    _ = rowOrigin // wired in Step 3a/3b below
    return grid, rowIdx
}
```

- [ ] **Step 3a: Read the existing rowIdx cache lock pattern**

Look at how `lastRowGlobalIdx` (or wherever `rowIdx` gets cached) is currently protected. Match that pattern for `mainScreenRowOrigin` — same lock, same lifecycle.

If there's no cache lock today (because no field gets written from `mainScreenGridWithRowIdx`), you have two choices:
1. **Add a dedicated `sync.RWMutex` for the new cache** — write under `Lock()` here, read under `RLock()` in mappers. Cheapest.
2. **Document mappers as not-thread-safe** — relies entirely on `a.mu` (texelterm's app lock) to serialize. Acceptable IF every external caller of `ViewportToContent` / `ContentToViewport` holds `a.mu`. Audit callers to confirm.

- [ ] **Step 3b: Apply the chosen pattern**

Edit `apps/texelterm/parser/vterm.go` to add the mutex (option 1) or document the contract (option 2). Update the field stash in `mainScreenGridWithRowIdx` accordingly. The mappers (Tasks 11/12) must consult the same mutex when reading.

(Keep the existing `mainScreenGridWithRowIdx` name — its callers don't change. The third value is captured into the cache as a side effect.)

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

func TestAdvanceCells_PastContentTerminates(t *testing.T) {
	// Regression: walking past the store's last written gid must not
	// loop indefinitely. Without the ReadLine == nil break, advanceCells
	// would loop forever (available=0, gid++, no progress).
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	v.mainScreen.SetLine(5, []Cell{{Rune: 'a'}, {Rune: 'b'}})

	// From (5, 5), try to advance 100 cells. Origin col is past the
	// row's content (the row only has 2 cells), so available=0 first
	// iter, then gid=6 which doesn't exist → break.
	done := make(chan struct{})
	go func() {
		v.advanceCells(5, 5, 100)
		close(done)
	}()
	select {
	case <-done:
		// Returned cleanly.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("advanceCells did not terminate within 100ms — likely infinite loop")
	}
}
```

Add `import "time"` if not already imported (only for this test).

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
// Bounded against runaway: when ReadLine returns nil (gid past the
// store), break — the position is "past content" and further advancing
// just walks empty space. Without this break, a click on a trailing-
// empty wrap-continuation row could loop forever (each iteration:
// available=0, gid++, no progress on `remaining`).
//
// The result for past-content positions is a (gid, col) just past the
// store's max gid; selection callers handle that as a zero-width
// selection (selectAtom finds no word; capture reads no cells).
func (v *VTerm) advanceCells(originGid int64, originCol int, n int) (int64, int) {
    if v.mainScreen == nil {
        return originGid, originCol
    }
    gid := originGid
    col := originCol
    remaining := n
    for remaining > 0 {
        cells := v.mainScreen.ReadLine(gid)
        if cells == nil {
            // Past the store's last written gid. Stop here — further
            // walking would loop without progress.
            return gid, col
        }
        rowLen := len(cells)
        available := rowLen - col
        if remaining < available {
            return gid, col + remaining
        }
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
// Safety: the loop is bounded both by step count AND by gid distance.
// Walking through many consecutive empty gids contributes 0 to `steps`
// per iteration; without a separate gid-based bound, a fixed iteration
// cap can cut the walk short before reaching the target through gappy
// stores. Bounding by (targetGid - originGid + 2) is provably tight —
// the walk visits exactly the gids in [originGid, targetGid].
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
    // Tight bound: we visit at most (targetGid - originGid + 1) gids;
    // +1 for the safety margin.
    iterCap := int(targetGid-originGid) + 2
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

Append. The test must use a wrap chain so naive `(visibleTop + y, x)` math diverges from origin-based math — otherwise the pre-rewrite implementation may pass by coincidence.

```go
func TestViewportToContent_WrappedContinuationRow(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// 100-char line wraps once at width 80 — row 0 displays gid 0
	// (chain head); row 1 displays gid 1 (or stays in gid 0 with
	// col offset, depending on chain layout).
	p := NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	// Render so mainScreenRowOrigin is populated.
	_, _ = v.GridWithRowIdx()

	// Click at row 1, col 5. The naive implementation returns
	// (visibleTop+1, 5). The origin-based implementation must return
	// the cell-bearing (gid, col) — origin[1].Gid + col-walk-from-origin.
	gid, col, _, ok := v.ViewportToContent(1, 5)
	if !ok {
		t.Fatalf("ViewportToContent(1, 5) ok=false")
	}

	// Independently re-derive what the answer should be from the cached
	// origin slice — this gives a check that doesn't pre-suppose the
	// implementation, but does require the origin slice to be built
	// correctly (which Phase 1 covers).
	o := v.mainScreenRowOrigin[1]
	wantGid, wantCol := o.Gid, o.Col+5
	// If +5 crosses a gid boundary in the chain, expand:
	storeCells := v.mainScreen.ReadLine(o.Gid)
	if wantCol >= len(storeCells) {
		wantCol -= len(storeCells)
		wantGid++
	}
	if gid != wantGid || col != wantCol {
		t.Errorf("ViewportToContent(1,5)=(gid=%d,col=%d), want (%d,%d)",
			gid, col, wantGid, wantCol)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test -run TestViewportToContent_WrappedContinuationRow ./apps/texelterm/parser/
```

Expected: FAIL — current `ViewportToContent` uses naive `(visibleTop + 1, 5)`. For a wrapped line, `visibleTop + 1` is the chain's second gid (or the same gid depending on store layout), but the col is wrong: 5 instead of `(origin col + 5)`. The naive math doesn't agree with what the renderer actually drew.

If the test passes with naive math, the wrap setup didn't actually wrap; check with a longer line or verify `mainScreenRowOrigin[1].Col != 0`.

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

### Task 14: Agreement test — origin matches rendered grid cells

**Files:**
- Modify: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

The reverted attempt failed because the mapper's chain walk disagreed with what `Render` actually drew. To catch that class of bug, the test must compare against **rendered grid cells**, not against the cache the mapper consults — otherwise it's tautological (mapper reads cache, test reads cache, they trivially agree).

For each non-blank row `y` in the rendered grid, the cell at `(y, 0)` of the grid must equal the store's cell at `(origin[y].Gid, origin[y].Col)`.

```go
func TestRenderedGridAgreesWithRowOrigin(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// Mix of short non-wrapping lines and a long wrapped line so the
	// origin slice has both per-gid and per-cell-offset entries.
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
	grid, _ := v.GridWithRowIdx()

	for y, o := range v.mainScreenRowOrigin {
		if o.Gid == -1 {
			continue // blank / past-content sentinel
		}
		// Compare the rendered grid's first cell on row y with the
		// store cell that origin[y] points to. This catches any
		// divergence between what the renderer drew and what the
		// origin slice claims about that row.
		drewRune := grid[y][0].Rune
		storeCells := v.mainScreen.ReadLine(o.Gid)
		if o.Col >= len(storeCells) {
			t.Errorf("row %d: origin (%d,%d) points past store row (len=%d)",
				y, o.Gid, o.Col, len(storeCells))
			continue
		}
		want := storeCells[o.Col].Rune
		// Both might be zero (empty cell rendered, empty store cell)
		// — that's fine. Mismatch is a real bug.
		if drewRune != want {
			t.Errorf("row %d col 0: drew rune=%q, but origin (%d,%d) says store rune=%q",
				y, drewRune, o.Gid, o.Col, want)
		}
	}
}
```

- [ ] **Step 2: Run the test**

```
go test -run TestRenderedGridAgreesWithRowOrigin ./apps/texelterm/parser/
```

Expected: PASS — `reflowChain` builds origin and rendered cells from the same concat walk, so they agree by construction.

If it fails, the origin slice has drifted from the rendered output — that IS the failure mode of the previous attempt. Fix the trim or chain walk in Task 1 / Task 5.

- [ ] **Step 3: Add a second agreement test that exercises columns past 0**

To catch divergences in the *interior* of wrapped rows (not just first-cell), spot-check several `(y, x)` positions:

```go
func TestRenderedGridAgreesWithMapperInterior(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	// 100 chars wrap-into-two-rows at width 80.
	p := NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	grid, _ := v.GridWithRowIdx()

	// Spot-check several (y, x) on the wrap chain.
	cases := []struct{ y, x int }{
		{0, 5}, {0, 79}, {1, 0}, {1, 15},
	}
	for _, c := range cases {
		gid, col, _, ok := v.ViewportToContent(c.y, c.x)
		if !ok {
			t.Errorf("(%d,%d): ViewportToContent failed", c.y, c.x)
			continue
		}
		drew := grid[c.y][c.x].Rune
		storeCells := v.mainScreen.ReadLine(gid)
		if col >= len(storeCells) {
			t.Errorf("(%d,%d): mapped (%d,%d) past store len=%d",
				c.y, c.x, gid, col, len(storeCells))
			continue
		}
		want := storeCells[col].Rune
		if drew != want {
			t.Errorf("(%d,%d): drew %q, mapper says (%d,%d)→%q",
				c.y, c.x, drew, gid, col, want)
		}
	}
}
```

```
go test -run TestRenderedGridAgreesWithMapperInterior ./apps/texelterm/parser/
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/texelterm/parser/viewport_mapping_test.go
git commit -m "Test: rendered grid agrees with rowOrigin (catches reverted-attempt bug)

Two tests compare the actual cells the renderer drew at (y, x)
against what (gid, col) the mapper resolves to. The reverted
attempt's failure was that the mapper's chain walk disagreed with
what Render drew — these tests catch that class directly. Issue
#224 plan, Task 14."
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

### Task 15a: Resize round-trip at vterm layer

**Files:**
- Modify: `apps/texelterm/parser/viewport_mapping_test.go`

- [ ] **Step 1: Write the failing test**

The vterm-level resize round-trip is distinct from the texelterm-app integration test in Task 16 — it pins the mapping math without involving selection state, click handlers, or paint code.

```go
func TestContentToViewport_AfterResize(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	// 100-char line that wraps once at width 80.
	p := NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab"
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	_, _ = v.GridWithRowIdx()

	// Capture (gid, col) for the rune at viewport (1, 0) — char 80 of
	// the logical line — at width 80.
	gid80, col80, _, ok := v.ViewportToContent(1, 0)
	if !ok {
		t.Fatalf("pre-resize ViewportToContent(1,0) failed")
	}

	// Resize to width 40. Same logical line now wraps to 3 rows; char 80
	// lands on row 2.
	v.Resize(40, 24)
	_, _ = v.GridWithRowIdx()

	y, x, vis := v.ContentToViewport(gid80, col80)
	if !vis {
		t.Fatalf("post-resize: (%d,%d) not visible", gid80, col80)
	}
	if y != 2 || x != 0 {
		t.Errorf("post-resize: (gid=%d,col=%d) → (%d,%d), want (2,0)",
			gid80, col80, y, x)
	}
}
```

- [ ] **Step 2: Run the test**

```
go test -run TestContentToViewport_AfterResize ./apps/texelterm/parser/
```

Expected: PASS — the origin slice rebuilds on the post-resize render; mapper consults the new slice.

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/parser/viewport_mapping_test.go
git commit -m "Test: ContentToViewport after resize at vterm layer

Pins the mapping math through a resize that re-wraps a logical line.
Issue #224 plan, Task 15a."
```

---

### Task 16: Resize integration test driving the highlight paint

**Files:**
- Create: `apps/texelterm/selection_wrap_resize_test.go`

- [ ] **Step 1: Write the failing test**

The user-reported bug is "highlight visibly drifts after resize." The vterm round-trip test (Task 15a) covers the math; this test must exercise the actual paint path — drive `applySelectionHighlightLocked`, capture the painted buffer, and assert that the highlighted cells correspond to the cells the selection captured.

Create `apps/texelterm/selection_wrap_resize_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Issue #224: selection highlight tracks the right cells across resize
// that re-wraps a logical line. Drives the full paint path so a bug
// in either the (gid, col) mapping OR the highlight paint surfaces.

package texelterm

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// Match the constructor / public surface to existing tests in this
// package. Look at apps/texelterm/term_test.go for the established
// pattern; some tests use NewTexelTerm(), others use New(). Pick the
// one that compiles and matches existing conventions.
func TestSelection_HighlightTracksWrappedContentAfterResize(t *testing.T) {
	app := newTestTexelTerm(t, 80, 24) // helper following term_test.go's pattern

	v := app.vterm
	v.EnableMemoryBuffer()

	// Type a 100-char line that wraps to two rows at width 80.
	p := parser.NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')

	// Programmatically install a multi-row selection on the wrap chain.
	// Use the public selection-state-machine entry point (StartMode +
	// the existing ClickType-driven dispatch). See selection_state_test.go
	// for the pattern. Specifically, target a word on the SECOND wrapped
	// row so the highlight position depends on origin tracking.
	gid, col, _, ok := v.ViewportToContent(1, 5)
	if !ok {
		t.Fatalf("pre-resize ViewportToContent(1,5) failed")
	}
	// Drive double-click at (1, 5) so SelectionStateMachine resolves a
	// SpaceWord selection rooted at (gid, col).
	app.selectionMachine.Start(gid, col, 1, DoubleClick, 0)

	// Render once so applySelectionHighlightLocked paints the highlight
	// with the pre-resize layout.
	bufPre := app.Render()
	highlightedPre := highlightedCells(bufPre)
	if len(highlightedPre) == 0 {
		t.Fatal("pre-resize: no highlighted cells; selection didn't paint")
	}

	// Capture the captured-text via SelectionStateMachine.Finish — this
	// is the bytes the user would copy.
	mime, data, finishOK := app.selectionMachine.Finish(gid, col, 1, 0)
	_ = mime
	if !finishOK {
		t.Fatal("Finish returned !ok")
	}
	captured := string(data)

	// Resize narrower; the same logical line now wraps to more rows.
	app.Resize(40, 24)

	// Re-install the selection at the captured (gid, col) range and re-render.
	// Because Finish was called, restart with the same anchors.
	app.selectionMachine.Start(gid, col, 1, DoubleClick, 0)
	bufPost := app.Render()
	highlightedPost := highlightedCells(bufPost)
	if len(highlightedPost) == 0 {
		t.Fatal("post-resize: no highlighted cells; the regression is back")
	}

	// Capture the post-resize text — must equal the pre-resize text.
	_, dataPost, _ := app.selectionMachine.Finish(gid, col, 1, 0)
	capturedPost := string(dataPost)
	if capturedPost != captured {
		t.Errorf("post-resize captured text differs:\npre:  %q\npost: %q", captured, capturedPost)
	}

	// Sanity: the rendered runes at the highlighted positions post-
	// resize must equal the rendered runes at the highlighted positions
	// pre-resize (modulo wrap layout). The text we captured ALSO lives
	// in the highlighted cells at new positions.
	highlightedRunesPost := runesAt(bufPost, highlightedPost)
	if !containsSubstring(highlightedRunesPost, captured) {
		t.Errorf("post-resize highlight cells %q do not contain captured text %q",
			highlightedRunesPost, captured)
	}
}

// Test helpers — implement based on the package's existing conventions.

// newTestTexelTerm builds a TexelTerm for tests, sized cols x rows. See
// term_test.go for the existing pattern (NewTexelTerm or New + Resize).
func newTestTexelTerm(t *testing.T, cols, rows int) *TexelTerm {
	t.Helper()
	// Implementation: follow term_test.go.
	panic("implement to match existing test pattern")
}

// highlightedCells returns the (y, x) of cells whose Style has the
// selection-highlight background. Compare with the configured highlight
// color from theming.ForApp("texelterm").GetColor("selection",
// "highlight_bg", ...).
func highlightedCells(buf [][]texelcoreCell) []struct{ Y, X int } {
	// Implementation: walk buf, compare bg.
	panic("implement based on the highlight bg color")
}

// runesAt extracts a string from buf at the given (y, x) positions in
// reading order.
func runesAt(buf [][]texelcoreCell, positions []struct{ Y, X int }) string {
	panic("implement: read buf[y][x].Ch in order")
}

func containsSubstring(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
```

(The helper implementations are left as panic stubs in the plan because the exact `texelcore.Cell` type, the `app.selectionMachine` field accessibility, and `Render()`'s return type need to be checked against the codebase. Use `term_test.go` as the template — there's already test infrastructure for driving render passes and inspecting buffer cells. If that infrastructure isn't sufficient, prefer extending it over duplicating it in this test file.)

(Further adjust constructor / field access patterns to match the codebase. If `selectionMachine` is private and there's no exported method to start a selection programmatically, route through `mouseCoordinator.HandleMouse` with synthesized `tcell.EventMouse` events at the same `(y, x)` — that's the same path the click handler takes.)

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
| 1 | 1–4b | `reflowChain` consolidated to emit `(rows, origin)`; covers non-wrapped, wrapped same-gid, cross-gid mid-row, trailing wrap-continuation, trim-padding lockstep, positional-gap. |
| 2 | 5–6 | `view.Render` and `Terminal.RenderReflowFull` expose origin slice. |
| 3 | 7 | `MainScreen` interface + parser-package `RowOrigin` alias. |
| 4 | 8 | `VTerm` caches `mainScreenRowOrigin` on every render under the right lock. |
| 5 | 9–10 | `advanceCells` (with past-content termination guard) and `cellsBetween` (with gid-distance bound) helpers. |
| 6 | 11–12 | `ViewportToContent` / `ContentToViewport` rewritten. |
| 7 | 13–15 | Wrapped round-trip + agreement-with-rendered-grid + sentinel tests. |
| 8 | 15a | Resize round-trip at vterm layer (math-only). |
| 9 | 16 | End-to-end resize integration test that drives the highlight paint. |
| 10 | 17 | Full-suite verification. |

**Branch:** Continue on `feature/issue-224-wrap-highlight` (already created with the spec commit).

---

## Deferred (consider after main plan lands)

These came up during plan review. They're worth doing but aren't on the critical path for fixing the user-visible bug.

1. **Demote `RenderReflow` and `RenderReflowWithRowIdx` shims off the `MainScreen` interface.** Keep them as concrete `Terminal` methods only. One canonical interface entry point (`RenderReflowFull`) is cleaner; the shims exist for source-code convenience, not interface contract. Audit publisher call sites first to confirm none reach through `MainScreen` for the shim methods.

2. **Cache invalidation hooks.** `mainScreenRowOrigin` should be cleared (set to nil) on alt-screen entry/exit and on resize. Today the main-screen path checks `inAltScreen` upfront and falls back, so a stale cache is dormant. But a defensive `clear` keeps the invariant tighter and prevents a class of "post-resize, pre-render click" mishaps.

3. **Length-equality assertion.** In `mainScreenGridFull`, after the render call, assert `len(rowIdx) == len(rowOrigin) == len(grid)`. Cheap defensive check that catches a class of slice-trimming bugs.

4. **Concurrent store mutation during walk.** `advanceCells` and `cellsBetween` read `v.mainScreen.ReadLine` per iteration; the store is live and writes can interleave under a fine-grained lock. The cache slice is a snapshot from render time; the store walked during the helper is not. In practice `a.mu` serializes texterm app paths, but this hasn't been audited for non-texelterm callers (headless, server). Worth a `sparse.Store` snapshot that the helpers can walk without re-acquiring the store lock per cell — bigger refactor, file separately.

5. **Selection state representation rework.** The plan keeps `(gid, col)` as the selection storage. A future cleanup could move to `(chain_head_gid, logical_col_within_chain)` which is reflow-independent — the current representation works but requires the origin-based mapper to translate at every render. Probably YAGNI unless the mapper becomes a hot path.
