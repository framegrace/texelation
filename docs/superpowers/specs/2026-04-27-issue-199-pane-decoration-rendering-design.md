# Issue #199 — Pane Border / Decoration Row Rendering

**Date:** 2026-04-27
**Branch:** `feature/issue-199-pane-decoration-rendering`
**Status:** Design — ready for plan
**Depends on:** Plan A (PR #202), Plan B (PR #203), Plan D (PR #204), Plan D2 (PR #205)
**Unblocks:** none directly; cleans up a precondition for Plan C selection rendering

## Context

Plan D2's manual e2e (2026-04-26) surfaced a visual bug: after daemon-restart rehydrate, the texelterm pane renders three blank "lighter gray" rows at the top, no top/bottom borders, and content shifted ~3 rows down. Investigation showed the bug is **pre-existing**, not D2-specific — it's been latent since Plan A. D2's rehydrate path made it user-visible by exercising a code path where `maxGid` is large; on clean start, content overwrites the same rows because `maxGid` clamps to zero.

Three classes of row are affected. All have `RowGlobalIdx[y] = -1` in the pane snapshot, and all are dropped by `bufferToDelta`'s `gid < 0` filter so they never reach the client:

1. **Pane top border** at rowIdx 0
2. **Pane bottom border** at rowIdx H-1
3. **App-internal decorations** like texelterm's internal statusbar at rowIdx H-2

Today the client treats every one of `vp.Rows` rows as a content row, computes `top := maxGid - (vp.Rows - 1)`, and queries `PaneCache.RowAt(gid)` for each rowIdx. The 3 lowest gids in the projected window are not in `PaneCache` (they don't correspond to content), so they render blank — and they land at the top of the pane.

Full root-cause analysis lives in Task 16 of the Plan D2 plan and in `~/.claude/projects/.../memory/project_issue199_pane_border_render_bug.md`.

## Goals

1. After daemon-restart rehydrate, pane top + bottom borders are visible and content sits inside them.
2. App-internal decoration rows (texelterm's statusbar) render their actual content, not blank.
3. Same fix applies to clean start, scrolled-mid-history, focus changes, and resize — not just the rehydrate path that surfaced the bug.
4. Rendering remains server-authoritative. Client gains no new rendering knowledge about borders, themes, or app internals.
5. Protocol bump from v2 → v3. No backward-compat shims (per project policy).

## Non-goals

- Per-client themes (no current goal; would require client-side theme state).
- Animating border color transitions (already handled by the effects pipeline; orthogonal).
- Selection-aware decoration filtering. Plan C will read the new `ContentTopRow` / `ContentBottomRow` fields when it lands; this spec just provides them.
- Reworking the alt-screen render path. Alt-screen panes already use a positional encoding and are unaffected.
- Bundling Task 17's deferred mediums. None of 17.A–F touch the same files.

## Architecture

### One render path, two layers

Server stays the source of truth for every visible cell. The client renders each non-altScreen pane by composing two layers:

- **Content layer** — gid-keyed `PaneCache` rows (existing, viewport-clipped). Holds rows where `RowGlobalIdx[y] >= 0`.
- **Decoration layer** — rowIdx-keyed positional cache (new). Holds rows where `RowGlobalIdx[y] < 0`: pane chrome (top/bottom borders) and app decorations (texterm's statusbar).

`PaneSnapshot` gains `ContentTopRow` / `ContentBottomRow` so the client knows which rowIdx range maps to gids and which falls back to the decoration layer.

### Why not client-side chrome rendering (rejected Approach A)

The original Plan D2 Task 16 entry suggested Option 4 + Option 2 (client renders pane chrome itself from `pane.Rect` + theme; server tells client where content sits). Rejected because:

- App-specific decorations (texterm internal statusbar at H-2) **cannot** be moved client-side — only the server-side app knows what's in that row. So the server must keep a positional decoration path regardless of where chrome is drawn.
- Once the server has a positional decoration path for app decor, also using it for borders avoids splitting render responsibility across two locations and duplicating border-drawing + theme logic on the client.
- Bandwidth win is small. Borders re-ship only on focus / title / resize events; steady-state deltas carry zero decoration rows because the existing positional `prev[y]` diff already filters unchanged rows.
- Server-authoritative chrome plays better with future Plan C (server-side selection): selection operates on gid-keyed rows; decoration rows are excluded by construction.

### Why not "ship every row including borders, drop ContentTopRow/Bottom" (rejected Approach C)

The viewport math fundamentally needs to know how many of the pane's `H` rows are content. Without that, `top := maxGid - (numContentRows - 1)` cannot be computed and the rowIdx-to-gid mapping stays broken. The client cannot derive `numContentRows` from the delta itself because incremental deltas don't carry every row. So `ContentTopRow` / `ContentBottomRow` are required regardless of how decoration rows are encoded.

## Protocol changes (v2 → v3)

### `protocol.PaneSnapshot` gains two fields

```go
type PaneSnapshot struct {
    // ... existing fields ...
    ContentTopRow    uint16 // first content rowIdx; 0 if no top decoration
    ContentBottomRow uint16 // last content rowIdx; H-1 if no bottom decoration
}
```

Semantics:

- `ContentBottomRow >= ContentTopRow`: pane has at least one content row.
- `ContentBottomRow < ContentTopRow`: pane has zero content rows (all-decoration apps; e.g., a static dialog). Client renders the entire pane from the decoration layer.
- `ContentTopRow == 0 && ContentBottomRow == H-1`: pane has no decoration rows (alt-screen panes hit this implicitly; main-screen panes without borders would too, though all current panes have borders).

For alt-screen panes the fields are populated but ignored — the existing alt-screen render path handles everything positionally via `PaneCache.AltRowAt`.

### `protocol.BufferDelta` gains one field

```go
type BufferDelta struct {
    // ... existing fields ...
    DecorRows []RowDelta // rows keyed by absolute rowIdx (not gid - RowBase)
}
```

`RowDelta` shape is unchanged. For `DecorRows`, `RowDelta.Row` is interpreted as the absolute rowIdx in the pane buffer. For the existing `Rows`, it remains `gid - RowBase`.

Separate slice rather than a flag on `RowDelta` keeps the two semantically-distinct row types unambiguous at decode time and means existing code paths don't have to branch on every row.

### Wire-format encoding

Append `ContentTopRow` (2 bytes) and `ContentBottomRow` (2 bytes) at the tail of the existing `PaneSnapshot` encoder. Append `DecorRows` (count-prefixed, same per-row encoding as `Rows`) at the tail of `BufferDelta`. No reordering of existing fields. Increment `protocol.Version` from 2 to 3.

### Backward compatibility

None. Per project policy, stale on-disk state should fail-and-overwrite, not auto-migrate. Bumping the protocol version causes pre-v3 clients to fail handshake; users restart their client.

## Server changes

### `texel/snapshot.go` — `capturePaneSnapshot`

Already populates `RowGlobalIdx[i] = -1` for non-content rows. Add: after building `RowGlobalIdx`, compute `ContentTopRow` (first index with `gid >= 0`) and `ContentBottomRow` (last index with `gid >= 0`). If no row has `gid >= 0`, set `ContentTopRow = 1` and `ContentBottomRow = 0` (the "zero content rows" sentinel — `Bottom < Top`). Store on `PaneSnapshot`.

### `internal/runtime/server/tree_convert.go` — `treeCaptureToProtocol`

Currently sets `Rows: nil` on protocol PaneSnapshot. Pass through the new `ContentTopRow` / `ContentBottomRow` fields. `Rows` stays nil — borders and decorations ride deltas, not snapshots.

### `internal/runtime/server/desktop_publisher.go` — `bufferToDelta`

For non-altScreen panes, drop the `gid < 0` skip. Instead route those rows into `DecorRows` keyed by absolute `y`:

```go
for y, row := range snap.Buffer {
    if len(row) == 0 {
        continue
    }
    if y >= len(snap.RowGlobalIdx) {
        continue
    }
    if y < len(prev) && rowsEqual(row, prev[y]) {
        continue
    }
    gid := snap.RowGlobalIdx[y]
    if gid < 0 {
        decorRows = append(decorRows, protocol.RowDelta{
            Row:   uint16(y),
            Spans: encodeRow(row),
        })
        continue
    }
    if gid < lo || gid > hi {
        continue
    }
    rows = append(rows, protocol.RowDelta{
        Row:   uint16(gid - lo),
        Spans: encodeRow(row),
    })
}
delta.DecorRows = decorRows
```

The existing positional `prev[y]` diff continues to filter unchanged rows for both layers — only changed decoration rows ship.

For alt-screen panes the path is unchanged; `DecorRows` stays empty.

## Client changes

### `client/buffercache.go` — decoration cache

Add a per-pane decoration cache as a field on `PaneState`:

```go
type PaneState struct {
    // ... existing fields ...
    ContentTopRow    uint16
    ContentBottomRow uint16
    DecorRows        map[uint16][]Cell // rowIdx -> cells; populated from BufferDelta.DecorRows
}
```

Apply `DecorRows` from each incoming `BufferDelta` into this map. Mark the pane dirty whenever a decoration row changes. `BufferCache.ResetRevisions` (used on session-reuse / new publisher) must also clear `DecorRows` per pane so a fresh publisher republishes everything.

### `internal/runtime/client/viewport_tracker.go` — `onBufferDelta`

Replace:

```go
top := maxGid - int64(vp.Rows-1)
```

with:

```go
numContentRows := int(pane.ContentBottomRow) - int(pane.ContentTopRow) + 1
if numContentRows <= 0 {
    return // pane has no content rows; nothing to anchor
}
top := maxGid - int64(numContentRows-1)
```

`pane.ContentTopRow` / `ContentBottomRow` are read from the `BufferCache.Pane` populated from the latest `PaneSnapshot`.

### `internal/runtime/client/renderer.go` — `rowSourceForPane`

Two-path lookup for non-altScreen panes:

```go
if vc.AltScreen {
    // unchanged: PaneCache.AltRowAt + RowCellsDirect fallback
}

// Decoration layer
if rowIdx < int(pane.ContentTopRow) || rowIdx > int(pane.ContentBottomRow) {
    if row, ok := pane.DecorRows[uint16(rowIdx)]; ok {
        return row
    }
    return nil
}

// Content layer
contentRowIdx := rowIdx - int(pane.ContentTopRow)
gid := vc.ViewTopIdx + int64(contentRowIdx)
row, found := pc.RowAt(gid)
if !found {
    return nil
}
return row
```

A miss on either layer returns nil (renders blank). The decoration layer should hit after the first delta following the snapshot; the content layer can legitimately miss while a `MsgFetchRange` is in flight (existing Plan A behavior — preserved).

## Data flow

### Initial connect / resume

1. Server captures snapshot → `ContentTopRow` / `ContentBottomRow` computed.
2. Server emits `MsgTreeSnapshot` with the new fields.
3. Server's first `BufferDelta` after the snapshot ships every changed row; on a fresh publisher (`prev` is empty), every row counts as changed, so all decoration rows ride along.
4. Client populates `BufferCache.Pane.ContentTopRow/Bottom` from the snapshot, then `DecorRows` from the delta.
5. Renderer composites both layers.

### Focus change

1. Server's `pane.border.Draw` repaints rows 0 and H-1 in the pane buffer with the new active/inactive style.
2. `bufferToDelta`'s positional diff detects rows 0 and H-1 changed → emits 2 entries in `DecorRows`. Content rows unchanged → empty `Rows`.
3. Client applies → decoration cache updates → pane re-renders with new border style.

### Resize

1. Pane geometry changes → `ContentTopRow` / `ContentBottomRow` recomputed in next snapshot.
2. Server emits fresh snapshot + delta.
3. Client updates and re-renders.

### Daemon-restart rehydrate (Plan D2 path)

Identical to "initial connect / resume" above. The publisher on the rehydrated session has empty `prev`, so the first post-resume delta carries all decoration rows. Borders + decorations render correctly from the first frame.

## Error handling

- **Decoration cache miss after first delta** — render blank. Mirrors content-row miss behavior. Preserves the "no stale chrome" guarantee.
- **`ContentTopRow > ContentBottomRow + 1`** with non-zero buffer height — server-side bug. Log via the existing publisher logger; treat pane as zero content rows on the wire (only sentinel `Bottom < Top` should be emitted; any other inversion is a bug).
- **Client receives `DecorRows` before its first `PaneSnapshot`** — should not happen (snapshot precedes deltas in the protocol). If it does, drop those `DecorRows` and rely on the next delta after `prev` resync. No explicit buffering needed.
- **Stale decoration cache on session reuse** — `BufferCache.ResetRevisions` (Plan D's revision-monotonicity fix) is the natural pivot point. Extend it to also clear `DecorRows` per pane. The new publisher's empty `prev` then republishes everything.

## Testing

### Unit (server)

- `TestBufferToDelta_DecorationRowsIncluded` — pane with top/bottom borders ships exactly 2 entries in `DecorRows` (rowIdx 0 and H-1) on first publish.
- `TestBufferToDelta_DecorationRowsDiffed` — second publish with unchanged borders ships zero `DecorRows`.
- `TestBufferToDelta_DecorationRowsDiffPartial` — repaint of just rowIdx 0 (e.g., title change) ships exactly one `DecorRows` entry.
- `TestBufferToDelta_TexelTermInternalStatusbar` — pane backed by texterm with internal statusbar at H-2 ships that row in `DecorRows`.
- `TestCapturePaneSnapshot_ContentBoundsComputed` — `RowGlobalIdx = [-1, 0, 1, 2, -1, -1]` produces `ContentTopRow=1, ContentBottomRow=3`.
- `TestCapturePaneSnapshot_ContentBoundsAllDecoration` — all rows have `gid<0` (no content) produces `ContentTopRow=1, ContentBottomRow=0` sentinel.
- `TestBufferToDelta_AltScreenLeavesDecorRowsEmpty` — alt-screen pane never emits `DecorRows`.

### Unit (client)

- `TestRowSourceForPane_DecorationLayer` — rowIdx 0 reads from `DecorRows`; rowIdx between bounds reads via gid; rowIdx H-1 reads from `DecorRows`.
- `TestOnBufferDelta_TopUsesContentRowCount` — pane with `ContentTopRow=1, ContentBottomRow=H-3` (texterm shape) computes `top = maxGid - (H-3)` not `maxGid - (H-1)`.
- `TestRowSourceForPane_DecorationCacheMissReturnsNil` — pane without populated `DecorRows` returns nil for decoration rowIdx, renders blank.
- `TestBufferCache_ResetRevisionsClearsDecorRows` — `ResetRevisions` empties the per-pane decoration map.

### Unit (protocol)

- `TestEncodeDecodePaneSnapshot_ContentBounds` — round-trip preserves new fields.
- `TestEncodeDecodeBufferDelta_DecorRows` — round-trip preserves DecorRows; empty slice round-trips as zero-length.
- `TestVersion3` — `protocol.Version == 3`; `MsgWelcome` reflects the bump.

### Integration (memconn)

- `TestPaneRenders_AllFourBorders_CleanStart` — fresh session, single pane, snapshot + first delta arrive → renderer composite has top/bottom border characters at rowIdx 0/H-1, left/right border chars at col 0/W-1, content in between.
- `TestPaneRenders_AllFourBorders_AfterRehydrate` — extends the Plan D2 cross-restart harness. After daemon restart + reconnect, the rendered pane has all four borders + texterm internal statusbar at H-2.
- `TestPaneRenders_AllFourBorders_ScrolledMidHistory` — scrolled away from live edge (autoFollow=false), borders + decorations remain rendered.
- `TestPaneRenders_FocusChangeRepaintsBorders` — pane focus toggles → BufferDelta carries 2 DecorRows → composite reflects new active-style borders.

### Manual e2e

Repeat Plan D2 Step 6: daemon AND client both restart with a long-running texelterm session.

Pass criteria:

1. All 4 pane borders visible from first paint.
2. Texterm internal statusbar at H-2 shows actual content (not blank).
3. Content occupies rows 1..H-3 and is not offset.
4. Focus toggle (Ctrl-A pane switch) repaints borders without flicker.
5. Window resize triggers correct `ContentTopRow` / `ContentBottomRow` recompute and re-render.

## Touchpoints summary

| File | Change |
|------|--------|
| `protocol/messages.go` | `PaneSnapshot.ContentTopRow/Bottom`, `BufferDelta.DecorRows`, `Version=3` |
| `protocol/encode.go` | Encode new fields |
| `protocol/decode.go` | Decode new fields |
| `texel/snapshot.go` | `capturePaneSnapshot` computes Content bounds |
| `internal/runtime/server/tree_convert.go` | `treeCaptureToProtocol` passes through |
| `internal/runtime/server/desktop_publisher.go` | `bufferToDelta` emits `DecorRows` |
| `client/buffercache.go` | `PaneState.{ContentTopRow,ContentBottomRow,DecorRows}` fields; `ResetRevisions` clears `DecorRows` |
| `internal/runtime/client/viewport_tracker.go` | `onBufferDelta` `top` uses content row count |
| `internal/runtime/client/renderer.go` | `rowSourceForPane` two-layer lookup |

## Invariants

- **Decoration contiguity** — decoration rows (`RowGlobalIdx[y] < 0`) must be contiguous at the top and/or bottom of the pane buffer. Concretely: `ContentTopRow` is the smallest `y` with `RowGlobalIdx[y] >= 0`, `ContentBottomRow` is the largest, and every `y` outside `[ContentTopRow, ContentBottomRow]` has `RowGlobalIdx[y] < 0` while every `y` inside has `RowGlobalIdx[y] >= 0`. This holds today by construction in `capturePaneSnapshot` (top border at 0, content via `RowGlobalIdxProvider` in [1..H-2], app statusbar / bottom border in the trailing slots). If a future app interleaves decoration mid-pane, this design must be revisited.
- **Decoration positional vs content gid-keyed** — `BufferDelta.DecorRows[*].Row` is always an absolute rowIdx; `BufferDelta.Rows[*].Row` is always `gid - RowBase`. These two encodings never collide because they live in different slices.
- **First-delta decoration completeness** — when the publisher sees an empty `prev` (fresh session, post-reset, post-resume), every changed row is shipped, including all decoration rows. This is what makes the rehydrate path render correctly from frame 1.

## Open questions

None. The exact wire byte layout for the new fields is mechanical and the plan will pin it.

## Self-review

- Placeholders: none.
- Internal consistency: the two new `PaneSnapshot` fields and the new `BufferDelta` field are referenced consistently across server, client, and tests.
- Scope: bounded — single-pass change covering one bug class. No bundled refactors.
- Ambiguity: `ContentTopRow`/`ContentBottomRow` semantics include the `Bottom < Top` zero-content sentinel. `DecorRows.Row` semantics (absolute rowIdx vs `gid - RowBase`) are explicit.
