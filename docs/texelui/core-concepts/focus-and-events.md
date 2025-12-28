# Focus and Events

How TexelUI manages focus and routes events.

## Focus Management

### Single Focus Model

Only one widget can have focus at a time:

```
┌─────────────────────────────────────┐
│                                     │
│   [ Username ]  <-- Not focused     │
│                                     │
│   [ Password ]  <-- FOCUSED         │
│                                     │
│   [ Login ]     <-- Not focused     │
│                                     │
└─────────────────────────────────────┘
```

### Focus Requirements

A widget can receive focus if:

1. `Focusable()` returns `true`
2. The widget is in the UIManager's widget tree

```go
btn := widgets.NewButton(0, 0, 0, 0, "Click")
btn.SetFocusable(true)  // Default for buttons

label := widgets.NewLabel(0, 0, 10, 1, "Text")
label.SetFocusable(false)  // Default for labels
```

### Programmatic Focus

```go
ui.Focus(widget)
```

This:
1. Calls `Blur()` on the previously focused widget
2. Sets the new focused widget
3. Calls `Focus()` on the new widget
4. Marks the screen dirty for redraw

### Focus Traversal

**Tab Order:**
Widgets are traversed in the order they were added, depth-first:

```go
ui.AddWidget(pane)      // Contains: input1, input2
ui.AddWidget(button)

// Tab order: input1 -> input2 -> button -> input1 ...
```

**Keyboard Navigation:**

| Key | Action |
|-----|--------|
| Tab | Focus next widget |
| Shift+Tab | Focus previous widget |
| Up | Focus previous (if widget doesn't handle) |
| Down | Focus next (if widget doesn't handle) |
| Enter | Focus next (if widget doesn't handle) |

### Focus Styling

Widgets can have different styles when focused:

```go
// In widget constructor
w.SetFocusedStyle(focusStyle, true)

// In Draw method
style := w.EffectiveStyle(defaultStyle)
// Returns focusStyle if focused, defaultStyle otherwise
```

Common focus indicators:
- Reverse video (swap fg/bg)
- Underline
- Border color change
- Cursor display

## Keyboard Events

### Event Flow

```
  User presses key
         │
         ▼
  UIManager.HandleKey(ev)
         │
    ┌────┴────┐
    │  Modal? │──Yes──▶ Forward to modal widget
    │         │              │
    └────┬────┘              ▼
         │ No           Handled? ──▶ Done
    ┌────┴────┐
    │  Tab?   │──Yes──▶ Focus traversal
    │         │              │
    └────┬────┘              ▼
         │ No           Invalidate, refresh
         ▼
  Forward to focused widget
         │
         ▼
    ┌────┴────┐
    │ Handled?│──No──▶ Check Enter/Up/Down
    │         │           for focus traversal
    └────┬────┘
         │ Yes
         ▼
  Invalidate, refresh
```

### Event Structure

```go
type EventKey struct {
    // Key type (KeyRune for characters, KeyEnter, KeyTab, etc.)
    key     Key
    // Character if key == KeyRune
    rune    rune
    // Modifier keys (ModShift, ModCtrl, ModAlt)
    mod     ModMask
}
```

### Common Key Patterns

**Character Input:**
```go
if ev.Key() == tcell.KeyRune {
    char := ev.Rune()
    // Handle character
}
```

**Special Keys:**
```go
switch ev.Key() {
case tcell.KeyEnter:
    // Handle Enter
case tcell.KeyBackspace, tcell.KeyBackspace2:
    // Handle Backspace
case tcell.KeyDelete:
    // Handle Delete
case tcell.KeyLeft, tcell.KeyRight, tcell.KeyUp, tcell.KeyDown:
    // Handle arrows
case tcell.KeyHome, tcell.KeyEnd:
    // Handle Home/End
case tcell.KeyTab:
    // Tab is handled by UIManager for focus traversal
    // unless widget is modal
}
```

**Modifiers:**
```go
if ev.Modifiers()&tcell.ModShift != 0 {
    // Shift is held
}
if ev.Modifiers()&tcell.ModCtrl != 0 {
    // Ctrl is held
}
if ev.Modifiers()&tcell.ModAlt != 0 {
    // Alt is held
}
```

## Mouse Events

### Event Flow

```
  User clicks/moves mouse
         │
         ▼
  UIManager.HandleMouse(ev)
         │
    ┌────┴────┐
    │ Modal + │
    │ Click   │──Yes──▶ Click outside? ──Yes──▶ DismissModal()
    │ outside?│              │
    └────┬────┘              │ No
         │                   ▼
    ┌────┴────┐         Forward to modal
    │ Button  │
    │ press?  │──Yes──▶ Find topmost widget at position
    │         │              │
    └────┬────┘              ▼
         │             Set focus and capture
         │                   │
    ┌────┴────┐              ▼
    │Captured?│──Yes──▶ Forward to captured widget
    │         │              │
    └────┬────┘              ▼
         │ No           Button released? ──Yes──▶ Clear capture
         │
    ┌────┴────┐
    │ Wheel?  │──Yes──▶ Forward to topmost at position
    │         │
    └────┬────┘
         │ No
         ▼
  Forward to widget at position (hover)
```

### Mouse Capture

When a button is pressed over a widget, that widget "captures" the mouse:
- All subsequent mouse events go to that widget
- Even if the mouse moves outside the widget
- Until the button is released

This enables:
- Drag operations
- Slider manipulation
- Button press/release detection

```go
func (b *Button) HandleMouse(ev *tcell.EventMouse) bool {
    x, y := ev.Position()

    switch ev.Buttons() {
    case tcell.Button1:
        // Mouse down - we have capture now
        b.pressed = true
        return true

    case tcell.ButtonNone:
        // Mouse up - capture will be released
        if b.pressed {
            b.pressed = false
            // Only activate if still over button
            if b.HitTest(x, y) {
                b.activate()
            }
        }
        return true
    }

    return false
}
```

### Mouse Event Types

```go
type EventMouse struct {
    buttons Buttons  // Which buttons are pressed
    x, y    int      // Position in terminal cells
    mod     ModMask  // Modifier keys
}
```

**Button Constants:**
```go
tcell.Button1      // Left button
tcell.Button2      // Middle button
tcell.Button3      // Right button
tcell.ButtonNone   // No button (release or move)
tcell.WheelUp      // Scroll wheel up
tcell.WheelDown    // Scroll wheel down
tcell.WheelLeft    // Scroll wheel left
tcell.WheelRight   // Scroll wheel right
```

### Z-Order Hit Testing

Mouse events go to the topmost widget at the click position:

```go
func (u *UIManager) topmostAtLocked(x, y int) Widget {
    // Get widgets sorted by z-index
    sorted := u.sortedWidgetsLocked()

    // Check in reverse order (highest z first)
    for i := len(sorted) - 1; i >= 0; i-- {
        if w := deepHit(sorted[i], x, y); w != nil {
            return w
        }
    }
    return nil
}
```

## Modal Widgets

Modal widgets take exclusive input control.

### Modal Behavior

When a widget is focused and `IsModal()` returns true:

1. **All keys go to it** - Including Tab, which normally does focus traversal
2. **Click outside dismisses** - Calls `DismissModal()`

### Implementing Modal

```go
type Dropdown struct {
    core.BaseWidget
    expanded bool
    // ...
}

func (d *Dropdown) IsModal() bool {
    return d.expanded
}

func (d *Dropdown) DismissModal() {
    d.expanded = false
    d.invalidate()
}

func (d *Dropdown) HandleKey(ev *tcell.EventKey) bool {
    if d.expanded {
        switch ev.Key() {
        case tcell.KeyEscape:
            d.DismissModal()
            return true
        case tcell.KeyEnter:
            d.selectCurrent()
            d.DismissModal()
            return true
        case tcell.KeyUp, tcell.KeyDown:
            // Navigate list
            return true
        case tcell.KeyTab:
            // Modal, so we receive Tab
            // Could use for autocomplete
            return true
        }
    }
    return false
}
```

### Modal Use Cases

- Dropdown lists (ComboBox)
- Color pickers (expanded state)
- Dialog boxes
- Context menus
- Autocomplete popups

## Event Handling Best Practices

### 1. Return true When Handled

```go
func (w *MyWidget) HandleKey(ev *tcell.EventKey) bool {
    if ev.Key() == tcell.KeyEnter {
        w.doSomething()
        return true  // We handled it
    }
    return false  // Pass to parent/focus traversal
}
```

### 2. Invalidate After State Changes

```go
func (w *MyWidget) HandleKey(ev *tcell.EventKey) bool {
    if ev.Key() == tcell.KeyRune {
        w.text += string(ev.Rune())
        w.invalidate()  // Mark for redraw
        return true
    }
    return false
}
```

### 3. Check Hit Test for Mouse

```go
func (w *MyWidget) HandleMouse(ev *tcell.EventMouse) bool {
    x, y := ev.Position()
    if !w.HitTest(x, y) {
        return false  // Not over this widget
    }
    // Handle mouse...
    return true
}
```

### 4. Handle Both Press and Release

```go
func (b *Button) HandleMouse(ev *tcell.EventMouse) bool {
    switch ev.Buttons() {
    case tcell.Button1:
        b.pressed = true
        return true
    case tcell.ButtonNone:
        wasPressed := b.pressed
        b.pressed = false
        if wasPressed {
            b.activate()
        }
        return wasPressed
    }
    return false
}
```

## What's Next?

- [Rendering](rendering.md) - The draw pipeline
- [Theming](theming.md) - Styling with semantic colors
- [Widget Interface](widget-interface.md) - Widget contracts
