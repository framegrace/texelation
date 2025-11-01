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
