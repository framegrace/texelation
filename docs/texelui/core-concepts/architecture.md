# Architecture

A deep dive into TexelUI's system architecture.

## System Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                         Application Layer                            │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                        Your App Code                           │ │
│  │                                                                │ │
│  │   app := adapter.NewUIApp("Title", ui)                         │ │
│  │   ui.AddWidget(...)                                            │ │
│  │   ui.Focus(...)                                                │ │
│  │                                                                │ │
│  └────────────────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────────────────┤
│                          TexelUI Library                             │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │   Widgets    │  │   Layout     │  │   Adapter    │              │
│  │              │  │              │  │              │              │
│  │ • Button     │  │ • VBox       │  │ • UIApp      │              │
│  │ • Input      │  │ • HBox       │  │              │              │
│  │ • TextArea   │  │ • Absolute   │  │ Bridges to   │              │
│  │ • ComboBox   │  │              │  │ texel.App    │              │
│  │ • ...        │  │              │  │              │              │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘              │
│         │                 │                 │                       │
│         └─────────────────┼─────────────────┘                       │
│                           ▼                                         │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                        Core Layer                              │ │
│  │                                                                │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │ │
│  │  │  UIManager  │  │   Painter   │  │ BaseWidget  │            │ │
│  │  │             │  │             │  │             │            │ │
│  │  │ • Widgets   │  │ • SetCell   │  │ • Position  │            │ │
│  │  │ • Focus     │  │ • Fill      │  │ • Size      │            │ │
│  │  │ • Events    │  │ • DrawText  │  │ • Focus     │            │ │
│  │  │ • Render    │  │ • DrawBorder│  │             │            │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘            │ │
│  │                                                                │ │
│  └────────────────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────────────────┤
│                         Theme Integration                            │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                     texel/theme                                │ │
│  │                                                                │ │
│  │   • Palette loading (Catppuccin)                               │ │
│  │   • Semantic color resolution                                  │ │
│  │   • Configuration management                                   │ │
│  │                                                                │ │
│  └────────────────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────────────────┤
│                         Runtime Layer                                │
│                                                                      │
│  ┌─────────────────────────┐   ┌─────────────────────────┐         │
│  │     Standalone Mode     │   │     TexelApp Mode       │         │
│  │                         │   │                         │         │
│  │  internal/devshell      │   │  Texelation Desktop     │         │
│  │                         │   │                         │         │
│  │  • tcell.Screen         │   │  • Protocol messages    │         │
│  │  • Event loop           │   │  • Pane management      │         │
│  │  • Direct rendering     │   │  • Session handling     │         │
│  │                         │   │                         │         │
│  └─────────────────────────┘   └─────────────────────────┘         │
└──────────────────────────────────────────────────────────────────────┘
```

## Package Structure

```
texelui/
├── core/                    # Core abstractions
│   ├── widget.go           # Widget interface, BaseWidget
│   ├── uimanager.go        # Root manager, focus, events
│   ├── painter.go          # Drawing primitives
│   ├── types.go            # Rect, Layout interface
│   └── layout_iface.go     # Layout interface
│
├── widgets/                 # Built-in widgets
│   ├── label.go            # Static text
│   ├── button.go           # Clickable button
│   ├── input.go            # Single-line input
│   ├── textarea.go         # Multi-line editor
│   ├── checkbox.go         # Boolean toggle
│   ├── combobox.go         # Dropdown selector
│   ├── colorpicker.go      # Color selector
│   ├── pane.go             # Container with background
│   ├── border.go           # Border decorator
│   └── tablayout.go        # Tabbed container
│
├── primitives/             # Reusable building blocks
│   ├── scrollablelist.go   # Scrolling list
│   ├── grid.go             # 2D grid
│   └── tabbar.go           # Tab navigation
│
├── layout/                 # Layout managers
│   ├── layout.go           # Absolute layout
│   ├── vbox.go             # Vertical stacking
│   └── hbox.go             # Horizontal arrangement
│
├── adapter/                # texel.App integration
│   └── texel_app.go        # UIApp adapter
│
└── color/                  # Color utilities
    ├── oklch.go            # OKLCH color space
    └── spaces.go           # Color space conversions
```

## Component Details

### UIManager

The heart of TexelUI - manages the widget tree and coordinates everything:

```go
type UIManager struct {
    mu       sync.Mutex      // Thread safety
    dirtyMu  sync.Mutex      // Dirty list protection

    W, H     int             // Current dimensions
    widgets  []Widget        // Z-ordered widget list
    focused  Widget          // Currently focused widget
    capture  Widget          // Widget capturing mouse events

    buf      [][]texel.Cell  // Render buffer
    dirty    []Rect          // Regions needing redraw
    lay      Layout          // Optional layout manager
    bgStyle  tcell.Style     // Background style

    notifier chan<- bool     // Refresh notification channel
}
```

**Key Responsibilities:**

1. **Widget Management**
   - Add/remove widgets
   - Maintain z-order (later = on top)
   - Propagate invalidation callbacks

2. **Focus Management**
   - Track focused widget
   - Handle Tab/Shift-Tab traversal
   - Support modal widgets

3. **Event Routing**
   - Route keyboard events to focused widget
   - Route mouse events based on position and z-order
   - Handle mouse capture during drags

4. **Rendering**
   - Maintain dirty region list
   - Merge overlapping regions
   - Compose widgets to buffer

### Widget Interface

The contract every widget must fulfill:

```go
type Widget interface {
    // Position management
    SetPosition(x, y int)
    Position() (int, int)

    // Size management
    Resize(w, h int)
    Size() (int, int)

    // Rendering
    Draw(p *Painter)

    // Focus
    Focusable() bool
    Focus()
    Blur()

    // Input
    HandleKey(ev *tcell.EventKey) bool

    // Hit testing
    HitTest(x, y int) bool
}
```

### BaseWidget

Provides default implementations:

```go
type BaseWidget struct {
    Rect      Rect           // Position and size
    focused   bool           // Focus state
    focusable bool           // Can receive focus?
    zIndex    int            // Z-ordering

    focusStyleEnabled bool   // Use focus style?
    focusedStyle      tcell.Style
}

// Key methods
func (b *BaseWidget) SetPosition(x, y int)
func (b *BaseWidget) Position() (int, int)
func (b *BaseWidget) Resize(w, h int)
func (b *BaseWidget) Size() (int, int)
func (b *BaseWidget) Focusable() bool
func (b *BaseWidget) Focus()
func (b *BaseWidget) Blur()
func (b *BaseWidget) IsFocused() bool
func (b *BaseWidget) HitTest(x, y int) bool
func (b *BaseWidget) EffectiveStyle(base tcell.Style) tcell.Style
```

### Painter

Clipped drawing operations:

```go
type Painter struct {
    buf  [][]texel.Cell  // Target buffer
    clip Rect            // Clipping rectangle
}

// Drawing primitives
func (p *Painter) SetCell(x, y int, ch rune, style tcell.Style)
func (p *Painter) Fill(rect Rect, ch rune, style tcell.Style)
func (p *Painter) DrawText(x, y int, s string, style tcell.Style)
func (p *Painter) DrawBorder(rect Rect, style tcell.Style, charset [6]rune)
```

All operations are automatically clipped to the painter's clip rectangle.

### Optional Interfaces

Widgets can implement these for additional functionality:

```go
// Mouse handling
type MouseAware interface {
    HandleMouse(ev *tcell.EventMouse) bool
}

// Dirty region notification
type InvalidationAware interface {
    SetInvalidator(func(Rect))
}

// Child widget iteration
type ChildContainer interface {
    VisitChildren(func(Widget))
}

// Deep hit testing
type HitTester interface {
    WidgetAt(x, y int) Widget
}

// Modal behavior
type Modal interface {
    IsModal() bool
    DismissModal()
}

// Z-order control
type ZIndexer interface {
    ZIndex() int
}
```

## Data Flow

### Adding a Widget

```
    ui.AddWidget(widget)
           │
           ▼
    Append to widgets[]
           │
           ▼
    Propagate invalidator
    (if InvalidationAware)
           │
           ▼
    Mark full screen dirty
           │
           ▼
    Request refresh
```

### Handling a Key Event

```
    HandleKey(ev)
           │
     ┌─────┴─────┐
     │  Modal?   │──Yes──▶ Forward to modal
     └─────┬─────┘              │
           │ No                 ▼
     ┌─────┴─────┐         Handled?
     │  Tab key? │──Yes──▶ Focus traversal
     └─────┬─────┘
           │ No
           ▼
    Forward to focused widget
           │
           ▼
     ┌─────┴─────┐
     │ Handled?  │──No──▶ Check for Enter (focus next)
     └─────┬─────┘        Check for Up/Down (focus prev/next)
           │ Yes
           ▼
    Mark dirty, refresh
```

### Rendering

```
    Render()
           │
           ▼
    Ensure buffer sized
           │
           ▼
    Copy dirty list, clear it
           │
     ┌─────┴─────┐
     │ No dirty? │──Yes──▶ Full frame render
     └─────┬─────┘
           │ No
           ▼
    Merge overlapping rects
           │
           ▼
    For each merged rect:
    ├── Create clipped Painter
    ├── Fill with background
    └── Draw each widget (z-order)
           │
           ▼
    Return buffer
```

## Thread Safety

UIManager uses two mutexes:

```go
mu       sync.Mutex  // Protects widgets, focus, capture, buffer
dirtyMu  sync.Mutex  // Protects dirty list and notifier
```

**Safe from any goroutine:**
- `Invalidate(rect)`
- `RequestRefresh()`

**Requires main thread:**
- `AddWidget()`
- `HandleKey()`
- `HandleMouse()`
- `Render()`

## Memory Management

### Buffer Reuse

The render buffer is reused between frames:

```go
func (u *UIManager) ensureBufferLocked() {
    // Only reallocate if size changed
    if u.buf != nil && len(u.buf) == h && len(u.buf[0]) == w {
        return
    }
    // Allocate new buffer
    u.buf = make([][]texel.Cell, h)
    // ...
}
```

### Dirty Rectangle Merging

Overlapping or adjacent rectangles are merged:

```go
func mergeRects(in []Rect) []Rect {
    // Iteratively merge until stable
    changed := true
    for changed {
        changed = false
        // Find and merge overlapping/adjacent rects
    }
    return out
}
```

This reduces the number of draw passes and improves performance.

## What's Next?

- [Widget Interface](/texelui/core-concepts/widget-interface.md) - Deep dive into widget contracts
- [Focus and Events](/texelui/core-concepts/focus-and-events.md) - Event routing details
- [Rendering](/texelui/core-concepts/rendering.md) - The draw pipeline
