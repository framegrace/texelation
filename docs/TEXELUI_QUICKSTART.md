# TexelUI Quickstart

This guide shows how to run the demo TextArea app and embed TexelUI into a TexelApp.

## Run the demo

The repo includes a demo command that launches a bordered, full‑window TextArea with focus and mouse support.

```
go run ./cmd/texelui-demo
```

Controls:
- Typing edits text; Enter inserts newlines
- Arrow keys move the caret; Home/End to line boundaries
- Shift + arrows selects text
- Ctrl+C / Ctrl+X / Ctrl+V use a local clipboard
- Mouse click sets caret; drag selects; wheel scrolls
- Tab / Shift+Tab cycles focus between focusable widgets (bordered area and editor)
- Ctrl+C (terminal) exits the demo

## Embedding TexelUI in a TexelApp

The adapter package wraps a `UIManager` as a `texel.App`:

```go
import (
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

ui := core.NewUIManager()
pane := widgets.NewPane(0,0,0,0, tcell.StyleDefault)
ui.AddWidget(pane)
border := widgets.NewBorder(0,0,0,0, tcell.StyleDefault)
ta := widgets.NewTextArea(0,0,0,0)
border.SetChild(ta)
ui.AddWidget(border)
ui.Focus(ta)

app := adapter.NewUIApp("My Editor", ui)
app.Resize(cols, rows) // usually from pane resize
```

The adapter forwards `Resize`, `Render`, `HandleKey`, and `HandleMouse` to the UI.

## Architecture at a glance

- `texelui/core` – Widget and UIManager primitives, painter, dirty‑region redraw, layout interface
- `texelui/widgets` – Pane, Border (decorator), TextArea (multiline editor)
- `texelui/adapter` – Wraps a `UIManager` as a `texel.App` for use in any pane

## Notes and roadmap

- Rendering uses a framebuffer with dirty‑region redraw; widgets can be extended to invalidate subregions.
- Current layout is absolute; additional managers can be added via the `Layout` interface.
- Focus traversal and click‑to‑focus are implemented; cursor blink and IME support are planned.

