# HBox Container

Horizontal layout container that arranges children from left to right.

## Overview

HBox is a container widget that automatically positions its children horizontally. Children are arranged from left to right with configurable spacing.

```
┌────────────────────────────────────────────────────┐
│  ┌──────────┐  ┌──────────┐  ┌──────────┐         │
│  │ Child 1  │→ │ Child 2  │→ │ Child 3  │         │
│  └──────────┘  └──────────┘  └──────────┘         │
│              spacing       spacing                 │
└────────────────────────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewHBox() *HBox
```

Creates an empty HBox container. Position defaults to (0,0) and size to (1,1).

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Spacing` | `int` | Horizontal gap between children (columns) |
| `Align` | `BoxAlign` | Child alignment: `BoxAlignStart`, `BoxAlignCenter`, `BoxAlignEnd` |
| `Style` | `tcell.Style` | Background style |

## Methods

### Adding Children

```go
// Add with natural size (widget's preferred dimensions)
func (h *HBox) AddChild(w Widget)

// Add with fixed width
func (h *HBox) AddChildWithSize(w Widget, width int)

// Add as flex child (expands to fill remaining space)
func (h *HBox) AddFlexChild(w Widget)

// Remove all children
func (h *HBox) ClearChildren()
```

### Positioning

```go
func (h *HBox) SetPosition(x, y int)
func (h *HBox) Resize(w, h int)
```

## Example: Button Row

```go
package main

import (
    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

func main() {
    devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Create horizontal button row
        buttons := widgets.NewHBox()
        buttons.Spacing = 2

        buttons.AddChild(widgets.NewButton("Save"))
        buttons.AddChild(widgets.NewButton("Cancel"))
        buttons.AddChild(widgets.NewButton("Help"))

        ui.AddWidget(buttons)
        ui.Focus(buttons)

        app := adapter.NewUIApp("Buttons", ui)
        app.OnResize(func(w, h int) {
            buttons.SetPosition(5, 5)
            buttons.Resize(w-10, 1)
        })
        return app, nil
    }, nil)
}
```

**Output:**
```
     [ Save ]  [ Cancel ]  [ Help ]
```

## Child Sizing

HBox offers three ways to size children:

### Natural Size (AddChild)

Uses the widget's natural/preferred width:

```go
hbox.AddChild(widgets.NewButton("OK"))      // Width: 8 (text + padding)
hbox.AddChild(widgets.NewButton("Cancel"))  // Width: 12 (text + padding)
hbox.AddChild(widgets.NewLabel("Status"))   // Width: 6 (text length)
```

### Fixed Size (AddChildWithSize)

Specifies an exact width:

```go
hbox.AddChildWithSize(label, 15)    // 15 columns wide
hbox.AddChildWithSize(input, 30)    // 30 columns wide
hbox.AddChildWithSize(button, 10)   // 10 columns wide
```

### Flex (AddFlexChild)

Expands to fill remaining space:

```go
hbox.AddChildWithSize(label, 10)   // Fixed 10 columns
hbox.AddFlexChild(input)           // Fills remaining space
hbox.AddChild(button)              // Natural size
```

**Flex distribution:**
```
┌─────────────────────────────────────────────────┐
│ Label (10)  │  Input (FLEX - fills)  │ [Button] │
└─────────────────────────────────────────────────┘
```

## Common Patterns

### Label + Input Field

The most common pattern - label with fixed width, input fills remaining space:

```go
row := widgets.NewHBox()
row.Spacing = 1

label := widgets.NewLabel("Name:")
input := widgets.NewInput()

row.AddChildWithSize(label, 12)  // Fixed label width
row.AddFlexChild(input)          // Input fills remaining space
```

### Toolbar

```go
toolbar := widgets.NewHBox()
toolbar.Spacing = 1

toolbar.AddChild(widgets.NewButton("New"))
toolbar.AddChild(widgets.NewButton("Open"))
toolbar.AddChild(widgets.NewButton("Save"))
toolbar.AddFlexChild(widgets.NewLabel(""))  // Spacer
toolbar.AddChild(widgets.NewButton("Help"))
```

**Result:**
```
[ New ] [ Open ] [ Save ]              [ Help ]
```

### Dialog Buttons (Right-Aligned)

```go
buttonRow := widgets.NewHBox()
buttonRow.Spacing = 2

// Spacer pushes buttons to the right
buttonRow.AddFlexChild(widgets.NewLabel(""))
buttonRow.AddChild(widgets.NewButton("OK"))
buttonRow.AddChild(widgets.NewButton("Cancel"))
```

**Result:**
```
                              [ OK ]  [ Cancel ]
```

### Centered Buttons

```go
buttonRow := widgets.NewHBox()
buttonRow.Spacing = 2

// Spacers on both sides center the buttons
buttonRow.AddFlexChild(widgets.NewLabel(""))
buttonRow.AddChild(widgets.NewButton("OK"))
buttonRow.AddChild(widgets.NewButton("Cancel"))
buttonRow.AddFlexChild(widgets.NewLabel(""))
```

**Result:**
```
           [ OK ]  [ Cancel ]
```

### Status Bar

```go
statusBar := widgets.NewHBox()
statusBar.Spacing = 2

statusBar.AddFlexChild(widgets.NewLabel("Ready"))  // Status text (flex)
statusBar.AddChild(widgets.NewLabel("Ln 1"))       // Line number
statusBar.AddChild(widgets.NewLabel("Col 1"))      // Column number
```

**Result:**
```
Ready                                    Ln 1  Col 1
```

## Spacing

```go
hbox := widgets.NewHBox()
hbox.Spacing = 3  // 3 columns between children
```

**Visual comparison:**
```
Spacing=0:           Spacing=1:           Spacing=2:
┌───┐┌───┐┌───┐      ┌───┐ ┌───┐ ┌───┐   ┌───┐  ┌───┐  ┌───┐
│ A ││ B ││ C │      │ A │ │ B │ │ C │   │ A │  │ B │  │ C │
└───┘└───┘└───┘      └───┘ └───┘ └───┘   └───┘  └───┘  └───┘
```

## Focus Management

HBox automatically manages focus for its children:

- **Tab** moves to the next focusable child
- **Shift+Tab** moves to the previous focusable child
- When HBox receives focus, it focuses its first (or last remembered) focusable child

```go
// Focus will cycle through: button1 -> button2 -> button3
hbox.AddChild(button1)   // Focusable
hbox.AddChild(button2)   // Focusable
hbox.AddChild(button3)   // Focusable
```

## Nesting with VBox

Create complex layouts by nesting HBox inside VBox:

```go
main := widgets.NewVBox()
main.Spacing = 1

// Header row
header := widgets.NewHBox()
header.AddChild(widgets.NewLabel("App Title"))
header.AddFlexChild(widgets.NewLabel(""))
header.AddChild(widgets.NewButton("X"))
main.AddChild(header)

// Form fields (each row is an HBox)
for _, fieldName := range []string{"Name:", "Email:", "Phone:"} {
    row := widgets.NewHBox()
    row.Spacing = 1
    row.AddChildWithSize(widgets.NewLabel(fieldName), 10)
    row.AddFlexChild(widgets.NewInput())
    main.AddChild(row)
}

// Footer with buttons
footer := widgets.NewHBox()
footer.Spacing = 2
footer.AddFlexChild(widgets.NewLabel(""))  // Push buttons right
footer.AddChild(widgets.NewButton("OK"))
footer.AddChild(widgets.NewButton("Cancel"))
main.AddChild(footer)
```

**Result:**
```
┌────────────────────────────────────────┐
│ App Title                          [X] │
│ Name:     [___________________________]│
│ Email:    [___________________________]│
│ Phone:    [___________________________]│
│                     [ OK ]  [ Cancel ] │
└────────────────────────────────────────┘
```

## Tips

1. **Use flex children for alignment** - Empty labels as spacers push content left/right/center

2. **Fixed widths for labels** - Ensures alignment across multiple rows

3. **Natural size for buttons** - Let buttons size to their text

4. **Nest inside VBox** - HBox rows work great in vertical layouts

## See Also

- [VBox](/texelui/layout/vbox.md) - Vertical stacking
- [ScrollPane](/texelui/layout/scrollpane.md) - Scrollable containers
- [Layout Overview](/texelui/layout/README.md) - When to use each layout
- [Building a Form](/texelui/tutorials/building-a-form.md) - Practical examples
