# Core Concepts

Understanding the fundamental concepts behind TexelUI.

## Overview

TexelUI is built on a few core principles:

1. **Widget Tree** - UI is a tree of composable widgets
2. **Single Focus** - One widget has focus at a time
3. **Event Routing** - Events flow through the tree
4. **Dirty Rendering** - Only changed regions are redrawn
5. **Theme Integration** - Consistent styling via semantic colors

## Topics

| Concept | Description |
|---------|-------------|
| [Architecture](architecture.md) | Overall system architecture with diagrams |
| [Widget Interface](widget-interface.md) | The Widget contract all widgets implement |
| [Focus and Events](focus-and-events.md) | How events are routed and focus is managed |
| [Rendering](rendering.md) | The drawing pipeline and dirty regions |
| [Theming](theming.md) | Theme system and semantic colors |

## Quick Reference

### The Widget Tree

```
                    UIManager
                        │
           ┌────────────┼────────────┐
           │            │            │
         Pane        Border      TabLayout
           │            │            │
      ┌────┴────┐    TextArea    ┌──┴──┐
      │         │               Tab1  Tab2
    Label    Button              │      │
                               Pane   Pane
```

### Event Flow

```
  User Input (Key/Mouse)
           │
           ▼
      UIManager
           │
    ┌──────┴──────┐
    │ Is Modal?   │───Yes───▶ Modal Widget
    │             │
    └──────┬──────┘
           │ No
    ┌──────┴──────┐
    │   Tab key?  │───Yes───▶ Focus Traversal
    │             │
    └──────┬──────┘
           │ No
           ▼
    Focused Widget
           │
    ┌──────┴──────┐
    │  Handled?   │───No───▶ Bubble to Parent
    │             │
    └──────┬──────┘
           │ Yes
           ▼
       Done
```

### Render Cycle

```
  Widget State Change
           │
           ▼
    Invalidate(rect)
           │
           ▼
   Add to Dirty List
           │
           ▼
   Request Refresh
           │
           ▼
  UIManager.Render()
           │
    ┌──────┴──────┐
    │Merge Rects  │
    └──────┬──────┘
           │
    For each merged rect:
           │
    ┌──────┴──────┐
    │ Clear Area  │
    └──────┬──────┘
           │
    ┌──────┴──────┐
    │Draw Widgets │
    │(z-order)    │
    └──────┬──────┘
           │
           ▼
    Return Buffer
```

## Key Types

### Rect
```go
type Rect struct {
    X, Y int  // Position (terminal cells)
    W, H int  // Size (terminal cells)
}
```

### Widget Interface
```go
type Widget interface {
    SetPosition(x, y int)
    Position() (int, int)
    Resize(w, h int)
    Size() (int, int)
    Draw(p *Painter)
    Focusable() bool
    Focus()
    Blur()
    HandleKey(ev *tcell.EventKey) bool
    HitTest(x, y int) bool
}
```

### Painter
```go
type Painter struct {
    buf  [][]texel.Cell
    clip Rect
}

func (p *Painter) SetCell(x, y int, ch rune, style tcell.Style)
func (p *Painter) Fill(rect Rect, ch rune, style tcell.Style)
func (p *Painter) DrawText(x, y int, s string, style tcell.Style)
func (p *Painter) DrawBorder(rect Rect, style tcell.Style, charset [6]rune)
```

## Design Principles

### Composition Over Inheritance

TexelUI uses composition rather than inheritance:

```go
// Good: Embed BaseWidget
type MyWidget struct {
    core.BaseWidget  // Provides common functionality
    // ... custom fields
}

// Bad: Deep inheritance hierarchies
// (Not possible in Go anyway)
```

### Explicit Over Implicit

Widgets have explicit positions and sizes:

```go
// Explicit sizing
btn := widgets.NewButton(10, 5, 20, 1, "Click")

// Not automatic layout by default
ui.AddWidget(btn) // Uses the position you specified
```

### Interface Segregation

Optional behaviors are separate interfaces:

```go
// Only implement if needed
type MouseAware interface {
    HandleMouse(ev *tcell.EventMouse) bool
}

type Modal interface {
    IsModal() bool
    DismissModal()
}

type ZIndexer interface {
    ZIndex() int
}
```

## What's Next?

Start with [Architecture](architecture.md) for a complete overview, or jump to specific topics based on your needs.
