# Theming

How TexelUI integrates with Texelation's theme system.

## Overview

TexelUI widgets automatically use Texelation's theme system, providing:

- **Consistent styling** across all apps
- **Semantic colors** that adapt to theme changes
- **Palette support** (Catppuccin variants)
- **Hot reload** capability

## Theme Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Your Widget                                │
│                                                                 │
│   tm := theme.Get()                                             │
│   fg := tm.GetSemanticColor("text.primary")                     │
│        │                                                        │
└────────┼────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Theme System                                 │
│                                                                 │
│   ┌─────────────┐   ┌─────────────┐   ┌─────────────┐          │
│   │   theme.    │   │  Semantic   │   │   Palette   │          │
│   │    json     │──▶│  Mappings   │──▶│   Colors    │          │
│   │             │   │             │   │             │          │
│   │ User config │   │ text.primary│   │ @text ────▶ │          │
│   │             │   │    ──▶      │   │ #cdd6f4     │          │
│   └─────────────┘   │   @text     │   └─────────────┘          │
│                     └─────────────┘                            │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Three-Layer Resolution

### Layer 1: Palettes

Color palettes define named colors:

```json
// ~/.config/texelation/palettes/mocha.json (or embedded)
{
    "rosewater": "#f5e0dc",
    "flamingo": "#f2cdcd",
    "pink": "#f5c2e7",
    "mauve": "#cba6f7",
    "red": "#f38ba8",
    "maroon": "#eba0ac",
    "peach": "#fab387",
    "yellow": "#f9e2af",
    "green": "#a6e3a1",
    "teal": "#94e2d5",
    "sky": "#89dceb",
    "sapphire": "#74c7ec",
    "blue": "#89b4fa",
    "lavender": "#b4befe",
    "text": "#cdd6f4",
    "subtext1": "#bac2de",
    "subtext0": "#a6adc8",
    "overlay2": "#9399b2",
    "overlay1": "#7f849c",
    "overlay0": "#6c7086",
    "surface2": "#585b70",
    "surface1": "#45475a",
    "surface0": "#313244",
    "base": "#1e1e2e",
    "mantle": "#181825",
    "crust": "#11111b"
}
```

Reference with `@` prefix: `@mauve`, `@base`, `@text`

### Layer 2: Semantic Mappings

Map UI concepts to palette colors:

```go
// In texel/theme/semantics.go
var StandardSemantics = Section{
    // Accent colors
    "accent":           "@mauve",
    "accent_secondary": "@lavender",

    // Backgrounds
    "bg.base":    "@base",
    "bg.mantle":  "@mantle",
    "bg.surface": "@surface0",

    // Text
    "text.primary":   "@text",
    "text.secondary": "@subtext1",
    "text.muted":     "@overlay0",
    "text.inverse":   "@base",

    // Actions
    "action.primary": "accent",  // References other semantic
    "action.success": "@green",
    "action.warning": "@yellow",
    "action.danger":  "@red",

    // Borders
    "border.active":   "accent",
    "border.inactive": "@overlay0",
    "border.focus":    "accent_secondary",

    // Components
    "caret": "@rosewater",
}
```

### Layer 3: User Configuration

Users can override in `~/.config/texelation/theme.json`:

```json
{
    "meta": {
        "palette": "mocha"
    },
    "ui": {
        "accent": "@blue",
        "bg.surface": "@surface1"
    }
}
```

## Using Theme Colors

### In Widget Constructors

```go
func NewMyWidget(x, y, w, h int) *MyWidget {
    tm := theme.Get()

    // Get semantic colors
    fg := tm.GetSemanticColor("text.primary")
    bg := tm.GetSemanticColor("bg.surface")
    accent := tm.GetSemanticColor("action.primary")

    w := &MyWidget{
        Style:      tcell.StyleDefault.Foreground(fg).Background(bg),
        AccentStyle: tcell.StyleDefault.Foreground(accent),
    }

    return w
}
```

### In Focus Styling

```go
func NewButton(x, y, w, h int, text string) *Button {
    tm := theme.Get()

    // Normal style
    fg := tm.GetSemanticColor("text.inverse")
    bg := tm.GetSemanticColor("action.primary")
    b.Style = tcell.StyleDefault.Foreground(fg).Background(bg)

    // Focus style
    focusBg := tm.GetSemanticColor("border.focus")
    b.SetFocusedStyle(
        tcell.StyleDefault.Foreground(fg).Background(focusBg),
        true,
    )

    return b
}
```

### Available Semantic Colors

| Key | Purpose | Default |
|-----|---------|---------|
| **Accents** | | |
| `accent` | Primary brand color | `@mauve` |
| `accent_secondary` | Secondary accent | `@lavender` |
| **Backgrounds** | | |
| `bg.base` | Main background | `@base` |
| `bg.mantle` | Sidebars, panels | `@mantle` |
| `bg.crust` | Deepest level | `@crust` |
| `bg.surface` | Cards, inputs | `@surface0` |
| **Text** | | |
| `text.primary` | Main text | `@text` |
| `text.secondary` | Descriptions | `@subtext1` |
| `text.muted` | Disabled, hints | `@overlay0` |
| `text.inverse` | On colored bg | `@base` |
| `text.accent` | Brand text | `accent` |
| **Actions** | | |
| `action.primary` | Buttons, CTAs | `accent` |
| `action.success` | Success states | `@green` |
| `action.warning` | Warnings | `@yellow` |
| `action.danger` | Destructive | `@red` |
| **Borders** | | |
| `border.active` | Active element | `accent` |
| `border.inactive` | Inactive border | `@overlay0` |
| `border.focus` | Focus ring | `accent_secondary` |
| `border.resizing` | During resize | `accent_secondary` |
| **Components** | | |
| `caret` | Input cursor | `@rosewater` |
| `selection` | Selected text | `@surface2` |

## Color Resolution

When you request a color, the theme system resolves it through multiple steps:

```go
tm.GetSemanticColor("action.primary")
```

Resolution chain:
1. Look up `ui.action.primary` → finds `"accent"`
2. Look up `ui.accent` → finds `"@mauve"`
3. Look up palette `mauve` → finds `"#cba6f7"`
4. Convert hex to tcell.Color

```
action.primary ──▶ accent ──▶ @mauve ──▶ #cba6f7 ──▶ tcell.Color
```

## Custom Widget Theming

### Use Semantic Colors

```go
// Good - adapts to theme
tm := theme.Get()
fg := tm.GetSemanticColor("text.primary")

// Bad - hardcoded
fg := tcell.ColorWhite
```

### Follow Conventions

| Widget Type | Background | Foreground | Focus |
|-------------|------------|------------|-------|
| Label | transparent | `text.primary` | N/A |
| Input | `bg.surface` | `text.primary` | underline |
| Button | `action.primary` | `text.inverse` | `border.focus` bg |
| Checkbox | transparent | `text.primary` | reverse video |

### Example: Themed Widget

```go
type ProgressBar struct {
    core.BaseWidget
    Value float64
    Style         tcell.Style
    FilledStyle   tcell.Style
    UnfilledStyle tcell.Style
}

func NewProgressBar(x, y, w int) *ProgressBar {
    tm := theme.Get()

    // Use semantic colors
    fg := tm.GetSemanticColor("text.primary")
    bg := tm.GetSemanticColor("bg.surface")
    accent := tm.GetSemanticColor("action.primary")
    muted := tm.GetSemanticColor("text.muted")

    return &ProgressBar{
        Style:         tcell.StyleDefault.Foreground(fg).Background(bg),
        FilledStyle:   tcell.StyleDefault.Foreground(accent),
        UnfilledStyle: tcell.StyleDefault.Foreground(muted),
        // ...
    }
}
```

## Theme Hot Reload

The theme can be reloaded at runtime:

```go
// Triggered by SIGHUP to server
theme.Reload()
```

For widgets to respond to theme changes:
1. Store only `tcell.Style` values (not color values)
2. Recreate styles on reload (future enhancement)
3. Currently, restart is needed for theme changes

## Available Palettes

Built-in Catppuccin variants:

| Palette | Description |
|---------|-------------|
| `mocha` | Dark theme (default) |
| `latte` | Light theme |
| `frappe` | Medium-dark theme |
| `macchiato` | Medium theme |

Set in theme.json:
```json
{
    "meta": {
        "palette": "latte"
    }
}
```

## Custom Palettes

Create custom palettes in `~/.config/texelation/palettes/`:

```json
// ~/.config/texelation/palettes/my-theme.json
{
    "text": "#ffffff",
    "base": "#000000",
    "accent": "#ff0000",
    // ... all required colors
}
```

Then reference in theme.json:
```json
{
    "meta": {
        "palette": "my-theme"
    }
}
```

## Best Practices

### 1. Always Use theme.Get()

```go
tm := theme.Get()  // Singleton, safe to call often
```

### 2. Prefer Semantic Colors

```go
// Good - meaningful, adapts to theme
tm.GetSemanticColor("text.primary")

// OK - direct palette access
tm.GetColor("ui", "custom_field", tcell.ColorWhite)

// Bad - hardcoded
tcell.ColorWhite
```

### 3. Provide Defaults

```go
color := tm.GetColor("ui", "my_key", tcell.ColorWhite)
// If key missing, uses white as fallback
```

### 4. Document Custom Keys

If you add custom theme keys, document them:

```json
// In your widget docs
{
    "ui": {
        "progressbar.filled": "@green",
        "progressbar.empty": "@overlay0"
    }
}
```

## What's Next?

- [Widget Interface](widget-interface.md) - How widgets work
- [Custom Widget Tutorial](../tutorials/custom-widget.md) - Build themed widgets
- [Architecture](architecture.md) - System overview
