# Configurable Keybinding System

## Overview

A keybinding system that maps keyboard shortcuts to named actions, supports multiple bindings per action, platform presets with overlay merging, and action descriptions for help pages. Widget-level keys (Enter for buttons, arrows for lists) stay hardcoded in texelui — only desktop management and app shortcuts are configurable.

## Motivation

Current keyboard shortcuts are hardcoded and assume a Linux/tmux environment. On macOS, Shift+Arrows (text selection), Ctrl+Arrows (Mission Control), and Alt+Arrows (word navigation) all conflict with system shortcuts. A configurable system with platform presets solves this without breaking Linux defaults.

## Package

`texelation/internal/keybind`

## Public API

### Types

```go
type Action string

type KeyCombo struct {
    Key       tcell.Key
    Rune      rune
    Modifiers tcell.ModMask
}

type ActionInfo struct {
    Description string
}

type Registry struct {
    keyToAction  map[KeyCombo]Action
    actionToKeys map[Action][]KeyCombo
}
```

### Functions

```go
// NewRegistry builds a registry from a preset, optional extra preset overlay,
// and user overrides. Preset can be "linux", "mac", or "auto" (detect platform).
func NewRegistry(preset string, extraPreset string, overrides map[string][]string) *Registry

// Match returns the action for a key event, or "" if unbound.
func (r *Registry) Match(ev *tcell.EventKey) Action

// KeysForAction returns the bound key combos for display in help/UI.
func (r *Registry) KeysForAction(a Action) []KeyCombo

// AllActions returns all registered actions with descriptions and current bindings.
func (r *Registry) AllActions() []ActionEntry

// FormatKeyCombo returns a human-readable string like "Ctrl+A".
func FormatKeyCombo(k KeyCombo) string

// ParseKeyCombo parses a string like "ctrl+a" into a KeyCombo.
func ParseKeyCombo(s string) (KeyCombo, error)
```

### ActionEntry (for help pages)

```go
type ActionEntry struct {
    Action      Action
    Description string
    Keys        []KeyCombo
}
```

## Action Registry

All actions are string constants with descriptions defined in Go code. Descriptions are not user-configurable (can be localized later via i18n if needed).

```go
var ActionDescriptions = map[Action]ActionInfo{
    // Desktop
    "help":                  {Description: "Open help overlay"},
    "screenshot":            {Description: "Save workspace screenshot as PNG"},
    "screensaver":           {Description: "Activate screensaver"},
    "config.editor":         {Description: "Open configuration editor"},
    "control.toggle":        {Description: "Toggle control mode"},

    // Pane
    "pane.navigate.up":      {Description: "Move focus to pane above"},
    "pane.navigate.down":    {Description: "Move focus to pane below"},
    "pane.navigate.left":    {Description: "Move focus to pane left"},
    "pane.navigate.right":   {Description: "Move focus to pane right"},
    "pane.resize.up":        {Description: "Shrink active pane vertically"},
    "pane.resize.down":      {Description: "Grow active pane vertically"},
    "pane.resize.left":      {Description: "Shrink active pane horizontally"},
    "pane.resize.right":     {Description: "Grow active pane horizontally"},

    // Workspace
    "workspace.switch.prev": {Description: "Switch to previous workspace"},
    "workspace.switch.next": {Description: "Switch to next workspace"},
    "workspace.tab.prev":    {Description: "Previous workspace (tab mode)"},
    "workspace.tab.next":    {Description: "Next workspace (tab mode)"},

    // Control mode (prefix + key)
    "control.close":         {Description: "Close active pane"},
    "control.vsplit":        {Description: "Split pane vertically"},
    "control.hsplit":        {Description: "Split pane horizontally"},
    "control.zoom":          {Description: "Toggle pane zoom"},
    "control.swap":          {Description: "Enter pane swap mode"},
    "control.launcher":      {Description: "Open app launcher"},
    "control.help":          {Description: "Open help"},
    "control.config":        {Description: "Open config editor"},
    "control.new_tab":       {Description: "Create new workspace"},
    "control.close_tab":     {Description: "Close workspace"},

    // Texelterm
    "texelterm.search":      {Description: "Toggle history search"},
    "texelterm.scrollbar":   {Description: "Toggle scrollbar"},
    "texelterm.transformer": {Description: "Toggle transformer pipeline"},
    "texelterm.screenshot":  {Description: "Save pane screenshot as PNG"},
    "texelterm.scroll.up":   {Description: "Scroll up one line"},
    "texelterm.scroll.down": {Description: "Scroll down one line"},
    "texelterm.scroll.pgup": {Description: "Scroll up one page"},
    "texelterm.scroll.pgdn": {Description: "Scroll down one page"},
}
```

## Key String Format

Human-readable, `+` separated. Modifiers lowercase, special keys lowercase. Parse rule: split on `+`, last segment is the key, everything before is modifiers.

**Modifiers:** `ctrl`, `shift`, `alt`

**Keys:** `a`-`z`, `0`-`9`, `up`, `down`, `left`, `right`, `f1`-`f12`, `enter`, `esc`, `tab`, `space`, `pgup`, `pgdn`, `home`, `end`, `delete`, `insert`, `backspace`

**Examples:** `"ctrl+a"`, `"shift+up"`, `"alt+left"`, `"f5"`, `"ctrl+shift+f"`

## Config File

Located at `~/.config/texelation/keybindings.json`. Created on first run from the platform-appropriate default.

```json
{
  "preset": "auto",
  "extraPreset": "",
  "actions": {
    "screenshot": ["f5", "ctrl+p"],
    "workspace.switch.prev": ["alt+left", "shift+left"]
  }
}
```

### Fields

- `preset`: Base defaults. `"linux"`, `"mac"`, or `"auto"` (detect platform at startup). Default: `"auto"`.
- `extraPreset`: Optional overlay preset merged on top of the base. Actions in the extra preset replace the base per-action. Useful for shared configs across machines — e.g., base `"auto"` detects Linux, `"extraPreset": "mac"` overlays Mac bindings for keys that differ.
- `actions`: User overrides. Each key is an action name, value is an array of key strings. Replaces the resolved binding for that action entirely.

### Merge Order

1. Load built-in preset (from `preset` field, or auto-detect platform)
2. If `extraPreset` is set, overlay it: for each action in the extra preset, replace the base binding
3. Apply `actions` overrides: for each action in the user config, replace the resolved binding

```
Base (auto=linux): pane.navigate.up = ["shift+up"]
Extra (mac):       pane.navigate.up = ["ctrl+a,up"]   ← replaces
User actions:      screenshot = ["f9"]                 ← replaces

Result:            pane.navigate.up = ["ctrl+a,up"]
                   screenshot = ["f9"]
                   help = ["f1"]   ← untouched from base
```

## Default Asset Files

Embedded via `//go:embed` in `defaults/`:
- `defaults/keybindings-linux.json`
- `defaults/keybindings-mac.json`

On first run (no `keybindings.json` in config dir), the platform-appropriate file is copied to `~/.config/texelation/keybindings.json`.

All presets are always embedded regardless of platform, so the `preset` and `extraPreset` fields can reference any of them.

## Integration with Key Handlers

Each handler replaces hardcoded key checks with `registry.Match(ev)`:

**Before:**
```go
if ev.Key() == tcell.KeyCtrlS {
    state.idleWatcher.ActivateNow()
}
```

**After:**
```go
switch state.bindings.Match(ev) {
case keybind.Screensaver:
    state.idleWatcher.ActivateNow()
}
```

The `Registry` is created once at startup and passed to:
- `clientState` (for client-level bindings: screensaver, screenshot, control mode)
- `DesktopEngine` (for desktop-level bindings: help, pane nav, workspace switch, resize)
- `TexelTerm` (for app-level bindings: search, scrollbar, transformer, scroll)

## Help Page Integration

`Registry.AllActions()` returns all actions sorted by category with descriptions and current key bindings. The help overlay renders this as a formatted table:

```
Desktop
  F1              Open help overlay
  F5              Save workspace screenshot as PNG
  Ctrl+A          Toggle control mode

Pane Navigation
  Shift+Up        Move focus to pane above
  Shift+Down      Move focus to pane below
  ...
```

The category is derived from the action name prefix (`pane.*` → "Pane", `workspace.*` → "Workspace", etc.).

## Testing

- **ParseKeyCombo**: valid strings (`"ctrl+a"`, `"shift+up"`, `"f5"`), invalid strings (`"ctrl+"`, `""`, `"foo+bar"`)
- **NewRegistry merge**: preset only, preset + extra, preset + extra + overrides. Verify overlay replaces per-action, user overrides replace per-action.
- **Match**: bind action, verify Match returns it. Verify multiple bindings for same action all resolve. Verify unbound key returns "".
- **Preset validation**: every action constant appears in both linux and mac presets. No duplicate key→action within a resolved registry.
- **AllActions**: returns sorted entries with descriptions and current keys.

## Out of Scope

- Widget-level keys in texelui (Enter for buttons, arrows for lists) — not configurable
- Control mode sub-commands (the single letters after Ctrl+A) — these are already conflict-free
- i18n for action descriptions — future enhancement
- Runtime rebinding (hot-reload) — restart required to apply changes
