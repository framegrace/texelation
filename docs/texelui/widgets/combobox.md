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
func NewComboBox(x, y, w int, items []string, editable bool) *ComboBox
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `x` | `int` | X position |
| `y` | `int` | Y position |
| `w` | `int` | Width in cells |
| `items` | `[]string` | Available options |
| `editable` | `bool` | Allow custom text entry |

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

    "github.com/gdamore/tcell/v2"
    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

func main() {
    err := devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Editable combobox with autocomplete
        countries := []string{
            "Australia", "Austria", "Belgium", "Brazil", "Canada",
            "France", "Germany", "India", "Japan", "Mexico",
            "Spain", "United Kingdom", "United States",
        }
        countryCombo := widgets.NewComboBox(10, 5, 30, countries, true)
        countryCombo.Placeholder = "Type to search..."
        countryCombo.OnChange = func(value string) {
            fmt.Printf("Selected: %s\n", value)
        }

        // Non-editable combobox (dropdown only)
        priorities := []string{"Low", "Medium", "High", "Critical"}
        priorityCombo := widgets.NewComboBox(10, 8, 20, priorities, false)
        priorityCombo.SetValue("Medium")

        ui.AddWidget(countryCombo)
        ui.AddWidget(priorityCombo)
        ui.Focus(countryCombo)

        return adapter.NewUIApp("ComboBox Demo", ui), nil
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
combo := widgets.NewComboBox(0, 0, 20, items, false)

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

- [Input](input.md) - Simple text entry
- [Checkbox](checkbox.md) - Boolean selection
- [ColorPicker](colorpicker.md) - Color selection
