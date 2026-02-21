# Texelterm Status Bar & Transformer Toggle Design

**Date:** 2026-02-21
**Status:** Approved

## Problem

1. **Ctrl+T only toggles overlay visibility**, not transformer processing. Transformers still run when "disabled" and can corrupt rendering for apps like Claude Code (detecting box-drawing as markdown/pipe tables).

2. **No status bar** in texelterm — users have no visibility into terminal modes (transformer state, insert/replace, TUI detection, wrap, reflow, alt screen).

## Design Decisions

- **Ctrl+T**: Binary ON/OFF toggle for the entire transformer pipeline + overlay visibility. Individual transformer selection stays in config.json.
- **Status bar**: Steals bottom 2 rows from terminal area (1 separator + 1 content). Always visible, no hide/show toggle.
- **Mode indicators**: All toggleable modes shown as 3-letter abbreviated labels. Enabled = bright (`text.primary`), disabled = dim (`text.muted`).
- **Overlay handling on disable**: Hide existing overlays (`ShowOverlay=false`) but keep them in memory. Re-enabling shows them again instantly.
- **Status messages**: Show confirmation messages for all toggle actions (e.g., "Transformers OFF").
- **Widget reuse**: Bridge the existing `texelui/widgets/StatusBar` widget into TexelTerm's render pipeline via `core.Painter`.

## Architecture

### StatusBar Integration (Painter Bridge)

TexelTerm's `Render()` returns `[][]core.Cell`. The StatusBar widget's `Draw()` takes a `*core.Painter`, which writes into `[][]core.Cell` with clipping. The bridge:

1. `Resize(cols, rows)` passes `rows - 2` to VTerm, reserves 2 rows for status bar
2. `Render()` allocates a `height`-row buffer
3. Rows `0..height-3` filled from VTerm grid (terminal content)
4. `core.NewPainter(buf, clipRect)` created over bottom 2 rows
5. `StatusBar.Draw(painter)` renders separator + content into the buffer

### StatusBar Left-Side Extension

The StatusBar widget currently supports only plain dimmed text on the left (from `KeyHintsProvider`). Mode indicators need per-token styling. Extension:

```go
// StyledSegment is a text fragment with optional custom styling.
type StyledSegment struct {
    Text  string
    Style tcell.Style // zero value = default hint style (dimmed)
}

// SetLeftSegments sets styled content for the left side.
// Takes priority over KeyHintsProvider when set.
func (s *StatusBar) SetLeftSegments(segments []StyledSegment)
```

TexelTerm builds segments per mode indicator, each with bright or dim styling.

### Transformer Pipeline Toggle

Add runtime enable/disable to `Pipeline`:

```go
type Pipeline struct {
    transformers      []Transformer
    enabled           bool  // runtime toggle
    // ... existing fields
}

func (p *Pipeline) SetEnabled(on bool)
func (p *Pipeline) Enabled() bool

func (p *Pipeline) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) bool {
    if !p.enabled { return false }
    // ... existing logic
}
```

All pipeline methods (`HandleLine`, `NotifyPromptStart`, `NotifyCommandStart`) short-circuit when disabled.

### Ctrl+T Handler

Replace `toggleOverlay()` with `toggleTransformers()`:

```go
func (a *TexelTerm) toggleTransformers() {
    if a.pipeline == nil { return }
    newState := !a.pipeline.Enabled()
    a.pipeline.SetEnabled(newState)
    a.vterm.SetShowOverlay(newState)
    a.vterm.MarkAllDirty()
    a.updateModeIndicators()
    if newState {
        a.statusBar.ShowMessage("Transformers ON")
    } else {
        a.statusBar.ShowMessage("Transformers OFF")
    }
}
```

### Mode Indicators

| Indicator | Source | Meaning |
|-----------|--------|---------|
| `TFM` | `pipeline.Enabled()` | Transformer pipeline on/off |
| `INS`/`RPL` | `vterm.InsertMode()` | Insert vs Replace mode |
| `TUI`/`NRM` | `fixedWidthDetector.IsInTUIMode()` | TUI app detected vs normal shell |
| `WRP` | `vterm.WrapEnabled()` | Line wrapping on/off |
| `RFL` | `vterm.ReflowEnabled()` | Reflow on resize on/off |
| `ALT` | `vterm.InAltScreen()` | Alt screen active (informational) |

Rendered as: `TFM INS NRM WRP RFL` with bright/dim per state. Updated on every `Render()` call.

### VTerm Getter Methods

New exported getters needed on VTerm:

```go
func (v *VTerm) InsertMode() bool   { return v.insertMode }
func (v *VTerm) WrapEnabled() bool  { return v.wrapEnabled }
func (v *VTerm) ReflowEnabled() bool { return v.reflowEnabled }
```

`InAltScreen()` and `fixedWidthDetector()` already exist.

## Files Changed

### TexelUI (`texelui/`)

| File | Change |
|------|--------|
| `widgets/statusbar.go` | Add `StyledSegment` type, `SetLeftSegments()` method, draw logic for styled segments |

### Texelation (`texelation/`)

| File | Change |
|------|--------|
| `apps/texelterm/transformer/transformer.go` | Add `enabled` field, `SetEnabled()`, `Enabled()` methods; guard all dispatch methods |
| `apps/texelterm/parser/vterm.go` | Add `InsertMode()`, `WrapEnabled()`, `ReflowEnabled()` getters |
| `apps/texelterm/term.go` | Store `pipeline` reference; create/start `StatusBar`; modify `Resize()` for status bar height; modify `Render()` for Painter bridge; replace `toggleOverlay()` with `toggleTransformers()`; add `updateModeIndicators()` |

## Config

No config changes. Ctrl+T is a session-level toggle (not persisted). Individual transformer enable/disable stays in `~/.config/texelation/apps/texelterm/config.json`.

## Non-Goals

- Cycling through individual transformers (txfmt only / tablefmt only) — deferred
- Hiding/showing the status bar — deferred
- Persisting toggle state across sessions — not needed
