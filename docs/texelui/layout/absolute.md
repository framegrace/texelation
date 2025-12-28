# Absolute Layout

Manual widget positioning with explicit coordinates.

## Overview

Absolute layout (the default) gives you complete control over widget positioning. You specify exact x, y, width, and height for each widget.

```
┌────────────────────────────────────────────┐
│                                            │
│     ┌─────────┐                            │
│     │ Widget  │  at (5, 2)                 │
│     └─────────┘                            │
│                                            │
│                    ┌──────────────┐        │
│                    │   Widget     │        │
│                    │   at (20, 6) │        │
│                    └──────────────┘        │
│                                            │
└────────────────────────────────────────────┘
```

## When to Use

- **Custom layouts** that don't fit standard patterns
- **Overlays and dialogs** positioned at specific locations
- **Pixel-perfect positioning** requirements
- **Non-linear arrangements** (diagonal, scattered, etc.)

## Usage

Absolute is the default - no layout manager needed:

```go
package main

import (
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

func main() {
    ui := core.NewUIManager()

    // Position widgets explicitly
    title := widgets.NewLabel(10, 2, 20, 1, "My Application")

    usernameLabel := widgets.NewLabel(5, 5, 10, 1, "Username:")
    usernameInput := widgets.NewInput(16, 5, 25)

    passwordLabel := widgets.NewLabel(5, 7, 10, 1, "Password:")
    passwordInput := widgets.NewInput(16, 7, 25)

    loginButton := widgets.NewButton(16, 10, 12, 1, "Login")
    cancelButton := widgets.NewButton(30, 10, 12, 1, "Cancel")

    ui.AddWidget(title)
    ui.AddWidget(usernameLabel)
    ui.AddWidget(usernameInput)
    ui.AddWidget(passwordLabel)
    ui.AddWidget(passwordInput)
    ui.AddWidget(loginButton)
    ui.AddWidget(cancelButton)
}
```

## Positioning Methods

### SetPosition

Move a widget to specific coordinates:

```go
widget := widgets.NewButton(0, 0, 10, 1, "Click")

// Move to new position
widget.SetPosition(20, 5)
```

### Constructor Coordinates

Most widgets accept position in their constructor:

```go
// NewLabel(x, y, w, h, text)
label := widgets.NewLabel(5, 3, 20, 1, "Hello")

// NewButton(x, y, w, h, text)
button := widgets.NewButton(10, 10, 15, 3, "Submit")

// NewInput(x, y, width)
input := widgets.NewInput(5, 5, 30)
```

### Resize

Change widget dimensions:

```go
widget.Resize(40, 10)  // width=40, height=10
```

## Coordinate System

```
(0,0) ─────────────────────────────────► X (columns)
  │
  │    (5,2)
  │      ┌─────────┐
  │      │ Widget  │
  │      └─────────┘
  │
  │              (15,8)
  │                ┌─────────┐
  │                │ Widget  │
  │                └─────────┘
  │
  ▼
  Y (rows)
```

- **X**: Column position (0 = leftmost)
- **Y**: Row position (0 = topmost)
- **W**: Width in columns
- **H**: Height in rows

## Overlapping Widgets

With absolute positioning, widgets can overlap. Render order determines visibility:

```go
// Background panel (added first, rendered first)
background := widgets.NewPane(0, 0, 40, 20, style)

// Foreground dialog (added last, rendered on top)
dialog := widgets.NewPane(10, 5, 20, 10, dialogStyle)

ui.AddWidget(background)
ui.AddWidget(dialog)  // Appears on top
```

For explicit layering control, implement `ZIndexer`:

```go
type ZIndexer interface {
    ZIndex() int  // Higher values render on top
}
```

## Centering Widgets

Calculate center position manually:

```go
func centerWidget(containerW, containerH, widgetW, widgetH int) (x, y int) {
    x = (containerW - widgetW) / 2
    y = (containerH - widgetH) / 2
    return
}

// Usage
ui.Resize(80, 24)
dialogW, dialogH := 40, 10
x, y := centerWidget(80, 24, dialogW, dialogH)
dialog := widgets.NewPane(x, y, dialogW, dialogH, style)
```

## Responsive Positioning

Handle terminal resize by recalculating positions:

```go
func (app *MyApp) Resize(w, h int) {
    app.width = w
    app.height = h

    // Recalculate widget positions
    app.header.SetPosition(0, 0)
    app.header.Resize(w, 1)

    // Center the main content
    contentW := min(60, w-4)
    contentH := h - 4
    contentX := (w - contentW) / 2
    app.content.SetPosition(contentX, 2)
    app.content.Resize(contentW, contentH)

    // Bottom-right corner
    app.statusBar.SetPosition(0, h-1)
    app.statusBar.Resize(w, 1)
}
```

## Common Patterns

### Dialog Box

```go
func createDialog(containerW, containerH int) *widgets.Pane {
    dialogW, dialogH := 40, 12
    x := (containerW - dialogW) / 2
    y := (containerH - dialogH) / 2

    dialog := widgets.NewPane(x, y, dialogW, dialogH, style)

    // Position elements within dialog
    title := widgets.NewLabel(1, 0, dialogW-2, 1, "Confirm")
    message := widgets.NewLabel(2, 2, dialogW-4, 3, "Are you sure?")
    okBtn := widgets.NewButton(dialogW/2-12, dialogH-3, 10, 1, "OK")
    cancelBtn := widgets.NewButton(dialogW/2+2, dialogH-3, 10, 1, "Cancel")

    dialog.AddChild(title)
    dialog.AddChild(message)
    dialog.AddChild(okBtn)
    dialog.AddChild(cancelBtn)

    return dialog
}
```

### Status Bar

```go
func createStatusBar(width, height int) *widgets.Label {
    // Always at bottom
    return widgets.NewLabel(0, height-1, width, 1, "Ready")
}
```

### Sidebar + Content

```go
func createLayout(w, h int) {
    sidebarWidth := 20

    sidebar := widgets.NewPane(0, 0, sidebarWidth, h, sidebarStyle)
    content := widgets.NewPane(sidebarWidth, 0, w-sidebarWidth, h, contentStyle)

    ui.AddWidget(sidebar)
    ui.AddWidget(content)
}
```

## Comparison with Layouts

| Aspect | Absolute | VBox/HBox |
|--------|----------|-----------|
| Control | Full | Automatic |
| Effort | More code | Less code |
| Resize handling | Manual | Automatic |
| Overlapping | Easy | Not supported |
| Use case | Custom UIs | Standard layouts |

## Tips

1. **Use constants** for common positions:
   ```go
   const (
       marginLeft = 2
       marginTop  = 1
       spacing    = 1
   )
   ```

2. **Create helper functions** for common calculations:
   ```go
   func rightAlign(containerW, widgetW, margin int) int {
       return containerW - widgetW - margin
   }
   ```

3. **Track container size** for responsive layouts:
   ```go
   type MyWidget struct {
       core.BaseWidget
       containerW, containerH int
   }
   ```

## See Also

- [VBox](vbox.md) - Automatic vertical stacking
- [HBox](hbox.md) - Automatic horizontal arrangement
- [Pane](../widgets/pane.md) - Container for grouping widgets
