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

## Constructor

```go
func NewBorder(x, y, w, h int, style tcell.Style) *Border
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `x` | `int` | X position |
| `y` | `int` | Y position |
| `w` | `int` | Total width (including border) |
| `h` | `int` | Total height (including border) |
| `style` | `tcell.Style` | Border style |

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

    "github.com/gdamore/tcell/v2"
    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

func main() {
    err := devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Create border
        border := widgets.NewBorder(10, 5, 40, 10, tcell.StyleDefault)

        // Create text area as child
        textarea := widgets.NewTextArea(0, 0, 0, 0)  // Size managed by border
        border.SetChild(textarea)

        ui.AddWidget(border)
        ui.Focus(textarea)

        return adapter.NewUIApp("Border Demo", ui), nil
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
border := widgets.NewBorder(10, 5, 40, 10, tcell.StyleDefault)
textarea := widgets.NewTextArea(0, 0, 0, 0)  // Initial size doesn't matter
border.SetChild(textarea)
// textarea is now at (11, 6) with size (38, 8)
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
border := widgets.NewBorder(0, 0, 40, 10, tcell.StyleDefault)

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
border := widgets.NewBorder(5, 3, 35, 3, tcell.StyleDefault)
input := widgets.NewInput(0, 0, 0)
border.SetChild(input)
```

### Bordered TextArea

```go
border := widgets.NewBorder(5, 3, 50, 12, tcell.StyleDefault)
textarea := widgets.NewTextArea(0, 0, 0, 0)
border.SetChild(textarea)
```

### Bordered Pane with Multiple Widgets

```go
border := widgets.NewBorder(5, 3, 50, 15, tcell.StyleDefault)
pane := widgets.NewPane(0, 0, 0, 0, tcell.StyleDefault)

// Add widgets to pane with relative coordinates
label := widgets.NewLabel(2, 1, 20, 1, "Title")
input := widgets.NewInput(2, 3, 30)
pane.AddChild(label)
pane.AddChild(input)

border.SetChild(pane)
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
