# TexelUI Quickstart

This guide shows how to try the shipped demos and embed TexelUI inside a `texel.App`.

## Run the demos

### Single Text Editor (bordered TextArea)
```bash
go run ./cmd/texelui-demo
```
- Full-window TextArea wrapped in a border and background pane.
- Focus follows mouse clicks; Tab/Shift+Tab cycle focus when multiple editors exist.
- Insert toggles replace mode (underline caret).

### Two Editors Side-by-Side
```bash
go run ./cmd/texelui-demo2
```
- Two bordered TextAreas to test focus traversal and resize handling.

### Widget Showcase (Label/Input/Checkbox/Button)
```bash
go run ./texelui/examples/widget_demo.go
```
- Demonstrates the core form widgets plus simple validation callbacks.
- Shows focus styling and button activation with Enter/Space or mouse.

## Embedding TexelUI in a TexelApp

The adapter package wraps a `UIManager` as a `texel.App`:

```go
import (
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

ui := core.NewUIManager()
bg := widgets.NewPane(0, 0, 0, 0, tcell.StyleDefault)
ui.AddWidget(bg)

border := widgets.NewBorder(0, 0, 0, 0, tcell.StyleDefault)
ta := widgets.NewTextArea(0, 0, 0, 0)
border.SetChild(ta)
ui.AddWidget(border)
ui.Focus(ta)

app := adapter.NewUIApp("My Editor", ui)
app.Resize(cols, rows) // call from pane resize
```

The adapter forwards `Resize`, `Render`, `HandleKey`, and `HandleMouse` to the UI. Widgets are positioned explicitly today; VBox/HBox layout structs exist but the default UIManager still uses absolute positioning.

## Widget set

- **Pane** – background surface with focus styling
- **Border** – decorator that hosts a single child
- **TextArea** – multiline editor (insert/replace, selection, mouse)
- **Label** – aligned static text
- **Button** – clickable/keyboard activatable with callbacks
- **Input** – single-line text entry with caret and placeholder
- **Checkbox** – toggle with focus highlight

## Theming

TexelUI widgets use the core theme’s semantic colors (e.g., `bg.surface`, `text.primary`). See [TexelUI theme keys](TEXELUI_THEME.md) for the exact set.

## Notes and roadmap

- Layout manager structs (VBox/HBox) are available; wiring them into UIManager and adding padding/grid helpers is still pending.
- Cursor blink and IME support remain future enhancements.
- The adapter demos are useful for sanity checks before embedding in a full TexelApp card.
