# Layout Reference

Layout containers for automatic widget positioning.

## Overview

TexelUI uses **layout containers** (VBox, HBox, ScrollPane) to automatically position child widgets. This is the recommended approach for most UIs.

For special cases requiring pixel-perfect control, you can use manual positioning.

## Layout Containers

| Container | Description |
|-----------|-------------|
| [VBox](/texelui/layout/vbox.md) | Vertical stacking (top to bottom) |
| [HBox](/texelui/layout/hbox.md) | Horizontal arrangement (left to right) |
| [ScrollPane](/texelui/layout/scrollpane.md) | Scrollable content with scrollbar |
| [TabPanel](/texelui/widgets/tabpanel.md) | Tabbed content panels |

## Quick Start

```go
// Create a vertical layout
form := widgets.NewVBox()
form.Spacing = 1  // Gap between children

// Add widgets - no positions needed!
form.AddChild(widgets.NewLabel("Name:"))
form.AddChild(widgets.NewInput())
form.AddChild(widgets.NewButton("Submit"))

// Add to UIManager and set size
ui.AddWidget(form)
app.OnResize(func(w, h int) {
    form.SetPosition(2, 2)
    form.Resize(w-4, h-4)
})
```

## Child Sizing Methods

Layout containers offer three ways to add children:

### Natural Size (AddChild)

```go
vbox.AddChild(label)  // Uses label's natural size (text length x 1)
vbox.AddChild(button) // Uses button's natural size (text + padding)
```

### Fixed Size (AddChildWithSize)

```go
hbox.AddChildWithSize(label, 15)   // 15 cells wide in HBox
vbox.AddChildWithSize(header, 3)   // 3 rows tall in VBox
```

### Flex (AddFlexChild)

```go
vbox.AddFlexChild(content)  // Expands to fill remaining space
```

## Layout Comparison

```
VBox:                        HBox:
┌────────────────────┐       ┌────────────────────┐
│ ┌────────────────┐ │       │ ┌────┐┌────┐┌────┐│
│ │    Child 1     │ │       │ │ C1 ││ C2 ││ C3 ││
│ └────────────────┘ │       │ └────┘└────┘└────┘│
│ ┌────────────────┐ │       └────────────────────┘
│ │    Child 2     │ │
│ └────────────────┘ │       ScrollPane:
│ ┌────────────────┐ │       ┌────────────────┬─┐
│ │    Child 3     │ │       │ Content        │▲│
│ └────────────────┘ │       │ (scrollable)   │█│
└────────────────────┘       │                │▼│
                             └────────────────┴─┘
```

## Nesting Containers

Combine layout containers for complex UIs:

```go
// Main vertical layout
main := widgets.NewVBox()
main.Spacing = 1

// Header row
header := widgets.NewHBox()
header.AddChild(widgets.NewLabel("Title"))
header.AddFlexChild(widgets.NewLabel(""))  // Spacer
header.AddChild(widgets.NewButton("X"))
main.AddChild(header)

// Content area (expands)
content := widgets.NewVBox()
content.AddChild(widgets.NewLabel("Field 1:"))
content.AddChild(widgets.NewInput())
main.AddFlexChild(content)

// Button row
buttons := widgets.NewHBox()
buttons.Spacing = 2
buttons.AddChild(widgets.NewButton("OK"))
buttons.AddChild(widgets.NewButton("Cancel"))
main.AddChild(buttons)
```

Result:
```
┌─────────────────────────────────┐
│ Title                       [X] │  <- HBox
├─────────────────────────────────┤
│ Field 1:                        │
│ [________________________]      │  <- VBox (flex)
│                                 │
├─────────────────────────────────┤
│ [ OK ]  [ Cancel ]              │  <- HBox
└─────────────────────────────────┘
```

## Properties

### Common Properties

```go
box.Spacing = 2                    // Gap between children
box.Align = widgets.BoxAlignStart  // Alignment (Start/Center/End)
```

### ScrollPane Properties

```go
sp := scroll.NewScrollPane()
sp.SetChild(content)
sp.SetContentHeight(100)           // Total content height
sp.ShowIndicators(true)            // Show scrollbar
sp.SetTrapsFocus(true)             // Wrap focus at boundaries
```

## When to Use Each

| Container | Best For |
|-----------|----------|
| **VBox** | Forms, vertical lists, main layouts |
| **HBox** | Button rows, toolbars, field+label pairs |
| **ScrollPane** | Long content, forms with many fields |
| **TabPanel** | Settings, multi-page dialogs |
| **Manual** | Overlays, custom positioning, animations |

## Manual Positioning (Advanced)

For cases where automatic layout doesn't fit:

```go
// Create widgets with explicit positions
label := widgets.NewLabel("Status")
label.SetPosition(5, 10)
label.Resize(20, 1)

// Or use absolute positioning in OnResize
app.OnResize(func(w, h int) {
    // Center a widget manually
    label.SetPosition(w/2 - 10, h/2)
})
```

Manual positioning is useful for:
- Floating dialogs/popups
- Custom animations
- Precise control over overlap
- Legacy code migration

## See Also

- [VBox Reference](/texelui/layout/vbox.md) - Detailed VBox documentation
- [HBox Reference](/texelui/layout/hbox.md) - Detailed HBox documentation
- [ScrollPane Reference](/texelui/layout/scrollpane.md) - Scrollable containers
- [Architecture](/texelui/core-concepts/architecture.md) - How layouts fit in
- [Building a Form](/texelui/tutorials/building-a-form.md) - Practical layout examples
