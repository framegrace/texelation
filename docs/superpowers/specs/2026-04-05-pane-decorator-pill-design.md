# Pane Decorator Pill

## Overview

A collapsible pill-shaped action bar drawn on the pane's top border row. Apps register actions via ControlBus messages; texelation adds its own window-manager actions (e.g., zoom toggle). The pill uses zero terminal space — it overlays the border characters. Replaces texelterm's internal toggle overlay entirely.

## Motivation

The current toggle pill lives inside the texelterm terminal area, consuming a row and coupling the UI to one specific app. Moving it to the pane border makes it a generic texelation feature that any app can use, frees up terminal space, and enables window-manager actions like zoom/maximize.

## Architecture

The pane owns the decorator. It stores a list of actions (app-side and texelation-side), draws them on the border, and handles mouse interaction.

### Zones

- **Left zone**: App actions (registered via ControlBus). E.g., config, transformers, wrap, search for texelterm.
- **Right zone**: Texelation (window manager) actions. Separated by `│` from app actions if non-empty.
- First texelation action: zoom toggle — `󰊓` (fullscreen) / `󰊕` (fullscreen-exit).

### States

- **Collapsed**: `` ≡ `` — single hamburger icon with Powerline rounded caps on the border.
- **Expanded**: `` [app actions] │ [wm actions] `` — all actions visible with caps on both ends.
- **Always expanded**: configurable via `pane_decorator_expanded` in system config. When true, the pill never collapses to hamburger.

Hover over the collapsed pill expands it. Mouse leaving collapses it. Both states use Powerline rounded endcaps (`` left, `` right).

## ControlBus API

### DecoratorAction

```go
type DecoratorAction struct {
    ID       string   // unique identifier
    Icon     rune     // Nerd Font icon character
    Help     string   // hover tooltip text for status bar
    Active   bool     // toggle state (highlighted when true)
    Disabled bool     // grayed out, not clickable
    OnClick  func()   // called on mouse click
}
```

### Messages

- `decorator.add` — payload: `DecoratorAction`. Adds or updates action by ID. Order is preserved (first add = leftmost).
- `decorator.remove` — payload: `string` (action ID). Removes the action.
- `decorator.update` — payload: `DecoratorAction`. Updates Active/Disabled state by ID without changing position or OnClick.

## Rendering

New file: `texel/pane_decorator.go`

### Drawing

The pane's `Render` method calls `drawDecorator` after drawing the border. The pill is drawn on the top border row, right-aligned, ending before the top-right corner character.

- Each action is 3 characters wide: ` icon ` (space, icon, space)
- Separator `│` between app and WM zones (1 char) — only drawn if both zones have actions
- Caps: 1 char each (`` left, `` right)
- Style: same foreground and background as the border (respects focus/resizing/normal state). Active actions use `border.active` foreground. Disabled actions use `text.muted`.
- Caps use the Powerline trick: pill BG as cap FG, border BG as cap BG — creates rounded integration with the border.

### Pill Width Calculation

```
collapsed: 3 (left cap + hamburger + right cap)
expanded:  2 (caps) + appActions*3 + separator(0 or 1) + wmActions*3
```

### Position

Right-aligned on the top border row: `x = absX1 - pillWidth`. The title (left-aligned) and pill (right-aligned) share the top border row. If they would overlap, the title is truncated.

## Mouse Handling

The pane's `handleMouse` checks if clicks fall on the top border row within the pill rect:

- **Collapsed pill clicked or hovered**: expand (set `decoratorExpanded = true`, refresh)
- **Mouse leaves expanded pill zone**: collapse (unless config says always expanded)
- **Action clicked**: identify which action by x position, call `OnClick()`
- **Action hovered**: show `Help` text in the status bar

## Configuration

System config (`texelation.json`):

```json
{
  "pane_decorator_expanded": false
}
```

When `true`, the pill is always expanded — no hamburger collapse. Default: `false` (collapsible).

## Texelation Window Manager Actions

The pane itself registers WM actions (right zone) when created:

- **Zoom toggle**: Icon `󰊓` when not zoomed, `󰊕` when zoomed. Calls `desktop.toggleZoom()`. Active state reflects zoom state.

Future WM actions (not in scope): minimize, close, split.

## TexelTerm Migration

### Removed

- `toggle_overlay.go` — all internal pill drawing, mouse handling, collapsed/expanded state
- `overlayExpanded` field from TexelTerm struct
- `tuiToggle`, `altToggle` — already removed
- `drawToggleOverlay` and `handleToggleOverlayMouse` calls from Render/HandleMouse

### Changed

- `updateModeIndicatorsLocked()` — instead of updating widget state, sends `decorator.update` ControlBus messages for each action's Active/Disabled state
- TexelTerm.Run() — after ControlBus is available, sends `decorator.add` for:
  - `cfg` — Config (⚙), help "Configuration (F4)"
  - `tfm` — Transformers (󰁨), help "Transformer pipeline (F8)"
  - `wrp` — Wrap (), help "Line wrapping"
  - `search` — Search (󰍉), help "Search history (F3)"

### Kept

- All toggle logic (OnClick handlers for each toggle)
- Status bar widget (for toast messages, search widgets)
- ToggleButton widgets as state holders (Active, Disabled) — they just don't render themselves anymore

### Result

TexelTerm gains its top row back — the full terminal height is usable. The pill is on the border, managed by the pane.

## Testing

- **TestPaneDecoratorDraw**: create pane with decorator actions, render, verify pill characters on border row
- **TestPaneDecoratorCollapse**: verify collapsed shows `≡`, expanded shows all action icons
- **TestDecoratorControlBus**: send `decorator.add`/`decorator.remove`/`decorator.update` messages, verify action list state
- **TestPaneDecoratorZoomToggle**: verify zoom action icon changes with zoom state
- **Manual**: run texelation, hover pill to expand, click toggles, verify zoom toggle works

## Out of Scope

- Configurable pill position (left, bottom) — future enhancement
- Pill animations (smooth expand/collapse) — future enhancement
- Additional WM actions (minimize, close, split) — future enhancement
- Moving the pill to texelui — stays in texelation for now
