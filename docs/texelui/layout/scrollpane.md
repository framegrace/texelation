# ScrollPane Container

Scrollable container for content that exceeds viewport size.

## Overview

ScrollPane is a container widget that provides vertical scrolling when content is taller than the available space. It displays a scrollbar and supports keyboard navigation (PgUp/PgDn) and mouse wheel scrolling.

```
┌────────────────────────────────┬─┐
│ Content line 1                 │▲│
│ Content line 2                 │ │
│ Content line 3                 │█│  <- Scrollbar thumb
│ Content line 4                 │ │
│ Content line 5                 │▼│
└────────────────────────────────┴─┘
        Viewport                   Scrollbar
```

## Import

```go
import "texelation/texelui/scroll"
```

## Constructor

```go
func NewScrollPane() *ScrollPane
```

Creates an empty ScrollPane. Position defaults to (0,0) and size to (1,1).

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Style` | `tcell.Style` | Background style |
| `IndicatorStyle` | `tcell.Style` | Scrollbar style |

## Methods

### Content Management

```go
// Set the child widget to scroll
func (sp *ScrollPane) SetChild(child Widget)

// Get the child widget
func (sp *ScrollPane) GetChild() Widget

// Set total content height (required for proper scrolling)
func (sp *ScrollPane) SetContentHeight(h int)

// Get current content height
func (sp *ScrollPane) ContentHeight() int
```

### Scrolling

```go
// Scroll by delta rows (positive = down)
func (sp *ScrollPane) ScrollBy(delta int) bool

// Scroll to make a specific row visible
func (sp *ScrollPane) ScrollTo(row int)

// Scroll to center a row in the viewport
func (sp *ScrollPane) ScrollToCentered(row int)

// Scroll to top/bottom
func (sp *ScrollPane) ScrollToTop()
func (sp *ScrollPane) ScrollToBottom()

// Ensure a specific row is visible
func (sp *ScrollPane) EnsureVisible(row int)

// Ensure focused widget is visible
func (sp *ScrollPane) EnsureFocusedVisible()
```

### State

```go
// Get current scroll offset
func (sp *ScrollPane) ScrollOffset() int

// Check if scrolling is possible
func (sp *ScrollPane) CanScroll() bool
func (sp *ScrollPane) CanScrollUp() bool
func (sp *ScrollPane) CanScrollDown() bool
```

### Display Options

```go
// Show/hide scroll indicators
func (sp *ScrollPane) ShowIndicators(show bool)

// Focus behavior at boundaries
func (sp *ScrollPane) SetTrapsFocus(trap bool)
```

## Example: Scrollable Form

```go
package main

import (
    "fmt"
    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/scroll"
    "texelation/texelui/widgets"
)

func main() {
    devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Create scroll pane
        sp := scroll.NewScrollPane()

        // Create form content (taller than viewport)
        form := widgets.NewVBox()
        form.Spacing = 1

        // Add many fields
        for i := 1; i <= 20; i++ {
            row := widgets.NewHBox()
            row.Spacing = 1
            row.AddChildWithSize(widgets.NewLabel(fmt.Sprintf("Field %d:", i)), 12)
            row.AddFlexChild(widgets.NewInput())
            form.AddChild(row)
        }

        // Set form as scroll pane content
        sp.SetChild(form)
        sp.SetContentHeight(40)  // 20 fields * 2 rows each

        ui.AddWidget(sp)
        ui.Focus(sp)

        app := adapter.NewUIApp("Form", ui)
        app.OnResize(func(w, h int) {
            sp.SetPosition(2, 2)
            sp.Resize(w-4, h-4)
        })
        return app, nil
    }, nil)
}
```

## Keyboard Navigation

| Key | Action |
|-----|--------|
| PgUp | Scroll up one page |
| PgDn | Scroll down one page |
| Ctrl+Home | Scroll to top |
| Ctrl+End | Scroll to bottom |
| Mouse wheel | Scroll 3 lines |

## Mouse Interaction

The scrollbar supports:
- **Click on arrows** - Scroll one line
- **Click on track** - Page up/down
- **Drag thumb** - Direct scroll position control
- **Mouse wheel** - Scroll anywhere in the pane

## Content Height

ScrollPane needs to know the total content height to calculate scrollbar size:

```go
// Option 1: Set explicitly
sp.SetContentHeight(100)  // 100 rows of content

// Option 2: Content implements ContentHeightProvider
type ContentHeightProvider interface {
    ContentHeight() int
}

// If child implements this, ScrollPane queries it automatically
```

## Focus Management

ScrollPane automatically scrolls to keep the focused widget visible:

```go
// When Tab navigates to a widget outside the viewport,
// ScrollPane scrolls to bring it into view
sp.EnsureFocusedVisible()
```

### Focus Trapping

Control what happens at focus boundaries:

```go
// Default: focus escapes at boundaries (Tab moves to next widget outside)
sp.SetTrapsFocus(false)

// Trap focus: wrap around at boundaries
sp.SetTrapsFocus(true)  // Tab wraps from last to first child
```

## Scrollbar Appearance

ScrollPane shows a vertical scrollbar with:
- Up arrow (▲) at top
- Track area in the middle
- Draggable thumb (█)
- Down arrow (▼) at bottom

```
┌─────────────────────────┬─┐
│ Content                 │▲│  Up arrow
│ Content                 │ │  Track
│ Content                 │█│  Thumb (draggable)
│ Content                 │ │  Track
│ Content                 │▼│  Down arrow
└─────────────────────────┴─┘
```

## Common Patterns

### Long Form

```go
sp := scroll.NewScrollPane()

form := widgets.NewVBox()
form.Spacing = 1

// Personal info
form.AddChild(widgets.NewLabel("=== Personal ==="))
form.AddChild(createField("Name:"))
form.AddChild(createField("Email:"))
form.AddChild(createField("Phone:"))

// Address
form.AddChild(widgets.NewLabel("=== Address ==="))
form.AddChild(createField("Street:"))
form.AddChild(createField("City:"))
form.AddChild(createField("Country:"))

// ... more sections

sp.SetChild(form)
sp.SetContentHeight(calculateFormHeight(form))
```

### Settings Panel

```go
sp := scroll.NewScrollPane()

settings := widgets.NewVBox()
settings.Spacing = 1

categories := []struct {
    name    string
    options []string
}{
    {"General", []string{"Auto-save", "Show tooltips"}},
    {"Appearance", []string{"Dark mode", "Large fonts"}},
    {"Privacy", []string{"Analytics", "Crash reports"}},
}

for _, cat := range categories {
    settings.AddChild(widgets.NewLabel("=== " + cat.name + " ==="))
    for _, opt := range cat.options {
        settings.AddChild(widgets.NewCheckbox(opt))
    }
}

sp.SetChild(settings)
```

### List View

```go
sp := scroll.NewScrollPane()

list := widgets.NewVBox()

for _, item := range items {
    row := widgets.NewHBox()
    row.AddChild(widgets.NewCheckbox(""))
    row.AddFlexChild(widgets.NewLabel(item.Name))
    row.AddChild(widgets.NewLabel(item.Date))
    list.AddChild(row)
}

sp.SetChild(list)
sp.SetContentHeight(len(items))  // One row per item
```

## PageNavigator Interface

For selection-based scrolling (like lists), content can implement PageNavigator:

```go
type PageNavigator interface {
    HandlePageNavigation(direction int, pageSize int) bool
}
```

This allows PgUp/PgDn to move selection rather than just scroll the viewport.

## Tips

1. **Always set content height** - ScrollPane needs this for proper scrollbar sizing

2. **Use with VBox** - VBox inside ScrollPane is the most common pattern

3. **Focus management is automatic** - Tab navigation scrolls to keep focus visible

4. **Handle resize** - Update ScrollPane size in OnResize callback

5. **Content width** - ScrollPane resizes content width to match viewport (minus scrollbar)

## See Also

- [VBox](/texelui/layout/vbox.md) - Vertical stacking (common child)
- [ScrollableList](/texelui/primitives/scrollablelist.md) - Pre-built scrolling list
- [Layout Overview](/texelui/layout/README.md) - When to use each container
