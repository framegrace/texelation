# Dual-Layer Line System Design

## Goal

Each terminal line carries both its original content and an optional formatted overlay, both persisted to disk, with a global toggle to switch between views.

## Problem

Transformers (txfmt, tablefmt) modify line content in-place, destroying the original. Formatted content also has persistence/scroll issues: suppressed lines create gaps, replaced content isn't properly notified to the persistence layer, and on restart the formatted view is lost.

## Architecture

LogicalLine gains two new fields: `Overlay []Cell` for formatted content and `Synthetic bool` for transformer-inserted lines (borders, separators). The original `Cells` field is never mutated by transformers. A global toggle switches the viewport between rendering `Cells` or `Overlay`. Both layers are persisted in a new TXLHIST2 disk format.

## Data Model

```go
type LogicalLine struct {
    Cells        []Cell  // Original content (ground truth, always present)
    FixedWidth   int     // Existing: non-zero means Cells shouldn't reflow
    Overlay      []Cell  // Formatted content (nil = no overlay)
    OverlayWidth int     // Width the overlay was created at (0 = use len(Overlay))
    Synthetic    bool    // True = inserted by transformer, hidden in original view
}
```

### Rules

- `Cells` is never mutated by transformers. It always holds the original terminal output.
- `Overlay` is set by transformers when they want to show a formatted version. `nil` means no formatting.
- `Synthetic` lines have no original content (`Cells` is nil). Content is only in `Overlay`. In original view, synthetic lines are hidden entirely.
- `Clone()` deep-copies both `Cells` and `Overlay`.
- Overlay is always treated as fixed-width at `OverlayWidth` (no reflow). Original `Cells` uses existing `FixedWidth` behavior.

## Persistence Format (TXLHIST2)

### Line encoding

```
[flags:1 byte]
[cell_count:4 bytes][cell1:16 bytes]...         <- Original Cells
[overlay section - only if flags bit 0 set]
  [overlay_width:4 bytes]
  [overlay_cell_count:4 bytes][cell1:16 bytes]... <- Overlay Cells
```

### Flags byte

| Bit | Mask   | Meaning          |
|-----|--------|------------------|
| 0   | `0x01` | Has overlay      |
| 1   | `0x02` | Synthetic line   |
| 2-7 |        | Reserved         |

### Migration

- New code reads TXLHIST1 files and treats every line as original-only (no overlay).
- New code writes TXLHIST2 with updated magic header.
- Lines without overlays cost +1 byte (flags byte) over TXLHIST1.

## Viewport Toggle

### Toggle state

A `showOverlay bool` field on VTerm/TexelTerm, default `true`. Toggled by a keybind (configurable, default `Ctrl+T`).

### Rendering logic

```
for each LogicalLine in visible range:
    if !showOverlay:
        skip if line.Synthetic
        render line.Cells with normal reflow (FixedWidth)
    else:
        if line.Overlay != nil:
            render line.Overlay as fixed-width (OverlayWidth)
        else:
            render line.Cells with normal reflow (FixedWidth)
```

### Toggle behavior

- Setting `showOverlay` invalidates the viewport cache.
- Physical line count changes (synthetic lines appear/disappear, reflow differences).
- Scroll position anchored on nearest logical line to current view center, then recomputed.

## Transformer Pipeline Changes

### Current flow (broken)

1. `OnLineCommit` calls transformer pipeline.
2. Transformer modifies `line.Cells` directly (destroys original).
3. Suppressed lines → `line.Clear()` → no persistence.
4. Inserted lines → new lines with content in `Cells`.

### New flow

1. `OnLineCommit` calls transformer pipeline.
2. `replaceFunc(lineIdx, overlayCells)` sets `line.Overlay` and `line.OverlayWidth`. `line.Cells` is untouched.
3. `insertFunc(beforeIdx, overlayCells)` creates `LogicalLine{Synthetic: true, Overlay: overlayCells}`.
4. No more suppression of persistence. Lines with overlays are persisted normally (both layers).

### Suppression semantics change

- `ShouldSuppress` still means "I'm buffering, don't persist yet."
- On flush, lines are un-suppressed and persisted with both layers.
- The `ShouldSuppress → line.Clear()` path is removed for overlay lines.

### VTerm method changes

- `RequestLineReplace` → `RequestLineOverlay`: sets `line.Overlay` instead of overwriting `line.Cells`.
- `RequestLineInsert` → creates `Synthetic` lines with content in `Overlay`.
- After flush, persistence is notified for all affected lines.

## Resize Behavior

- Overlay content persists at original width, clips/pads on resize (never re-run transformers).
- Original content reflows normally (existing behavior).
- Toggling is like a resize: viewport cache invalidated, physical lines recomputed.

## Decisions Log

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Toggle scope | Global | Simple, predictable UX |
| Synthetic lines in original view | Hidden | Clean original view |
| Resize behavior for overlays | Fixed-width, clip/pad | Avoids re-running transformers |
| Data model | Inline on LogicalLine | Single object to persist, minimal indirection |
| Disk format | TXLHIST2 (versioned) | Clean migration, backwards compatible reads |
| FixedWidth | Per-layer | Overlay always fixed, original uses existing behavior |
