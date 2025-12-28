# Config Editor Plan

This document tracks the plan for splitting configuration files and building a TexelUI-based configuration editor.

Last updated: 2025-12-28

## Goals
- Split configuration into system config + per-app config while keeping theme.json as the shared visual base.
- Allow per-app theme overlays (optional) without breaking global theme cohesion.
- Provide a TexelUI configuration editor that can edit theme/system/app configs with auto-generated forms.
- Add hotkeys to open the configuration editor in texelation and in standalone apps.
- Keep reload paths fast and predictable (reload where possible, restart where required).

## Current State (as of investigation)
- Theme + config are mixed in `~/.config/texelation/theme.json` via `texel/theme` (map-of-sections).
- Server config lives in `~/.config/texelation/config.json` via `config/config.go` (only `defaultApp`).
- Layout transitions read from theme (`texel/desktop_engine_core.go`).
- TexelTerm reads non-visual settings from theme (`apps/texelterm/term.go`).
- Theme reload exists (`theme.Reload`), invoked on SIGHUP.
- Floating panels exist (launcher/help) using `DesktopEngine.ShowFloatingPanel`.
- Control mode is Ctrl+A; `handleControlMode` routes keys like `l`, `h`, `x`, `|`, `-`.

## Proposed Config Layout (decided)
- Themes: `~/.config/texelation/themes/<name>.json`
  - Pure theme settings only: palette, semantic colors, default TexelUI assignments.
  - Theme selection moved to system config (active theme name).
  - Future: a dedicated theme editor app will create/edit these files.
- System config: `~/.config/texelation/texelation.json`
  - Default app, active theme name, global behavior.
  - Control keys and other non-theme global settings.
- App config: `~/.config/texelation/apps/<app>/config.json`
  - All non-theme settings that were previously in theme.json.
  - Optional TexelUI color overlays per app (same shape as theme sections).
- Migration: read legacy sections from theme.json and copy into per-app/system files on first load.

## Phased Plan

### Phase 0 - Decisions and Inventory
- Enumerate current config keys by section and classify as theme/system/app.
- Finalize file names/paths and on-disk schema for per-app configs.
- Use Go struct tags for schema metadata.
- Hotkeys:
  - Texelation control key: Ctrl+A then F (open system config editor).
  - TexelApp hotkey: Ctrl+F (open app config editor).

### Inventory (current keys -> target)
- Theme (per-theme files):
  - `meta.palette` (theme selector within theme file).
  - `ui.*` semantic colors and TexelUI defaults (surface/background, etc).
- System config (`texelation.json`):
  - `defaultApp` (currently in `config/config.go`).
  - `layout_transitions.*` (currently read from theme).
  - `effects.bindings` (currently theme defaults + client effects config).
- App config (`apps/texelterm/config.json`):
  - `texelterm.*` (wrap/reflow/display buffer/visual bell).
  - `texelterm.scroll.*` (scroll velocity/accel).
  - `texelterm.selection.*` (edge zone, max scroll speed).
  - `texelterm.history.*` (memory lines, persist dir).
  - `selection.highlight_*` (currently used in texelterm, to move into TexelUI overlay).

### Phase 1 - Config Store Refactor
- Introduce a config store package (or extend `config`) with:
  - Load/Reload/Save for system and per-app configs.
  - Typed getters (GetString/GetInt/GetBool/GetFloat) and defaults registration.
  - Thread-safe access similar to `texel/theme`.
- Move non-theme defaults out of `texel/theme/defaults.go` into config defaults:
  - `texelterm` sections, layout transitions, other non-visual settings.
- Update consumers:
  - Desktop layout transitions -> system config.
  - TexelTerm -> app config.
  - Any future app settings -> app config.
- Add migration path from legacy theme sections to new files (with backup/write-once).
- Remove auto-generation of config files at runtime.
  - Store default JSONs under a `defaults/` directory in the repo.
  - Install copies these defaults into the user config directory.
- Define reload plumbing:
  - SIGHUP reloads theme + system + app configs.
  - Add a new event (e.g., `EventConfigChanged`) or control bus trigger for apps.

### Phase 2 - Per-App Theme Overlay
- Define overlay structure in app config (e.g., `theme_overrides` map of sections/keys).
- Add helper to combine base theme with overrides without mutating global:
  - `theme.WithOverrides(overrides theme.Config)` or similar.
- Update apps to use per-app theme where available:
  - TexelUI adapter/app-level style resolution.
  - TexelTerm palette generation (if overlay affects ANSI palette).
- Decide how overlays interact with runtime theme updates (recompute on reload).

### Phase 3 - Config Editor App (TexelUI)
- Add new app (e.g., `apps/config-editor`) and standalone entrypoint.
- Discover config targets (system, theme, per-app list).
- UI structure:
  - Outer selector for config target (tabs or list).
  - Inner `TabLayout` for sections in the selected config.
  - Form builder generates widgets per field.
- Form builder heuristics + schema support:
  - String -> Input/TextArea (length or multi-line).
  - Bool -> Checkbox.
  - Number -> Input with numeric validation.
  - Enum/list -> ComboBox.
  - Color -> ColorPicker (detect hex/@semantic).
  - Object/array -> JSON TextArea fallback.
- Actions:
  - Save/Apply (persist + reload).
  - Reload/Reset (discard local edits).
- Surface validation errors and restart-required warnings.
- If a change requires restart, show a modal with:
  - "Restart required" message.
  - "Restart now" action (triggers restart if accepted).

### Phase 4 - Hotkeys and Integration
- Control mode key to open config editor as floating panel.
- App hotkey to open config editor targeted at active app.
- For standalone apps, handle same hotkey internally (open config UI).
- Update help text and documentation.

### Phase 5 - Tests and Docs
- Tests:
  - Config load/save/migration.
  - Overlay merge logic.
  - Basic form builder mapping (widget types).
- Docs:
  - Update config paths, hotkeys, and reload behavior.
  - Note which settings require restart vs live reload.

## Open Questions
- None at the moment. Pending scope decisions may add new questions here.

## Schema Inventory (draft, Go struct tags)

The configuration editor will derive field metadata from Go struct tags.
Tags below use a proposed `ui` tag convention; see "Tag Conventions" section.

### Tag Conventions (proposed)

Use a single `ui` struct tag with comma-separated `key=value` pairs.
Values should avoid commas; use `|` for list separators.

Supported keys (initial set):
- `label`: Human-readable label.
- `section`: Section/tab label in the UI.
- `widget`: Widget type (`input`, `textarea`, `number`, `checkbox`, `combo`, `color`, `list`, `object`).
- `options`: Option source for combo/list (`a|b|c` or symbolic names).
- `min`, `max`, `step`: Numeric constraints for `number`.
- `restart`: `true` if change requires restart.
- `schema`: Schema key used to resolve nested/param forms.

Example:
`ui:"label=Duration (ms),widget=number,min=0,step=10,section=Layout"`

Options sources (symbolic):
- `theme_dir`: `~/.config/texelation/themes/`
- `palettes`: theme palette files (built-in + user)
- `effect_ids`: from effects registry (fadeTint, rainbow, flash)
- `effect_events`: from `effects.ParseTrigger`
- `effect_targets`: `pane|workspace`
- `key_names`: known key names for effect key bindings

Key name list (initial):
- Letters: `A`..`Z`
- Numbers: `0`..`9`
- Function: `F1`..`F12`
- Navigation: `Up`, `Down`, `Left`, `Right`, `Home`, `End`, `PageUp`, `PageDown`
- Editing: `Enter`, `Tab`, `Backspace`, `Delete`, `Insert`, `Escape`
- Modifiers (for display): `Ctrl`, `Alt`, `Shift`

### System Config (`~/.config/texelation/texelation.json`)

```go
// SystemConfig defines texelation-wide settings.
type SystemConfig struct {
    DefaultApp      string                 `json:"defaultApp" ui:"label=Default App,widget=combo,options=launcher|texelterm|welcome,section=General"`
    ActiveTheme     string                 `json:"activeTheme" ui:"label=Active Theme,widget=combo,options=theme_dir,section=Theme"`
    LayoutTransitions LayoutTransitions    `json:"layout_transitions" ui:"section=Layout Transitions"`
    Effects         EffectsConfig          `json:"effects" ui:"section=Effects"`
}

type LayoutTransitions struct {
    Enabled      bool   `json:"enabled" ui:"label=Enabled,widget=checkbox"`
    DurationMs   int    `json:"duration_ms" ui:"label=Duration (ms),widget=number,min=0,step=10"`
    Easing       string `json:"easing" ui:"label=Easing,widget=combo,options=linear|smoothstep|ease-in-out|spring"`
    MinThreshold int    `json:"min_threshold" ui:"label=Min Threshold,widget=number,min=0,step=1"`
}

type EffectsConfig struct {
    Bindings []EffectBinding `json:"bindings" ui:"label=Bindings,widget=list"`
}

// EffectBinding is a list entry with effect + event + target + params.
type EffectBinding struct {
    Event  string               `json:"event" ui:"label=Event,widget=combo,options=effect_events"`
    Target string               `json:"target" ui:"label=Target,widget=combo,options=effect_targets"`
    Effect string               `json:"effect" ui:"label=Effect,widget=combo,options=effect_ids"`
    Params map[string]interface{} `json:"params" ui:"label=Params,widget=object,schema=effect_params"`
}
```

Notes:
- `options=theme_dir` means populate from `~/.config/texelation/themes/`.
- `effect_ids` from registry: `fadeTint`, `rainbow`, `flash`.
- `effect_events` from `effects.ParseTrigger` (see `internal/effects/config.go`).
- `effect_targets` are `pane`, `workspace`.
- `effect_params` resolved by effect ID (see below).

### Effect Parameter Schemas (per effect)

```go
type FadeTintParams struct {
    Color      string  `json:"color" ui:"label=Color,widget=color"`
    Intensity  float64 `json:"intensity" ui:"label=Intensity,widget=number,min=0,max=1,step=0.05"`
    DurationMs int     `json:"duration_ms" ui:"label=Duration (ms),widget=number,min=0,step=10"`
}

type RainbowParams struct {
    SpeedHz float64 `json:"speed_hz" ui:"label=Speed (Hz),widget=number,min=0,step=0.1"`
}

type FlashParams struct {
    Color      string   `json:"color" ui:"label=Color,widget=color"`
    DurationMs int      `json:"duration_ms" ui:"label=Duration (ms),widget=number,min=0,step=10"`
    Keys       []string `json:"keys" ui:"label=Keys,widget=list,options=key_names"`
}
```

### App Config (`~/.config/texelation/apps/<app>/config.json`)

```go
// TexelTermConfig lives under apps/texelterm/config.json.
type TexelTermConfig struct {
    WrapEnabled         bool              `json:"wrap_enabled" ui:"label=Wrap Lines,widget=checkbox,section=Core,restart=true"`
    ReflowEnabled       bool              `json:"reflow_enabled" ui:"label=Reflow On Resize,widget=checkbox,section=Core,restart=true"`
    DisplayBufferEnabled bool             `json:"display_buffer_enabled" ui:"label=Display Buffer,widget=checkbox,section=Core,restart=true"`
    VisualBellEnabled   bool              `json:"visual_bell_enabled" ui:"label=Visual Bell,widget=checkbox,section=Core"`
    Scroll              TexelTermScroll   `json:"scroll" ui:"section=Scroll"`
    Selection           TexelTermSelection `json:"selection" ui:"section=Selection"`
    History             TexelTermHistory  `json:"history" ui:"section=History"`
    TexelUI             TexelUIOverrides  `json:"texelui" ui:"section=TexelUI"`
}

type TexelTermScroll struct {
    VelocityDecay     float64 `json:"velocity_decay" ui:"label=Velocity Decay,widget=number,min=0,step=0.05"`
    VelocityIncrement float64 `json:"velocity_increment" ui:"label=Velocity Increment,widget=number,min=0,step=0.05"`
    MaxVelocity       float64 `json:"max_velocity" ui:"label=Max Velocity,widget=number,min=0,step=0.5"`
    DebounceMs        int     `json:"debounce_ms" ui:"label=Debounce (ms),widget=number,min=0,step=5"`
    ExponentialCurve  float64 `json:"exponential_curve" ui:"label=Exponential Curve,widget=number,min=0,step=0.05"`
}

type TexelTermSelection struct {
    EdgeZone       int `json:"edge_zone" ui:"label=Edge Zone,widget=number,min=1,step=1"`
    MaxScrollSpeed int `json:"max_scroll_speed" ui:"label=Max Scroll Speed,widget=number,min=1,step=1"`
}

type TexelTermHistory struct {
    MemoryLines int    `json:"memory_lines" ui:"label=Memory Lines,widget=number,min=0,step=100,restart=true"`
    PersistDir  string `json:"persist_dir" ui:"label=Persist Dir,widget=input,restart=true"`
}

// TexelUIOverrides are per-app color overlays; values accept hex or @semantic.
type TexelUIOverrides struct {
    SurfaceBg string `json:"surface_bg" ui:"label=Surface Background,widget=color"`
    SurfaceFg string `json:"surface_fg" ui:"label=Surface Foreground,widget=color"`
    ButtonBg  string `json:"button_bg" ui:"label=Button Background,widget=color"`
    ButtonFg  string `json:"button_fg" ui:"label=Button Foreground,widget=color"`
    Selection SelectionColors `json:"selection" ui:"label=Selection Colors,section=Selection"`
}

type SelectionColors struct {
    HighlightBg string `json:"highlight_bg" ui:"label=Highlight Background,widget=color"`
    HighlightFg string `json:"highlight_fg" ui:"label=Highlight Foreground,widget=color"`
}
```

Notes:
- `restart=true` marks fields that only apply on app start and should trigger the restart modal.
- Colors accept `#RRGGBB` or `@semantic` tokens.
- This schema is the first concrete app config; other apps follow the same pattern.

### Theme Files (`~/.config/texelation/themes/<name>.json`)

```go
type ThemeFile struct {
    Meta   ThemeMeta          `json:"meta" ui:"section=Meta"`
    UI     ThemeSemantics     `json:"ui" ui:"section=Semantics"`
    TexelUI TexelUIThemeDefaults `json:"texelui" ui:"section=TexelUI Defaults"`
}

type ThemeMeta struct {
    Palette string `json:"palette" ui:"label=Palette,widget=combo,options=palettes"`
}

// ThemeSemantics mirrors the semantic keys in theme/semantics.go.
// Keys are strings because they reference palette colors (e.g., "@mauve").
type ThemeSemantics map[string]string

type TexelUIThemeDefaults struct {
    SurfaceBg string `json:"surface_bg" ui:"label=Surface Background,widget=color"`
    SurfaceFg string `json:"surface_fg" ui:"label=Surface Foreground,widget=color"`
    ButtonBg  string `json:"button_bg" ui:"label=Button Background,widget=color"`
    ButtonFg  string `json:"button_fg" ui:"label=Button Foreground,widget=color"`
    Selection SelectionColors `json:"selection" ui:"label=Selection Colors,section=Selection"`
}
```

Notes:
- Theme editing is future work; schema listed for completeness.

## Defaults Directory Layout (draft)

```
defaults/
  texelation.json
  apps/
    texelterm/
      config.json
  themes/
    mocha.json
    latte.json
    frappe.json
    macchiato.json
```
