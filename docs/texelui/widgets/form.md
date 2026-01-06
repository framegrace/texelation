# Form Widget

Specialized layout widget for label/field pairs with automatic alignment and focus management.

## Overview

Form provides a structured way to create data entry forms with consistent label alignment. It manages label/field pairs, highlights labels when their associated field has focus, and handles focus cycling between fields.

```
┌────────────────────────────────────────────────────┐
│  Full Name:          [John Doe_________________]  │
│  Email:              [john@example.com_________]  │
│  Country:            [United States          ▼]   │
│                                                   │
│  Bio:                ┌─────────────────────────┐  │
│                      │ Software developer...   │  │
│                      └─────────────────────────┘  │
│                                                   │
│  [X] Subscribe to newsletter                      │
└────────────────────────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructors

```go
// Create with default configuration
func NewForm() *Form

// Create with custom configuration
func NewFormWithConfig(config FormConfig) *Form
```

## FormConfig

```go
type FormConfig struct {
    PaddingX   int // Horizontal padding (default 2)
    PaddingY   int // Vertical padding (default 1)
    LabelWidth int // Width of label column (default 22)
    RowSpacing int // Vertical spacing between rows (default 0)
}

// Get default configuration
config := widgets.DefaultFormConfig()
```

## Methods

### Adding Fields

```go
// Add a labeled field (most common)
func (f *Form) AddField(label string, field core.Widget)

// Add a full-width field (no label column)
func (f *Form) AddFullWidthField(field core.Widget, height int)

// Add a spacer row for visual separation
func (f *Form) AddSpacer(height int)

// Add a custom row with full control
func (f *Form) AddRow(row FormRow)

// Remove all rows
func (f *Form) ClearRows()
```

### FormRow Structure

```go
type FormRow struct {
    Label     *Label      // Optional label (nil for full-width)
    Field     core.Widget // The input field
    Height    int         // Row height (default 1)
    FullWidth bool        // If true, field spans full width
}
```

### Content Height

```go
// Get total height needed for all rows
func (f *Form) ContentHeight() int
```

## Example: User Registration Form

```go
package main

import (
    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

func main() {
    devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Create form
        form := widgets.NewForm()

        // Add labeled fields
        nameInput := widgets.NewInput()
        nameInput.Placeholder = "Enter your name"
        form.AddField("Full Name:", nameInput)

        emailInput := widgets.NewInput()
        emailInput.Placeholder = "user@example.com"
        form.AddField("Email:", emailInput)

        countries := []string{"USA", "Canada", "UK", "Germany"}
        countryCombo := widgets.NewComboBox(countries, true)
        form.AddField("Country:", countryCombo)

        // Add spacer
        form.AddSpacer(1)

        // Add multi-line field with custom height
        bioArea := widgets.NewTextArea()
        form.AddRow(widgets.FormRow{
            Label:  widgets.NewLabel("Bio:"),
            Field:  bioArea,
            Height: 4,
        })

        // Add full-width checkbox
        subscribeCheck := widgets.NewCheckbox("Subscribe to newsletter")
        form.AddFullWidthField(subscribeCheck, 1)

        ui.AddWidget(form)
        ui.Focus(form)

        app := adapter.NewUIApp("Registration", ui)
        app.OnResize(func(w, h int) {
            form.SetPosition(2, 2)
            form.Resize(w-4, h-4)
        })
        return app, nil
    }, nil)
}
```

## Label Highlighting

Form automatically highlights labels when their associated field has focus:
- **Focused field**: Label is bold with primary text color
- **Unfocused field**: Label is dimmed with secondary text color

This provides clear visual feedback about which field is active.

## Focus Management

Form handles Tab/Shift+Tab to cycle through focusable fields:

```go
// Form maintains focus state
form.Focus()           // Focus first (or last focused) field
form.Blur()            // Blur all fields, remember last focused
form.CycleFocus(true)  // Move to next field
form.CycleFocus(false) // Move to previous field
```

When the user Tabs past the last field or Shift+Tabs before the first field, focus escapes to the parent container.

## Custom Configuration

```go
// Create form with custom settings
config := widgets.FormConfig{
    PaddingX:   4,   // More horizontal padding
    PaddingY:   2,   // More vertical padding
    LabelWidth: 15,  // Narrower labels
    RowSpacing: 1,   // Space between rows
}
form := widgets.NewFormWithConfig(config)
```

## Comparison with VBox/HBox

| Feature | Form | VBox + HBox |
|---------|------|-------------|
| Label alignment | Automatic | Manual (AddChildWithSize) |
| Label highlighting | Automatic | Manual styling |
| Focus cycling | Built-in | Automatic via container |
| Multi-line fields | Height parameter | AddChildWithSize |
| Flexibility | Moderate | High |

**Use Form when**:
- Building traditional data entry forms
- Want automatic label alignment
- Want label-focus highlighting

**Use VBox/HBox when**:
- Need more layout flexibility
- Building non-form UIs
- Want more control over layout

## With ScrollPane

For forms with many fields that may overflow:

```go
import "texelation/texelui/scroll"

// Create scrollable form
sp := scroll.NewScrollPane()

form := widgets.NewForm()
// Add many fields...
form.AddField("Field 1:", widgets.NewInput())
form.AddField("Field 2:", widgets.NewInput())
// ... more fields

sp.SetChild(form)
sp.SetContentHeight(form.ContentHeight())

ui.AddWidget(sp)
```

## Common Patterns

### Settings Form

```go
form := widgets.NewForm()

// Theme selection
themes := []string{"Light", "Dark", "System"}
themeCombo := widgets.NewComboBox(themes, false)
form.AddField("Theme:", themeCombo)

// Font size
fontInput := widgets.NewInput()
fontInput.Text = "14"
form.AddField("Font Size:", fontInput)

form.AddSpacer(1)

// Checkboxes as full-width fields
form.AddFullWidthField(widgets.NewCheckbox("Show line numbers"), 1)
form.AddFullWidthField(widgets.NewCheckbox("Word wrap"), 1)
form.AddFullWidthField(widgets.NewCheckbox("Auto-save"), 1)
```

### Contact Form

```go
form := widgets.NewForm()

form.AddField("Name:", widgets.NewInput())
form.AddField("Email:", widgets.NewInput())
form.AddField("Subject:", widgets.NewInput())

form.AddSpacer(1)

// Message with more height
form.AddRow(widgets.FormRow{
    Label:  widgets.NewLabel("Message:"),
    Field:  widgets.NewTextArea(),
    Height: 6,
})
```

## Tips

1. **Use consistent label widths** - The default 22 chars works for most labels

2. **Group related fields** - Use AddSpacer to create visual sections

3. **Consider scrolling** - Wrap in ScrollPane for long forms

4. **Focus order** - Fields are focused in the order they were added

## See Also

- [VBox](/texelui/layout/vbox.md) - More flexible vertical layout
- [HBox](/texelui/layout/hbox.md) - Horizontal layout for form rows
- [ScrollPane](/texelui/layout/scrollpane.md) - For scrollable forms
- [Input](/texelui/widgets/input.md) - Text input field
- [ComboBox](/texelui/widgets/combobox.md) - Dropdown selection
