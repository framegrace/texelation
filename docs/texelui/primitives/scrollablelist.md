# ScrollableList

A vertical scrolling list with keyboard and mouse navigation.

```
┌────────────────────────┐
│ ▲ (more above)         │
│   Item 1               │
│ ► Item 2      ← selected
│   Item 3               │
│   Item 4               │
│ ▼ (more below)         │
└────────────────────────┘
```

## Import

```go
import "texelation/texelui/primitives"
```

## Constructor

```go
func NewScrollableList(x, y, w, h int) *ScrollableList
```

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Style` | `tcell.Style` | Normal item style |
| `SelectedStyle` | `tcell.Style` | Selected item style |
| `Renderer` | `ListItemRenderer` | Custom item renderer |

## Methods

| Method | Description |
|--------|-------------|
| `SetItems(items []interface{})` | Set list items |
| `GetItems() []interface{}` | Get all items |
| `SelectedIndex() int` | Get selected index |
| `SetSelectedIndex(idx int)` | Set selected index |
| `SelectedItem() interface{}` | Get selected item |

## Example

```go
package main

import (
    "texelation/texelui/core"
    "texelation/texelui/primitives"
)

func createList() *primitives.ScrollableList {
    list := primitives.NewScrollableList(5, 3, 30, 10)

    // Set items
    items := []interface{}{
        "Apple", "Banana", "Cherry", "Date",
        "Elderberry", "Fig", "Grape", "Honeydew",
    }
    list.SetItems(items)

    return list
}
```

## Behavior

### Keyboard Navigation

| Key | Action |
|-----|--------|
| Up | Select previous item |
| Down | Select next item |
| Home | Select first item |
| End | Select last item |
| Page Up | Scroll up one page |
| Page Down | Scroll down one page |

### Mouse

| Action | Result |
|--------|--------|
| Click item | Select item |
| Wheel up/down | Scroll list |

### Auto-Centering

The selected item is automatically centered in the view when possible.

### Scroll Indicators

When there's more content above or below, indicators appear:

```
▲  (scroll up indicator)
...
▼  (scroll down indicator)
```

## Custom Rendering

Implement `ListItemRenderer` for custom item display:

```go
type ListItemRenderer interface {
    RenderItem(painter *core.Painter, item interface{}, rect core.Rect, selected, focused bool)
}

// Example: Custom renderer for color items
type ColorItemRenderer struct{}

func (r *ColorItemRenderer) RenderItem(p *core.Painter, item interface{}, rect core.Rect, selected, focused bool) {
    color := item.(ColorItem)

    // Draw color swatch
    swatchStyle := tcell.StyleDefault.Background(color.Value)
    p.Fill(core.Rect{X: rect.X, Y: rect.Y, W: 2, H: 1}, ' ', swatchStyle)

    // Draw label
    style := tcell.StyleDefault
    if selected {
        style = style.Reverse(true)
    }
    p.DrawText(rect.X+3, rect.Y, color.Name, style)
}

// Use it
list := primitives.NewScrollableList(0, 0, 30, 10)
list.Renderer = &ColorItemRenderer{}
```

## Default Rendering

Without a custom renderer, items are rendered as strings:

```go
// Items converted via fmt.Sprint()
list.SetItems([]interface{}{"String", 123, true})
// Displays: "String", "123", "true"
```

## Used By

- **ColorPicker** (Semantic mode) - Displays semantic color names

## Implementation Details

### Source File
`texelui/primitives/scrollablelist.go`

### Key Features

1. **Viewport management** - Only visible items are rendered
2. **Smart scrolling** - Keeps selected item visible
3. **Scroll indicators** - Shows when more content exists
4. **Custom rendering** - Pluggable item display

## See Also

- [Grid](grid.md) - 2D selection grid
- [TabBar](tabbar.md) - Tab navigation
- [ColorPicker](../widgets/colorpicker.md) - Uses ScrollableList
