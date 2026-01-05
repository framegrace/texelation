# TexelUI Color Usage (defaults)

Scope and assumptions:
- Palette: `mocha` (default in `texel/theme/theme.go`).
- Semantics: `StandardSemantics` in `texel/theme/semantics.go`.
- UI defaults: `texel/theme/defaults.go` for `ui.surface_bg` and `ui.surface_fg`.
- Resolutions use the same rules as `theme.Config.resolveColorString`.

## Palette color names (mocha)

| Palette name | Hex |
| --- | --- |
| base | #1e1e2e |
| blue | #89b4fa |
| crust | #11111b |
| flamingo | #f2cdcd |
| green | #a6e3a1 |
| lavender | #b4befe |
| mantle | #181825 |
| maroon | #eba0ac |
| mauve | #cba6f7 |
| overlay0 | #6c7086 |
| overlay1 | #7f849c |
| overlay2 | #9399b2 |
| peach | #fab387 |
| pink | #f5c2e7 |
| red | #f38ba8 |
| rosewater | #f5e0dc |
| sapphire | #74c7ec |
| sky | #89dceb |
| subtext0 | #a6adc8 |
| subtext1 | #bac2de |
| surface0 | #313244 |
| surface1 | #45475a |
| surface2 | #585b70 |
| teal | #94e2d5 |
| text | #cdd6f4 |
| yellow | #f9e2af |

## Semantic color names used by TexelUI

| Semantic key | Default mapping | Resolved (mocha) | Notes |
| --- | --- | --- | --- |
| accent | @mauve | #cba6f7 | StandardSemantics |
| action.primary | accent -> @mauve | #cba6f7 | StandardSemantics |
| bg.base | @base | #1e1e2e | StandardSemantics |
| bg.surface | @surface0 | #313244 | StandardSemantics |
| bg.selection | (not defined) | ColorDefault | Not in StandardSemantics |
| border.active | accent -> @mauve | #cba6f7 | StandardSemantics |
| border.default | (not defined) | ColorDefault | Not in StandardSemantics |
| border.focus | accent_secondary -> @lavender | #b4befe | StandardSemantics |
| caret | @rosewater | #f5e0dc | StandardSemantics |
| text.inverse | @base | #1e1e2e | StandardSemantics |
| text.muted | @overlay0 | #6c7086 | StandardSemantics |
| text.primary | @text | #cdd6f4 | StandardSemantics |

## TexelUI defaults by widget

Notes:
- Value format: `semantic -> palette -> hex`.
- `ColorDefault` means no semantic default exists; it falls back to `tcell.ColorDefault` unless user overrides.

### Core

**UIManager** (`texelui/core/uimanager.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Surface background | ui.surface_bg | bg.surface -> @surface0 -> #313244 |
| Surface foreground | ui.surface_fg | text.primary -> @text -> #cdd6f4 |

### Widgets

**Input** (`texelui/widgets/input.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |
| Caret | caret | caret -> @rosewater -> #f5e0dc |
| Placeholder | literal | tcell.ColorGray |

**TextArea** (`texelui/widgets/textarea.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |
| Caret | caret | caret -> @rosewater -> #f5e0dc |

**Button** (`texelui/widgets/button.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.inverse | text.inverse -> @base -> #1e1e2e |
| Background | action.primary | action.primary -> accent -> @mauve -> #cba6f7 |
| Focus background | border.focus | border.focus -> accent_secondary -> @lavender -> #b4befe |
| Focus text | text.inverse | text.inverse -> @base -> #1e1e2e |

**Label** (`texelui/widgets/label.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |

**Checkbox** (`texelui/widgets/checkbox.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |
| Focused (reverse) | bg.surface / text.primary | fg=bg.surface -> #313244, bg=text.primary -> #cdd6f4 |

**Border** (`texelui/widgets/border.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Border foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Border background | bg.surface | bg.surface -> @surface0 -> #313244 |
| Focus border foreground | border.active | border.active -> accent -> @mauve -> #cba6f7 |
| Focus border background | bg.surface | bg.surface -> @surface0 -> #313244 |

**Pane** (`texelui/widgets/pane.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |

**TabLayout** (`texelui/widgets/tablayout.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |

**ComboBox** (`texelui/widgets/combobox.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |
| Dim text (placeholder/auto-complete) | text.muted | text.muted -> @overlay0 -> #6c7086 |
| Focus button foreground | accent | accent -> @mauve -> #cba6f7 |
| Dropdown committed selection fg | bg.surface | bg.surface -> @surface0 -> #313244 |
| Dropdown committed selection bg | accent | accent -> @mauve -> #cba6f7 |
| Dropdown hover bg | bg.selection | ColorDefault (not defined) |
| Dropdown border fg | border.default | ColorDefault (not defined) |

**ColorPicker** (`texelui/widgets/colorpicker/*.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Base text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Base background | bg.surface | bg.surface -> @surface0 -> #313244 |
| Focus ring foreground | border.focus | border.focus -> accent_secondary -> @lavender -> #b4befe |
| Sample letter background | bg.base | bg.base -> @base -> #1e1e2e |

### Primitives

**TabBar** (`texelui/primitives/tabbar.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |

**Grid** (`texelui/primitives/grid.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |

**ScrollableList** (`texelui/primitives/scrollablelist.go`)

| Usage | Key | Default value |
| --- | --- | --- |
| Text foreground | text.primary | text.primary -> @text -> #cdd6f4 |
| Background | bg.surface | bg.surface -> @surface0 -> #313244 |

## TexelUI color setting keys (per widget)

Format: `<Widget>.<primitive|core>.<purpose>`

**UIManager**
- UIManager.core.surface_bg
- UIManager.core.surface_fg

**Input**
- Input.core.text_fg
- Input.core.bg
- Input.core.caret
- Input.core.placeholder_fg
- Input.core.focus_fg
- Input.core.focus_bg

**TextArea**
- TextArea.core.text_fg
- TextArea.core.bg
- TextArea.core.caret
- TextArea.core.focus_fg
- TextArea.core.focus_bg

**Button**
- Button.core.text_fg
- Button.core.bg
- Button.core.focus_fg
- Button.core.focus_bg

**Label**
- Label.core.text_fg
- Label.core.bg

**Checkbox**
- Checkbox.core.text_fg
- Checkbox.core.bg
- Checkbox.core.focus_fg
- Checkbox.core.focus_bg

**Border**
- Border.core.border_fg
- Border.core.border_bg
- Border.core.focus_border_fg
- Border.core.focus_border_bg

**Pane**
- Pane.core.text_fg
- Pane.core.bg
- Pane.core.focus_fg
- Pane.core.focus_bg

**TabLayout**
- TabLayout.core.bg
- TabLayout.TabBar.text_fg
- TabLayout.TabBar.bg
- TabLayout.TabBar.focus_fg
- TabLayout.TabBar.focus_bg

**ComboBox**
- ComboBox.core.text_fg
- ComboBox.core.bg
- ComboBox.core.dim_fg
- ComboBox.core.accent_fg
- ComboBox.core.dropdown.selected_fg
- ComboBox.core.dropdown.selected_bg
- ComboBox.core.dropdown.hover_fg
- ComboBox.core.dropdown.hover_bg
- ComboBox.core.dropdown.border_fg
- ComboBox.core.dropdown.border_bg

**ColorPicker**
- ColorPicker.core.text_fg
- ColorPicker.core.bg
- ColorPicker.core.focus_fg
- ColorPicker.core.focus_bg
- ColorPicker.core.sample_bg
- ColorPicker.ScrollableList.text_fg
- ColorPicker.ScrollableList.bg
- ColorPicker.ScrollableList.focus_fg
- ColorPicker.ScrollableList.focus_bg
- ColorPicker.Grid.text_fg
- ColorPicker.Grid.bg
- ColorPicker.Grid.focus_fg
- ColorPicker.Grid.focus_bg

**TabBar**
- TabBar.core.text_fg
- TabBar.core.bg
- TabBar.core.focus_fg
- TabBar.core.focus_bg

**Grid**
- Grid.core.text_fg
- Grid.core.bg
- Grid.core.focus_fg
- Grid.core.focus_bg

**ScrollableList**
- ScrollableList.core.text_fg
- ScrollableList.core.bg
- ScrollableList.core.focus_fg
- ScrollableList.core.focus_bg

## TexelUI color setting keys (unified list)

- Border.core.border_bg
- Border.core.border_fg
- Border.core.focus_border_bg
- Border.core.focus_border_fg
- Button.core.bg
- Button.core.focus_bg
- Button.core.focus_fg
- Button.core.text_fg
- Checkbox.core.bg
- Checkbox.core.focus_bg
- Checkbox.core.focus_fg
- Checkbox.core.text_fg
- ColorPicker.Grid.bg
- ColorPicker.Grid.focus_bg
- ColorPicker.Grid.focus_fg
- ColorPicker.Grid.text_fg
- ColorPicker.ScrollableList.bg
- ColorPicker.ScrollableList.focus_bg
- ColorPicker.ScrollableList.focus_fg
- ColorPicker.ScrollableList.text_fg
- ColorPicker.core.bg
- ColorPicker.core.focus_bg
- ColorPicker.core.focus_fg
- ColorPicker.core.sample_bg
- ColorPicker.core.text_fg
- ComboBox.core.accent_fg
- ComboBox.core.bg
- ComboBox.core.dim_fg
- ComboBox.core.dropdown.border_bg
- ComboBox.core.dropdown.border_fg
- ComboBox.core.dropdown.hover_bg
- ComboBox.core.dropdown.hover_fg
- ComboBox.core.dropdown.selected_bg
- ComboBox.core.dropdown.selected_fg
- ComboBox.core.text_fg
- Grid.core.bg
- Grid.core.focus_bg
- Grid.core.focus_fg
- Grid.core.text_fg
- Input.core.bg
- Input.core.caret
- Input.core.focus_bg
- Input.core.focus_fg
- Input.core.placeholder_fg
- Input.core.text_fg
- Label.core.bg
- Label.core.text_fg
- Pane.core.bg
- Pane.core.focus_bg
- Pane.core.focus_fg
- Pane.core.text_fg
- ScrollableList.core.bg
- ScrollableList.core.focus_bg
- ScrollableList.core.focus_fg
- ScrollableList.core.text_fg
- TabBar.core.bg
- TabBar.core.focus_bg
- TabBar.core.focus_fg
- TabBar.core.text_fg
- TabLayout.TabBar.bg
- TabLayout.TabBar.focus_bg
- TabLayout.TabBar.focus_fg
- TabLayout.TabBar.text_fg
- TabLayout.core.bg
- TextArea.core.bg
- TextArea.core.caret
- TextArea.core.focus_bg
- TextArea.core.focus_fg
- TextArea.core.text_fg
- UIManager.core.surface_bg
- UIManager.core.surface_fg
