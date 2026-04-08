# Config Schema Validation

## Overview

Use the embedded default config as the schema for validating user config files. Unknown keys are logged as warnings and hidden from the config editor. No separate schema format needed.

## Motivation

Users accumulate stale config keys (removed features, renamed options) that clutter the config editor and cause confusion. The embedded defaults already define which keys are valid — we just need to enforce it.

## How It Works

### Validation

`config.ValidateAgainstDefaults(appName, userConfig)` compares user config keys against the embedded default config for that app. Returns a list of unknown keys (keys in user config but not in defaults).

Called during `loadAppLocked` (app configs) and `loadSystemLocked` (system config). Unknown keys are logged with `log.Printf("[CONFIG] Unknown key %q in section %q — not in defaults")`.

### Schema Source

The embedded defaults at `defaults/apps/<app>/config.json` and `defaults/texelation.json` define the valid keys. The schema is derived from the structure:
- Top-level keys in each section are valid field names
- Type is inferred from the default value (`bool`, `float64`, `string`, etc.)
- Arrays and nested objects are opaque at this level (validated as a unit, not element-by-element)

### Config Editor Filtering

`buildAppSections` and `buildSystemSections` in the config editor currently iterate ALL keys from the loaded config. Change to iterate keys from the DEFAULTS, looking up values from the user config:

```
for each section in defaults:
    for each key in defaults[section]:
        value = userConfig[section][key] ?? defaults[section][key]
        show field(key, value, type)
```

Keys in user config that aren't in defaults are skipped — they don't appear in the editor.

### File Preservation

Unknown keys stay in the JSON file. The validation only affects:
1. Log output (warning)
2. Config editor display (hidden)

The in-memory config retains all keys so apps that read them directly still work (forward compatibility — newer config with older binary).

### Scope

- App configs (`apps/<app>/config.json`) — validated against `defaults/apps/<app>/config.json`
- System config (`texelation.json`) — validated against `defaults/texelation.json`
- Flat validation only — top-level section keys and their direct children. Arrays/objects are opaque.

## Testing

- **TestValidateAgainstDefaults**: config with valid + unknown keys, verify unknown keys returned
- **TestConfigEditorFiltering**: mock config with extra keys, verify editor sections only contain default keys

## Out of Scope

- Recursive validation of nested structures (transformer pipeline array)
- Type checking (wrong type for a key)
- Removing unknown keys from the file
- Separate schema format or Go struct tags
