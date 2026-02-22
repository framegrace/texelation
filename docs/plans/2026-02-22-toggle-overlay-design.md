# Toggle Button Overlay Design

## Problem

Toggle buttons (TFM, TUI, WRP, ALT) sit in the status bar at the bottom of the terminal. This draws attention to the bottom of the screen — the area where users type most. Moving them to a small overlay in the top-right corner keeps them visible without being distracting.

## Design

### Layout

```
┌──────────────────────────────────────────────────────┐
│                                        ⚡🖥️↩️📑     │ ← row 0, overlay (1 char from right)
│ Terminal content...                                  │
│                                                      │
│ key hints                      hover help / messages │ ← status bar (bottom, 1 row)
└──────────────────────────────────────────────────────┘
```

### Overlay

- Borderless, opaque strip at row 0
- 4 toggle buttons in a horizontal row, each ` icon ` (3 chars padded)
- Background: `bg.surface` (same as current toggle button style)
- Position: `x = cols - totalButtonWidth - 1`, `y = 0`
- Always visible, drawn on top of terminal content in `Render()`

### Status Bar

- Stays at bottom, 1 row, no separator
- No longer hosts toggle buttons (left widgets removed)
- Shows key hints (left) and hover help / timed messages (right)

### Mouse Handling

- `HandleMouse()` checks if click/hover falls within the overlay rect
- If yes: forward to the appropriate `ToggleButton.HandleMouse()`
- Hover help still appears in the status bar via `setHoverHelp()`

### Changes

All changes are in `texelation/apps/texelterm/term.go`:

1. **`New()`**: Stop calling `sb.SetLeftWidgets()` — toggles no longer in status bar
2. **`Render()`**: After terminal content, draw toggle buttons at top-right via Painter
3. **`Resize()`**: Reposition toggle buttons on resize
4. **`HandleMouse()`**: Add hit test for overlay rect before forwarding to terminal

No changes to texelui (`StatusBar` or `ToggleButton` widgets).
