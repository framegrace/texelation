# TexelUI Usage & Examples

TexelUI is a small widget toolkit (Label, Button, Input, Checkbox, TextArea, VBox/HBox layouts) that can run standalone or be embedded in a `texel.App` via the adapter. This page shows common patterns with minimal code.

## Standalone (no desktop)

```bash
# From the TexelUI repo:
# Widget showcase (Label/Input/Checkbox/Button + layouts)
go run ./cmd/texelui-demo
```

These open directly in your terminal; no server/client needed.

## Embed TexelUI into a texel.App

```go
import (
    "github.com/gdamore/tcell/v2"
    "github.com/framegrace/texelui/adapter"
    "github.com/framegrace/texelui/core"
    "github.com/framegrace/texelui/widgets"
)

func NewNoteApp() core.App {
    ui := core.NewUIManager()

    // Background pane
    bg := widgets.NewPane(0, 0, 0, 0, tcell.StyleDefault)
    ui.AddWidget(bg)

    // Border + TextArea
    border := widgets.NewBorder(0, 0, 0, 0, tcell.StyleDefault)
    ta := widgets.NewTextArea(0, 0, 0, 0)
    border.SetChild(ta)
    ui.AddWidget(border)
    ui.Focus(ta)

    app := adapter.NewUIApp("Notes", ui)
    app.Resize(0, 0) // real size set by pane resize
    app.OnResize(func(w, h int) {
        bg.SetPosition(0, 0); bg.Resize(w, h)
        border.SetPosition(0, 0); border.Resize(w, h)
    })
    return app
}
```

Hooking into the desktop/pipeline:
```go
pipe := cards.NewPipeline(nil, cards.WrapApp(NewNoteApp()))
return pipe // satisfies core.App
```

## Using VBox/HBox layouts

UIManager defaults to absolute positioning. You can set a layout to place children automatically:

```go
import "github.com/framegrace/texelui/layout"

ui := core.NewUIManager()
ui.SetLayout(&layout.VBox{Spacing: 1})

title := widgets.NewLabel(0, 0, 0, 1, "Login")
user := widgets.NewInput(0, 0, 20); user.Placeholder = "username"
pass := widgets.NewInput(0, 0, 20); pass.Placeholder = "password"
pass.SetPasswordMode(true) // if added in your widget impl
submit := widgets.NewButton(0, 0, 0, 1, "Sign in")

ui.AddWidget(title)
ui.AddWidget(user)
ui.AddWidget(pass)
ui.AddWidget(submit)
```

When you `Resize(w,h)`, UIManager will stack the children top-to-bottom with spacing.

## Focus & invalidation

- `ui.Focus(widget)` sets focus; Tab/Shift+Tab traverse focusable widgets.
- Widgets implement `Invalidate(rect)` via the injected invalidator; most built-ins already mark themselves dirty on change.
- If you mutate widget state manually, call `ui.Invalidate(core.Rect{...})` or set a notifier and send a refresh when needed.

## Theming

TexelUI uses semantic colours (`bg.surface`, `text.primary`, `action.primary`, etc.). See the TexelUI theming docs in `github.com/framegrace/texelui` for the keys. Defaults are populated automatically by `github.com/framegrace/texelui/theme`.

## Tips

- Keep layouts simple: use VBox/HBox for forms; fall back to absolute positioning for bespoke UIs.
- For overlays inside a card pipeline, reuse the adapter app and layer it in a card (e.g., long-line editor overlay).
- Add tests mirroring `texelui/widgets/widgets_test.go` to cover focus, draw, and input handling.***
