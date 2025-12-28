# Input

A single-line text entry widget with caret, placeholder, and horizontal scrolling.

```
[Enter your name_____________]
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewInput(x, y, w int) *Input
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `x` | `int` | X position (cells from left) |
| `y` | `int` | Y position (cells from top) |
| `w` | `int` | Width in cells |

Height is always 1 (single-line).

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Text` | `string` | Current text content |
| `CaretPos` | `int` | Caret position (in runes) |
| `Placeholder` | `string` | Hint text when empty |
| `Style` | `tcell.Style` | Text appearance |
| `CaretStyle` | `tcell.Style` | Caret appearance |
| `OnChange` | `func(string)` | Text change callback |

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

        // Create label
        label := widgets.NewLabel(5, 5, 10, 1, "Name:")

        // Create input field
        input := widgets.NewInput(16, 5, 30)
        input.Placeholder = "Enter your name"

        // Handle text changes
        input.OnChange = func(text string) {
            fmt.Printf("Text: %s\n", text)
        }

        ui.AddWidget(label)
        ui.AddWidget(input)
        ui.Focus(input)

        return adapter.NewUIApp("Input Demo", ui), nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Behavior

### Text Editing

| Key | Action |
|-----|--------|
| Characters | Insert at caret position |
| Backspace | Delete character before caret |
| Delete | Delete character at caret |
| Left/Right | Move caret |
| Home | Move caret to start |
| End | Move caret to end |
| Insert | Toggle insert/replace mode |

### Insert vs Replace Mode

| Mode | Caret Style | Behavior |
|------|-------------|----------|
| Insert (default) | Reverse video block | New characters push existing text |
| Replace | Underline | New characters overwrite existing |

Toggle with **Insert** key.

### Mouse

| Action | Result |
|--------|--------|
| Click | Position caret at click location |

### Focus Appearance

When focused:
- Underline appears under the entire field
- Caret is visible
- Placeholder text is hidden

When not focused:
- No underline
- Placeholder text shown if empty

## Horizontal Scrolling

When text is longer than the input width, the view scrolls to keep the caret visible:

```
Text: "Hello, this is a very long text that doesn't fit"
Width: 20
Display: [ong text that does]  (scrolled to show caret)
           ^caret
```

Scroll state is managed via the internal `OffX` property.

## Placeholder Text

Shows dimmed hint text when the input is empty and not focused:

```go
input := widgets.NewInput(10, 5, 30)
input.Placeholder = "user@example.com"
```

## Form Example

```go
ui := core.NewUIManager()

// Username field
userLabel := widgets.NewLabel(5, 3, 10, 1, "Username:")
userInput := widgets.NewInput(16, 3, 25)
userInput.Placeholder = "johndoe"

// Email field
emailLabel := widgets.NewLabel(5, 5, 10, 1, "Email:")
emailInput := widgets.NewInput(16, 5, 25)
emailInput.Placeholder = "user@example.com"

// Add all widgets
ui.AddWidget(userLabel)
ui.AddWidget(userInput)
ui.AddWidget(emailLabel)
ui.AddWidget(emailInput)

// Set initial focus
ui.Focus(userInput)
```

## Validation Example

```go
input := widgets.NewInput(10, 5, 30)
statusLabel := widgets.NewLabel(10, 7, 30, 1, "")

input.OnChange = func(text string) {
    if len(text) < 3 {
        statusLabel.Text = "Too short (min 3 chars)"
    } else {
        statusLabel.Text = "OK"
    }
}
```

## Getting/Setting Text

```go
// Get current text
value := input.Text

// Set text programmatically
input.Text = "New value"
// Note: This doesn't trigger OnChange

// To trigger OnChange, call it manually after setting:
input.Text = "New value"
if input.OnChange != nil {
    input.OnChange(input.Text)
}
```

## Styling

### Default Style

Uses theme semantic colors:
- Background: `bg.surface`
- Foreground: `text.primary`
- Caret: `caret` color

### Custom Style

```go
input := widgets.NewInput(0, 0, 30)
input.Style = tcell.StyleDefault.
    Foreground(tcell.ColorWhite).
    Background(tcell.ColorDarkBlue)
```

## Implementation Details

### Source File
`texelui/widgets/input.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.InvalidationAware`

### Key Features

1. **Unicode Support**: Text is handled as runes, not bytes
2. **Horizontal Scroll**: Automatically scrolls to keep caret visible
3. **Insert/Replace Mode**: Toggle with Insert key
4. **Mouse Positioning**: Click to position caret
5. **Placeholder**: Shows hint when empty and not focused

## See Also

- [TextArea](/texelui/widgets/textarea.md) - Multi-line text editor
- [Label](/texelui/widgets/label.md) - Static text display
- [ComboBox](/texelui/widgets/combobox.md) - Input with dropdown
