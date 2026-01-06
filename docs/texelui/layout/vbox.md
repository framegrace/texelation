# VBox Container

Vertical layout container that stacks children from top to bottom.

## Overview

VBox is a container widget that automatically positions its children in a vertical stack. Children are arranged from top to bottom with configurable spacing.

```
┌────────────────────────────────┐
│  ┌──────────────────────────┐  │
│  │        Child 1           │  │
│  └──────────────────────────┘  │
│              ↓ spacing         │
│  ┌──────────────────────────┐  │
│  │        Child 2           │  │
│  └──────────────────────────┘  │
│              ↓ spacing         │
│  ┌──────────────────────────┐  │
│  │        Child 3           │  │
│  └──────────────────────────┘  │
└────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewVBox() *VBox
```

Creates an empty VBox container. Position defaults to (0,0) and size to (1,1).

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Spacing` | `int` | Vertical gap between children (rows) |
| `Align` | `BoxAlign` | Child alignment: `BoxAlignStart`, `BoxAlignCenter`, `BoxAlignEnd` |
| `Style` | `tcell.Style` | Background style |

## Methods

### Adding Children

```go
// Add with natural size (widget's preferred dimensions)
func (v *VBox) AddChild(w Widget)

// Add with fixed height
func (v *VBox) AddChildWithSize(w Widget, height int)

// Add as flex child (expands to fill remaining space)
func (v *VBox) AddFlexChild(w Widget)

// Remove all children
func (v *VBox) ClearChildren()
```

### Positioning

```go
func (v *VBox) SetPosition(x, y int)
func (v *VBox) Resize(w, h int)
```

## Example: Simple Form

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

        // Create vertical layout
        form := widgets.NewVBox()
        form.Spacing = 1

        // Add widgets - VBox handles positioning
        form.AddChild(widgets.NewLabel("Username:"))
        form.AddChild(widgets.NewInput())
        form.AddChild(widgets.NewLabel("Password:"))
        form.AddChild(widgets.NewInput())
        form.AddChild(widgets.NewButton("Login"))

        ui.AddWidget(form)
        ui.Focus(form)

        app := adapter.NewUIApp("Login", ui)
        app.OnResize(func(w, h int) {
            form.SetPosition(5, 2)
            form.Resize(40, h-4)
        })
        return app, nil
    }, nil)
}
```

**Output:**
```
     Username:
     [____________________]

     Password:
     [____________________]

     [ Login ]
```

## Child Sizing

VBox offers three ways to size children:

### Natural Size (AddChild)

Uses the widget's natural/preferred size:

```go
vbox.AddChild(widgets.NewLabel("Name:"))   // Height: 1 (text height)
vbox.AddChild(widgets.NewButton("OK"))     // Height: 1 (button height)
vbox.AddChild(widgets.NewTextArea())       // Height: 4 (default)
```

### Fixed Size (AddChildWithSize)

Specifies an exact height:

```go
vbox.AddChildWithSize(header, 3)    // 3 rows tall
vbox.AddChildWithSize(content, 10)  // 10 rows tall
vbox.AddChildWithSize(footer, 2)    // 2 rows tall
```

### Flex (AddFlexChild)

Expands to fill remaining space:

```go
vbox.AddChild(header)           // Natural size (e.g., 1 row)
vbox.AddFlexChild(content)      // Fills remaining space
vbox.AddChild(statusBar)        // Natural size (e.g., 1 row)
```

**Flex distribution:**
```
┌────────────────────────────────┐
│ Header (1 row)                 │
├────────────────────────────────┤
│                                │
│ Content (FLEX - fills space)   │
│                                │
├────────────────────────────────┤
│ Status (1 row)                 │
└────────────────────────────────┘
```

Multiple flex children share space equally:

```go
vbox.AddFlexChild(panel1)  // Gets 50% of remaining space
vbox.AddFlexChild(panel2)  // Gets 50% of remaining space
```

## Spacing

```go
vbox := widgets.NewVBox()
vbox.Spacing = 2  // 2 rows between children
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

## Nesting Layouts

Combine VBox and HBox for complex layouts:

```go
// Main vertical layout
main := widgets.NewVBox()
main.Spacing = 1

// Field row (horizontal): Label + Input
fieldRow := widgets.NewHBox()
fieldRow.Spacing = 1
fieldRow.AddChildWithSize(widgets.NewLabel("Name:"), 10)
fieldRow.AddFlexChild(widgets.NewInput())
main.AddChild(fieldRow)

// Another field row
emailRow := widgets.NewHBox()
emailRow.Spacing = 1
emailRow.AddChildWithSize(widgets.NewLabel("Email:"), 10)
emailRow.AddFlexChild(widgets.NewInput())
main.AddChild(emailRow)

// Button row
buttons := widgets.NewHBox()
buttons.Spacing = 2
buttons.AddChild(widgets.NewButton("OK"))
buttons.AddChild(widgets.NewButton("Cancel"))
main.AddChild(buttons)
```

**Result:**
```
┌────────────────────────────────────┐
│ Name:     [_______________________]│
│ Email:    [_______________________]│
│ [ OK ]  [ Cancel ]                 │
└────────────────────────────────────┘
```

## Focus Management

VBox automatically manages focus for its children:

- **Tab** moves to the next focusable child
- **Shift+Tab** moves to the previous focusable child
- When VBox receives focus, it focuses its first (or last remembered) focusable child

```go
// Focus will cycle through: input1 -> input2 -> button
vbox.AddChild(widgets.NewLabel("Name:"))   // Not focusable
vbox.AddChild(input1)                       // Focusable
vbox.AddChild(widgets.NewLabel("Email:"))  // Not focusable
vbox.AddChild(input2)                       // Focusable
vbox.AddChild(button)                       // Focusable
```

## Common Patterns

### Form with Labels

```go
form := widgets.NewVBox()
form.Spacing = 1

// Use HBox for each label+field pair
addField := func(label string) *widgets.Input {
    row := widgets.NewHBox()
    row.Spacing = 1
    row.AddChildWithSize(widgets.NewLabel(label), 12)
    input := widgets.NewInput()
    row.AddFlexChild(input)
    form.AddChild(row)
    return input
}

nameInput := addField("Name:")
emailInput := addField("Email:")
phoneInput := addField("Phone:")
```

### Header + Content + Footer

```go
layout := widgets.NewVBox()

header := widgets.NewLabel("My Application")
header.Align = widgets.AlignCenter
layout.AddChild(header)

content := widgets.NewVBox()
// ... add content widgets
layout.AddFlexChild(content)  // Expands

footer := widgets.NewLabel("Status: Ready")
layout.AddChild(footer)
```

### Settings Sections

```go
settings := widgets.NewVBox()
settings.Spacing = 2

// General section
settings.AddChild(widgets.NewLabel("=== General ==="))
settings.AddChild(widgets.NewCheckbox("Enable notifications"))
settings.AddChild(widgets.NewCheckbox("Start minimized"))

// Appearance section
settings.AddChild(widgets.NewLabel("=== Appearance ==="))
settings.AddChild(widgets.NewCheckbox("Dark mode"))
settings.AddChild(widgets.NewCheckbox("Show toolbar"))
```

## Tips

1. **Use flex children for expanding content** - Headers/footers stay fixed, content grows

2. **Nest HBox inside VBox** - Label+Input pairs work great in HBox rows

3. **Set spacing consistently** - Same spacing value across the app looks cleaner

4. **Remember to resize the VBox** - Use `OnResize` to give VBox its dimensions

## See Also

- [HBox](/texelui/layout/hbox.md) - Horizontal arrangement
- [ScrollPane](/texelui/layout/scrollpane.md) - Scrollable containers
- [Layout Overview](/texelui/layout/README.md) - When to use each layout
- [Building a Form](/texelui/tutorials/building-a-form.md) - Practical examples
