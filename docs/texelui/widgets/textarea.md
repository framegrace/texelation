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
func NewTextArea() *TextArea
```

Creates a multi-line text editor. Position defaults to (0,0) and size to 20x5.
Use `SetPosition(x, y)` and `Resize(w, h)` to adjust, or place in a layout container.

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Lines` | `[]string` | Text content as lines (direct access) |
| `CaretX` | `int` | Caret column position |
| `CaretY` | `int` | Caret line position |
| `OffY` | `int` | Vertical scroll offset |
| `Style` | `tcell.Style` | Text appearance |
| `OnChange` | `func(text string)` | Called when content changes |

## Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `Text()` | `string` | Get all content as single string |
| `SetText(text string)` | - | Set content from string |
| `SetInvalidator(fn func(core.Rect))` | - | Set invalidation callback |

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

        // Create a VBox layout
        vbox := widgets.NewVBox()
        vbox.Spacing = 1

        // Add a label
        label := widgets.NewLabel("Enter your notes:")
        vbox.AddChild(label)

        // Create a textarea with height allocation
        textarea := widgets.NewTextArea()
        vbox.AddChildWithSize(textarea, 10)

        ui.AddWidget(vbox)
        ui.Focus(vbox)

        app := adapter.NewUIApp("TextArea Demo", ui)
        app.OnResize(func(w, h int) {
            vbox.SetPosition(5, 3)
            vbox.Resize(50, h-6)
        })
        return app, nil
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
textarea := widgets.NewTextArea()

// Set content using SetText (recommended - triggers OnChange)
textarea.SetText("Hello\nWorld")

// Get content as single string
text := textarea.Text()  // "Hello\nWorld"

// Direct access to lines is also available
textarea.Lines = []string{"Hello", "World"}  // Does NOT trigger OnChange
lines := textarea.Lines  // []string{"Hello", "World"}
```

## Change Callback

```go
textarea := widgets.NewTextArea()
textarea.OnChange = func(text string) {
    fmt.Printf("Content changed: %s\n", text)
}
```

The `OnChange` callback is triggered by:
- Typing characters
- Pressing Enter (new line)
- Backspace/Delete
- Paste operations
- `SetText()` method

## With Border

TextArea is commonly used with a Border widget:

```go
border := widgets.NewBorder(0, 0, 42, 12, tcell.StyleDefault)
textarea := widgets.NewTextArea()  // Size managed by border
border.SetChild(textarea)

ui.AddWidget(border)
ui.Focus(textarea)

// Position and size in OnResize
app.OnResize(func(w, h int) {
    border.SetPosition(5, 3)
})
```

## In a Form Layout

Using the Form widget for a typical form row:

```go
form := widgets.NewForm()

// TextArea needs explicit height via AddRow
form.AddRow(widgets.FormRow{
    Label:  widgets.NewLabel("Description:"),
    Field:  widgets.NewTextArea(),
    Height: 5,
})
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

- [Input](/texelui/widgets/input.md) - Single-line text entry
- [Border](/texelui/widgets/border.md) - Border decorator
- [Pane](/texelui/widgets/pane.md) - Container widget
