# Widget Interface

Understanding the Widget contract that all TexelUI widgets implement.

## The Core Interface

Every widget must implement these methods:

```go
type Widget interface {
    // Position
    SetPosition(x, y int)
    Position() (int, int)

    // Size
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

    // Hit Testing
    HitTest(x, y int) bool
}
```

## Method Details

### SetPosition / Position

```go
SetPosition(x, y int)  // Set widget's top-left corner
Position() (int, int)  // Get current position
```

Position is in **terminal cell coordinates** (not pixels):
- Origin (0, 0) is the top-left of the screen/container
- X increases to the right
- Y increases downward

```
(0,0)────────────────────▶ X
  │
  │    ┌─────────────┐
  │    │   Widget    │
  │    │ at (10, 5)  │
  │    └─────────────┘
  │
  ▼
  Y
```

### Resize / Size

```go
Resize(w, h int)       // Set widget's dimensions
Size() (int, int)      // Get current dimensions
```

Size is measured in terminal cells. A widget at position (10, 5) with size (20, 3) occupies:
- Columns 10-29 (20 cells wide)
- Rows 5-7 (3 cells tall)

### Draw

```go
Draw(p *Painter)
```

Render the widget to the provided Painter. The Painter handles clipping automatically.

**Example implementation:**

```go
func (b *Button) Draw(painter *core.Painter) {
    // Get effective style (may differ when focused)
    style := b.EffectiveStyle(b.Style)

    // Fill background
    painter.Fill(core.Rect{
        X: b.Rect.X,
        Y: b.Rect.Y,
        W: b.Rect.W,
        H: b.Rect.H,
    }, ' ', style)

    // Draw text centered
    text := "[ " + b.Text + " ]"
    x := b.Rect.X + (b.Rect.W - len(text)) / 2
    y := b.Rect.Y + b.Rect.H / 2
    painter.DrawText(x, y, text, style)
}
```

### Focusable / Focus / Blur

```go
Focusable() bool  // Can this widget receive focus?
Focus()           // Called when widget gains focus
Blur()            // Called when widget loses focus
```

Typical pattern:

```go
func (w *MyWidget) Focusable() bool {
    return true  // or false for non-interactive widgets
}

func (w *MyWidget) Focus() {
    w.focused = true
    w.invalidate()  // Redraw with focus style
}

func (w *MyWidget) Blur() {
    w.focused = false
    w.invalidate()  // Redraw without focus style
}
```

### HandleKey

```go
HandleKey(ev *tcell.EventKey) bool
```

Process keyboard input when the widget has focus.

**Return values:**
- `true` - Event was handled, don't propagate
- `false` - Event not handled, allow propagation

**Common patterns:**

```go
func (w *MyWidget) HandleKey(ev *tcell.EventKey) bool {
    switch ev.Key() {
    case tcell.KeyEnter:
        // Handle Enter
        return true

    case tcell.KeyRune:
        // Handle character input
        r := ev.Rune()
        // Process rune...
        return true

    case tcell.KeyLeft, tcell.KeyRight:
        // Handle arrow keys
        return true
    }

    // Not handled - let UIManager try focus traversal
    return false
}
```

### HitTest

```go
HitTest(x, y int) bool
```

Test if a point is within this widget's bounds.

Default implementation:

```go
func (b *BaseWidget) HitTest(x, y int) bool {
    return b.Rect.Contains(x, y)
}
```

Where `Contains` checks:
```go
func (r Rect) Contains(x, y int) bool {
    return x >= r.X && x < r.X+r.W &&
           y >= r.Y && y < r.Y+r.H
}
```

## BaseWidget

The `core.BaseWidget` struct provides default implementations:

```go
type BaseWidget struct {
    Rect      Rect        // Position and size
    focused   bool        // Current focus state
    focusable bool        // Can receive focus?
    zIndex    int         // Z-ordering

    focusStyleEnabled bool
    focusedStyle      tcell.Style
}
```

### Using BaseWidget

Embed it in your widget:

```go
type MyWidget struct {
    core.BaseWidget   // Embed - provides default implementations

    // Your custom fields
    Text     string
    OnClick  func()
}
```

### Overriding Methods

Override only what you need:

```go
// Keep BaseWidget's Position/Size implementations
// Override only Draw and HandleKey

func (w *MyWidget) Draw(p *core.Painter) {
    // Custom drawing
}

func (w *MyWidget) HandleKey(ev *tcell.EventKey) bool {
    // Custom key handling
    return false
}
```

### Focus Styling

BaseWidget provides automatic focus styling:

```go
// In constructor
w.SetFocusedStyle(focusStyle, true)

// In Draw
style := w.EffectiveStyle(baseStyle)
// Returns focusedStyle if focused, otherwise baseStyle
```

## Optional Interfaces

### MouseAware

Handle mouse events:

```go
type MouseAware interface {
    HandleMouse(ev *tcell.EventMouse) bool
}
```

**Example:**

```go
func (b *Button) HandleMouse(ev *tcell.EventMouse) bool {
    x, y := ev.Position()
    if !b.HitTest(x, y) {
        return false
    }

    switch ev.Buttons() {
    case tcell.Button1:
        b.pressed = true
        return true
    case tcell.ButtonNone:
        if b.pressed {
            b.pressed = false
            b.activate()
            return true
        }
    }
    return false
}
```

### InvalidationAware

Receive the dirty-region callback:

```go
type InvalidationAware interface {
    SetInvalidator(func(Rect))
}
```

**Example:**

```go
type MyWidget struct {
    core.BaseWidget
    inv func(core.Rect)
}

func (w *MyWidget) SetInvalidator(fn func(core.Rect)) {
    w.inv = fn
}

func (w *MyWidget) invalidate() {
    if w.inv != nil {
        w.inv(w.Rect)
    }
}
```

### ChildContainer

Allow traversal of child widgets:

```go
type ChildContainer interface {
    VisitChildren(func(Widget))
}
```

**Example:**

```go
func (p *Pane) VisitChildren(fn func(core.Widget)) {
    for _, child := range p.children {
        fn(child)
    }
}
```

### HitTester

Custom deep hit testing:

```go
type HitTester interface {
    WidgetAt(x, y int) Widget
}
```

**Example:**

```go
func (p *Pane) WidgetAt(x, y int) core.Widget {
    // Check children in reverse z-order (topmost first)
    for i := len(p.children) - 1; i >= 0; i-- {
        child := p.children[i]
        if child.HitTest(x, y) {
            // Check if child has deeper children
            if ht, ok := child.(core.HitTester); ok {
                if deep := ht.WidgetAt(x, y); deep != nil {
                    return deep
                }
            }
            return child
        }
    }
    // Return self if point is in bounds
    if p.HitTest(x, y) {
        return p
    }
    return nil
}
```

### Modal

Block input to other widgets:

```go
type Modal interface {
    IsModal() bool
    DismissModal()
}
```

When a widget is both focused and `IsModal()` returns true:
- It receives ALL key events (including Tab)
- Clicking outside calls `DismissModal()`

**Example (ComboBox dropdown):**

```go
func (c *ComboBox) IsModal() bool {
    return c.expanded  // Modal when dropdown is open
}

func (c *ComboBox) DismissModal() {
    c.expanded = false
    c.invalidate()
}
```

### ZIndexer

Control draw order:

```go
type ZIndexer interface {
    ZIndex() int
}
```

Higher z-index widgets are drawn on top. Default is 0.

**Example:**

```go
// Modal dialogs typically use high z-index
func (d *Dialog) ZIndex() int {
    return 100
}
```

## Best Practices

### 1. Embed BaseWidget

```go
type MyWidget struct {
    core.BaseWidget  // Always embed first
    // Custom fields...
}
```

### 2. Initialize in Constructor

```go
func NewMyWidget(x, y, w, h int) *MyWidget {
    w := &MyWidget{}
    w.SetPosition(x, y)
    w.Resize(w, h)
    w.SetFocusable(true)  // If interactive
    return w
}
```

### 3. Use Theme Colors

```go
tm := theme.Get()
fg := tm.GetSemanticColor("text.primary")
bg := tm.GetSemanticColor("bg.surface")
style := tcell.StyleDefault.Foreground(fg).Background(bg)
```

### 4. Invalidate on State Changes

```go
func (w *MyWidget) SetValue(v int) {
    w.value = v
    w.invalidate()  // Request redraw
}
```

### 5. Return true When Handling Events

```go
func (w *MyWidget) HandleKey(ev *tcell.EventKey) bool {
    if ev.Key() == tcell.KeyEnter {
        w.activate()
        return true  // We handled it
    }
    return false  // Let UIManager handle
}
```

## What's Next?

- [Focus and Events](/texelui/core-concepts/focus-and-events.md) - Event routing details
- [Rendering](/texelui/core-concepts/rendering.md) - The draw pipeline
- [Custom Widget Tutorial](/texelui/tutorials/custom-widget.md) - Build your own widget
