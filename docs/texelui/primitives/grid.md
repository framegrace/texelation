# Grid

A 2D grid with dynamic column layout and cell selection.

```
┌────────────────────────────────────┐
│ ██  ██  ██  ██  ██  ██  ██  ██    │
│ ██  ██  ██  ██  ██  ██  ██  ██    │
│ ██  [█]  ██  ██  ██  ██  ██  ██   │  ← selected
│ ██  ██  ██  ██  ██  ██  ██  ██    │
└────────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/primitives"
```

## Constructor

```go
func NewGrid(x, y, w, h int) *Grid
```

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `MinCellWidth` | `int` | Minimum cell width |
| `MaxCols` | `int` | Maximum columns (0 = unlimited) |
| `Style` | `tcell.Style` | Normal cell style |
| `SelectedStyle` | `tcell.Style` | Selected cell style |
| `Renderer` | `GridCellRenderer` | Custom cell renderer |

## Methods

| Method | Description |
|--------|-------------|
| `SetItems(items []interface{})` | Set grid items |
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

func createColorGrid() *primitives.Grid {
    grid := primitives.NewGrid(5, 3, 40, 8)
    grid.MinCellWidth = 4

    // Set color items
    colors := []interface{}{
        Color{Name: "red", Value: tcell.ColorRed},
        Color{Name: "green", Value: tcell.ColorGreen},
        Color{Name: "blue", Value: tcell.ColorBlue},
        // ... more colors
    }
    grid.SetItems(colors)

    return grid
}
```

## Behavior

### Dynamic Columns

Columns are calculated based on available width and `MinCellWidth`:

```
Width: 40, MinCellWidth: 4
Columns: 40 / 4 = 10 columns

Width: 30, MinCellWidth: 4
Columns: 30 / 4 = 7 columns
```

### Keyboard Navigation

| Key | Action |
|-----|--------|
| Left | Previous cell |
| Right | Next cell |
| Up | Move up one row |
| Down | Move down one row |
| Tab | Next cell (wraps) |
| Home | First cell |
| End | Last cell |

### Mouse

| Action | Result |
|--------|--------|
| Click cell | Select cell |

## Custom Rendering

Implement `GridCellRenderer` for custom cell display:

```go
type GridCellRenderer interface {
    RenderCell(painter *core.Painter, item interface{}, rect core.Rect, selected, focused bool)
}

// Example: Color swatch renderer
type ColorCellRenderer struct{}

func (r *ColorCellRenderer) RenderCell(p *core.Painter, item interface{}, rect core.Rect, selected, focused bool) {
    color := item.(Color)

    // Fill cell with color
    style := tcell.StyleDefault.Background(color.Value)
    p.Fill(rect, ' ', style)

    // Add border if selected
    if selected {
        borderStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite)
        // Draw selection indicator
        p.SetCell(rect.X, rect.Y, '[', borderStyle)
        p.SetCell(rect.X+rect.W-1, rect.Y, ']', borderStyle)
    }
}

// Use it
grid := primitives.NewGrid(0, 0, 40, 8)
grid.Renderer = &ColorCellRenderer{}
```

## Layout Calculation

```go
// Given:
// - Width: 36
// - MinCellWidth: 4
// - Items: 24

// Calculate columns
cols := width / minCellWidth  // 36 / 4 = 9

// Calculate rows
rows := (len(items) + cols - 1) / cols  // (24 + 9 - 1) / 9 = 3

// Cell width (may be larger than min)
cellWidth := width / cols  // 36 / 9 = 4
```

## Used By

- **ColorPicker** (Palette mode) - Displays color swatches

## Implementation Details

### Source File
`texelui/primitives/grid.go`

### Key Features

1. **Dynamic layout** - Columns adjust to width
2. **2D navigation** - Arrow keys move in grid
3. **Custom rendering** - Pluggable cell display
4. **Mouse support** - Click to select

## See Also

- [ScrollableList](scrollablelist.md) - Vertical list
- [TabBar](tabbar.md) - Tab navigation
- [ColorPicker](../widgets/colorpicker.md) - Uses Grid
