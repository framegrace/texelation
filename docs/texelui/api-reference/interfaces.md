# Interfaces Reference

Complete reference for all TexelUI interfaces.

## Core Interfaces

### Widget

The fundamental interface all widgets must implement.

```go
type Widget interface {
    // Position returns the widget's top-left corner
    Position() Point

    // SetPosition moves the widget to (x, y)
    SetPosition(x, y int)

    // Size returns the widget's dimensions
    Size() Size

    // Resize changes the widget's dimensions
    Resize(w, h int)

    // Draw renders the widget using the painter
    Draw(p *Painter)

    // HandleKey processes keyboard input
    // Returns true if the event was consumed
    HandleKey(ev *tcell.EventKey) bool

    // IsFocusable returns whether the widget can receive focus
    IsFocusable() bool
}
```

**Implemented by:** All widgets

**Notes:**
- Use `BaseWidget` for default implementations
- `Draw` receives a clipped `Painter`
- `HandleKey` is only called when focused

---

### Layout

Interface for automatic widget positioning.

```go
type Layout interface {
    // Apply positions all children within the container bounds
    Apply(container Rect, children []Widget)
}
```

**Implementations:**
- `layout.VBox` - Vertical stacking
- `layout.HBox` - Horizontal arrangement

**Usage:**
```go
ui := core.NewUIManager()
ui.SetLayout(layout.NewVBox(1))
```

---

## Optional Widget Interfaces

These interfaces extend widget functionality. Implement only what you need.

### MouseAware

Handle mouse events.

```go
type MouseAware interface {
    // HandleMouse processes mouse input
    // Returns true if the event was consumed
    HandleMouse(ev *tcell.EventMouse) bool
}
```

**Example:**
```go
func (w *MyWidget) HandleMouse(ev *tcell.EventMouse) bool {
    x, y := ev.Position()
    if !w.Rect().Contains(x, y) {
        return false
    }

    switch ev.Buttons() {
    case tcell.Button1:
        w.onClick(x, y)
        return true
    }
    return false
}
```

---

### InvalidationAware

Receive notifications about dirty regions.

```go
type InvalidationAware interface {
    // MarkDirty marks the widget as needing redraw
    MarkDirty()

    // IsDirty returns whether the widget needs redraw
    IsDirty() bool

    // ClearDirty resets the dirty flag after rendering
    ClearDirty()
}
```

**Notes:**
- `BaseWidget` provides a default implementation
- UIManager calls `MarkDirty()` on focus changes
- Custom widgets should call `MarkDirty()` when internal state changes

---

### Modal

Indicate the widget should capture all input.

```go
type Modal interface {
    // IsModal returns true if this widget is modal
    IsModal() bool
}
```

**Usage:**
When `IsModal()` returns true:
- All keyboard events go to this widget
- Mouse events outside the widget are blocked or dismissed
- Focus cannot leave the widget via Tab

**Example:**
```go
type Dialog struct {
    core.BaseWidget
    isOpen bool
}

func (d *Dialog) IsModal() bool {
    return d.isOpen
}
```

---

### ZIndexer

Control widget layering order.

```go
type ZIndexer interface {
    // ZIndex returns the widget's z-order
    // Higher values render on top
    ZIndex() int
}
```

**Default:** Widgets without ZIndexer are rendered in add order.

**Example:**
```go
const (
    ZBackground = 0
    ZContent    = 10
    ZOverlay    = 100
    ZModal      = 1000
)

func (d *Dialog) ZIndex() int {
    return ZModal
}
```

---

### ChildContainer

Indicate the widget contains other widgets.

```go
type ChildContainer interface {
    // Children returns the widget's child widgets
    Children() []Widget

    // AddChild adds a child widget
    AddChild(w Widget)

    // RemoveChild removes a child widget
    RemoveChild(w Widget)
}
```

**Implemented by:** `Pane`, `Border`, `TabLayout`

**Notes:**
- Children are drawn relative to parent
- Parent handles event routing to children
- Layout can be set on container widgets

---

### FocusContainer

Manage focus within a container.

```go
type FocusContainer interface {
    // FocusedChild returns the currently focused child
    FocusedChild() Widget

    // SetFocusedChild sets focus to a specific child
    SetFocusedChild(w Widget)

    // FocusNextChild moves focus to next focusable child
    FocusNextChild()

    // FocusPrevChild moves focus to previous focusable child
    FocusPrevChild()
}
```

**Usage:** For containers that manage their own focus cycle.

---

## Texelation Integration Interfaces

### texel.App

The interface for Texelation applications.

```go
type App interface {
    // Run starts the application
    Run() error

    // Stop cleanly shuts down the application
    Stop()

    // Resize notifies the app of new dimensions
    Resize(cols, rows int)

    // Render returns the current cell buffer
    Render() [][]Cell

    // HandleKey processes keyboard input
    HandleKey(ev *tcell.EventKey)
}
```

**Implementation:** `adapter.UIApp` implements this for TexelUI applications.

---

### ThemeSetter

Receive theme updates from Texelation.

```go
type ThemeSetter interface {
    // SetTheme updates the widget/app's theme
    SetTheme(theme *theme.Theme)
}
```

**Example:**
```go
func (a *MyApp) SetTheme(th *theme.Theme) {
    a.theme = th
    a.updateStyles()
}
```

---

### StorageSetter

Access persistent storage.

```go
type StorageSetter interface {
    // SetStorage provides access to persistent storage
    SetStorage(storage Storage)
}

type Storage interface {
    Get(key string) (string, error)
    Set(key string, value string) error
    Delete(key string) error
}
```

**Example:**
```go
func (a *MyApp) SetStorage(s Storage) {
    a.storage = s

    // Load saved state
    if val, err := s.Get("last_value"); err == nil {
        a.input.SetText(val)
    }
}
```

---

### PaneIDSetter

Know the containing pane's ID.

```go
type PaneIDSetter interface {
    // SetPaneID provides the pane ID
    SetPaneID(id int)
}
```

---

### RefreshNotifierSetter

Request UI refreshes.

```go
type RefreshNotifierSetter interface {
    // SetRefreshNotifier provides a channel to request refreshes
    SetRefreshNotifier(ch chan<- bool)
}
```

**Example:**
```go
func (a *MyApp) SetRefreshNotifier(ch chan<- bool) {
    a.refresh = ch
}

func (a *MyApp) onDataUpdate() {
    // Trigger redraw
    a.refresh <- true
}
```

---

### ControlBusProvider

Provide access to control bus for inter-component communication.

```go
type ControlBusProvider interface {
    // ControlBus returns the app's control bus
    ControlBus() *ControlBus
}
```

---

### PipelineProvider

Provide access to render pipeline (for card-based UIs).

```go
type PipelineProvider interface {
    // Pipeline returns the app's render pipeline
    Pipeline() RenderPipeline
}
```

---

## Renderer Interfaces

### ListItemRenderer

Custom rendering for ScrollableList items.

```go
type ListItemRenderer interface {
    // RenderItem renders a single list item
    RenderItem(
        painter *Painter,
        item interface{},
        rect Rect,
        selected bool,
        focused bool,
    )
}
```

**Example:**
```go
type ColorItemRenderer struct{}

func (r *ColorItemRenderer) RenderItem(p *Painter, item interface{}, rect Rect, selected, focused bool) {
    color := item.(Color)

    // Draw color swatch
    swatchStyle := tcell.StyleDefault.Background(color.Value)
    p.Fill(Rect{rect.X, rect.Y, 2, 1}, ' ', swatchStyle)

    // Draw label
    labelStyle := tcell.StyleDefault
    if selected && focused {
        labelStyle = labelStyle.Reverse(true)
    }
    p.DrawText(rect.X+3, rect.Y, color.Name, labelStyle)
}
```

---

### GridCellRenderer

Custom rendering for Grid cells.

```go
type GridCellRenderer interface {
    // RenderCell renders a single grid cell
    RenderCell(
        painter *Painter,
        item interface{},
        rect Rect,
        selected bool,
        focused bool,
    )
}
```

**Example:**
```go
type ColorCellRenderer struct{}

func (r *ColorCellRenderer) RenderCell(p *Painter, item interface{}, rect Rect, selected, focused bool) {
    color := item.(tcell.Color)

    // Fill with color
    style := tcell.StyleDefault.Background(color)
    p.Fill(rect, ' ', style)

    // Selection indicator
    if selected {
        borderStyle := tcell.StyleDefault.
            Foreground(tcell.ColorWhite).
            Background(color)
        p.SetCell(rect.X, rect.Y, '[', borderStyle)
        p.SetCell(rect.X+rect.W-1, rect.Y, ']', borderStyle)
    }
}
```

---

## Interface Composition

Combine interfaces for rich widgets:

```go
type MyWidget struct {
    core.BaseWidget  // Provides Widget basics

    children []Widget
    focused  Widget
    zIndex   int
}

// Implement Widget (via BaseWidget + Draw/HandleKey)
func (w *MyWidget) Draw(p *Painter) { /* ... */ }
func (w *MyWidget) HandleKey(ev *tcell.EventKey) bool { /* ... */ }

// Implement MouseAware
func (w *MyWidget) HandleMouse(ev *tcell.EventMouse) bool { /* ... */ }

// Implement ChildContainer
func (w *MyWidget) Children() []Widget { return w.children }
func (w *MyWidget) AddChild(c Widget) { w.children = append(w.children, c) }
func (w *MyWidget) RemoveChild(c Widget) { /* ... */ }

// Implement ZIndexer
func (w *MyWidget) ZIndex() int { return w.zIndex }

// Implement Modal
func (w *MyWidget) IsModal() bool { return w.isOpen }
```

## See Also

- [API Reference](/texelui/api-reference/README.md) - Full API
- [Widget Interface](/texelui/core-concepts/widget-interface.md) - Deep dive
- [Custom Widget Tutorial](/texelui/tutorials/custom-widget.md) - Building widgets
