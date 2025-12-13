# Developer Guide

A quick starting point for building on Texelation. It links to detailed docs and provides concise examples for TexelUI, cards, effects, and app integration.

## Quick Links

- Architecture: `docs/CLIENT_SERVER_ARCHITECTURE.md`
- TexelUI: `docs/TEXELUI_QUICKSTART.md`, `docs/TEXELUI_THEME.md`, `docs/programmer/TEXELUI_USAGE.md`
- Card pipeline: `docs/TEXEL_APP_GUIDE.md`, `docs/cards_control.md`
- Effects: `docs/EFFECTS_GUIDE.md`
- Plans: `docs/plans/TEXELUI_PLAN.md`, `docs/plans/LONG_LINE_EDITOR_PLAN.md`

## TexelUI Basics (Standalone or Integrated)

TexelUI and TexelApps can run **standalone** (`go run ./cmd/<app>`) without the desktop. Texelation integration is optional; the same apps can be embedded into the card pipeline later.

```go
import (
    "github.com/gdamore/tcell/v2"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

ui := core.NewUIManager()
bg := widgets.NewPane(0, 0, 0, 0, tcell.StyleDefault)
ui.AddWidget(bg)

border := widgets.NewBorder(0, 0, 0, 0, tcell.StyleDefault)
ta := widgets.NewTextArea(0, 0, 0, 0)
border.SetChild(ta)
ui.AddWidget(border)
ui.Focus(ta)

app := adapter.NewUIApp("My TexelUI App", ui)
app.Resize(cols, rows) // call from pane resize
```

- Widgets are positioned absolutely today; VBox/HBox helpers exist (`texelui/layout/*`) but UIManager still defaults to manual coordinates.
- Theme keys rely on semantic colours such as `bg.surface`, `text.primary`, `action.primary`.

## Card Pipeline Primer (App-side)

Cards are an **app-internal** composition mechanism. They wrap a `texel.App` to layer effects/overlays/diagnostics before handing the final buffer to the desktop. The desktop only sees a `texel.App`; the pipeline lives entirely inside the app. The standard pattern is:

```go
base := NewMyAppCore(...)
app  := cards.DefaultPipeline(base /*, extra cards... */)
```

```go
import (
    "texelation/internal/effects"
    "texelation/texel/cards"
)

flash, _ := cards.NewEffectCard("flash", effects.EffectConfig{
    "duration_ms": 120,
    "color":       "#ffffff",
})

pipe := cards.NewPipeline(nil, cards.WrapApp(myApp), flash)
bus := pipe.ControlBus()
// Optional: register custom toggles
_ = bus.Register("app.toggle-thing", "Toggle something", func(_ interface{}) error {
    myApp.Toggle()
    return nil
})
```

- Earlier cards draw first; later cards overlay on top.
- Control bus capabilities are discoverable via `bus.Capabilities()`. Effects register `effects.<id>` automatically.

## Creating Effects

Follow `docs/EFFECTS_GUIDE.md` for full details. In short:
- Implement `effects.Effect` (`ID`, `Active`, `Update`, `HandleTrigger`, `ApplyWorkspace`/`ApplyPane`).
- Register via `effects.Register(id, factory)`; parse config with helper parsers.
- Use the shared `Timeline` for animations (duration/easing) and `tintStyle` for safe colour blending.
- Add unit tests under `internal/effects/` and wire app-level triggers (e.g., BEL triggers flash in texelterm).

## App Integration & Registry

- New built-in apps are registered in `cmd/texel-server/main.go` via `registry.RegisterBuiltIn`.
- Wrapper apps (e.g., custom texelterm commands) are discovered from `~/.config/texelation/apps/` and can supply commands/args.
- Implement `texel.ControlBusProvider` if your app needs to expose controls (launcher uses `launcher.select-app` / `launcher.close`).
- For snapshot restore, register factories with `desktop.RegisterSnapshotFactory` keyed by app type.

## Where to Explore Next

- `texelui/examples/widget_demo.go` – widget usage in a small form.
- `texelui/adapter` – bridging TexelUI into `texel.App`.
- `apps/texelterm/term.go` – pipeline composition and BEL-triggered effects.
- `registry/` – manifest parsing and wrapper factories.
