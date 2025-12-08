# TexelUI Theme Keys

TexelUI widgets use the core theme’s **semantic** colors rather than a separate `ui.*` namespace. Ensure these keys exist in your theme; sensible defaults are applied automatically.

## Keys used by TexelUI

| Key                | Purpose                                   |
|--------------------|-------------------------------------------|
| `bg.surface`       | Background for panes, borders, inputs     |
| `text.primary`     | Default foreground for text widgets       |
| `text.inverse`     | Inverted text for buttons/focus states    |
| `border.focus`     | Focus highlight for borders/buttons       |
| `border.active`    | Active border color (used by decorators)  |
| `action.primary`   | Primary action background (buttons)       |
| `caret`            | Caret color for text inputs               |

## Where keys are used

- **Pane** (`texelui/widgets/pane.go`): `bg.surface`, `text.primary` (focus styles reuse the same semantics).
- **Border** (`texelui/widgets/border.go`): `bg.surface`, `text.primary`, `border.focus`/`border.active` for focused descendants.
- **TextArea** (`texelui/widgets/textarea.go`): `bg.surface`, `text.primary`.
- **Input** (`texelui/widgets/input.go`): `bg.surface`, `text.primary`, `caret`.
- **Label** (`texelui/widgets/label.go`): `bg.surface`, `text.primary`.
- **Checkbox** (`texelui/widgets/checkbox.go`): `bg.surface`, `text.primary`.
- **Button** (`texelui/widgets/button.go`): `action.primary`, `text.inverse`, `border.focus` for focus styling.

## Notes

- TexelUI currently positions widgets absolutely; layout helpers (VBox/HBox) exist but UIManager defaults to explicit coordinates.
- If a key is missing, theme defaults are applied automatically via `texel/theme`.
- Caret rendering still uses reverse/underline styles where appropriate; the `caret` color is applied to tinted caret styles.
