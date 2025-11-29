# Theming & Palette System

## Overview
Texelation supports a standardized theming system based on the **Catppuccin** style guide. This ensures consistency across the core desktop, apps, and UI widgets.

## Components

### 1. Palettes
Palettes are JSON files located in `texel/theme/palettes/`. We ship with the 4 standard Catppuccin variants:
- `latte` (Light)
- `frappe`
- `macchiato`
- `mocha` (Default)

You can add custom palettes to `~/.config/texelation/palettes/<name>.json`.

### 2. Semantic Styling ("Action Styles")
Instead of hardcoding colors, the codebase uses semantic keys (e.g., `ui.action.primary`, `ui.bg.base`). These keys are mapped to palette colors in `texel/theme/semantics.go`.

| Semantic Key | Default Value | Description |
| :--- | :--- | :--- |
| `accent` | `@mauve` | **Main Brand Color** (Pivot point for theme) |
| `accent_secondary` | `@lavender` | Secondary/Dimmed accent |
| `bg.base` | `@base` | Main background |
| `bg.mantle` | `@mantle` | Sidebars, Statusbar |
| `bg.surface` | `@surface0` | Inputs, Panels |
| `text.primary` | `@text` | Main text |
| `text.accent` | `accent` | Brand/Logo text |
| `action.primary` | `accent` | Buttons, Call-to-Action |
| `action.danger` | `@red` | Destructive actions |
| `border.active` | `accent` | Active pane border |
| `border.focus` | `accent_secondary` | Focused widget ring |
| `border.resizing` | `accent_secondary` | Resizing state border |

### 3. Configuration (`theme.json`)
To switch themes or change the accent color, edit your `~/.config/texelation/theme.json`:
```json
{
  "meta": {
    "palette": "latte"
  },
  "ui": {
    "accent": "@blue",           // Change global accent to Blue
    "accent_secondary": "@sky",  // Change secondary accent
    "button_bg": "@green"        // Override specific element
  }
}
```

## For Developers

### Using Colors
Use `theme.GetSemanticColor(key)` whenever possible.
```go
tm := theme.Get()
bg := tm.GetSemanticColor("bg.surface")
fg := tm.GetSemanticColor("text.primary")
```
