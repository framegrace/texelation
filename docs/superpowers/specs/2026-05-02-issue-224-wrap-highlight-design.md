# Wrap-aware selection mapping (issue #224) — Design

## Problem

Selection highlights in a texelterm pane drift away from the cells they cover whenever a logical line wraps across multiple visual rows and the wrap boundary shifts. The most common trigger is resize, but any layout change between click and render exposes the same bug.

The captured text remains correct because `GetContentText` reads cells directly from the sparse store by `(gid, col)`. What drifts is the *projection* — `ContentToViewport` (`(gid, col)` → visual `(y, x)`) and `ViewportToContent` (the inverse) use naive `y = gid - visibleTop` math, which assumes one logical line per visual row. With wrapping, a single logical line spans N visual rows and the assumption fails.

## Why the previous attempt failed

A first attempt at this fix (commits 73f9fd0 + 4d4d7cb, reverted in b60f7a3) routed both mappers through the view's existing reflow-aware `CursorToView` / `ViewToCursor`. Round-trip math passed in unit tests but production showed:

- Non-wrapped lines: highlight several rows below the click. The unit-tested invariant wasn't the one the renderer actually depends on at runtime.
- Wrapped lines: `ViewToCursor` returns the cell-bearing gid (e.g. `6` in a `5..6` chain) while the renderer's `rowIdx` tags every reflowed sub-row with the chain-head gid (`5`). The selection layer saw a gid the renderer never reports as "drawn here," so highlight and capture diverged in opposite directions.

The fundamental lesson: the renderer at draw time *knows* exactly which cells it placed at each row — it builds rows by slicing a concatenated chain. Re-deriving that layout in a parallel function risks divergence. The fix must preserve the information the renderer already has.

## Approach

Add a per-row "origin" slice — `[]RowOrigin` where `RowOrigin{Gid, Col}` is the `(gid, col)` of the first cell on each visual row — to the render path. Cache it on `VTerm` parallel to the existing `lastRowGlobalIdx`. `ContentToViewport` and `ViewportToContent` consult this cache instead of computing.

Selection state representation is unchanged — it still holds `(gid, col)` of the cell-bearing gid. Wire protocol is unchanged. Capture (`GetContentText`) is unchanged.

The renderer's existing `rowIdx` (chain-head per row) keeps its current semantics for the publisher's clipping path. Origin is a separate, additive channel for selection.

## Architecture

```
Render (every frame, under a.mu)
 └─ vterm.GridFull
     └─ mainScreen.RenderReflowFull
         └─ Terminal.RenderReflowFull
             ├─ RecomputeLiveAnchor
             └─ view.Render
                 └─ for each chain: reflowChain → (rows, origin)
             returns (rows, rowGI, rowOrigin)
     stashes lastRowGlobalIdx + mainScreenRowOrigin under v.mu

Click (mouse press / drag, under a.mu)
 └─ ViewportToContent(y, x)
     ├─ origin := v.mainScreenRowOrigin[y]
     ├─ if origin.Gid == -1 → naive fallback (blank/past-content rows)
     └─ else: walk x cells from origin, crossing gid boundaries → (gid, col)

Highlight (after render loop, same lock)
 └─ ContentToViewport(gid, col)
     ├─ scan v.mainScreenRowOrigin
     ├─ find row y where origin[y] ≤ (gid, col) < origin[y+1]
     └─ x := cell-distance from origin[y] to (gid, col)
 └─ paint buf[startRow..endRow] as today

Capture (mouse-up)
 └─ GetContentText(startLine, startOffset, endLine, endOffset)
     reads cells from store + PageStore fault — unchanged
```

## Components

### `apps/texelterm/parser/sparse/view_reflow.go`

`reflowChain` is consolidated to always emit origin alongside rows — there are no other callers, so no shim is needed:

```go
type RowOrigin struct {
    Gid int64 // -1 sentinel for blank / past-content rows
    Col int   // col within Gid; meaningless when Gid == -1
}

func reflowChain(s *Store, startGI, endGI int64, viewWidth int) (
    rows [][]parser.Cell, origin []RowOrigin)
```

The concat phase tracks origin per cell:

```go
var logical []parser.Cell
var cellOrigin []RowOrigin
for gi := startGI; gi <= endGI; gi++ {
    line := s.GetLine(gi)
    for col := range line {
        logical = append(logical, line[col])
        cellOrigin = append(cellOrigin, RowOrigin{Gid: gi, Col: col})
    }
}
```

After `trimTrailingPadding`, slice by `viewWidth`. Each output row's origin = `cellOrigin[off]`. Trailing-empty-rows get `RowOrigin{Gid: endGI, Col: len(endGI_cells)}` ("past content"). The positional-gap and empty-chain edge cases set `Gid = -1`.

### `apps/texelterm/parser/sparse/view_window.go`

The single `Render` walker returns `([][]Cell, []int64, []RowOrigin)`. The unwrapped / nowrap branch fills `origin = (gi, 0)` per row. The wrapped branch unpacks `reflowChain`'s second return. Blank-row gaps and bottom padding fill `RowOrigin{Gid: -1}`. The `skip > 0` slice-trimming applies to all three slices in lockstep.

### `apps/texelterm/parser/sparse/terminal.go`

```go
func (t *Terminal) RenderReflowFull() ([][]parser.Cell, []int64, []RowOrigin) {
    cursorGI, cursorCol := t.write.Cursor()
    t.view.RecomputeLiveAnchor(t.store, cursorGI, cursorCol, t.write.WriteTop())
    return t.view.Render(t.store)
}

// Existing methods become discard-shims so the publisher path stays untouched.
func (t *Terminal) RenderReflowWithRowIdx() ([][]parser.Cell, []int64) {
    rows, gids, _ := t.RenderReflowFull()
    return rows, gids
}
func (t *Terminal) RenderReflow() [][]parser.Cell {
    rows, _, _ := t.RenderReflowFull()
    return rows
}
```

### `apps/texelterm/parser/main_screen.go`

`MainScreen` interface adds `RenderReflowFull() ([][]Cell, []int64, []RowOrigin)`. Existing methods stay on the interface — they're the shapes the publisher already calls. Both delegate to the same view walker; no double-walk.

`RowOrigin` is exported from the parser package as a type alias of `sparse.RowOrigin`:

```go
type RowOrigin = sparse.RowOrigin
```

### `apps/texelterm/parser/vterm.go` and `vterm_main_screen.go`

A new cached slice on `VTerm`, set under `v.mu` at the end of every render — same lock and lifecycle as the existing `mainScreen` grid cache:

```go
mainScreenRowOrigin []RowOrigin // length matches the rendered grid; -1 sentinel for blank rows
```

`mainScreenGridWithRowIdx` is renamed to `mainScreenGridFull`, captures all three returns, stashes origin. The old name stays as a 2-return wrapper for any internal caller that doesn't want origin.

`ViewportToContent(y, x)`:

```go
if y < 0 || y >= len(v.mainScreenRowOrigin) {
    // Out of viewport — naive fallback.
    return visibleTop + int64(y), x, gid == cursorLine, true
}
o := v.mainScreenRowOrigin[y]
if o.Gid == -1 {
    // Blank row / unwritten gap — naive fallback.
    return visibleTop + int64(y), x, gid == cursorLine, true
}
gid, col := advanceCells(s, o.Gid, o.Col, x)
return gid, col, gid == cursorLine, true
```

`ContentToViewport(gid, col)`:

```go
viewportWidth := v.width // viewport columns
for y, o := range v.mainScreenRowOrigin {
    if o.Gid == -1 { continue }
    if x, ok := cellsBetween(s, o.Gid, o.Col, gid, col, viewportWidth); ok {
        return y, x, true
    }
}
return 0, 0, false
```

`cellsBetween(s, originGid, originCol, targetGid, targetCol, maxCells)` walks cells from origin advancing one at a time, crossing gid boundaries, returning `(stepsTaken, true)` if target is reached within `maxCells` steps, otherwise `(0, false)`. Bounded by viewport width per call. Using a row-bound rather than a `nextOrigin` lookup avoids special-casing the last row and keeps the predicate self-contained.

`advanceCells(s, originGid, originCol, x)` is the dual used by `ViewportToContent`: walk `x` cells from origin, return the resulting `(gid, col)`. Same per-cell loop, bounded by viewport width.

### `apps/texelterm/term.go`

Unchanged. `applySelectionHighlightLocked` already calls `vterm.ContentToViewport`; the new origin-based math just produces correct results. Cached origin lives inside vterm; the app doesn't see it directly.

### Publisher (`internal/runtime/server/desktop_publisher.go`)

Unchanged. Continues to use `RowGlobalIdx` (chain head) for delta clipping. Origin is texelterm-internal.

## Sentinel choices

| Row condition | `rowGI[y]` | `rowOrigin[y]` |
|---|---|---|
| Normal unwrapped row | `gi` | `(gi, 0)` |
| Wrapped chain head row | chain head `gi` | `(gi, 0)` |
| Wrapped continuation row | chain head `gi` | cell-bearing `(g, c)` |
| Blank row in chain walk gap | `-1` | `(Gid: -1)` |
| Bottom padding (viewport > content) | `-1` | `(Gid: -1)` |
| Trailing empty in chain | chain head `gi` | `(endGI, len(endGI_cells))` |
| Positional-gap one-row chain | `gi` | `(gi, 0)` |

`rowOrigin` and `rowGI` agree on `-1` for "no real content here" cases, so existing callers that already special-case `-1` keep working.

## Lock model

- `a.mu` (texelterm app) wraps both `Render` and `HandleMouse`. Origin slice is read & written under it.
- `v.mu` (vterm RWLock) protects the cached origin slice itself. Render takes write; mappers take read.
- `view.mu` is taken inside `Render` only; mappers don't touch it (they read the cached origin from vterm).

No new lock acquisitions; no double-walk of chains.

## Boundary semantics

When a row's first cell is at `(gid_X, col_K)` because the row crosses a gid boundary mid-row, and a click at column `x` would resolve to the first cell of `gid_X+1`, the resolved gid is **`gid_X+1`** (cell-bearing). This matches what `selectAtom`, `GetContentText`, and the existing wrapped-chain copy logic expect.

`Selection.AnchorOffset` / `CurrentOffset` follow the existing exclusive-end convention. A drag from row Y col 5 to row Y col 30 on a row whose cells span `(gid_5, 50)..(gid_5, 79), (gid_6, 0)..(gid_6, 19)` resolves to anchor `(gid_5, 55)`, head `(gid_6, 0)` — capture iterates `cells[55:80]` of gid 5 (the chain's `Wrapped=true` flag suppresses the `\n` join) plus `cells[0:0]` of gid 6, yielding 25 chars from gid 5 alone, which matches the 25 cells visually selected.

## Testing

Three layers, each independently meaningful.

### Pure-function tests on `reflowChain`

```go
TestReflowChain_OriginNonWrapped
// 1-gid chain with 50 chars at width 80. rows=1, origin[0]={gid, 0}.

TestReflowChain_OriginWrappedTwoGids
// gid 5 (80 chars) + gid 6 (60 chars), width 80. rows=2.
// origin[0]={5, 0}, origin[1]={6, 0} (boundary aligned with width).

TestReflowChain_OriginWrappedCrossingGidMidRow
// gid 5 (80 chars) + gid 6 (60 chars), width 50. rows=3.
// origin[0]={5, 0}, origin[1]={5, 50}, origin[2]={6, 20}.

TestReflowChain_OriginWithTrailingPaddingTrimmed
// Trim affects rows count but not origins of remaining rows.

TestReflowChain_OriginPositionalGap
// rowHasPositionalGap path: 1 row, origin={start, 0}.

TestReflowChain_OriginTrailingEmptyRows
// Cursor on blank wrap continuations. Trailing empties get
// origin={endGI, len(endGI_cells)} ("past content").
```

### Round-trip tests on `vterm.ViewportToContent` ↔ `ContentToViewport`

These tests verify *agreement with the renderer*, not just inverse-of-self — the failure mode of the previous attempt was that the mapper's walk produced different answers than what was drawn.

```go
TestViewportContent_AgreesWithRenderedRowOrigin
// For each row y, ViewportToContent(y, 0) must return exactly origin[y].
// ContentToViewport(origin[y]) must return exactly y.

TestViewportContent_RoundTripNonWrapped
// Click (y, x) → (gid, col) → (y, x). All cells in the viewport.

TestViewportContent_RoundTripWrappedChain
// Same, on continuation rows AND mid-row gid boundaries.

TestViewportContent_BlankRowFallsBackToNaive
// Click on a blank gap row. origin == -1 → naive math returns
// (visibleTop+y, x), preserving today's behavior.

TestViewportContent_SelectionScrolledOffViewport
// Endpoint outside the rendered window → ContentToViewport returns
// visible=false. Highlight clamp logic in term.go handles painting.

TestContentToViewport_AfterResize
// Type 100-char line at width 80 (1 wrap). Resize to width 40
// (2 wraps). The (gid, col) of a known character maps to the
// correct new visual row+col.
```

### Integration test in `apps/texelterm`

```go
TestSelection_HighlightTracksWrappedContentAfterResize
// Build a 200-char wrapped line, double-click on the second
// wrapped row to select a word. Resize narrower (more wraps).
// SelectionRange unchanged. ContentToViewport of the start
// returns the row that now displays that word.
```

### Regression coverage

These tests stay green with no changes:

- `TestGetContentText_WrappedChainJoinsWithoutNewline`
- `TestGetContentText_FaultsEvictedRowsFromPageStore`
- `TestSelectionStateMachine_*`
- All `TestClickDetector_*`

## Out of scope

- Pixel-level visual rendering tests (manual via the visual diff harness when needed).
- Concurrent click-during-render correctness (lock model serializes; Go's `sync.RWMutex` provides the guarantee).
- Pane-level composition (border, statusbar) — origin stays in vterm coords; the pane already shifts by border offset.
- Optimizing `ContentToViewport`'s O(rows) scan into O(log rows). Premature given viewport heights ≤ ~100 rows.
- Selection state representation changes (e.g., chain-relative coords). The existing `(gid, col)` representation is not the source of the bug.

## Risks

- **Origin slice goes stale between render and mapper call**. Mitigated by the `a.mu` lock that wraps both `Render` and `HandleMouse` in texelterm — they serialize. New code does not introduce additional concurrency.
- **Sentinel `-1` mishandling**. Every `ContentToViewport` / `ViewportToContent` path checks the sentinel and falls back to naive math. Tested directly.
- **Off-viewport endpoints**. `ContentToViewport` returns `visible=false`; the existing clamp branch in `applySelectionHighlightLocked` handles painting. Tested.
- **Performance**. One concat-walk per chain at render time (already paid by `reflowChain` today; the origin-tracking is a parallel slice append, no extra scan). Mapper calls are O(rows) ≤ ~100, run twice per render frame at most.

## Acceptance

- All three test layers above pass.
- The dropped agreement test (`TestViewportContent_AgreesWithRenderedRowIdx` from the reverted attempt) is resurrected as `TestViewportContent_AgreesWithRenderedRowOrigin` and passes.
- Manual: select content that includes a wrap boundary, capture the text, resize the pane (wider, narrower, both). Selection highlight remains on the same logical cells. Captured text stays unchanged.
- No regression on click-row → highlight-row for non-wrapped content.
- Publisher behavior unchanged (server-side delta clipping still uses `RowGlobalIdx` chain-head semantics).
