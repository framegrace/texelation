# ColorPicker

A comprehensive color selection widget with multiple modes.

```
Collapsed: [█▓] accent

Expanded:  [█▓] accent
           ┌─────────────────────────────────┐
           │ ► Semantic   Palette   OKLCH    │
           ├─────────────────────────────────┤
           │   accent                        │
           │   accent_secondary              │
           │ ► text.primary        ← selected│
           │   text.secondary                │
           │   bg.surface                    │
           └─────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewColorPicker(config ColorPickerConfig) *ColorPicker
```

Creates a color picker widget. Position defaults to (0,0).
Use `SetPosition(x, y)` to adjust, or place in a layout container.

### ColorPickerConfig

```go
type ColorPickerConfig struct {
    EnableSemantic bool   // Enable semantic color mode
    EnablePalette  bool   // Enable palette color mode
    EnableOKLCH    bool   // Enable OKLCH color mode
    Label          string // Display label
}
```

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Label` | `string` | Display label |
| `Style` | `tcell.Style` | Widget appearance |

## Example

```go
package main

import (
    "log"

    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

func main() {
    err := devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Create layout
        vbox := widgets.NewVBox()
        vbox.Spacing = 1

        // Color picker row
        row := widgets.NewHBox()
        row.Spacing = 1
        row.AddChildWithSize(widgets.NewLabel("Theme Color:"), 14)

        picker := widgets.NewColorPicker(widgets.ColorPickerConfig{
            EnableSemantic: true,
            EnablePalette:  true,
            EnableOKLCH:    true,
            Label:          "Theme Color",
        })
        picker.SetValue("accent")
        row.AddFlexChild(picker)
        vbox.AddChild(row)

        ui.AddWidget(vbox)
        ui.Focus(vbox)

        app := adapter.NewUIApp("ColorPicker Demo", ui)
        app.OnResize(func(w, h int) {
            vbox.SetPosition(5, 3)
            vbox.Resize(50, h-6)
        })
        return app, nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Color Modes

### Semantic Mode

Select from theme semantic colors:
- `accent`, `accent_secondary`
- `text.primary`, `text.secondary`, `text.muted`
- `bg.surface`, `bg.base`
- `action.primary`, `action.success`, `action.danger`
- And more...

### Palette Mode

Select from palette colors (Catppuccin):
- `@rosewater`, `@flamingo`, `@pink`, `@mauve`
- `@red`, `@maroon`, `@peach`, `@yellow`
- `@green`, `@teal`, `@sky`, `@sapphire`
- `@blue`, `@lavender`, `@text`, `@base`
- And more...

Displayed as a grid of color swatches.

### OKLCH Mode

Direct color input using OKLCH color space:
- **L** (Lightness): 0-100%
- **C** (Chroma): 0-0.4
- **H** (Hue): 0-360°

## Behavior

### Collapsed State

Shows a color swatch and label:
```
[█▓] accent
```

Click or press Enter to expand.

### Expanded State

Shows tabbed interface with available modes:
```
[█▓] accent
┌─────────────────────────────────┐
│ ► Semantic   Palette   OKLCH   │
├─────────────────────────────────┤
│   (mode content)               │
└─────────────────────────────────┘
```

### Keyboard

| Key | Action |
|-----|--------|
| Enter | Expand/Select |
| Escape | Collapse |
| Tab | Switch mode tabs |
| 1-3 | Jump to mode |
| Up/Down | Navigate list |
| Arrow keys | Navigate grid (Palette) |

### Mouse

| Action | Result |
|--------|--------|
| Click collapsed | Expand |
| Click tab | Switch mode |
| Click item | Select color |
| Click outside | Collapse |

### Modal Behavior

When expanded, ColorPicker becomes modal and receives all input.

## Getting Results

```go
picker := widgets.NewColorPicker(config)

// Set initial value
picker.SetValue("accent")  // Semantic
picker.SetValue("@blue")   // Palette

// Get result
result := picker.GetResult()
// result.Color   - tcell.Color
// result.Mode    - ColorPickerMode
// result.Source  - string ("accent", "@blue", "oklch(...)")
// result.R, G, B - int32 RGB values
```

### ColorPickerResult

```go
type ColorPickerResult struct {
    Color  tcell.Color
    Mode   ColorPickerMode
    Source string
    R, G, B int32
}
```

## Implementation Details

### Source Files
- `texelui/widgets/colorpicker.go` - Main widget
- `texelui/widgets/colorpicker/mode.go` - Mode interface
- `texelui/widgets/colorpicker/semantic.go` - Semantic mode
- `texelui/widgets/colorpicker/palette.go` - Palette mode
- `texelui/widgets/colorpicker/oklch.go` - OKLCH mode

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.InvalidationAware`
- `core.Modal`
- `core.ZIndexer`
- `core.ChildContainer`

### Uses Primitives
- `ScrollableList` for Semantic mode
- `Grid` for Palette mode
- `TabBar` for mode switching

## See Also

- [ComboBox](/texelui/widgets/combobox.md) - Dropdown selection
- [ScrollableList](/texelui/primitives/scrollablelist.md) - List primitive
- [Grid](/texelui/primitives/grid.md) - Grid primitive
- [Theming](/texelui/core-concepts/theming.md) - Theme colors
