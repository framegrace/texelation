# Texelterm Status Bar & Transformer Toggle Design

**Date:** 2026-02-21
**Status:** Approved (rev 2 — ToggleButton widget)

## Problem

1. **Ctrl+T only toggles overlay visibility**, not transformer processing. Transformers still run when "disabled" and can corrupt rendering for apps like Claude Code (detecting box-drawing as markdown/pipe tables).

2. **No status bar** in texelterm — users have no visibility into terminal modes (transformer state, insert/replace, TUI detection, wrap, reflow, alt screen).

## Design Decisions

- **Ctrl+T**: Binary ON/OFF toggle for the entire transformer pipeline + overlay visibility. Individual transformer selection stays in config.json.
- **Status bar**: Steals bottom 2 rows from terminal area (1 separator + 1 content). Always visible, no hide/show toggle.
- **Mode indicators**: Clickable `ToggleButton` widgets in the status bar. Active = reversed style, Inactive = normal style. 3-letter labels (TFM, INS, WRP, etc.).
- **Overlay handling on disable**: Hide existing overlays (`ShowOverlay=false`) but keep them in memory. Re-enabling shows them again instantly.
- **Status messages**: Show confirmation messages for all toggle actions (e.g., "Transformers OFF").
- **Widget reuse**: Bridge the existing `texelui/widgets/StatusBar` widget into TexelTerm's render pipeline via `core.Painter`.

## Architecture

### ToggleButton Widget

New widget in `texelui/widgets/togglebutton.go`:

```go
type ToggleButton struct {
    core.BaseWidget
    Label    string
    Active   bool
    OnToggle func(active bool)
}
```

- Compact: text only, 1 row, `len(label)` wide, no padding
- Active = reversed style (swap FG/BG), Inactive = normal style
- Clickable via `HandleMouse` — left click toggles and fires `OnToggle`
- Not focusable (lives in status bar, not in tab order)

### StatusBar Left-Side Extension

Replace `KeyHintsProvider` text with hosted child widgets:

```go
// SetLeftWidgets sets widgets to display on the left side of the status bar.
// Takes priority over KeyHintsProvider hints when set.
// Widgets are positioned left-to-right with 1-char gaps.
func (s *StatusBar) SetLeftWidgets(widgets []core.Widget)

// HandleMouse forwards mouse events to left-side widgets via hit testing.
func (s *StatusBar) HandleMouse(ev *tcell.EventMouse) bool
```

During `Draw()`, the StatusBar positions each widget sequentially and calls its `Draw()` method. During `HandleMouse()`, it hit-tests child widgets and forwards clicks.

### StatusBar Integration (Painter Bridge)

TexelTerm's `Render()` returns `[][]core.Cell`. The StatusBar widget's `Draw()` takes a `*core.Painter`, which writes into `[][]core.Cell` with clipping. The bridge:

1. `Resize(cols, rows)` passes `rows - 2` to VTerm, reserves 2 rows for status bar
2. `Render()` allocates a `height`-row buffer
3. Rows `0..height-3` filled from VTerm grid (terminal content)
4. `core.NewPainter(buf, clipRect)` created over the full buffer
5. `StatusBar.Draw(painter)` renders separator + content + toggle buttons into the buffer

### TexelTerm Mouse Routing

In `HandleMouse()`, add status bar check before mouse coordinator delegation:

```
history navigator → scrollbar → status bar (if y >= termRows) → mouse coordinator
```

Status bar mouse events are forwarded to `statusBar.HandleMouse()`, which hit-tests its child ToggleButton widgets.

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
    a.tfmToggle.Active = newState  // Update toggle button state
    if newState {
        a.statusBar.ShowMessage("Transformers ON")
    } else {
        a.statusBar.ShowMessage("Transformers OFF")
    }
}
```

### Mode Indicators

| Indicator | Source | Clickable | Action |
|-----------|--------|-----------|--------|
| `TFM` | `pipeline.Enabled()` | Yes | Toggle transformer pipeline |
| `INS`/`RPL` | `vterm.InsertMode()` | No (read-only) | Reflects terminal IRM state |
| `TUI`/`NRM` | `vterm.IsInTUIMode()` | No (read-only) | Reflects TUI detection |
| `WRP` | `vterm.WrapEnabled()` | Yes | Toggle line wrapping |
| `RFL` | `vterm.ReflowEnabled()` | Yes | Toggle reflow on resize |
| `ALT` | `vterm.InAltScreen()` | No (read-only) | Reflects alt screen state |

Read-only indicators still use ToggleButton for visual consistency but have no `OnToggle` callback. Their `Active` state is updated on every `Render()` to reflect current terminal state.

### VTerm Getter Methods

New exported getters needed on VTerm:

```go
func (v *VTerm) InsertMode() bool    { return v.insertMode }
func (v *VTerm) WrapEnabled() bool   { return v.wrapEnabled }
func (v *VTerm) ReflowEnabled() bool { return v.reflowEnabled }
func (v *VTerm) IsInTUIMode() bool   { ... }  // wraps unexported fixedWidthDetector()
```

`InAltScreen()` already exists.

## Files Changed

### TexelUI (`texelui/`)

| File | Change |
|------|--------|
| `widgets/togglebutton.go` | **New**: ToggleButton widget with Active state, reversed styling, HandleMouse |
| `widgets/statusbar.go` | Add `SetLeftWidgets()` method, `HandleMouse()` forwarding, draw child widgets |

### Texelation (`texelation/`)

| File | Change |
|------|--------|
| `apps/texelterm/transformer/transformer.go` | Add `enabled` field, `SetEnabled()`, `Enabled()` methods; guard all dispatch methods |
| `apps/texelterm/parser/vterm.go` | Add `InsertMode()`, `WrapEnabled()`, `ReflowEnabled()`, `IsInTUIMode()` getters |
| `apps/texelterm/term.go` | Store `pipeline` ref; create ToggleButtons + StatusBar; modify `Resize()` for status bar height; modify `Render()` for Painter bridge; replace `toggleOverlay()` with `toggleTransformers()`; add mouse routing for status bar; add `updateModeIndicatorsLocked()` |

## Config

No config changes. Ctrl+T is a session-level toggle (not persisted). Individual transformer enable/disable stays in `~/.config/texelation/apps/texelterm/config.json`.

## Non-Goals

- Cycling through individual transformers (txfmt only / tablefmt only) — deferred
- Hiding/showing the status bar — deferred
- Persisting toggle state across sessions — not needed
