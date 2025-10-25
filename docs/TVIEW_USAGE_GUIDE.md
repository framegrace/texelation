# TView Widget Integration Guide

This guide explains how to add tview widgets to any Texelation app.

## Overview

Texelation provides a **tviewbridge** package that allows you to run tview widgets as background threads in your app, with proper synchronization and no flickering.

## Key Concepts

1. **VirtualScreen** - An in-memory tcell.Screen that captures tview's drawing
2. **TViewApp** - Wrapper that runs tview's event loop in a background goroutine
3. **Double Buffering** - Prevents partial frames from being visible
4. **Transparency** - Default backgrounds are treated as transparent, allowing layered rendering

## Basic Usage

### Step 1: Import Required Packages

```go
import (
    "github.com/gdamore/tcell/v2"
    "github.com/rivo/tview"

    "texelation/internal/tviewbridge"
    "texelation/texel"
)
```

### Step 2: Add TViewApp Field to Your App

```go
type MyApp struct {
    width, height int
    tviewApp      *tviewbridge.TViewApp
}
```

### Step 3: Create TView Widgets in Run()

```go
func (a *MyApp) Run() error {
    // Set default dimensions if needed
    if a.width == 0 || a.height == 0 {
        a.width = 80
        a.height = 24
    }

    // Create your tview widgets
    textView := tview.NewTextView().
        SetDynamicColors(true).
        SetTextAlign(tview.AlignCenter)

    // IMPORTANT: Set background color
    // - tcell.ColorDefault = transparent (base layer shows through)
    // - Any other color = opaque (blocks base layer)
    textView.SetBackgroundColor(tcell.ColorDarkCyan)
    textView.SetText("[yellow]Hello from TView![white]")

    // Optional: Add borders
    textView.SetBorder(true).
        SetTitle(" My Widget ").
        SetBorderColor(tcell.ColorPurple)

    // Create TViewApp wrapper
    a.tviewApp = tviewbridge.NewTViewApp("MyApp", textView)
    a.tviewApp.Resize(a.width, a.height)

    // Start tview's event loop in background
    return a.tviewApp.Run()
}
```

### Step 4: Implement Stop()

```go
func (a *MyApp) Stop() {
    if a.tviewApp != nil {
        a.tviewApp.Stop()
    }
}
```

### Step 5: Implement Resize()

```go
func (a *MyApp) Resize(cols, rows int) {
    a.width = cols
    a.height = rows

    // Forward resize to tview app
    if a.tviewApp != nil {
        a.tviewApp.Resize(cols, rows)
    }
}
```

### Step 6: Implement Render() with Composite Pattern

```go
func (a *MyApp) Render() [][]texel.Cell {
    // Create base layer (always initialized, never empty)
    baseBuffer := make([][]texel.Cell, a.height)
    bgStyle := tcell.StyleDefault.Background(tcell.ColorDarkBlue)

    for y := 0; y < a.height; y++ {
        baseBuffer[y] = make([]texel.Cell, a.width)
        for x := 0; x < a.width; x++ {
            // Optional: Add pattern for debugging
            ch := ' '
            if (x+y)%2 == 0 {
                ch = '·'
            }
            baseBuffer[y][x] = texel.Cell{Ch: ch, Style: bgStyle}
        }
    }

    // If tview isn't ready, return base layer
    if a.tviewApp == nil {
        return baseBuffer
    }

    // Get tview buffer
    tviewBuffer := a.tviewApp.Render()

    // Composite: overlay tview on base buffer
    // Treat default background as transparent
    defaultBg, _, _ := tcell.StyleDefault.Decompose()

    for y := 0; y < len(tviewBuffer) && y < len(baseBuffer); y++ {
        for x := 0; x < len(tviewBuffer[y]) && x < len(baseBuffer[y]); x++ {
            tviewCell := tviewBuffer[y][x]
            _, bg, _ := tviewCell.Style.Decompose()

            // If default bg + space = transparent, keep base layer
            if bg == defaultBg && tviewCell.Ch == ' ' {
                continue
            } else {
                baseBuffer[y][x] = tviewCell
            }
        }
    }

    return baseBuffer
}
```

### Step 7: Implement Refresh Notifications

```go
func (a *MyApp) SetRefreshNotifier(ch chan<- bool) {
    if a.tviewApp != nil {
        a.tviewApp.SetRefreshNotifier(ch)
    }
}
```

### Step 8: Other Required Methods

```go
func (a *MyApp) GetTitle() string {
    return "My App"
}

func (a *MyApp) HandleKey(ev *tcell.EventKey) {
    // Forward keys to tview if you want interactive widgets
    if a.tviewApp != nil {
        a.tviewApp.HandleKey(ev)
    }
}
```

## Complete Example

See `apps/welcome/welcome_static_tview.go` for a complete working example.

## Advanced: Centering Widgets with Flex

To center a widget and make it smaller than the full pane:

```go
// Create your content widget
textView := tview.NewTextView()
textView.SetBackgroundColor(tcell.ColorDarkCyan)
textView.SetText("Centered content")

// Wrap in Flex for centering
flex := tview.NewFlex().
    AddItem(nil, 0, 1, false).                    // Left spacer (proportional)
    AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
        AddItem(nil, 0, 1, false).                // Top spacer (proportional)
        AddItem(textView, 18, 0, false).          // Content (18 rows fixed)
        AddItem(nil, 0, 1, false),                // Bottom spacer (proportional)
        0, 1, false).                             // Inner flex (proportional)
    AddItem(nil, 0, 1, false)                     // Right spacer (proportional)

// Flex defaults to transparent background, so base layer shows through
a.tviewApp = tviewbridge.NewTViewApp("MyApp", flex)
```

## Transparency Rules

1. **Transparent Areas** (base layer visible):
   - `tcell.ColorDefault` background + space character
   - Flex containers without explicit background
   - Any widget with `SetBackgroundColor(tcell.ColorDefault)`

2. **Opaque Areas** (tview content shown):
   - Any color except `tcell.ColorDefault`
   - Any non-space character
   - Borders and text always overlay

## Layout Options

### Fixed Size Box

```go
box := tview.NewBox().
    SetBorder(true).
    SetTitle(" Fixed Box ")
```

### List Widget

```go
list := tview.NewList().
    AddItem("Item 1", "Description", '1', nil).
    AddItem("Item 2", "Description", '2', nil)
list.SetBackgroundColor(tcell.ColorDarkGreen)
```

### Table Widget

```go
table := tview.NewTable().
    SetBorders(true).
    SetFixed(1, 1)
table.SetBackgroundColor(tcell.ColorDarkMagenta)
```

### Form Widget

```go
form := tview.NewForm().
    AddInputField("Name", "", 20, nil, nil).
    AddButton("Submit", func() {})
form.SetBackgroundColor(tcell.ColorDarkRed)
```

## Performance Notes

1. **Background Thread** - TView runs independently, doesn't block your render loop
2. **Double Buffering** - Prevents tearing during updates
3. **Refresh Notifications** - TView notifies when redraws are needed
4. **Composite Overhead** - Minimal per-cell checks during Render()

## Common Patterns

### Pattern 1: Simple Overlay

Base layer + single tview widget (like welcome screen)

### Pattern 2: Multi-Widget Layout

Use Flex/Grid to arrange multiple widgets with different backgrounds

### Pattern 3: Hybrid Rendering

Combine custom drawing in base layer + tview widgets on top

### Pattern 4: Interactive Widgets

Forward HandleKey() to tview for forms, lists, inputs, etc.

## Troubleshooting

### Issue: Widget Not Visible

**Solution:** Check that tviewApp.Run() completed and widget has non-default background

### Issue: Flickering

**Solution:** Ensure you're using the composite pattern with base buffer

### Issue: Partial Frames

**Solution:** VirtualScreen double buffering should prevent this - check logs

### Issue: Wrong Colors

**Solution:** Verify background colors - default vs explicit colors

## Testing

TView apps work with all tree operations:
- Splits (vertical/horizontal)
- Zoom in/out
- Pane resize
- Pane swap
- Terminal resize

See `internal/runtime/server/tview_tree_operations_test.go` for examples.
