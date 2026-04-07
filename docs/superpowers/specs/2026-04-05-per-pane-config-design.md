# Per-Pane Config with Save as Default

## Overview

Each texelterm pane gets its own copy of the config, stored in the per-pane AppStorage. Changes made via the config editor or pill toggles are persisted per-pane and survive server restarts. A "Save as Default" button promotes the pane's config to the global file so new panes inherit it.

## Motivation

Currently all texelterm instances share one global config file. Toggling transformers or changing scroll settings affects all panes and doesn't persist per-pane. Users want different settings per terminal (e.g., transformers on in one, off in another) that survive restarts.

## Lifecycle

### Pane Creation (AppStorage available)

1. `SetStorage(storage)` is called on the TexelTerm instance
2. Check `storage.Get("config")`:
   - **nil** (first run): copy `config.App("texelterm")` → `storage.Set("config", globalConfig)`
   - **exists** (restart): use the stored pane config as-is
3. All subsequent config reads go through `paneConfig()` helper

### Config Access

```go
func (a *TexelTerm) paneConfig() config.Config
```

- If `a.storage != nil`: deserialize from `storage.Get("config")`, return it
- If `a.storage == nil` (standalone): return `config.App("texelterm")` directly

All existing `config.App("texelterm")` calls in TexelTerm are replaced with `a.paneConfig()`.

### Config Writes

When the config editor saves or a pill toggle changes state:

1. Update the pane config map
2. Write to AppStorage: `storage.Set("config", updatedConfig)`
3. Call `ReloadConfig()` to apply runtime-changeable settings

### Standalone Mode

`a.storage == nil` — `paneConfig()` returns `config.App("texelterm")`. The config editor writes to the global file directly. No per-pane storage, no "Save as Default" button. Current behavior unchanged.

## Save as Default

A button at the bottom of the config panel, only shown when AppStorage is available (per-pane mode).

On click:
1. Read current pane config from AppStorage
2. Write to global file: `config.SetApp("texelterm", paneConfig)` + `config.SaveApp("texelterm")`
3. Toast: "Saved as default for new terminals"

New panes created after this start with the saved config.

## Config Editor Changes

### New Config Source

The config panel currently uses `configeditor.NewAppConfigPanel("texelterm", ...)` which reads from the global `config.App()`. For per-pane mode, a new constructor or option is needed:

```go
configeditor.NewAppConfigPanelWithData(appName string, data config.Config, onSave func(config.Config), ...)
```

- `data`: the pane's config map (from AppStorage)
- `onSave`: callback to write changes back to AppStorage

The existing `NewAppConfigPanel` continues to work for standalone mode (reads/writes global config).

### Save as Default Button

Added at the bottom of the config panel when `onSaveAsDefault` callback is provided:

```go
configeditor.NewAppConfigPanelWithData("texelterm", paneConfig, onSave,
    configeditor.WithSaveAsDefault(func(cfg config.Config) {
        config.SetApp("texelterm", cfg)
        config.SaveApp("texelterm")
    }),
)
```

The button uses accent color, label: "Save as Default".

## What Gets Persisted Per-Pane

All texelterm config sections stored as a single JSON blob under the `"config"` key in AppStorage:
- `texelterm` section (visual_bell_enabled)
- `texelterm.scroll` section (debounce, velocity, etc.)
- `texelterm.selection` section (edge_zone, max_scroll_speed)
- `texelterm.history` section (memory_lines, persist_dir)
- `transformers` section (enabled, pipeline)

## Schema Validation

The existing schema validation (from the previous PR) applies to the pane config too. The defaults are the same — `config.AppDefaults("texelterm")`. Unknown keys in the pane's stored config are warned and hidden from the editor, same as with the global config.

## Testing

- **TestPaneConfig_CopyOnFirstRun**: create TexelTerm with mock AppStorage, verify global config copied to storage
- **TestPaneConfig_ReuseOnRestart**: pre-populate storage with config, verify it's used instead of global
- **TestPaneConfig_Standalone**: no storage set, verify global config used directly
- **TestSaveAsDefault**: modify pane config, save as default, verify global file updated

## Out of Scope

- Config diff/merge between pane and global
- Migration of existing pane storage on config schema changes
- Per-pane config for apps other than texelterm (architecture supports it but not wired)
