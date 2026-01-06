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

### Layout Containers
| Widget | Description | Source |
|--------|-------------|--------|
| [VBox](/texelui/layout/vbox.md) | Vertical stacking layout | `widgets/box.go` |
| [HBox](/texelui/layout/hbox.md) | Horizontal stacking layout | `widgets/box.go` |
| [Form](/texelui/widgets/form.md) | Label/field pairs with alignment | `widgets/form.go` |
| [TabPanel](/texelui/widgets/tabpanel.md) | High-level tabbed container | `widgets/tabpanel.go` |
| [ScrollPane](/texelui/layout/scrollpane.md) | Scrollable container | `scroll/scrollpane.go` |

### Container Widgets
| Widget | Description | Source |
|--------|-------------|--------|
| [Pane](/texelui/widgets/pane.md) | Container with background | `widgets/pane.go` |
| [Border](/texelui/widgets/border.md) | Decorative border | `widgets/border.go` |
| [TabLayout](/texelui/widgets/tablayout.md) | Low-level tabbed container | `widgets/tablayout.go` |

## Common Patterns

### Creating Widgets

Widgets use position-less constructors with minimal required parameters:

```go
label := widgets.NewLabel("Hello")
button := widgets.NewButton("Click Me")
input := widgets.NewInput()
checkbox := widgets.NewCheckbox("Remember me")
```

Position and size are handled by layout containers or explicit calls:

```go
// Using layout containers (recommended)
vbox := widgets.NewVBox()
vbox.AddChild(label)
vbox.AddChild(button)

// Or manual positioning (advanced)
button.SetPosition(10, 5)
button.Resize(20, 1)
```

### Adding to UI with Layout Containers

```go
ui := core.NewUIManager()

// Create a layout container
vbox := widgets.NewVBox()
vbox.Spacing = 1

// Add widgets to the container
label := widgets.NewLabel("Welcome!")
vbox.AddChild(label)

btn := widgets.NewButton("Click Me")
vbox.AddChild(btn)

// Add container to UI
ui.AddWidget(vbox)
ui.Focus(vbox)

// Size on resize
app := adapter.NewUIApp("Demo", ui)
app.OnResize(func(w, h int) {
    vbox.SetPosition(2, 2)
    vbox.Resize(w-4, h-4)
})
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
- TabPanel / TabLayout
- Form (manages focus of children)
- VBox / HBox (manages focus of children)

### Non-Focusable Widgets
- Label
- Pane (container only)
- Border (container only)
- ScrollPane (container only, delegates to child)

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

form := widgets.NewVBox()
form.Spacing = 1

// Create form row with HBox
row := widgets.NewHBox()
row.Spacing = 1
row.AddChildWithSize(widgets.NewLabel("Name:"), 10)
input := widgets.NewInput()
input.Placeholder = "Enter your name"
row.AddFlexChild(input)
form.AddChild(row)

// Submit button
btn := widgets.NewButton("Submit")
btn.OnClick = func() {
    fmt.Printf("Name: %s\n", input.Text)
}
form.AddChild(btn)

ui.AddWidget(form)
ui.Focus(form)

app := adapter.NewUIApp("Form", ui)
app.OnResize(func(w, h int) {
    form.SetPosition(5, 3)
    form.Resize(40, 10)
})
```

### Using Form Widget

```go
ui := core.NewUIManager()

form := widgets.NewForm()
nameInput := widgets.NewInput()
form.AddField("Name:", nameInput)

emailInput := widgets.NewInput()
form.AddField("Email:", emailInput)

form.AddFullWidthField(widgets.NewCheckbox("Subscribe"), 1)

ui.AddWidget(form)
ui.Focus(form)
```

### Tabbed Interface

```go
ui := core.NewUIManager()

tabs := widgets.NewTabPanel()

// Add tabs with content
homeContent := widgets.NewLabel("Welcome to the home tab!")
tabs.AddTab("Home", homeContent)

settingsContent := widgets.NewVBox()
settingsContent.AddChild(widgets.NewCheckbox("Enable dark mode"))
settingsContent.AddChild(widgets.NewCheckbox("Show notifications"))
tabs.AddTab("Settings", settingsContent)

tabs.OnTabChange = func(index int) {
    fmt.Printf("Switched to tab %d\n", index)
}

ui.AddWidget(tabs)
ui.Focus(tabs)
```

### Dropdown Selection

```go
options := []string{"Red", "Green", "Blue", "Yellow"}
combo := widgets.NewComboBox(options, false)
combo.SetValue("Green")

combo.OnChange = func(value string) {
    fmt.Printf("Selected: %s\n", value)
}

// Add to layout
vbox := widgets.NewVBox()
row := widgets.NewHBox()
row.AddChildWithSize(widgets.NewLabel("Color:"), 10)
row.AddFlexChild(combo)
vbox.AddChild(row)
```

## What's Next?

Click on any widget name above to see its detailed documentation, or explore:

- [Primitives](/texelui/primitives/README.md) - Building blocks for custom widgets
- [Layout](/texelui/layout/README.md) - Automatic widget positioning
- [Custom Widget Tutorial](/texelui/tutorials/custom-widget.md) - Build your own
