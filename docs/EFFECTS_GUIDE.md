# Developing Texelation Effects

This guide walks through everything a contributor needs to know to implement a
new visual effect for Texelation. It covers the effect lifecycle, configuration
hooks, testing patterns, and the latest infrastructure used by both the remote
client and the card pipeline.

---

## 1. Effect Architecture Overview

Effects live under `internal/effects`. Each effect must implement the
`Effect` interface defined in `interfaces.go`:

```go
type Effect interface {
    ID() string
    Active() bool
    Update(now time.Time)
    HandleTrigger(trigger EffectTrigger)
    ApplyWorkspace(buffer [][]client.Cell)
    ApplyPane(pane *client.PaneState, buffer [][]client.Cell)
}
```

Key concepts:

* **Targets** – effects can run against the entire workspace buffer
  (`ApplyWorkspace`) or against a single pane (`ApplyPane`). If you only care
  about global overlays (e.g. rainbow tint, flash) you can leave `ApplyPane`
  empty.
* **Triggers** – the effect manager emits `EffectTrigger` instances when control
  mode toggles, keys are pressed, panes become active/resizing, etc. The effect
  chooses which triggers it cares about inside `HandleTrigger`.
* **Timeline** – reusable easing helper (`timeline.go`) that handles animation
  curves and delta time. Effects call `AnimateTo(key, target, duration)` to move
  values smoothly.

Both the remote client runtime and `texel/cards/effect_card.go` share the same
registry, so an effect automatically works in the runtime and via cards if the
configuration is exposed.

---

## 2. Creating a New Effect

1. **Create the file** under `internal/effects/`. Use a descriptive name
   (`sparkle.go`, `glitch.go`, etc.).
2. **Define the struct** holding any state you need (timelines, colours,
   parameters). Keep fields private and expose configuration through the factory.
3. **Implement the interface**:
   * `ID()` should return the registry identifier (lowercase string).
   * `Active()` should return `true` only when the effect must render.
   * `HandleTrigger()` toggles internal state or schedules animations.
   * `Update(now)` advances timelines or timers.
   * `ApplyWorkspace` / `ApplyPane` mutate the provided `[][]client.Cell` buffer.
4. **Register the effect** in an `init()` block using `effects.Register(id,
   factory)`. The factory receives an `EffectConfig` (essentially
   `map[string]interface{}`) parsed from JSON.
5. **Parse configuration** with helper functions:
   * `parseColorOrDefault(cfg, "color", tcell.ColorWhite)`
   * `parseFloatOrDefault(cfg, "mix", 0.5)`
   * `parseDurationOrDefault(cfg, "duration_ms", 250)`
   * If the effect needs defaults for cells that emit `tcell.ColorDefault`,
     accept `"default_fg"` / `"default_bg"` and pass them to `tintStyle`.
6. **Blend colours safely** using `tintStyle` from `helpers.go`. The helper now
   accepts an explicit fallback foreground/background, and it already converts
   palette indexes to true RGB.
7. **Handle triggers** carefully:
   * Workspace-wide overlays typically listen for `TriggerWorkspaceControl` or
     `TriggerWorkspaceKey`.
   * Pane-specific effects listen for `TriggerPaneActive`, `TriggerPaneResizing`,
     or custom events that your app publishes via the control bus.
   * Do not assume a trigger will always be accompanied by a matching deactivation.
     Defensive defaults help avoid “stuck” overlays.
8. **Write unit tests** under `internal/effects/`. Use the helper types to build
   minimal cell buffers and assert that colours/attributes change as expected.
   The new `keyflash_test.go` is a good template.
9. **Update documentation** – add a short entry to `docs/EFFECTS_GUIDE.md`
   (this file) and, if needed, to `docs/EFFECT_CARD_MIGRATION.md` so app authors
   know the effect exists.

---

## 3. Configuration & Theme Integration

Effects are configured via JSON-like dictionaries:

```jsonc
{
  "event": "workspace.key",
  "target": "workspace",
  "effect": "flash",
  "params": {
    "duration_ms": 100,
    "color": "#FFFFFF",
    "max_intensity": 0.75,
    "keys": ["F"]
  }
}
```

* Themes embed bindings under the `"effects"` section; see
  `internal/runtime/client/ui_state.go` for parsing.
* `texel/cards/effect_card.go` accepts the same configuration when composing app
  pipelines. Any extra keys (`trigger_type`, `trigger_active`, `trigger_key`)
  are stripped before the effect factory runs.
* The client now injects resolved default foreground/background colours into the
  configuration so effects blending with `"default"` cells still pick the
  correct palette.

---

## 4. Using `EffectCard` in Pipelines

The new `EffectCard` lets apps apply any registered effect without bespoke card
code:

```go
flash, err := cards.NewEffectCard("flash", effects.EffectConfig{
    "duration_ms":   100,
    "color":         "#FFFFFF",
    "max_intensity": 0.75,
    "trigger_type":  "workspace.control",
})
if err != nil {
    log.Printf("flash effect unavailable: %v", err)
}
pipe := cards.NewPipeline(nil, cards.WrapApp(app), flash)
```

The card automatically:

* Spins the effect’s `Timeline` via a ticker (~60 Hz).
* Registers `effects.<id>` with the control bus (e.g. `cards.FlashTriggerID`).
* Converts between `[][]texel.Cell` and `[][]client.Cell`.
* Respects `"default_fg"` / `"default_bg"` from configuration.

---

## 5. Testing Checklist

1. **Unit tests** – verify colour blending, trigger handling, and timeline
   behaviour using deterministic buffers.
2. **Integration tests** – when applicable, add coverage to app-level tests
   (e.g. `apps/texelterm/term_test.go` ensures the BEL flash toggles).
3. **Headless run** – use `client/cmd/texel-headless` or `cmd/texel-stress` to
   confirm the effect does not introduce message loops or diff explosions.
4. **Performance review** – long-running or high-frequency triggers should avoid
   allocations inside `ApplyWorkspace`. Reuse local variables and keep blending
  linear in buffer size.

---

## 6. Future Improvements

These items are on the radar for the effects subsystem:

* **Effect layering** – allow deterministic stacking of multiple overlays
  without requiring separate cards.
* **Per-pane bindings** – expose declarative mappings for individual app panes
  (e.g. specific status widgets) instead of global triggers.
* **Editor tooling** – ship a small CLI that previews effects against sample
  buffers for faster iteration.
* **Schema validation** – add JSON-schema style validation so theme mistakes are
  reported with actionable errors.

Contributors are encouraged to update this section whenever new work is scoped.

---

## 7. Quick Reference

| Task                         | Where to start                                    |
| --------------------------- | ------------------------------------------------- |
| Register a new effect       | `internal/effects/registry.go`                    |
| Reuse timeline helpers      | `internal/effects/timeline.go`                    |
| Tint utilities              | `internal/effects/helpers.go`                     |
| Effect card adapter         | `texel/cards/effect_card.go`                      |
| Theme binding consumption   | `internal/runtime/client/ui_state.go`             |
| Sample configuration        | `docs/EFFECT_CARD_MIGRATION.md`                   |

Keep this guide updated as new helpers or best practices emerge.
