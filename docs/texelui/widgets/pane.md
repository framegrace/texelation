# Pane

A container widget with background fill and child widget support.

```
┌──────────────────────────────┐
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│ ░░░░ Child widgets here ░░░░ │
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
└──────────────────────────────┘
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewPane(x, y, w, h int, style tcell.Style) *Pane
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `x` | `int` | X position |
| `y` | `int` | Y position |
| `w` | `int` | Width |
| `h` | `int` | Height |
| `style` | `tcell.Style` | Background style |

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Style` | `tcell.Style` | Background appearance |

## Methods

| Method | Description |
|--------|-------------|
| `AddChild(w Widget)` | Add a child widget |
| `RemoveChild(w Widget)` | Remove a child widget |

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

        // Create background pane
        pane := widgets.NewPane(5, 3, 50, 15, tcell.StyleDefault)

        // Add child widgets
        title := widgets.NewLabel(7, 4, 46, 1, "Welcome")
        title.Align = widgets.AlignCenter

        nameLabel := widgets.NewLabel(7, 6, 10, 1, "Name:")
        nameInput := widgets.NewInput(18, 6, 30)

        submitBtn := widgets.NewButton(18, 9, 15, 1, "Submit")

        // Add children to pane
        pane.AddChild(title)
        pane.AddChild(nameLabel)
        pane.AddChild(nameInput)
        pane.AddChild(submitBtn)

        // Add pane to UI
        ui.AddWidget(pane)
        ui.Focus(nameInput)

        return adapter.NewUIApp("Pane Demo", ui), nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Behavior

### Background Fill

The pane fills its entire area with the background color from its style.

### Child Management

Children are drawn on top of the pane's background:

```go
pane := widgets.NewPane(0, 0, 60, 20, tcell.StyleDefault)

// Add widgets
pane.AddChild(label)
pane.AddChild(input)
pane.AddChild(button)

// Remove a widget
pane.RemoveChild(label)
```

### Relative Positioning

When a pane moves, its children move with it:

```go
pane := widgets.NewPane(10, 10, 40, 20, tcell.StyleDefault)
label := widgets.NewLabel(12, 12, 20, 1, "Hello")  // At (12, 12)
pane.AddChild(label)

// Move pane
pane.SetPosition(20, 20)
// Label is now at (22, 22) - moved by same offset
```

### Z-Order

Children are drawn in the order they were added. Later children appear on top:

```go
pane.AddChild(background)  // Drawn first (bottom)
pane.AddChild(content)     // Drawn second
pane.AddChild(overlay)     // Drawn last (top)
```

### Focus Routing

When a child has focus, keyboard events are routed to that child.

## Nested Panes

Panes can contain other panes for complex layouts:

```go
// Main container
mainPane := widgets.NewPane(0, 0, 80, 24, tcell.StyleDefault)

// Sidebar
sidebar := widgets.NewPane(0, 0, 20, 24, sidebarStyle)
mainPane.AddChild(sidebar)

// Content area
content := widgets.NewPane(22, 0, 58, 24, contentStyle)
mainPane.AddChild(content)

ui.AddWidget(mainPane)
```

## With Border

Combine Pane with Border for a framed container:

```go
border := widgets.NewBorder(5, 3, 50, 15, tcell.StyleDefault)
pane := widgets.NewPane(0, 0, 48, 13, tcell.StyleDefault)

// Add widgets to pane
pane.AddChild(label)
pane.AddChild(input)

// Set pane as border's child
border.SetChild(pane)

ui.AddWidget(border)
```

## Implementation Details

### Source File
`texelui/widgets/pane.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.InvalidationAware`
- `core.ChildContainer`
- `core.HitTester`
- `core.ZIndexer`

### Child Z-Ordering

Children can have their own z-index values, which affects both draw order and mouse hit testing:

```go
overlay := widgets.NewPane(...)
overlay.SetZIndex(10)  // Draw on top
pane.AddChild(overlay)
```

## See Also

- [Border](/texelui/widgets/border.md) - Decorative border
- [TabLayout](/texelui/widgets/tablayout.md) - Tabbed container
- [Layout](/texelui/layout/README.md) - Automatic positioning
