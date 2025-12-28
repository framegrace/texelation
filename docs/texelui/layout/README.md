# Layout Reference

Layout managers for automatic widget positioning.

## Overview

TexelUI provides layout managers to automatically position widgets. By default, widgets use absolute positioning (you set x, y, w, h explicitly).

## Available Layouts

| Layout | Description |
|--------|-------------|
| [Absolute](absolute.md) | Manual positioning (default) |
| [VBox](vbox.md) | Vertical stacking |
| [HBox](hbox.md) | Horizontal arrangement |

## Layout Interface

```go
type Layout interface {
    Apply(container Rect, children []Widget)
}
```

Layouts position widgets but don't resize them. Widgets must have their sizes set before layout.

## Using Layouts

### Set Layout on UIManager

```go
import "texelation/texelui/layout"

ui := core.NewUIManager()
ui.SetLayout(layout.NewVBox(1))  // 1-cell spacing
```

### Layout Application

Layouts are applied during `Resize()` and `Render()`:

```go
ui.AddWidget(widget1)
ui.AddWidget(widget2)
ui.AddWidget(widget3)

// When Resize is called, VBox positions widgets vertically
ui.Resize(80, 24)
```

## Comparison

```
Absolute (Default):          VBox:                    HBox:
┌────────────────────┐       ┌────────────────────┐   ┌────────────────────┐
│ Widget at (5, 3)   │       │ Widget 1           │   │ W1  W2  W3         │
│                    │       │ Widget 2           │   │                    │
│    Widget at (20,8)│       │ Widget 3           │   │                    │
└────────────────────┘       └────────────────────┘   └────────────────────┘
```

## When to Use Each

| Layout | Use Case |
|--------|----------|
| **Absolute** | Custom UIs, overlays, pixel-perfect positioning |
| **VBox** | Forms, vertical lists, stacked content |
| **HBox** | Button rows, toolbars, horizontal menus |

## Combining Layouts

Layouts apply to UIManager's direct children. For nested layouts, use container widgets:

```go
// Main UI with horizontal layout
ui := core.NewUIManager()
ui.SetLayout(layout.NewHBox(2))

// Left pane (with its own layout)
leftPane := widgets.NewPane(0, 0, 30, 20, tcell.StyleDefault)
// Position children within leftPane manually

// Right pane
rightPane := widgets.NewPane(0, 0, 50, 20, tcell.StyleDefault)

ui.AddWidget(leftPane)
ui.AddWidget(rightPane)
```

## Manual vs Automatic

| Approach | Pros | Cons |
|----------|------|------|
| **Manual (Absolute)** | Full control, predictable | Tedious, no auto-resize |
| **VBox/HBox** | Quick, consistent spacing | Limited flexibility |
| **Mixed** | Best of both | More complex |

## Quick Examples

### Form with VBox

```go
ui := core.NewUIManager()
ui.SetLayout(layout.NewVBox(1))

ui.AddWidget(widgets.NewLabel(0, 0, 30, 1, "Username:"))
ui.AddWidget(widgets.NewInput(0, 0, 30))
ui.AddWidget(widgets.NewLabel(0, 0, 30, 1, "Password:"))
ui.AddWidget(widgets.NewInput(0, 0, 30))
ui.AddWidget(widgets.NewButton(0, 0, 12, 1, "Login"))
```

### Button Row with HBox

```go
ui := core.NewUIManager()
ui.SetLayout(layout.NewHBox(2))

ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Save"))
ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Cancel"))
ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Help"))
```

## See Also

- [Architecture](../core-concepts/architecture.md) - How layouts fit in
- [Widget Interface](../core-concepts/widget-interface.md) - Widget sizing
- [Pane](../widgets/pane.md) - Container for nested layouts
