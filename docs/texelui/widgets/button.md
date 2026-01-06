# Button

A clickable widget that triggers an action when activated.

```
[ Click Me! ]
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewButton(text string) *Button
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `text` | `string` | Button label |

Creates a button that auto-sizes to fit its text. Position defaults to (0,0).
Use `SetPosition(x, y)` and `Resize(w, h)` to adjust if needed.

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Text` | `string` | Button label text |
| `Style` | `tcell.Style` | Normal appearance |
| `OnClick` | `func()` | Click callback |

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

        // Create a button (auto-sizes to text)
        btn := widgets.NewButton("Click Me!")

        // Handle clicks
        btn.OnClick = func() {
            fmt.Println("Button clicked!")
        }

        ui.AddWidget(btn)
        ui.Focus(btn)

        app := adapter.NewUIApp("Button Demo", ui)
        app.OnResize(func(w, h int) {
            btn.SetPosition(10, 5)
        })
        return app, nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Behavior

### Activation

The button can be activated by:
- **Enter key** when focused
- **Space key** when focused
- **Mouse click** (left button)

### Visual States

| State | Appearance |
|-------|------------|
| Normal | `[ Text ]` with `action.primary` background |
| Focused | Same with `border.focus` background |
| Pressed | Inverted colors (fg/bg swapped) |

### Keyboard

| Key | Action |
|-----|--------|
| Enter | Activate button |
| Space | Activate button |
| Tab | Move to next widget |
| Shift+Tab | Move to previous widget |

### Mouse

| Action | Result |
|--------|--------|
| Click | Focus and activate |
| Press + Release outside | Cancel activation |

## Styling

### Default Style

Uses theme semantic colors:
- Background: `action.primary`
- Foreground: `text.inverse`
- Focus: `border.focus` background

### Custom Style

```go
btn := widgets.NewButton("Custom")

// Override style
btn.Style = tcell.StyleDefault.
    Foreground(tcell.ColorWhite).
    Background(tcell.ColorBlue)
```

## Auto-Sizing

Buttons auto-size to fit text with padding:

```go
// Width = len("Click") + 4 (for "[ " and " ]")
btn := widgets.NewButton("Click")  // Width = 9
```

Formula: `width = len(text) + 4`

## Multiple Buttons

```go
ui := core.NewUIManager()

// Create a button row with HBox
row := widgets.NewHBox()
row.Spacing = 2

saveBtn := widgets.NewButton("Save")
saveBtn.OnClick = func() {
    // Save logic
}

cancelBtn := widgets.NewButton("Cancel")
cancelBtn.OnClick = func() {
    // Cancel logic
}

row.AddChild(saveBtn)
row.AddChild(cancelBtn)

ui.AddWidget(row)
ui.Focus(row)  // Focus container (will focus first child)
```

## Button Row with HBox

```go
// Create horizontal button row
row := widgets.NewHBox()
row.Spacing = 2

row.AddChild(widgets.NewButton("Save"))
row.AddChild(widgets.NewButton("Cancel"))
row.AddChild(widgets.NewButton("Help"))

ui.AddWidget(row)
```

## Implementation Details

### Source File
`texelui/widgets/button.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.ZIndexer` (via `BaseWidget`)

### Draw Logic

```go
func (b *Button) Draw(painter *core.Painter) {
    style := b.EffectiveStyle(b.Style)

    // Invert when pressed
    if b.pressed {
        fg, bg, attr := style.Decompose()
        style = tcell.StyleDefault.Foreground(bg).Background(fg).Attributes(attr)
    }

    // Fill background
    painter.Fill(b.Rect, ' ', style)

    // Draw text: "[ Text ]"
    displayText := "[ " + b.Text + " ]"
    x := b.Rect.X + (b.Rect.W - len(displayText)) / 2
    y := b.Rect.Y + b.Rect.H / 2
    painter.DrawText(x, y, displayText, style)
}
```

## See Also

- [Label](/texelui/widgets/label.md) - Static text display
- [Checkbox](/texelui/widgets/checkbox.md) - Toggle button
- [Input](/texelui/widgets/input.md) - Text entry
