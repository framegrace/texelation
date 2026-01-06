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
func NewLabel(text string) *Label
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `text` | `string` | Display text |

Creates a label that auto-sizes to fit its text. Position defaults to (0,0).
Use `SetPosition(x, y)` and `Resize(w, h)` to adjust if needed.

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

        // Create a VBox for vertical layout
        vbox := widgets.NewVBox()
        vbox.Spacing = 1

        // Left-aligned label (default)
        leftLabel := widgets.NewLabel("Left aligned")
        vbox.AddChild(leftLabel)

        // Center-aligned label
        centerLabel := widgets.NewLabel("Centered text")
        centerLabel.Align = widgets.AlignCenter
        vbox.AddChild(centerLabel)

        // Right-aligned label
        rightLabel := widgets.NewLabel("Right aligned")
        rightLabel.Align = widgets.AlignRight
        vbox.AddChild(rightLabel)

        // Button (labels aren't focusable)
        btn := widgets.NewButton("OK")
        vbox.AddChild(btn)

        ui.AddWidget(vbox)
        ui.Focus(btn)

        app := adapter.NewUIApp("Label Demo", ui)
        app.OnResize(func(w, h int) {
            vbox.SetPosition(5, 3)
            vbox.Resize(30, h-6)
        })
        return app, nil
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
