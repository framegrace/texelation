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
func (f *FlashCard) RegisterControls(reg cards.ControlRegistry) error {
    return reg.Register(cards.FlashTriggerID, "Activate the flash overlay", func(_ interface{}) error {
        f.Trigger()
        return nil
    })
}
```

`RegisterControls` is called automatically when the pipeline is constructed. The
registry will reject duplicate identifiers.

Apps can also register their own controls on the bus, for example to expose
custom toggles while still delegating to shared effects:

```go
bus := pipeline.ControlBus()
if err := bus.Register("terminal.toggle-autoscroll", "Toggle terminal autoscroll", toggleFn); err != nil {
    log.Printf("register control failed: %v", err)
}
```

## Triggering Effects

The texelterm app triggers a flash overlay when it receives a BEL sequence:

```go
func (t *TexelTerm) onBell() {
    if bus := t.bus; bus != nil {
        _ = bus.Trigger(cards.FlashTriggerID, nil)
    }
}
```

Because the flash card registers `effects.flash`, the app never needs to reach
into the card to toggle state directly. Developers can follow the same pattern
for future overlays or behavioural hooks.

## Example: Custom Function-Key Toggle

You can layer multiple cards and expose custom toggles without touching card
internals. The example below intercepts `F12`, toggles a diagnostics card via
the bus, and forwards other keys through the pipeline unchanged:

```go
flash := cards.NewFlashCard(120*time.Millisecond, tcell.ColorWhite)
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
