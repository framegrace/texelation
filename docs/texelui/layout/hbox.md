# HBox Layout

Horizontal arrangement layout that positions widgets left to right.

## Overview

HBox automatically arranges widgets horizontally, perfect for button rows, toolbars, and side-by-side content.

```
┌────────────────────────────────────────────────┐
│  ┌─────────┐   ┌─────────┐   ┌─────────┐      │
│  │ Widget1 │ → │ Widget2 │ → │ Widget3 │      │
│  └─────────┘   └─────────┘   └─────────┘      │
│               spacing                          │
└────────────────────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/layout"
```

## Constructor

```go
func NewHBox(spacing int) *HBox
```

**Parameters:**
- `spacing` - Horizontal gap between widgets (in columns)

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

    // Apply HBox layout with 2-column spacing
    ui.SetLayout(layout.NewHBox(2))

    // Widgets are arranged horizontally automatically
    ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Save"))
    ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Cancel"))
    ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Help"))

    devshell.Run(ui)
}
```

**Output:**

```
[ Save ]  [ Cancel ]  [ Help ]
```

## How It Works

1. HBox iterates through widgets in add order
2. Each widget is positioned at `x = previous widget right + spacing`
3. Widget y position is preserved (set in constructor or manually)
4. Widget dimensions are preserved (HBox doesn't resize widgets)

```go
// Layout algorithm (simplified)
func (h *HBox) Apply(container Rect, children []Widget) {
    x := container.X
    for _, child := range children {
        pos := child.Position()
        child.SetPosition(x, pos.Y)

        x += child.Size().W + h.spacing
    }
}
```

## Spacing

Control the gap between widgets:

```go
// No spacing (widgets touch)
ui.SetLayout(layout.NewHBox(0))

// 2-column spacing (typical)
ui.SetLayout(layout.NewHBox(2))

// 4-column spacing (more separation)
ui.SetLayout(layout.NewHBox(4))
```

**Visual comparison:**

```
Spacing=0:   [Save][Cancel][Help]

Spacing=2:   [Save]  [Cancel]  [Help]

Spacing=4:   [Save]    [Cancel]    [Help]
```

## Widget Sizing

HBox positions widgets but **doesn't resize them**. Set dimensions before adding:

```go
// Different widths
small := widgets.NewButton(0, 0, 8, 1, "OK")
medium := widgets.NewButton(0, 0, 12, 1, "Cancel")
large := widgets.NewButton(0, 0, 20, 1, "More Options...")

ui.AddWidget(small)
ui.AddWidget(medium)
ui.AddWidget(large)
```

**Result:**

```
[ OK ]  [ Cancel ]  [ More Options... ]
```

## Y Position

Widgets keep their y position. Use this for vertical alignment:

```go
// All at y=0 (top-aligned)
ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Top"))

// One lower (y=2)
ui.AddWidget(widgets.NewButton(0, 2, 10, 3, "Lower"))

// Back to top
ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Top"))
```

**Result:**

```
[ Top ]              [ Top ]
           ┌────────┐
           │ Lower  │
           └────────┘
```

## Button Row Pattern

Common pattern for dialog buttons:

```go
func createButtonRow() *core.UIManager {
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewHBox(2))

    ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Save"))
    ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Cancel"))
    ui.AddWidget(widgets.NewButton(0, 0, 10, 1, "Apply"))

    return ui
}
```

## Toolbar Pattern

Horizontal toolbar with icons:

```go
func createToolbar() *core.UIManager {
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewHBox(1))

    tools := []string{"New", "Open", "Save", "|", "Cut", "Copy", "Paste"}

    for _, tool := range tools {
        if tool == "|" {
            // Separator
            ui.AddWidget(widgets.NewLabel(0, 0, 1, 1, "│"))
        } else {
            ui.AddWidget(widgets.NewButton(0, 0, len(tool)+2, 1, tool))
        }
    }

    return ui
}
```

**Result:**

```
[New] [Open] [Save] │ [Cut] [Copy] [Paste]
```

## Sidebar Layout Pattern

Two-column layout with HBox:

```go
func createTwoColumn(totalWidth, height int) *core.UIManager {
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewHBox(1))

    sidebarWidth := 20
    contentWidth := totalWidth - sidebarWidth - 1  // -1 for spacing

    sidebar := widgets.NewPane(0, 0, sidebarWidth, height, sidebarStyle)
    content := widgets.NewPane(0, 0, contentWidth, height, contentStyle)

    ui.AddWidget(sidebar)
    ui.AddWidget(content)

    return ui
}
```

**Result:**

```
┌──────────────────┐ ┌──────────────────────────────────────┐
│    Sidebar       │ │              Content                 │
│                  │ │                                      │
│    - Item 1      │ │                                      │
│    - Item 2      │ │                                      │
│    - Item 3      │ │                                      │
└──────────────────┘ └──────────────────────────────────────┘
```

## Mixed Heights

Widgets with different heights work correctly:

```go
ui.SetLayout(layout.NewHBox(2))

// Single-line label
ui.AddWidget(widgets.NewLabel(0, 0, 10, 1, "Status:"))

// Multi-line text area
ui.AddWidget(widgets.NewTextArea(0, 0, 30, 5))

// Single-line button
ui.AddWidget(widgets.NewButton(0, 0, 8, 1, "Send"))
```

**Result:**

```
Status:   ┌────────────────────────────┐  [Send]
          │                            │
          │                            │
          │                            │
          └────────────────────────────┘
```

## Combining Layouts

Use Pane containers for mixed horizontal and vertical layouts:

```go
ui := core.NewUIManager()
ui.SetLayout(layout.NewHBox(2))

// Left panel with vertical layout
leftPane := widgets.NewPane(0, 0, 30, 15, leftStyle)
// Add vertically-stacked widgets inside leftPane

// Right panel with vertical layout
rightPane := widgets.NewPane(0, 0, 45, 15, rightStyle)
// Add vertically-stacked widgets inside rightPane

ui.AddWidget(leftPane)
ui.AddWidget(rightPane)
```

## Responsive Width

Calculate widget widths based on available space:

```go
func createResponsiveRow(totalWidth int) *core.UIManager {
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewHBox(2))

    buttonCount := 3
    spacing := 2
    totalSpacing := spacing * (buttonCount - 1)  // 4
    availableWidth := totalWidth - totalSpacing
    buttonWidth := availableWidth / buttonCount

    ui.AddWidget(widgets.NewButton(0, 0, buttonWidth, 1, "Save"))
    ui.AddWidget(widgets.NewButton(0, 0, buttonWidth, 1, "Cancel"))
    ui.AddWidget(widgets.NewButton(0, 0, buttonWidth, 1, "Help"))

    return ui
}
```

## Layout Interface

HBox implements the Layout interface:

```go
type Layout interface {
    Apply(container Rect, children []Widget)
}
```

**When applied:**
- During `UIManager.Resize()`
- During `UIManager.Render()`

## Overflow Behavior

If widgets exceed container width, they continue off-screen:

```go
// Container is 40 wide
ui.Resize(40, 10)
ui.SetLayout(layout.NewHBox(2))

// These widgets total 44 columns + spacing
ui.AddWidget(widgets.NewButton(0, 0, 12, 1, "Button 1"))
ui.AddWidget(widgets.NewButton(0, 0, 12, 1, "Button 2"))
ui.AddWidget(widgets.NewButton(0, 0, 12, 1, "Button 3"))
ui.AddWidget(widgets.NewButton(0, 0, 12, 1, "Button 4"))  // Partially off-screen
```

Consider calculating widths to fit, or using a scrollable container.

## Tips

1. **Calculate total width** - `sum(widget widths) + (count-1) * spacing`

2. **Use consistent heights** - Same height for button rows looks cleaner

3. **Combine with VBox** - Use Pane containers for complex layouts:
   ```go
   // Main VBox layout
   mainUI.SetLayout(layout.NewVBox(2))
   mainUI.AddWidget(headerRow)      // HBox inside
   mainUI.AddWidget(contentArea)
   mainUI.AddWidget(buttonRow)      // HBox inside
   ```

4. **Visual separators** - Use thin Label widgets for dividers:
   ```go
   ui.AddWidget(widgets.NewLabel(0, 0, 1, 1, "│"))
   ```

## See Also

- [VBox](vbox.md) - Vertical stacking
- [Absolute](absolute.md) - Manual positioning
- [Layout Overview](README.md) - When to use each layout
