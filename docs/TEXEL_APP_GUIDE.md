# Building Texel Apps

Texel apps are the building blocks of the desktop. This guide explains how to
write a new app, how the card layout works, how to plug in effects, and which
tools exist for testing. The goal is to provide a single reference for new
contributors so they can go from idea to shipping app quickly.

---

## 1. The `texel.App` Interface

Texel apps are self-contained binaries that can run inside the Texelation
desktop **or** be launched standalone via `go run ./cmd/app-runner -app <name>`.
Design your app so it operates correctly even outside the desktop; compatibility
with the card pipeline is treated as an additional perk rather than a hard
requirement.

Every app must implement `texel.App` (see `texel/app.go`):

```go
type App interface {
    Run() error
    Stop()
    Resize(cols, rows int)
    Render() [][]texel.Cell
    GetTitle() string
    HandleKey(ev *tcell.EventKey)
    SetRefreshNotifier(refreshChan chan<- bool)
}
```

* **Run / Stop** – long-running logic (PTY loops, timers, RPC clients). `Run`
  is launched by the card pipeline in its own goroutine.
* **Resize** – called whenever the containing pane changes size. Apps should
  update internal buffers and, if necessary, request a re-render via the refresh
  channel.
* **Render** – must return a fully-populated `[][]texel.Cell`. Rows should be
  allocated exactly once and reused to avoid churn.
* **HandleKey** – app-specific input handling. Global control-mode commands are
  processed by the desktop before the event reaches individual apps.
* **SetRefreshNotifier** – the pipeline supplies a buffered channel. Writing to
  it schedules a render; never block while sending (use non-blocking select).

When creating a new app, follow the existing structure under `apps/`:

```
apps/clock/
apps/statusbar/
apps/texelterm/
apps/welcome/
```

Package layout tips:

1. Keep the public surface small (`New(...) texel.App`).
2. Hide internal state behind private structs.
3. Group helpers (rendering, PTY, network) into sub-packages if they can be
   reused.

---

## 2. Card Pipeline & Layout

Texel apps are wrapped in a **card pipeline** (`texel/cards/pipeline.go`) before
the desktop renders them. Think of cards as Android-style view layers: the app
card produces the base buffer, and additional cards decorate it.

```
app buffer ─► effect card ─► diagnostic overlay ─► final buffer
```

Key components:

* `cards.WrapApp(app)` – adapts a `texel.App` into the card interface.
* `cards.EffectCard` – wraps any registered effect (flash, rainbow, fadeTint).
* `cards.Pipeline` – executes the cards in order, handles lifecycle wiring, and
  exposes a control bus.
* Control function (`NewPipeline(controlFunc, ...)`) – intercepts keys before
  cards process them (useful for toggles like F12 diagnostics).

Example pipeline used by TexelTerm:

```go
flash, _ := cards.NewEffectCard("flash", effects.EffectConfig{
    "duration_ms":   100,
    "color":         "#A0A0A0",
    "max_intensity": 0.75,
    "trigger_type":  "workspace.control",
})

pipe := cards.NewPipeline(nil,
    cards.WrapApp(term),
    flash,
)

term.AttachControlBus(pipe.ControlBus())
return pipe
```

Future enhancements on the roadmap include **card sub-queues** (allowing apps
to push temporary overlays without affecting the main chain) and scripted card
layouts for multi-view apps. Track `FUTURE_IMPROVEMENTS.md` for updates.

---

## 3. Control Bus & Triggers

Every pipeline exposes a `ControlBus` (see `texel/cards/control_bus.go`). Cards
that implement `ControllableCard` register capabilities (ID + description).

* The new effect card registers `effects.<id>` – e.g. `cards.FlashTriggerID`.
* Apps can introspect capabilities (`bus.Capabilities()`) to decide which
  features are available at runtime.
* Triggers accept an optional payload. For the flash effect a boolean toggles
  active state, while other cards may expect structured data.

Example BEL handler in TexelTerm:

```go
func (t *TexelTerm) onBell() {
    if bus := t.controlBus; bus != nil {
        _ = bus.Trigger(cards.FlashTriggerID, nil)
    }
}
```

Design tips:

1. Treat the control bus as the public API between app logic and presentation.
2. Keep card IDs descriptive (`diagnostics.toggle`, `effects.flash`).
3. If a card requires configuration, expose it via JSON-style maps so it works
   with both the desktop runtime and pipelines (mirroring the theme format).

---

## 4. Integrating Effects

Effects can be applied in two ways:

1. **Theme bindings** – the remote client’s `ui_state` reads the user theme and
   instantiates effects globally (e.g. fadeTint for inactive panes).
2. **Pipeline composition** – apps compose `EffectCard` instances directly. The
   configuration is identical to the theme format, making it easy to prototype
   new overlays before promoting them to a shared theme.

See `docs/EFFECTS_GUIDE.md` for the effect development walkthrough.

When composing effects remember that the card order matters. Place structural
overlays (diagnostics, grids) after colour-based effects so they render on top.

---

## 5. Testing Playbook

1. **Unit tests** – co-locate in `apps/<name>/_test.go`. Keep render logic
   deterministic so tests can assert on `[][]texel.Cell` contents.
2. **Pipeline smoke tests** – extend `texel/cards/pipeline_test.go` to cover new
   card behaviours (e.g. ensuring refresh propagation works).
3. **Integration tests** – use `client/cmd/texel-headless` or `cmd/texel-stress`
   to verify protocol interactions end-to-end.
4. **Manual runs** – `go run .` for desktop, `go run ./cmd/app-runner -app foo`
   for standalone app debugging.
5. **Effects** – when adding an effect card, add unit coverage similar to
   `internal/effects/keyflash_test.go` and wire a regression into the
   corresponding app test (as TexelTerm does for BEL).

Tips for deterministic tests:

* Stick to fixed timestamps by injecting `time.Now` wrappers when necessary.
* Avoid random sources in renderers or seed them explicitly.
* Capture PTY output using the helper scripts in `apps/texelterm/term_test.go`.

---

## 6. Future Work & Ideas

These are active items or areas we want to explore:

* **Card sub-queues** – allow apps to enqueue temporary cards (e.g. toast
  notifications) without rebuilding the main pipeline.
* **Declarative card layouts** – YAML/JSON descriptors that define the pipeline
  so end users can customise the stack without recompiling.
* **Reusable diagnostics card** – shared overlay that surfaces latency, diff
  backlog, and theme mismatches.
* **TUI bridge** – investigate lightweight widget helpers so cards can host
  richer controls without dragging in heavyweight frameworks.

Update this section whenever new ideas land in `docs/FUTURE_ROADMAP.md` or as we
prototype Android-inspired layout features.

---

## 7. Quick Reference

| Task                              | Location                               |
| --------------------------------- | -------------------------------------- |
| App interface definition          | `texel/app.go`                         |
| Card pipeline implementation      | `texel/cards/pipeline.go`              |
| Control bus helpers               | `texel/cards/control_bus.go`           |
| Effect card adapter               | `texel/cards/effect_card.go`           |
| TexelTerm example pipeline        | `apps/texelterm/term.go`               |
| BEL regression test               | `apps/texelterm/term_test.go`          |
| Future improvements tracker       | `docs/FUTURE_ROADMAP.md`               |

Keep this document up to date whenever we add new capabilities or refine the
card layout system.
