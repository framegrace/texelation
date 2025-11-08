# TexelUI Theme Keys

TexelUI widgets use the global theme to style surfaces, text, borders, and focus states. All keys live under the `ui` section. Reasonable defaults are applied to the theme file automatically on first run.

## Keys

| Key                  | Purpose                       | Default      |
|----------------------|-------------------------------|--------------|
| `ui.surface_bg`      | Background of containers      | `#000000`    |
| `ui.surface_fg`      | Foreground of containers      | `#f8f8f2`    |
| `ui.text_bg`         | Background for text widgets   | `#000000`    |
| `ui.text_fg`         | Foreground for text widgets   | `#f8f8f2`    |
| `ui.caret_fg`        | Caret color (used by editors) | `#c0c0c0`    |
| `ui.focus_surface_bg`| Focused container bg          | `#101010`    |
| `ui.focus_surface_fg`| Focused container fg          | `#ffffff`    |
| `ui.focus_text_bg`   | Focused text widget bg        | `#101010`    |
| `ui.focus_text_fg`   | Focused text widget fg        | `#ffffff`    |
| `ui.focus_border_fg` | Border fg when focused        | `#ffff00`    |
| `ui.focus_border_bg` | Border bg when focused        | `#000000`    |

Notes:
- Colors are true RGB hex strings. If a key is missing, the code falls back to a sensible default.
- The caret is rendered with reverse video for insert mode and underline for replace mode (see TextArea behavior). The `ui.caret_fg` color is currently used by caret implementations that prefer a tint rather than reverse; reverse mode ignores it.

## Where keys are used

- Pane (`texelui/widgets/pane.go`)
  - Base style from `ui.surface_*`
  - Focus style from `ui.focus_surface_*`

- Border (`texelui/widgets/border.go`)
  - Base style (passed by caller)
  - Descendant-focused highlight uses `ui.focus_border_*`
  - Border itself also supports focus styling via BaseWidget (uses `ui.focus_border_*`)

- TextArea (`texelui/widgets/textarea.go`)
  - Base text style from `ui.text_*`
  - Focused style from `ui.focus_text_*`
  - Caret color from `ui.caret_fg` (only for caret styles that use a tint)

## Enabling focus styles on custom widgets

All widgets embed `core.BaseWidget`, which provides focus styling helpers:

- `SetFocusedStyle(style tcell.Style, enabled bool)` to configure a focused style.
- In your `Draw`, call `EffectiveStyle(base)` to obtain the style that respects focus.

Example:

```go
style := tcell.StyleDefault.Background(themeBg).Foreground(themeFg)
w.SetFocusedStyle(tcell.StyleDefault.Background(focusBg).Foreground(focusFg), true)
drawStyle := w.EffectiveStyle(style)
```

## Changing theme

The theme file is managed by `texel/theme`. On startup, defaults are applied and saved if missing. To customize, edit the theme file or use your app’s configuration flow to point to a different theme.

