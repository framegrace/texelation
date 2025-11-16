# Cards Control Bus

Texel cards now expose a control bus so that applications can interact with
post-processing layers without reaching into card internals. Each pipeline owns
one control bus.

## Key Concepts

- **ControlBus**: retrieved from `*cards.Pipeline` via `ControlBus()`. Apps can
  trigger existing controls, inspect available capabilities, or register new
  handlers.
- **Capabilities**: a list of `{id, description}` pairs advertising triggers
  registered by cards. Use `bus.Capabilities()` to discover what an app can call.
- **Triggers**: invoke `bus.Trigger(id, payload)` to activate a control. Payloads
  are optional and type-specific.

## Registering Controls

Cards that want to publish controls implement `cards.ControllableCard`:

```go
func (c *DiagnosticsCard) RegisterControls(reg cards.ControlRegistry) error {
    return reg.Register("diagnostics.toggle", "Toggle diagnostics overlay", func(_ interface{}) error {
        c.Toggle()
        return nil
    })
}
```

`RegisterControls` is called automatically when the pipeline is constructed. The
registry will reject duplicate identifiers. Built-in visual effects created via
`cards.NewEffectCard` already register an `effects.<id>` trigger (for example
`cards.FlashTriggerID`).

Apps can also register their own controls on the bus, for example to expose
custom toggles while still delegating to shared effects:

```go
bus := pipeline.ControlBus()
if err := bus.Register("terminal.toggle-autoscroll", "Toggle terminal autoscroll", toggleFn); err != nil {
    log.Printf("register control failed: %v", err)
}
```

## Example: Custom Function-Key Toggle

You can layer multiple cards and expose custom toggles without touching card
internals. The example below intercepts `F12`, toggles a diagnostics card via
the bus, and forwards other keys through the pipeline unchanged:

```go
// import (
//     "texelation/internal/effects"
//     "texelation/texel/cards"
// )
flash, _ := cards.NewEffectCard("flash", effects.EffectConfig{
    "duration_ms": 120,
    "color":       "#FFFFFF",
})
diag := NewDiagnosticsCard() // implements cards.ControllableCard
var pipe *cards.Pipeline
pipe = cards.NewPipeline(func(ev *tcell.EventKey) bool {
    if ev.Key() == tcell.KeyF12 {
        if err := pipe.ControlBus().Trigger("diagnostics.toggle", nil); err != nil {
            log.Printf("toggle failed: %v", err)
        }
        return true
    }
    return false
}, cards.WrapApp(app), diag, flash)
```

`diag` registers the `diagnostics.toggle` capability inside `RegisterControls`.
The control function intercepts the key, triggers the card, and leaves the rest
of the stack untouched. Apps can compose additional effects the same way while
keeping the control surface self-documenting via `Capabilities()`.
