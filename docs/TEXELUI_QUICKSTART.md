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
- Insert toggles Replace mode: caret becomes underlined and typing overwrites
- Wrapping: text wraps at the widget width and reflows on resize
- Mouse click sets caret; wheel scrolls
- Tab / Shift+Tab cycles focus between focusable widgets
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

### Using TexelUI inside a TexelApp card

TexelUI apps are regular `texel.App`s; you can wrap them as cards and plug into the pipeline:

```go
import (
    "texelation/texel/cards"
    "texelation/texelui/adapter"
)

// Build a UI-backed app (single TextArea)
uiApp := adapter.NewTextEditorApp("Editor")

// Wrap into a card and append to your pipeline
pipe := cards.NewPipeline(nil /*control bus*/, cards.WrapApp(uiApp))
// Start the card and integrate the pipeline into your desktop rendering path
for _, c := range pipe.Cards() { pipe.StartCard(c) }
```

See also: [Texel app & card pipeline guide](TEXEL_APP_GUIDE.md)

## Architecture at a glance

- `texelui/core` – Widget and UIManager primitives, painter, dirty‑region redraw, layout interface
- `texelui/widgets` – Pane, Border (decorator), TextArea (multiline editor)
- `texelui/adapter` – Wraps a `UIManager` as a `texel.App` for use in any pane

## Theming

TexelUI widgets read their colors from the global theme (section `ui`). Start with [TexelUI theme keys](TEXELUI_THEME.md) to customize surface/text colors and focus highlights.

## Notes and roadmap

- Rendering uses a framebuffer with dirty‑region redraw; widgets can be extended to invalidate subregions.
- Current layout is absolute; additional managers can be added via the `Layout` interface.
- Focus traversal and click‑to‑focus are implemented; cursor blink and IME support are planned.
