# Primitives Reference

Reusable building blocks for constructing complex widgets.

## What Are Primitives?

Primitives are lower-level components that provide common functionality for widgets. They handle specific UI patterns that are reused across multiple widgets.

## Available Primitives

| Primitive | Description | Used By |
|-----------|-------------|---------|
| [ScrollableList](scrollablelist.md) | Vertical scrolling list | ColorPicker (Semantic mode) |
| [Grid](grid.md) | 2D grid with dynamic columns | ColorPicker (Palette mode) |
| [TabBar](tabbar.md) | Horizontal tab navigation | ColorPicker, TabLayout |

## When to Use Primitives

Use primitives when building custom widgets that need:

- **ScrollableList**: Any scrollable list of items
- **Grid**: 2D selection grids, color palettes, icon grids
- **TabBar**: Mode switching, tab navigation

## Architecture

```
┌─────────────────────────────────────────────┐
│                 Your Widget                 │
│                                             │
│  ┌─────────────┐  ┌─────────────────────┐  │
│  │   TabBar    │  │   ScrollableList    │  │
│  │             │  │   or Grid           │  │
│  │ (mode tabs) │  │   (content)         │  │
│  └─────────────┘  └─────────────────────┘  │
│                                             │
└─────────────────────────────────────────────┘
```

## Custom Rendering

All primitives support custom item rendering via interfaces:

```go
// For ScrollableList
type ListItemRenderer interface {
    RenderItem(painter *Painter, item interface{}, rect Rect, selected, focused bool)
}

// For Grid
type GridCellRenderer interface {
    RenderCell(painter *Painter, item interface{}, rect Rect, selected, focused bool)
}
```

## Example: Custom Widget with ScrollableList

```go
type MyListWidget struct {
    core.BaseWidget
    list *primitives.ScrollableList
}

func NewMyListWidget(x, y, w, h int) *MyListWidget {
    widget := &MyListWidget{}
    widget.SetPosition(x, y)
    widget.Resize(w, h)

    // Create scrollable list
    widget.list = primitives.NewScrollableList(x, y, w, h)
    widget.list.SetItems([]interface{}{"Item 1", "Item 2", "Item 3"})

    return widget
}

func (w *MyListWidget) Draw(p *core.Painter) {
    w.list.Draw(p)
}

func (w *MyListWidget) HandleKey(ev *tcell.EventKey) bool {
    return w.list.HandleKey(ev)
}
```

## Importing Primitives

```go
import "texelation/texelui/primitives"
```

## See Also

- [Custom Widget Tutorial](../tutorials/custom-widget.md) - Building widgets
- [Widget Interface](../core-concepts/widget-interface.md) - Widget contracts
- [ColorPicker](../widgets/colorpicker.md) - Uses all three primitives
