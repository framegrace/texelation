# VBox Layout

Vertical stacking layout that arranges widgets top to bottom.

## Overview

VBox automatically positions widgets in a vertical stack, perfect for forms, menus, and lists.

```
┌────────────────────────────────┐
│  ┌──────────────────────────┐  │
│  │        Widget 1          │  │
│  └──────────────────────────┘  │
│              ↓ spacing         │
│  ┌──────────────────────────┐  │
│  │        Widget 2          │  │
│  └──────────────────────────┘  │
│              ↓ spacing         │
│  ┌──────────────────────────┐  │
│  │        Widget 3          │  │
│  └──────────────────────────┘  │
└────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/layout"
```

## Constructor

```go
func NewVBox(spacing int) *VBox
```

**Parameters:**
- `spacing` - Vertical gap between widgets (in rows)

## Example

```go
package main

import (
    "texelation/texelui/core"
    "texelation/texelui/layout"
    "texelation/texelui/widgets"
    "texelation/internal/devshell"
)

func main() {
    ui := core.NewUIManager()

    // Apply VBox layout with 1-row spacing
    ui.SetLayout(layout.NewVBox(1))

    // Widgets are stacked vertically automatically
    ui.AddWidget(widgets.NewLabel(0, 0, 30, 1, "Username:"))
    ui.AddWidget(widgets.NewInput(0, 0, 30))
    ui.AddWidget(widgets.NewLabel(0, 0, 30, 1, "Password:"))
    ui.AddWidget(widgets.NewInput(0, 0, 30))
    ui.AddWidget(widgets.NewButton(0, 0, 12, 1, "Login"))

    devshell.Run(ui)
}
```

**Output:**

```
Username:
┌────────────────────────────┐
│                            │
└────────────────────────────┘

Password:
┌────────────────────────────┐
│                            │
└────────────────────────────┘

[ Login ]
```

## How It Works

1. VBox iterates through widgets in add order
2. Each widget is positioned at `y = previous widget bottom + spacing`
3. Widget x position is preserved (set in constructor or manually)
4. Widget dimensions are preserved (VBox doesn't resize widgets)

```go
// Layout algorithm (simplified)
func (v *VBox) Apply(container Rect, children []Widget) {
    y := container.Y
    for _, child := range children {
        pos := child.Position()
        child.SetPosition(pos.X, y)

        y += child.Size().H + v.spacing
    }
}
```

## Spacing

Control the gap between widgets:

```go
// No spacing (widgets touch)
ui.SetLayout(layout.NewVBox(0))

// 1-row spacing
ui.SetLayout(layout.NewVBox(1))

// 2-row spacing (more breathing room)
ui.SetLayout(layout.NewVBox(2))
```

**Visual comparison:**

```
Spacing=0:     Spacing=1:     Spacing=2:
┌──────┐       ┌──────┐       ┌──────┐
│  A   │       │  A   │       │  A   │
├──────┤       └──────┘       └──────┘
│  B   │
├──────┤       ┌──────┐       ┌──────┐
│  C   │       │  B   │       │  B   │
└──────┘       └──────┘       └──────┘

               ┌──────┐       ┌──────┐
               │  C   │       │  C   │
               └──────┘       └──────┘
```

## Widget Sizing

VBox positions widgets but **doesn't resize them**. Set dimensions before adding:

```go
// Width matters - VBox won't change it
label := widgets.NewLabel(0, 0, 30, 1, "Name:")    // 30 wide
input := widgets.NewInput(0, 0, 20)                 // 20 wide
button := widgets.NewButton(0, 0, 10, 1, "Submit") // 10 wide

ui.AddWidget(label)
ui.AddWidget(input)
ui.AddWidget(button)
```

**Result:**

```
┌────────────────────────────────┐
│Name:                           │  (30 wide)
├──────────────────────┐         │
│                      │         │  (20 wide)
└──────────────────────┘         │
├────────────┐                   │  (10 wide)
│  Submit    │                   │
└────────────┘                   │
```

## X Position

Widgets keep their x position. Use this for alignment:

```go
// Left-aligned (x=0)
label := widgets.NewLabel(0, 0, 30, 1, "Left")

// Indented (x=4)
input := widgets.NewInput(4, 0, 26)

// Centered (calculated x)
button := widgets.NewButton(9, 0, 12, 1, "Submit")

ui.AddWidget(label)
ui.AddWidget(input)
ui.AddWidget(button)
```

**Result:**

```
Left
    ┌────────────────────────┐
    │                        │
    └────────────────────────┘
         [  Submit  ]
```

## Form Layout Pattern

Build forms with label-input pairs:

```go
func createForm() *core.UIManager {
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewVBox(1))

    // Form fields
    fields := []struct {
        label   string
        width   int
    }{
        {"Name:", 40},
        {"Email:", 40},
        {"Phone:", 20},
        {"Address:", 50},
    }

    for _, f := range fields {
        ui.AddWidget(widgets.NewLabel(0, 0, len(f.label), 1, f.label))
        ui.AddWidget(widgets.NewInput(0, 0, f.width))
    }

    // Submit button
    ui.AddWidget(widgets.NewButton(0, 0, 12, 1, "Submit"))

    return ui
}
```

## Menu Pattern

Vertical menus with consistent spacing:

```go
func createMenu() *core.UIManager {
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewVBox(0))  // No spacing for compact menu

    menuItems := []string{
        "New File",
        "Open...",
        "Save",
        "Save As...",
        "Exit",
    }

    for _, item := range menuItems {
        ui.AddWidget(widgets.NewButton(0, 0, 15, 1, item))
    }

    return ui
}
```

## Combining with Manual Layout

VBox applies to UIManager's direct children. Use containers for nested layouts:

```go
ui := core.NewUIManager()
ui.SetLayout(layout.NewVBox(2))

// Header section
header := widgets.NewLabel(0, 0, 40, 1, "Settings")

// Form section (manually laid out inside Pane)
formPane := widgets.NewPane(0, 0, 40, 8, style)
// Add form widgets to formPane with absolute positioning

// Button row (manually laid out inside Pane)
buttonPane := widgets.NewPane(0, 0, 40, 3, style)
// Add buttons horizontally inside buttonPane

ui.AddWidget(header)
ui.AddWidget(formPane)
ui.AddWidget(buttonPane)
```

**Result:**

```
Settings                         (VBox positions this)

┌──────────────────────────────┐ (VBox positions this)
│  Name: [_______________]     │
│  Email: [______________]     │  (contents manually
│                              │   positioned inside)
└──────────────────────────────┘

┌──────────────────────────────┐ (VBox positions this)
│  [ Save ]    [ Cancel ]      │  (buttons manually
└──────────────────────────────┘   positioned inside)
```

## Layout Interface

VBox implements the Layout interface:

```go
type Layout interface {
    Apply(container Rect, children []Widget)
}
```

**When applied:**
- During `UIManager.Resize()`
- During `UIManager.Render()`

## Dynamic Content

Add or remove widgets dynamically:

```go
func (app *MyApp) addNotification(msg string) {
    label := widgets.NewLabel(0, 0, 40, 1, msg)
    app.ui.AddWidget(label)
    // VBox will position it on next render
}

func (app *MyApp) clearNotifications() {
    app.ui.ClearWidgets()
    // Re-add permanent widgets
}
```

## Tips

1. **Set widget widths explicitly** - VBox won't resize widgets horizontally

2. **Use spacing consistently** - Same spacing value across the app looks better

3. **Container widgets for complex layouts** - Use Pane to group widgets that need their own layout

4. **Consider height** - VBox stacks based on widget heights; multi-line widgets work correctly

## See Also

- [HBox](/texelui/layout/hbox.md) - Horizontal arrangement
- [Absolute](/texelui/layout/absolute.md) - Manual positioning
- [Layout Overview](/texelui/layout/README.md) - When to use each layout
