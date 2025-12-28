# Label

A static text display widget with alignment options.

```
Hello, World!
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewLabel(x, y, w, h int, text string) *Label
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `x` | `int` | X position (cells from left) |
| `y` | `int` | Y position (cells from top) |
| `w` | `int` | Width (0 = auto-size to text) |
| `h` | `int` | Height in cells |
| `text` | `string` | Display text |

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Text` | `string` | Display text |
| `Align` | `TextAlign` | Text alignment |
| `Style` | `tcell.Style` | Text appearance |

### Alignment Constants

```go
widgets.AlignLeft    // Left-aligned (default)
widgets.AlignCenter  // Centered
widgets.AlignRight   // Right-aligned
```

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

        // Left-aligned label (default)
        leftLabel := widgets.NewLabel(5, 3, 30, 1, "Left aligned")

        // Center-aligned label
        centerLabel := widgets.NewLabel(5, 5, 30, 1, "Centered text")
        centerLabel.Align = widgets.AlignCenter

        // Right-aligned label
        rightLabel := widgets.NewLabel(5, 7, 30, 1, "Right aligned")
        rightLabel.Align = widgets.AlignRight

        ui.AddWidget(leftLabel)
        ui.AddWidget(centerLabel)
        ui.AddWidget(rightLabel)

        // Labels are not focusable, so focus something else
        btn := widgets.NewButton(5, 10, 0, 0, "OK")
        ui.AddWidget(btn)
        ui.Focus(btn)

        return adapter.NewUIApp("Label Demo", ui), nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Behavior

### Focus

Labels are **not focusable** by default. They are display-only widgets.

### Auto-Sizing

When width is 0, the label auto-sizes to fit its text:

```go
// Width = len("Hello") = 5
label := widgets.NewLabel(0, 0, 0, 1, "Hello")
```

### Text Truncation

If text is longer than width, it's truncated:

```go
// "Hello, World!" becomes "Hello, Wor" (width 10)
label := widgets.NewLabel(0, 0, 10, 1, "Hello, World!")
```

## Alignment Examples

```
Width: 20 characters
┌────────────────────┐
│Left aligned        │  AlignLeft
│   Center aligned   │  AlignCenter
│        Right aligned│  AlignRight
└────────────────────┘
```

## Dynamic Text

Labels can be updated at runtime:

```go
statusLabel := widgets.NewLabel(5, 10, 30, 1, "Ready")

button := widgets.NewButton(5, 5, 0, 0, "Click")
button.OnClick = func() {
    statusLabel.Text = "Button clicked!"
    // Note: If the label doesn't redraw automatically,
    // you may need to trigger a refresh
}
```

## Styling

### Default Style

Uses theme semantic colors:
- Foreground: `text.primary`
- Background: transparent (inherits parent)

### Custom Style

```go
label := widgets.NewLabel(0, 0, 20, 1, "Warning!")
label.Style = tcell.StyleDefault.
    Foreground(tcell.ColorYellow).
    Bold(true)
```

### Title Style

```go
title := widgets.NewLabel(0, 0, 40, 1, "Application Title")
title.Align = widgets.AlignCenter

tm := theme.Get()
accent := tm.GetSemanticColor("accent")
title.Style = tcell.StyleDefault.Foreground(accent).Bold(true)
```

## Common Patterns

### Form Label

```go
// Label + Input pair
label := widgets.NewLabel(5, 5, 10, 1, "Name:")
input := widgets.NewInput(16, 5, 25)

ui.AddWidget(label)
ui.AddWidget(input)
```

### Status Message

```go
statusLabel := widgets.NewLabel(5, 20, 50, 1, "")

// Update on events
func showSuccess(msg string) {
    statusLabel.Text = "✓ " + msg
    statusLabel.Style = tcell.StyleDefault.Foreground(tcell.ColorGreen)
}

func showError(msg string) {
    statusLabel.Text = "✗ " + msg
    statusLabel.Style = tcell.StyleDefault.Foreground(tcell.ColorRed)
}
```

### Section Header

```go
header := widgets.NewLabel(0, 0, 60, 1, "═══ Settings ═══")
header.Align = widgets.AlignCenter
```

## Implementation Details

### Source File
`texelui/widgets/label.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)

### Draw Logic

```go
func (l *Label) Draw(painter *core.Painter) {
    if l.Text == "" {
        return
    }

    text := l.Text
    if len(text) > l.Rect.W {
        text = text[:l.Rect.W]
    }

    // Calculate x based on alignment
    var x int
    switch l.Align {
    case AlignCenter:
        x = l.Rect.X + (l.Rect.W - len(text)) / 2
    case AlignRight:
        x = l.Rect.X + l.Rect.W - len(text)
    default: // AlignLeft
        x = l.Rect.X
    }

    painter.DrawText(x, l.Rect.Y, text, l.Style)
}
```

## See Also

- [Button](/texelui/widgets/button.md) - Clickable text
- [Input](/texelui/widgets/input.md) - Editable text
- [TextArea](/texelui/widgets/textarea.md) - Multi-line text
