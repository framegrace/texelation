# Client Render Optimization — Incremental Compositing

**Date**: 2026-03-31
**Status**: Approved

## Overview

Optimize the texelation client render path to near-zero CPU at idle by tracking dirtiness at pane and row level, reusing a persistent workspace buffer, and only re-compositing and re-rendering what actually changed.

## Problem

The client re-renders the entire screen every frame: allocates a full `height × width` workspace buffer, copies every pane's cells into intermediate buffers, composites all panes, writes every cell to tcell, even when nothing changed. This causes unnecessary CPU usage at idle.

## Design

### Persistent Workspace Buffer

Replace the per-frame `workspaceBuffer` allocation with a `prevBuffer [][]client.Cell` on `clientState`. Allocated once on first render or resize, reused across frames. A `fullRenderNeeded` flag forces a complete re-composite.

`fullRenderNeeded` is set on:
- First render (`prevBuffer` is nil)
- Screen resize
- Tree snapshot received (pane geometry changed — move, resize, add, remove)
- Any workspace effect is `Active()`
- Workspace switch

### Pane Dirty Tracking

`BufferCache.ApplyDelta` already knows which pane changed. Add a `dirty bool` flag on `PaneState`. Set it when `ApplyDelta` updates the pane. `ApplySnapshot` sets `dirty` on all panes (geometry may have changed).

During compositing, `compositeInto` skips panes where `dirty == false`. After compositing a dirty pane, clear the flag.

### Row Dirty Tracking Within Panes

`ApplyDelta` already knows which rows changed (from `RowDelta`). Add `dirtyRows map[int]bool` on `PaneState`. Set rows dirty when delta arrives. During compositing, only copy and process dirty rows from the pane. Clear after compositing.

For the workspace buffer, only overwrite the screen rows/columns that correspond to dirty pane rows.

When a pane is fully dirty (from snapshot or first connection), `dirtyRows` is nil — meaning all rows are dirty.

### Pane Effects Mark Pane Dirty

When `ApplyPaneEffects` runs and the effect is `Active()`, mark the pane as dirty. This forces re-compositing of that pane's cells through the effect. When the effect ends, the pane is no longer force-dirtied.

### Workspace Effects Fall Back to Full Render

If any workspace effect is `Active()`, set `fullRenderNeeded`. This triggers the current full-render path. When no workspace effects are active, the incremental path runs. Workspace effects are rare and transient (fade, screensaver).

### Diff-Based Screen Output

Replace `showWorkspaceBuffer` (writes every cell to tcell) with a diff against `prevBuffer`. Only call `screen.SetContent` for cells whose `Ch` or `Style` changed. Remove `screen.Clear()` from the incremental path — it's no longer needed since we only update changed cells.

The full-render path still uses `screen.Clear()` + write-all for correctness on first render and geometry changes.

### Render Flow

```
render(state, screen):
  if fullRenderNeeded OR workspace effects active:
    → current full-render path (allocate, composite all, write all)
    → store result in prevBuffer
    → clear all pane dirty flags
    → fullRenderNeeded = false
    return

  hasDynamic := false
  for each pane in sortedPanes:
    if not pane.dirty:
      // Still check for animated cells in the cached buffer
      // (needed to keep animTicker running)
      continue

    for each row in pane:
      if dirtyRows != nil and row not in dirtyRows:
        continue

      copy row from cache
      resolve dynamic colors (FromDesc/Resolve for DynBG/DynFG)
      if any cell is animated: hasDynamic = true
      write to prevBuffer at pane's screen position

    apply pane effects if active (marks pane dirty while effect runs)
    clear pane.dirty and pane.dirtyRows

  // Diff-based output: only SetContent for cells that changed
  for each row in prevBuffer:
    for each cell:
      if cell != oldCell:
        screen.SetContent(x, y, cell.Ch, nil, cell.Style)

  screen.Show()
  state.dynAnimating = hasDynamic
```

### Animation Ticker Interaction

The `dynAnimating` flag still tracks whether animated cells exist. During incremental renders, if no pane is dirty, `hasDynamic` stays false from the previous frame's value. This is correct: if animated cells exist but nothing changed, the animation ticker keeps running and forces a render. But in `compositeInto`, the animated pane IS dirty (the ticker triggers a render, and animated panes produce different resolved colors each frame — the pane itself hasn't changed, but the resolved output has).

For animated panes: the animation ticker triggers a render. The pane's `dirty` flag may be false (no new delta from server). But animated cells need re-resolution. Solution: when `hasDynamic` was true on the previous frame, mark those panes as dirty before the next incremental render. Or simpler: skip pane-level dirty tracking for panes that contain animated cells — re-composite them every frame.

### Files Changed

| File | Change |
|------|--------|
| `client/buffercache.go` | Add `dirty bool`, `dirtyRows map[int]bool` to `PaneState`; set in `ApplyDelta`/`ApplySnapshot`; add `ClearDirty()` method |
| `internal/runtime/client/renderer.go` | Persistent buffer, incremental compositing, diff-based output, full-render fallback |
| `internal/runtime/client/client_state.go` | Add `prevBuffer [][]client.Cell`, `fullRenderNeeded bool` |
| `internal/runtime/client/protocol_handler.go` | Set `fullRenderNeeded` on tree snapshot |

### Expected Impact

- **Idle**: near-zero CPU (no renders at all)
- **Typing in terminal**: only the active pane's changed rows are re-composited and diffed
- **Workspace switch / resize**: one full render, then incremental
- **Effects active**: full render per frame (same as today, effects are transient)
- **Animated colors**: animated panes re-composite per frame, non-animated panes skip
