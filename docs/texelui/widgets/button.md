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
func NewButton(x, y, w, h int, text string) *Button
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `x` | `int` | X position (cells from left) |
| `y` | `int` | Y position (cells from top) |
| `w` | `int` | Width (0 = auto-size to text) |
| `h` | `int` | Height (0 = default 1) |
| `text` | `string` | Button label |

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

        // Create a button with auto-sizing
        btn := widgets.NewButton(10, 5, 0, 0, "Click Me!")

        // Handle clicks
        btn.OnClick = func() {
            fmt.Println("Button clicked!")
        }

        ui.AddWidget(btn)
        ui.Focus(btn)

        return adapter.NewUIApp("Button Demo", ui), nil
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
btn := widgets.NewButton(0, 0, 0, 0, "Custom")

// Override style
btn.Style = tcell.StyleDefault.
    Foreground(tcell.ColorWhite).
    Background(tcell.ColorBlue)
```

## Auto-Sizing

When width is 0, button auto-sizes to fit text:

```go
// Width = len("Click") + 4 (for "[ " and " ]")
btn := widgets.NewButton(0, 0, 0, 0, "Click")  // Width = 9
```

Formula: `width = len(text) + 4`

## Multiple Buttons

```go
ui := core.NewUIManager()

saveBtn := widgets.NewButton(10, 5, 12, 1, "Save")
saveBtn.OnClick = func() {
    // Save logic
}

cancelBtn := widgets.NewButton(25, 5, 12, 1, "Cancel")
cancelBtn.OnClick = func() {
    // Cancel logic
}

ui.AddWidget(saveBtn)
ui.AddWidget(cancelBtn)
ui.Focus(saveBtn)  // Focus first button
```

## Button Row with HBox

```go
import "texelation/texelui/layout"

ui := core.NewUIManager()
ui.SetLayout(layout.NewHBox(2))  // 2-cell spacing

btn1 := widgets.NewButton(0, 0, 12, 1, "Save")
btn2 := widgets.NewButton(0, 0, 12, 1, "Cancel")
btn3 := widgets.NewButton(0, 0, 12, 1, "Help")

ui.AddWidget(btn1)
ui.AddWidget(btn2)
ui.AddWidget(btn3)
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

- [Label](label.md) - Static text display
- [Checkbox](checkbox.md) - Toggle button
- [Input](input.md) - Text entry
