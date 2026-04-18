# Sparse Resize-Reflow via View-Side Projection + NoWrap Rows

**Status**: Design
**Date**: 2026-04-16
**Relates to**: `docs/superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md`

## Background

The sparse cutover (PR #179, 2026-04-14) replaced the pre-sparse `MemoryBuffer` (which stored logical lines natively and reflowed on resize) with a width-indexed `sparse.Store` (cells addressed by `(globalIdx, col)`, with `Wrapped` flags on last cells of wrapped lines). The cutover deliberately dropped reflow-on-resize — it was explicitly a non-goal, because the old reflow path was tangled with `liveEdgeBase` / `ViewportWindow` anchoring bugs that the sparse redesign was built to kill.

With the sparse stack landed and stable, reflow is now wanted back. User-visible behavior goal:

> Same reflow as before: I can resize at any point in history or at the edge. Once resized, I want to see all the content adapt at that — widening rejoins wrapped lines, shrinking splits them. Both live shell output and scrollback history.

The sparse store already retains enough information to reconstruct logical lines: the `Wrapped` chain on last cells of physical rows marks logical-line boundaries. `ConvertPhysicalToLogical` performs this reconstruction today at the persistence boundary. This spec moves reconstruction into the read path so the view can reflow on the fly.

## Goals

- Widening the viewport rejoins wrapped lines; narrowing splits them.
- Both live shell output and scrollback history reflow.
- O(1) cost in content size on resize (no walk-and-rewrite of the store).
- Storage format unchanged in structure: cells at `(globalIdx, col)`, `Wrapped` flag on wraps.
- Cursor `(globalIdx, col)` store representation unchanged.
- DECSTBM scroll regions and other structured content keep working (no silent layout corruption under reflow).
- Provide a clean pivot point for a future user-level "reflow off" toggle.

## Non-goals

- Alt-screen reflow. Alt-screen keeps current dense-grid behavior (TUIs redraw on SIGWINCH). Explicitly out of scope per the sparse design spec's non-goals.
- Changing the storage format. Cells stay width-indexed.
- Changing the cursor's store representation. Stays `(cursorGlobalIdx, cursorCol)`.
- Rewriting pre-existing scrollback to include NoWrap metadata. Old sessions load with NoWrap=false and reflow as best they can.
- Revisiting pre-sparse `liveEdgeBase` / `ViewportWindow` bugs. The sparse stack already fixed those; this spec builds on top of that foundation.

## The Approach

### Architecture

Three components, minimal structural change:

```
VTerm (write path, mostly unchanged)
    • writes cells at current view width
    • sets Wrapped on last cell when line wraps
    • NEW: propagates DECSTBM-active state to the row's NoWrap flag
          │
          ▼
sparse.Store (minor extension)
    • cells at (globalIdx, col) — unchanged
    • NEW: per-row NoWrap flag + getters/setters
          │
          ▼
ViewWindow (significant new logic)
    • walks rows top-to-bottom
    • per row: NoWrap → render 1:1; else → reflow chain at view width
    • tracks view anchor independent of writeTop
    • provides forward/inverse cursor mappings
          │
          ▼
     Screen buffer
```

**Principle:** writes and storage are unchanged in structure. Reflow happens entirely in the view layer. The NoWrap flag is the escape hatch for content that would misbehave under reflow.

### The NoWrap flag

Per physical row in the store. Set at write time based on whether DECSTBM is active. Sticky per-row — once true, stays true (avoids ambiguity if DECSTBM toggles mid-row).

**Detection rule (v1):** `NoWrap = true` when the write happens while DECSTBM has been set to non-default margins (any margins other than `[0, height-1]`).

**Why DECSTBM only:**
- It's the strongest signal an app is managing its own layout.
- Direct cursor addressing (`ESC[H`) is used heavily by shells for prompt redraw, which is reflow-friendly.
- Keeps the rule simple. Add more detectors later if specific apps break.

**Chain propagation rule:** if any row in a `Wrapped` chain is NoWrap, the whole chain is treated as NoWrap for rendering. Avoids half-reflowed logical lines. See memory note `project_sparse_nowrap_chain_propagation.md` — revisit if this produces visually ugly reflow in practice.

### Rendering

Per-frame, the `ViewWindow` walks rows from its view anchor, groups physical rows into `Wrapped` chains, and renders each chain:

```
for chain in chains_from(viewAnchor):
    chainNoWrap = any(row.NoWrap for row in chain) OR globalReflowOff
    if chainNoWrap:
        emit each row in chain as one view row (1:1, truncate/pad to viewWidth)
    else:
        concat all cells in chain → logical line
        re-wrap logical line at viewWidth → N view rows
        emit each view row (respecting viewAnchorOffset on the first chain)
    if emitted >= viewHeight: break
```

Per-frame cost: bounded by view height × average chain length. Not a hot-path concern for typical terminals.

### Cursor mapping

Cursor is at `(cursorGlobalIdx, cursorCol)` in the store. Visual position is derived via two mappings:

**Forward** `(globalIdx, col)` → `(viewRow, viewCol)`:
1. Find chain containing `(globalIdx, col)`.
2. If chain is NoWrap: `viewRow = offset from view-top, viewCol = col` (truncated to viewWidth).
3. Else: compute logical-col = sum of prior row lengths in chain + col; reflow at viewWidth.
4. Add view-row contribution of prior chains in view to get absolute viewRow.

**Inverse** `(viewRow, viewCol)` → `(globalIdx, col)`:
1. Walk chains from view anchor, accumulating view rows each contributes.
2. Find the chain covering the target viewRow.
3. Within that chain, map `(viewRow - chainStartViewRow, viewCol)` back to `(globalIdx, col)`.
4. If target is past content end, fabricate blank-area result (matches current VT "address past end" behavior).

Both walks are bounded by the view height plus the tail of the chain the cursor lands in. Hard cap on chain walk: `4 × viewHeight` — pathologically long logical lines (e.g., `yes` piped with no newlines) get visually truncated rather than freezing the UI.

### View anchoring

`ViewWindow` gains a new anchor independent of `writeTop`:

```go
type ViewWindow struct {
    // existing fields...
    viewAnchor       int64 // globalIdx of first row of logical chain at view top
    viewAnchorOffset int   // if that chain reflows, which of its view rows is at top
    globalReflowOff  bool  // user toggle (default false)
}
```

**Live mode** (user not scrolled back): `viewAnchor` is recomputed lazily on each `Render()` call so the cursor is visible. The recompute is cheap (once per frame) and batches naturally — a burst of writes between frames costs one recompute.

**Scrolled-back mode**: `viewAnchor` is pinned to the user-selected position. User scroll operations adjust `viewAnchor`/`viewAnchorOffset`.

**Mode transitions:**
- On user scroll: switch to scrolled-back, pin anchor.
- On explicit jump-to-end action: switch to live, recompute anchor on next render.
- Auto-jump-on-input (user types → scroll to bottom): configurable toggle, default ON (standard terminal UX). User can disable to stay scrolled back while typing.

### Writes unchanged

VTerm writes continue at current view width (`WriteWindow.width`, updated on resize as today). Wrap semantics unchanged: `cursorX > rightEdge` → mark wrapped, advance globalIdx, continue at col 0. New writes land at new width; old content sits at whatever widths it was written. Reflow only cares about logical-line identity (via `Wrapped` chains), not per-row widths.

### Resize

`VTerm.Resize(newW, newH)`:
1. `WriteWindow.Resize(newW, newH)` — unchanged: updates width, height, writeTop, HWM, cursor clamping in store terms.
2. `ViewWindow.OnResize(newW, newH)` — rebuilds view projection. If live, anchor is recomputed on next render. If scrolled back, `viewAnchor` stays pinned.

Store is never walked or rewritten. Cost is O(1) in content size; O(height) render work happens at next frame anyway.

### Persistence

Two persistence paths, both must carry NoWrap:

- **`.lhist` (long-term scrollback):** `LogicalLine` gains a `NoWrap bool` field in the serialized form. Old files without the field load with `NoWrap=false` (reflowable). New files persist the flag per line. At persistence time the `Wrapped` chain has already been collapsed to a single LogicalLine, so chain-propagation has already applied — a single bit per line suffices.

- **WAL (crash recovery):** the WAL records row-level operations. The record for "write row cells" must carry the NoWrap flag alongside the cells, so replay reconstructs the flag. This is an additive field on the existing row-write entry; old WAL entries decode with NoWrap=false. MainScreenState session metadata is unchanged (NoWrap is per-row, not per-session).

### User toggle

Global `globalReflowOff bool` on ViewWindow. One branch in the render loop: `effectiveNoWrap = row.NoWrap || globalReflowOff`.

Semantics: the toggle only affects reflowable rows. NoWrap rows are **always** 1:1 regardless of toggle state — NoWrap is a hard property. No "force reflow" direction exists; there's no footgun where the user can reflow DECSTBM content and see it shatter.

UX wiring (keybind, per-pane setting, persistence) is out of scope for this design; the architecture provides the pivot point.

## Data flow

### Write flow

```
VTerm receives cell
  ├─ check wrapNext: if cursorX > rightEdge:
  │    markLineWrapped(cursorGlobalIdx)
  │    lineFeedForWrap() advances globalIdx, cursorX = 0
  ├─ store.Set(cursorGlobalIdx, cursorX, cell)
  └─ store.SetRowNoWrap(cursorGlobalIdx, decstbmActive)
        (sticky OR: row.NoWrap = row.NoWrap || decstbmActive)
```

### Render flow

```
ViewWindow.Render():
  start at (viewAnchor, viewAnchorOffset)
  while emitted < viewHeight AND content remains:
    walk chain from current idx (find chain end via Wrapped flags)
    chainNoWrap = any(row.NoWrap) OR globalReflowOff
    if chainNoWrap: emit rows 1:1
    else: concat + re-wrap at viewWidth, emit
    advance idx past chain
  pad remainder with blanks
  overlay cursor at CursorToView(cursorGlobalIdx, cursorCol)
```

### Cursor addressing flow (TUI `ESC[r;cH`)

```
VTerm parses CSI H/f → target (viewRow, viewCol)
ViewToCursor(viewRow, viewCol):
  walk chains from viewAnchor, accumulating view rows
  find chain covering viewRow
  within chain: map (viewRow - chainStart, viewCol) → (globalIdx, col)
  if past content end: fabricate blank-area result
WriteWindow.SetCursorAbsolute(globalIdx, col)
```

### Resize flow

```
VTerm.Resize(newW, newH):
  WriteWindow.Resize(newW, newH)        // store-level anchoring unchanged
  ViewWindow.OnResize(newW, newH)       // next Render() rebuilds projection
```

### Persistence flow

**Save:** `.lhist` LogicalLine entries carry NoWrap. At persistence time, the Wrapped chain has already been collapsed to a LogicalLine, so "any row's NoWrap → chain NoWrap" has already applied. Single bit per line.

**Load:** old files → `NoWrap=false` default. New files restore per-line. Rows in the store get the flag set during reconstruction.

## Edge cases

### Malformed Wrapped chains

Row has `Wrapped=true` but next globalIdx is empty. Chain walker stops at first missing row; treat terminator as end-of-chain. Debug-level log; no spam.

### Pathologically long logical lines

Chain walk capped at `4 × viewHeight`. Beyond cap: visually truncate (show tail that fits, discard head for reflow). Store data is unaffected. Prevents render-path footgun from unbounded walks.

### Cursor outside any known chain

Post-erase or gap scenarios: `CursorToView` treats the cursor's row as a degenerate single-row chain, renders at its own globalIdx offset. Matches current behavior.

### Resize to very small dimensions

Existing `WriteWindow.Resize` rejects `w ≤ 0 || h ≤ 0`. Small-but-positive sizes (e.g., width=1) go through. Reflow at width 1: each cell becomes one view row. Slow but correct.

### Inverse mapping past content end

`ViewToCursor` walks chains until exhausted, then fabricates `globalIdx = writeTop + (viewRow - contentRows)`, `col = viewCol`. Matches existing VT "address past content extends blank space" behavior.

### Resize mid-wrapped-write

VTerm's `wrapNext` evaluates against the new `rightEdge` at next write. If the new width makes the current cursor position in-bounds, `wrapNext` naturally clears. No new code needed.

### NoWrap flag on a row whose content is shifted by DECSTBM operations

When `NewlineInRegion` / `InsertLines` / `DeleteLines` shift rows, the NoWrap flag must move with the cells. `store.SetLine` extended to copy the flag along with cells. Narrow change.

### Corrupt persisted NoWrap bit

NoWrap is visual-only. Wrong value means a display quirk (row doesn't reflow when it should, or vice versa), not a correctness issue. No validation; recovery is "start a new session."

### Toggle flip while scrolled back

Recompute render at current `viewAnchor`. Flipping the toggle on (reflow off) snaps reflowable chains to 1:1; flipping it back restores reflow. Visible content position shifts at the flip but no data is lost.

## Testing

### Reflow correctness (`view_window_reflow_test.go`)

- Single logical line reflows at various widths (40, 80, 120).
- Multiple short lines (no wrapping) — reflow is a no-op.
- Mixed reflowable + NoWrap rows interleave correctly.
- NoWrap chain renders 1:1 even when contents would wrap.
- Chain propagation: mixed NoWrap in a chain renders whole chain as 1:1.
- Long line hits the `4 × height` cap gracefully.
- Reflow at width 1 (degenerate but valid).
- Global toggle: reflow-off renders everything 1:1 except NoWrap rows (already 1:1).

### Cursor mapping

- Round-trip: `CursorToView(ViewToCursor(r, c)) == (r, c)` for valid (r, c).
- Round-trip: `ViewToCursor(CursorToView(g, c)) == (g, c)` for in-view cursor.
- Cursor past view bottom → returns out-of-bounds.
- Cursor in NoWrap chain: 1:1 mapping.
- Cursor in reflowed chain: logical-col math correct.
- Inverse mapping past content end: extends blank.

### End-to-end resize

- Fill viewport → narrow → verify content reflowed.
- Fill viewport → widen → verify content rejoined.
- Shrink then expand → verify round-trip preserves original content.
- Resize with cursor in various positions (reflowable / NoWrap content).
- Resize while DECSTBM is active — NoWrap rows preserved 1:1.
- Session save/restore with NoWrap rows — flags survive round-trip.

### Regression

All existing tests in `sparse/*_test.go`, `parser/*_test.go`, and recovery tests must pass unchanged. Particular watchpoints:

- `TestRecovery_HWMSurvivesShrinkCloseExpand` — HWM persistence unaffected.
- Sparse cursor tests — `(globalIdx, col)` representation unchanged.
- DECSTBM tests — IL/DL/NewlineInRegion arithmetic unchanged (NoWrap preserves globalIdx-1:1 semantics for those regions).

### Visual/manual

- `ls -la` in a wide terminal, resize narrower mid-output — reflow looks right.
- Scroll back through history after reflow — historical content reflows.
- Main-screen app using DECSTBM (e.g., `progress`) — content stable on resize.
- Toggle reflow off with a reflowable viewport — snaps to 1:1.

## Rollout plan

1. Extend `sparse.Store` with per-row NoWrap flag + getters/setters. Make `SetLine` carry the flag through.
2. Add `decstbmActive` tracking to VTerm; propagate to `SetRowNoWrap` on writes.
3. Implement `ViewWindow.Render()`, `CursorToView`, `ViewToCursor`, `ScrollBy`. Keep old 1:1 path accessible as a fallback in case regressions surface.
4. Extend `.lhist` and WAL row-write serialization with NoWrap field (optional trailing, backward compatible).
5. Wire global toggle (default false, no keybind yet).
6. Test suite expansion per Section 5.
7. Manual verification + dogfooding.
8. Update `CLAUDE.md` — the "Scrollback Persistence" section currently states "No reflow on resize — the store is width-set-at-construction". Update to reflect view-side reflow.

Each step lands as its own PR where feasible; the `ViewWindow` change is the largest single chunk.

## Open questions (for implementation plan)

- Exact `.lhist` serialization: new version byte vs. optional trailing field? Preference: optional trailing for backward compat.
- Pending-scroll-region tracking: should NoWrap also apply during `DECSTBM` stores made outside the non-default range? (Answer leans toward "only non-default ranges count", per Section 3.2.)
- Visual-truncation behavior for the `4 × height` cap — show ellipsis? Truncate silently? Probably silent truncation + a render-time log.
- UX for the toggle (keybind, per-pane config) — out of scope for this design, tracked separately.
