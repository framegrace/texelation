# Keybinding Editor

## Overview

A new "Keybindings" tab in the system config editor (Ctrl+F). Auto-generated from the keybinding registry's action definitions, grouped by category. Each action has a modifier ComboBox and a key Input field. Changes are hot-reloaded — no restart required.

## Motivation

Keybindings are configurable via `keybindings.json` but there's no UI to edit them. Users must hand-edit JSON. A visual editor makes keybindings discoverable and easy to customize.

## Layout

### Tab Structure

The Keybindings tab contains sub-tabs, one per category from `ActionDescriptions`:

- **Desktop** — help, screenshot, screensaver, config.editor, control.toggle
- **Pane** — navigate up/down/left/right, resize up/down/left/right
- **Workspace** — switch prev/next
- **Control** — close, vsplit, hsplit, zoom, swap, launcher, help, config, new_tab, close_tab
- **Terminal** — search, scrollbar, transformer, screenshot, scroll up/down/pgup/pgdn

### Per-Action Row

Each action is a form row:

```
Open help overlay      [<Ctrl>      ▾] [a          ]
Move focus up          [<Shift>     ▾] [up         ]
Close active pane      [<Control Mode>▾] [x          ]
```

- **Label**: action description from `ActionDescriptions`
- **Modifier ComboBox**: dropdown with options:
  - `(none)` — no modifier, bare key
  - `<Ctrl>` — Ctrl modifier
  - `<Alt>` — Alt modifier
  - `<Shift>` — Shift modifier
  - `<Control Mode>` — the control mode prefix key (currently Ctrl+A, but editable via the `control.toggle` action)
- **Key Input**: text field for the key name (e.g., `up`, `left`, `f1`, `a`, `x`, `|`, `-`)

For the first binding only (primary). Multiple bindings per action are supported in the config but the editor shows/edits only the first one.

### Control Mode Modifier

The `<Control Mode>` label in the modifier combo is dynamic — it reflects whatever `control.toggle` is currently bound to. If `control.toggle` is changed (in the Desktop tab), the label updates.

The `control.toggle` action itself is edited as a regular key binding (modifier + key). Changing it changes what `<Control Mode>` means for all other actions that use it.

## Data Flow

### Loading

1. Read `~/.config/texelation/keybindings.json` (preset, extraPreset, actions)
2. Build the resolved registry to get current bindings per action
3. For each action, decompose the first binding's `KeyCombo` into modifier + key parts for the combo and input

### Saving

1. On any change (modifier combo or key input): compose the modifier + key into a key string (e.g., `"shift+up"`, `"ctrl+a"`, `"f1"`)
2. Update the `actions` map in the keybinding config
3. Write to `~/.config/texelation/keybindings.json`
4. Hot-reload the registry (see below)

### Hot-Reload

On save, the config editor emits a keybinding-specific apply. The desktop handles it by:

1. Re-reading `keybindings.json`
2. Building a new `keybind.Registry`
3. Calling `desktop.SetKeybindings(newRegistry)`
4. Iterating all texelterm panes and calling `SetKeybindings(newRegistry)` on those that implement it
5. The client-side registry (in `clientState`) is also rebuilt

No restart required — the Registry is a pure lookup table.

## Implementation

### New File

`apps/configeditor/keybinding_editor.go` — builds the keybindings tab content. Contains:

- `buildKeybindingsTab(registry, onSave)` — creates the TabPanel with sub-tabs per category
- `buildKeybindingCategory(actions, registry, onSave)` — creates a Form with modifier combo + key input per action
- `decomposeKeyCombo(kc KeyCombo) (modifier, key string)` — splits a KeyCombo into modifier label and key name
- `composeKeyString(modifier, key string) string` — builds `"shift+up"` from parts

### Config Editor Integration

In `configeditor.go`, `buildSystemSections` adds a new tab:

```go
panel.AddTab("Keybindings", e.buildKeybindingsTab(target))
```

### Apply Path

New `applyKind`: `applyKeybindings`. When triggered:

1. Reload keybindings from the saved file
2. Rebuild registry
3. Push to desktop and all texelterm instances

## Modifier Decomposition

Given a `KeyCombo`, determine which modifier option to show:

| KeyCombo | Modifier | Key |
|----------|----------|-----|
| `Shift+Up` | `<Shift>` | `up` |
| `Ctrl+A` | `<Ctrl>` | `a` |
| `F1` | `(none)` | `f1` |
| `Alt+Left` | `<Alt>` | `left` |
| `x` (after control.toggle) | `<Control Mode>` | `x` |

Control mode actions (those in the `Control` category) always show `<Control Mode>` as modifier since they're only reachable after the prefix key.

## Testing

- **TestDecomposeKeyCombo**: verify Shift+Up → (`<Shift>`, `up`), F1 → (`(none)`, `f1`)
- **TestComposeKeyString**: verify (`<Shift>`, `up`) → `"shift+up"`, (`(none)`, `f1`) → `"f1"`
- **Manual**: open config editor, Keybindings tab, change a binding, verify it takes effect immediately

## Out of Scope

- Multiple bindings per action in the editor (only first shown)
- Conflict detection (two actions bound to same key)
- Key capture mode ("press a key to bind")
- Per-pane keybinding overrides
