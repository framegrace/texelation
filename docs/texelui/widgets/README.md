# Widgets Reference

Complete reference for all TexelUI widgets.

## Widget Categories

### Input Widgets
| Widget | Description | Source |
|--------|-------------|--------|
| [Input](/texelui/widgets/input.md) | Single-line text entry | `widgets/input.go` |
| [TextArea](/texelui/widgets/textarea.md) | Multi-line text editor | `widgets/textarea.go` |
| [Checkbox](/texelui/widgets/checkbox.md) | Boolean toggle | `widgets/checkbox.go` |
| [ComboBox](/texelui/widgets/combobox.md) | Dropdown with autocomplete | `widgets/combobox.go` |
| [ColorPicker](/texelui/widgets/colorpicker.md) | Color selection | `widgets/colorpicker.go` |

### Display Widgets
| Widget | Description | Source |
|--------|-------------|--------|
| [Label](/texelui/widgets/label.md) | Static text display | `widgets/label.go` |
| [Button](/texelui/widgets/button.md) | Clickable action trigger | `widgets/button.go` |

### Container Widgets
| Widget | Description | Source |
|--------|-------------|--------|
| [Pane](/texelui/widgets/pane.md) | Container with background | `widgets/pane.go` |
| [Border](/texelui/widgets/border.md) | Decorative border | `widgets/border.go` |
| [TabLayout](/texelui/widgets/tablayout.md) | Tabbed container | `widgets/tablayout.go` |

## Common Patterns

### Creating Widgets

All widgets follow the same constructor pattern:

```go
widget := widgets.NewWidgetName(x, y, w, h, ...params)
```

Parameters:
- `x, y` - Position in terminal cells
- `w, h` - Size in terminal cells (some widgets auto-size if 0)
- Additional widget-specific parameters

### Adding to UI

```go
ui := core.NewUIManager()
btn := widgets.NewButton(10, 5, 0, 0, "Click Me")
ui.AddWidget(btn)
ui.Focus(btn)  // Set initial focus
```

### Handling Events

```go
// Button click
btn.OnClick = func() {
    // Handle click
}

// Input change
input.OnChange = func(text string) {
    // Handle text change
}

// Checkbox change
check.OnChange = func(checked bool) {
    // Handle toggle
}
```

## Widget Base Properties

All widgets share these via `BaseWidget`:

| Property | Type | Description |
|----------|------|-------------|
| `Rect` | `core.Rect` | Position and size |
| `focused` | `bool` | Has focus? |
| `focusable` | `bool` | Can receive focus? |
| `zIndex` | `int` | Draw order |

### Methods (from BaseWidget)

```go
SetPosition(x, y int)      // Set position
Position() (int, int)      // Get position
Resize(w, h int)           // Set size
Size() (int, int)          // Get size
SetFocusable(bool)         // Enable/disable focus
IsFocused() bool           // Check focus state
SetZIndex(int)             // Set draw order
ZIndex() int               // Get draw order
SetFocusedStyle(style, enabled)  // Set focus appearance
EffectiveStyle(base) Style // Get current style
```

## Focus Behavior

### Focusable Widgets
- Button
- Input
- TextArea
- Checkbox
- ComboBox
- ColorPicker
- TabLayout

### Non-Focusable Widgets
- Label
- Pane (container only)
- Border (container only)

### Focus Traversal
- **Tab** - Next focusable widget
- **Shift+Tab** - Previous focusable widget
- **Click** - Focus clicked widget

## Theme Integration

All widgets use semantic theme colors:

```go
tm := theme.Get()

// Common patterns
fg := tm.GetSemanticColor("text.primary")
bg := tm.GetSemanticColor("bg.surface")
accent := tm.GetSemanticColor("action.primary")
```

See [Theming](/texelui/core-concepts/theming.md) for details.

## Quick Examples

### Simple Form

```go
ui := core.NewUIManager()

label := widgets.NewLabel(5, 3, 10, 1, "Name:")
input := widgets.NewInput(16, 3, 30)
input.Placeholder = "Enter your name"

btn := widgets.NewButton(16, 5, 12, 1, "Submit")
btn.OnClick = func() {
    fmt.Printf("Name: %s\n", input.Text)
}

ui.AddWidget(label)
ui.AddWidget(input)
ui.AddWidget(btn)
ui.Focus(input)
```

### Tabbed Interface

```go
tabs := []primitives.TabItem{
    {Label: "Home", ID: "home"},
    {Label: "Settings", ID: "settings"},
}
tabLayout := widgets.NewTabLayout(0, 0, 80, 24, tabs)

homePane := widgets.NewPane(0, 0, 80, 22, tcell.StyleDefault)
settingsPane := widgets.NewPane(0, 0, 80, 22, tcell.StyleDefault)

tabLayout.SetTabContent(0, homePane)
tabLayout.SetTabContent(1, settingsPane)

ui.AddWidget(tabLayout)
ui.Focus(tabLayout)
```

### Dropdown Selection

```go
options := []string{"Red", "Green", "Blue", "Yellow"}
combo := widgets.NewComboBox(10, 5, 20, options, false)
combo.SetValue("Green")

combo.OnChange = func(value string) {
    fmt.Printf("Selected: %s\n", value)
}
```

## What's Next?

Click on any widget name above to see its detailed documentation, or explore:

- [Primitives](/texelui/primitives/README.md) - Building blocks for custom widgets
- [Layout](/texelui/layout/README.md) - Automatic widget positioning
- [Custom Widget Tutorial](/texelui/tutorials/custom-widget.md) - Build your own
