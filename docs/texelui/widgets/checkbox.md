# Checkbox

A boolean toggle widget with a label.

```
[X] Remember me
[ ] Subscribe to newsletter
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewCheckbox(label string) *Checkbox
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `label` | `string` | Text label next to checkbox |

Creates a checkbox that auto-sizes to fit `[X] label`. Position defaults to (0,0).
Use `SetPosition(x, y)` to adjust if needed.

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Label` | `string` | Text next to checkbox |
| `Checked` | `bool` | Current state |
| `Style` | `tcell.Style` | Normal appearance |
| `OnChange` | `func(bool)` | State change callback |

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

        // Create VBox for vertical layout
        vbox := widgets.NewVBox()
        vbox.Spacing = 1

        // Create checkboxes
        rememberMe := widgets.NewCheckbox("Remember me")
        rememberMe.Checked = true  // Default checked
        vbox.AddChild(rememberMe)

        newsletter := widgets.NewCheckbox("Subscribe to newsletter")
        vbox.AddChild(newsletter)

        terms := widgets.NewCheckbox("I accept the terms")
        terms.OnChange = func(checked bool) {
            fmt.Printf("Terms accepted: %v\n", checked)
        }
        vbox.AddChild(terms)

        ui.AddWidget(vbox)
        ui.Focus(vbox)

        app := adapter.NewUIApp("Checkbox Demo", ui)
        app.OnResize(func(w, h int) {
            vbox.SetPosition(10, 5)
            vbox.Resize(40, 10)
        })
        return app, nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Behavior

### Toggle

| Trigger | Action |
|---------|--------|
| Space | Toggle checked state |
| Enter | Toggle checked state |
| Mouse click | Toggle checked state |

### Visual States

| State | Appearance |
|-------|------------|
| Unchecked | `[ ] Label` |
| Checked | `[X] Label` |
| Focused | Reverse video on checkbox portion |

### Keyboard

| Key | Action |
|-----|--------|
| Space | Toggle |
| Enter | Toggle |
| Tab | Next widget |
| Shift+Tab | Previous widget |

## Styling

### Default Style

Uses theme semantic colors:
- Foreground: `text.primary`
- Focus: Reverse video on `[X]` portion

### Focus Appearance

When focused, the checkbox portion `[X]` or `[ ]` is displayed in reverse video.

## Form Example

```go
ui := core.NewUIManager()

title := widgets.NewLabel(10, 3, 30, 1, "Preferences")

darkMode := widgets.NewCheckbox(10, 5, "Enable dark mode")
notifications := widgets.NewCheckbox(10, 6, "Enable notifications")
sounds := widgets.NewCheckbox(10, 7, "Enable sounds")
sounds.Checked = true

saveBtn := widgets.NewButton(10, 10, 0, 0, "Save")
saveBtn.OnClick = func() {
    fmt.Printf("Dark: %v, Notify: %v, Sounds: %v\n",
        darkMode.Checked,
        notifications.Checked,
        sounds.Checked,
    )
}

ui.AddWidget(title)
ui.AddWidget(darkMode)
ui.AddWidget(notifications)
ui.AddWidget(sounds)
ui.AddWidget(saveBtn)
ui.Focus(darkMode)
```

## Reading State

```go
checkbox := widgets.NewCheckbox(0, 0, "Option")

// Check current state
if checkbox.Checked {
    // Is checked
}

// Set state programmatically
checkbox.Checked = true
```

## Validation Example

```go
terms := widgets.NewCheckbox(10, 10, "I accept the terms")
submitBtn := widgets.NewButton(10, 12, 0, 0, "Submit")
status := widgets.NewLabel(10, 14, 30, 1, "")

submitBtn.OnClick = func() {
    if !terms.Checked {
        status.Text = "Please accept the terms"
        return
    }
    status.Text = "Form submitted!"
}
```

## Implementation Details

### Source File
`texelui/widgets/checkbox.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.InvalidationAware`

### Draw Logic

```go
func (c *Checkbox) Draw(painter *core.Painter) {
    style := c.EffectiveStyle(c.Style)

    // Draw checkbox
    box := "[ ]"
    if c.Checked {
        box = "[X]"
    }

    // Focus highlight on checkbox portion
    if c.IsFocused() {
        fg, bg, _ := style.Decompose()
        focusStyle := tcell.StyleDefault.Foreground(bg).Background(fg)
        painter.DrawText(c.Rect.X, c.Rect.Y, box, focusStyle)
    } else {
        painter.DrawText(c.Rect.X, c.Rect.Y, box, style)
    }

    // Draw label
    painter.DrawText(c.Rect.X+4, c.Rect.Y, c.Label, style)
}
```

## See Also

- [Button](/texelui/widgets/button.md) - Action trigger
- [Input](/texelui/widgets/input.md) - Text entry
- [ComboBox](/texelui/widgets/combobox.md) - Selection from list
