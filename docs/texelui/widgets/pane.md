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
func NewPane() *Pane
```

Creates a container widget with background fill. Position defaults to (0,0) and size to 20x10.
Use `SetPosition(x, y)` and `Resize(w, h)` to adjust, or place in a layout container.

Set the `Style` property to customize the background appearance.

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

Using Pane with VBox for automatic layout:

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

        // Create background pane with style
        pane := widgets.NewPane()
        pane.Style = tcell.StyleDefault.Background(tcell.ColorDarkBlue)

        // Use VBox for layout inside pane
        vbox := widgets.NewVBox()
        vbox.Spacing = 1

        title := widgets.NewLabel("Welcome")
        title.Align = widgets.AlignCenter
        vbox.AddChild(title)

        row := widgets.NewHBox()
        row.Spacing = 1
        row.AddChildWithSize(widgets.NewLabel("Name:"), 10)
        nameInput := widgets.NewInput()
        row.AddFlexChild(nameInput)
        vbox.AddChild(row)

        submitBtn := widgets.NewButton("Submit")
        vbox.AddChild(submitBtn)

        pane.AddChild(vbox)

        ui.AddWidget(pane)
        ui.Focus(vbox)

        app := adapter.NewUIApp("Pane Demo", ui)
        app.OnResize(func(w, h int) {
            pane.SetPosition(5, 3)
            pane.Resize(50, 15)
            vbox.SetPosition(7, 4)
            vbox.Resize(46, 11)
        })
        return app, nil
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
pane := widgets.NewPane()
pane.Resize(60, 20)

// Add widgets
pane.AddChild(label)
pane.AddChild(input)
pane.AddChild(button)

// Remove a widget
pane.RemoveChild(label)
```

### Relative Positioning

When a pane moves, its children move with it (for manually positioned children):

```go
pane := widgets.NewPane()
pane.SetPosition(10, 10)
pane.Resize(40, 20)

label := widgets.NewLabel("Hello")
label.SetPosition(12, 12)  // Relative to parent
pane.AddChild(label)

// Move pane
pane.SetPosition(20, 20)
// Children move with the pane
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
mainPane := widgets.NewPane()
mainPane.Resize(80, 24)

// Sidebar
sidebar := widgets.NewPane()
sidebar.Style = sidebarStyle
sidebar.Resize(20, 24)
mainPane.AddChild(sidebar)

// Content area (use HBox for automatic layout instead)
hbox := widgets.NewHBox()
hbox.AddChildWithSize(sidebar, 20)
hbox.AddFlexChild(content)
mainPane.AddChild(hbox)

ui.AddWidget(mainPane)
```

## With Border

Combine Pane with Border for a framed container:

```go
border := widgets.NewBorder()
pane := widgets.NewPane()

// Add VBox to pane for layout
vbox := widgets.NewVBox()
vbox.AddChild(label)
vbox.AddChild(input)
pane.AddChild(vbox)

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
