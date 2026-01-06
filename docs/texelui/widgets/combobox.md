# ComboBox

A dropdown selector with optional text editing and autocomplete.

```
Collapsed: [United States              ▼]

Expanded:  [United States              ▼]
           ├─────────────────────────────┤
           │ ▲ United Kingdom            │
           │   United States     ← selected
           │   Uruguay                   │
           │ ▼ Uzbekistan                │
           └─────────────────────────────┘
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewComboBox(items []string, editable bool) *ComboBox
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `items` | `[]string` | Available options |
| `editable` | `bool` | Allow custom text entry |

Creates a dropdown selector. Position defaults to (0,0) and width to 20.
Use `SetPosition(x, y)` and `Resize(w, 1)` to adjust, or place in a layout container.

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Items` | `[]string` | Available options |
| `Text` | `string` | Current value |
| `Placeholder` | `string` | Hint when empty |
| `Editable` | `bool` | Allow typing |
| `OnChange` | `func(string)` | Value change callback |

## Example

```go
package main

import (
    "fmt"
    "log"

    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

func main() {
    err := devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Main layout
        vbox := widgets.NewVBox()
        vbox.Spacing = 1

        // Editable combobox with autocomplete
        countries := []string{
            "Australia", "Austria", "Belgium", "Brazil", "Canada",
            "France", "Germany", "India", "Japan", "Mexico",
            "Spain", "United Kingdom", "United States",
        }
        countryRow := widgets.NewHBox()
        countryRow.Spacing = 1
        countryRow.AddChildWithSize(widgets.NewLabel("Country:"), 12)
        countryCombo := widgets.NewComboBox(countries, true)
        countryCombo.Placeholder = "Type to search..."
        countryCombo.OnChange = func(value string) {
            fmt.Printf("Selected: %s\n", value)
        }
        countryRow.AddFlexChild(countryCombo)
        vbox.AddChild(countryRow)

        // Non-editable combobox (dropdown only)
        priorities := []string{"Low", "Medium", "High", "Critical"}
        priorityRow := widgets.NewHBox()
        priorityRow.Spacing = 1
        priorityRow.AddChildWithSize(widgets.NewLabel("Priority:"), 12)
        priorityCombo := widgets.NewComboBox(priorities, false)
        priorityCombo.SetValue("Medium")
        priorityRow.AddFlexChild(priorityCombo)
        vbox.AddChild(priorityRow)

        ui.AddWidget(vbox)
        ui.Focus(vbox)

        app := adapter.NewUIApp("ComboBox Demo", ui)
        app.OnResize(func(w, h int) {
            vbox.SetPosition(5, 3)
            vbox.Resize(40, h-6)
        })
        return app, nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Behavior

### Editable Mode

When `editable=true`:
- User can type freely
- Items are filtered as you type
- Autocomplete suggestion shown (dimmed)
- Tab accepts the autocomplete suggestion
- Right arrow accepts one character of suggestion

### Non-Editable Mode

When `editable=false`:
- User can only select from list
- Up/Down opens the dropdown
- Typing jumps to matching items

### Keyboard

| Key | Action |
|-----|--------|
| Up/Down | Open dropdown / Navigate list |
| Enter | Select highlighted item |
| Escape | Close dropdown |
| Tab | Accept autocomplete (editable) |
| Right | Accept one char of autocomplete |
| Type | Filter list (editable) / Jump to match |

### Mouse

| Action | Result |
|--------|--------|
| Click field | Focus / Open dropdown |
| Click item | Select item |
| Click outside | Close dropdown |
| Wheel | Scroll list |

### Modal Behavior

When expanded, the ComboBox becomes modal:
- Receives all keyboard input (including Tab)
- Clicking outside dismisses the dropdown

## Autocomplete

In editable mode, the ComboBox shows inline autocomplete:

```
Typed: "Uni"
Display: [Uni|ted States_____________▼]
              ^^^^^^^^^^^^ dimmed suggestion
```

- **Tab** accepts the full suggestion
- **Right arrow** accepts one character
- Continue typing to refine

## Filtering

In editable mode, the dropdown filters as you type:

```
Items: ["Apple", "Banana", "Cherry", "Date"]
Typed: "a"
Filtered: ["Apple", "Banana", "Date"]  (contains "a")
```

## Setting/Getting Value

```go
items := []string{"Option A", "Option B", "Option C"}
combo := widgets.NewComboBox(items, false)

// Set value
combo.SetValue("Option B")

// Get current value
value := combo.Text
```

## Scroll Indicators

When the list is scrollable, indicators appear:

```
[Selected Item              ▼]
├─────────────────────────────┤
│ ▲ (more items above)        │
│   Item A                    │
│   Item B                    │
│   Item C                    │
│ ▼ (more items below)        │
└─────────────────────────────┘
```

## Implementation Details

### Source File
`texelui/widgets/combobox.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.InvalidationAware`
- `core.Modal`
- `core.ZIndexer`

### Z-Index

When expanded, the ComboBox sets a high z-index (100) to ensure the dropdown appears above other widgets.

## See Also

- [Input](/texelui/widgets/input.md) - Simple text entry
- [Checkbox](/texelui/widgets/checkbox.md) - Boolean selection
- [ColorPicker](/texelui/widgets/colorpicker.md) - Color selection
