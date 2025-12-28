# TextArea

A multi-line text editor with scrolling, selection, and clipboard support.

```
┌────────────────────────────────┐
│ This is a multi-line          │
│ text editor with support for  │
│ scrolling, selection, and     │
│ clipboard operations.         │
└────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewTextArea(x, y, w, h int) *TextArea
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `x` | `int` | X position |
| `y` | `int` | Y position |
| `w` | `int` | Width in cells |
| `h` | `int` | Height in cells |

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Lines` | `[]string` | Text content as lines |
| `CaretX` | `int` | Caret column position |
| `CaretY` | `int` | Caret line position |
| `OffY` | `int` | Vertical scroll offset |
| `Style` | `tcell.Style` | Text appearance |

## Example

```go
package main

import (
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

        // Create bordered text area
        border := widgets.NewBorder(5, 3, 50, 12, tcell.StyleDefault)
        textarea := widgets.NewTextArea(0, 0, 48, 10)
        border.SetChild(textarea)

        ui.AddWidget(border)
        ui.Focus(textarea)

        return adapter.NewUIApp("TextArea Demo", ui), nil
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
| Characters | Insert at caret |
| Enter | Insert new line |
| Backspace | Delete before caret |
| Delete | Delete at caret |

### Navigation

| Key | Action |
|-----|--------|
| Arrow keys | Move caret |
| Home | Start of line |
| End | End of line |
| Ctrl+Home | Start of document |
| Ctrl+End | End of document |
| Page Up | Scroll up |
| Page Down | Scroll down |

### Clipboard

| Key | Action |
|-----|--------|
| Ctrl+V | Paste from clipboard |

### Mouse

| Action | Result |
|--------|--------|
| Click | Position caret |
| Wheel | Scroll content |

### Scrolling

The text area automatically scrolls to keep the caret visible. Vertical scrolling is tracked via `OffY`.

## Getting/Setting Content

```go
textarea := widgets.NewTextArea(0, 0, 40, 10)

// Set content
textarea.SetText("Hello\nWorld")

// Get content as string
text := textarea.GetText()  // "Hello\nWorld"

// Access lines directly
lines := textarea.Lines  // []string{"Hello", "World"}
```

## With Border

TextArea is commonly used with a Border widget:

```go
border := widgets.NewBorder(5, 3, 42, 12, tcell.StyleDefault)
textarea := widgets.NewTextArea(0, 0, 0, 0)  // Size managed by border
border.SetChild(textarea)

ui.AddWidget(border)
ui.Focus(textarea)
```

## Insert/Replace Mode

Like Input, TextArea supports insert and replace modes:

| Key | Action |
|-----|--------|
| Insert | Toggle insert/replace mode |

In replace mode, new characters overwrite existing ones.

## Implementation Details

### Source File
`texelui/widgets/textarea.go`
`texelui/widgets/textarea_keys.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.InvalidationAware`

### Key Features

1. **Multi-line editing** with line-by-line storage
2. **Vertical scrolling** to view long content
3. **Word wrapping** at widget boundaries (visual only)
4. **Mouse support** for caret positioning and scrolling
5. **Clipboard paste** via Ctrl+V

## See Also

- [Input](input.md) - Single-line text entry
- [Border](border.md) - Border decorator
- [Pane](pane.md) - Container widget
