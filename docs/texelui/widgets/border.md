# Border

A decorative border widget that wraps a single child.

```
╭──────────────────────────╮
│                          │
│   Content goes here      │
│                          │
╰──────────────────────────╯
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructors

```go
// Create with default style
func NewBorder() *Border

// Create with custom style
func NewBorderWithStyle(style tcell.Style) *Border
```

Creates a decorative border container. Position defaults to (0,0) and size to 20x10.
Use `SetPosition(x, y)` and `Resize(w, h)` to adjust, or place in a layout container.

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Style` | `tcell.Style` | Border appearance |
| `Charset` | `[6]rune` | Border characters |
| `Child` | `Widget` | Contained widget |

### Default Charset (Rounded)

```go
[6]rune{'─', '│', '╭', '╮', '╰', '╯'}
// horizontal, vertical, top-left, top-right, bottom-left, bottom-right
```

## Methods

| Method | Description |
|--------|-------------|
| `SetChild(w Widget)` | Set the child widget |
| `ClientRect() Rect` | Get inner area dimensions |

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

        // Create border with child
        border := widgets.NewBorder()
        textarea := widgets.NewTextArea()
        border.SetChild(textarea)

        ui.AddWidget(border)
        ui.Focus(textarea)

        app := adapter.NewUIApp("Border Demo", ui)
        app.OnResize(func(w, h int) {
            border.SetPosition(10, 5)
            border.Resize(40, 10)
        })
        return app, nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Behavior

### Client Area

The border takes 1 cell on each side, leaving the inner area for content:

```
Total: 40 x 10
┌──────────────────────────────────────┐  ← 1 cell
│                                      │
│   Client area: 38 x 8                │  ← Content
│                                      │
└──────────────────────────────────────┘  ← 1 cell
↑                                      ↑
1 cell                              1 cell
```

### Automatic Child Sizing

When you set a child, it's automatically positioned and sized to fit:

```go
border := widgets.NewBorder()
border.Resize(40, 10)
textarea := widgets.NewTextArea()
border.SetChild(textarea)
// textarea is now sized to fit the inner area (38, 8)
```

### Focus-Aware Styling

The border changes color when its child has focus:

| State | Border Color |
|-------|--------------|
| Child not focused | `border.default` theme color |
| Child focused | `border.active` theme color |

### Resize Handling

When the border is resized, its child is automatically resized too:

```go
border.Resize(60, 15)
// Child is now sized to (58, 13)
```

## Custom Border Characters

```go
border := widgets.NewBorder()

// Sharp corners
border.Charset = [6]rune{'─', '│', '┌', '┐', '└', '┘'}

// Double lines
border.Charset = [6]rune{'═', '║', '╔', '╗', '╚', '╝'}

// ASCII
border.Charset = [6]rune{'-', '|', '+', '+', '+', '+'}
```

## Common Patterns

### Bordered Input

```go
border := widgets.NewBorder()
input := widgets.NewInput()
border.SetChild(input)

// Size in OnResize
app.OnResize(func(w, h int) {
    border.SetPosition(5, 3)
    border.Resize(35, 3)
})
```

### Bordered TextArea

```go
border := widgets.NewBorder()
textarea := widgets.NewTextArea()
border.SetChild(textarea)

// Size in OnResize
app.OnResize(func(w, h int) {
    border.SetPosition(5, 3)
    border.Resize(50, 12)
})
```

### Bordered VBox with Multiple Widgets

```go
border := widgets.NewBorder()
vbox := widgets.NewVBox()
vbox.Spacing = 1

// Add widgets to VBox
title := widgets.NewLabel("Title")
vbox.AddChild(title)

input := widgets.NewInput()
vbox.AddChild(input)

border.SetChild(vbox)
```

## Implementation Details

### Source File
`texelui/widgets/border.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.InvalidationAware`
- `core.ChildContainer`
- `core.HitTester`

### Focus Detection

The border checks if its child (or any descendant) has focus to apply the active border style:

```go
func (b *Border) childHasFocus() bool {
    if b.Child == nil {
        return false
    }
    if fs, ok := b.Child.(core.FocusState); ok && fs.IsFocused() {
        return true
    }
    // Also check nested children...
}
```

## See Also

- [Pane](/texelui/widgets/pane.md) - Container widget
- [TabLayout](/texelui/widgets/tablayout.md) - Tabbed container
- [TextArea](/texelui/widgets/textarea.md) - Multi-line editor
