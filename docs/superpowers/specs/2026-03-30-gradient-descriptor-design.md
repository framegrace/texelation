# Gradient Descriptor for DynamicColorDesc

**Date**: 2026-03-30
**Status**: Approved

## Overview

Extend `DynamicColorDesc` to support linear and radial gradients as serializable descriptors. Gradient stops may reference animated DynamicColors (Pulse, Fade). This allows gradients to travel across the protocol and be reconstructed on the client side, completing the client-side DynamicColor resolution system.

## Problem

`GradientBuilder.Build()` returns `Func(...)` which produces `DescTypeNone` — the gradient cannot be serialized. Widgets using gradients with animated stop colors (e.g. BlendInfoLine with a Pulse accent) lose animation when rendered client-side. The server snapshots the gradient once and the client sees a frozen image.

## Design

### New Descriptor Types

```go
const (
    DescTypeNone       uint8 = 0
    DescTypeSolid      uint8 = 1
    DescTypePulse      uint8 = 2
    DescTypeFade       uint8 = 3
    DescTypeLinearGrad uint8 = 4
    DescTypeRadialGrad uint8 = 5
)
```

### Extended DynamicColorDesc

```go
type DynamicColorDesc struct {
    Type   uint8
    Base   uint32
    Target uint32
    Easing uint8
    Speed  float32
    Min    float32
    Max    float32
    Stops  []GradientStopDesc // nil for non-gradient types
}

type GradientStopDesc struct {
    Position float32
    Color    DynamicColorDesc // Solid, Pulse, or Fade — no nested gradients
}
```

### Field Reuse for Gradients

| Field | Linear Gradient | Radial Gradient |
|-------|----------------|-----------------|
| `Base` | Angle as `math.Float32bits(angleDeg)` | Unused (0) |
| `Target` | Unused (0) | CY as `math.Float32bits(cy)` |
| `Easing` | Coord source (0=screen, 1=pane, 2=local) | Coord source |
| `Speed` | Unused (0) | CX |
| `Min` | Unused (0) | Unused (0) |
| `Max` | Unused (0) | Unused (0) |
| `Stops` | 2+ GradientStopDesc | 2+ GradientStopDesc |

### IsAnimated

A gradient is animated if any stop is animated:

```go
func (d DynamicColorDesc) IsAnimated() bool {
    if d.Type >= DescTypePulse && d.Type <= DescTypeFade {
        return true
    }
    for _, s := range d.Stops {
        if s.Color.IsAnimated() {
            return true
        }
    }
    return false
}
```

### GradientBuilder.Build() Changes

`Build()` stores the gradient configuration in `desc` instead of (only) returning a `Func` closure:

1. For each `ColorStop`:
   - If `Dynamic` is set: use `Dynamic.Describe()` as the stop's `DynamicColorDesc`
   - If only `Color` is set: create `DescTypeSolid` with `PackRGB`
2. Set `desc.Type` to `DescTypeLinearGrad` or `DescTypeRadialGrad`
3. Pack angle/center/coord-source into the reused fields
4. Store the stop descriptors in `desc.Stops`
5. Still create the `Func` closure for direct use (standalone mode)

The DynamicColor returned has both `fn` (for local resolution) AND `desc` (for serialization).

### FromDesc() Reconstruction

```go
case DescTypeLinearGrad:
    angleDeg := math.Float32frombits(uint32(d.Base))
    source := coordSource(d.Easing)
    stops := make([]ColorStop, len(d.Stops))
    for i, sd := range d.Stops {
        stops[i] = ColorStop{
            Position: sd.Position,
            Dynamic:  FromDesc(sd.Color),
        }
    }
    gb := Linear(angleDeg, stops...).withSource(source)
    return gb.Build()

case DescTypeRadialGrad:
    cx := d.Speed
    cy := math.Float32frombits(uint32(d.Target))
    source := coordSource(d.Easing)
    // same stop reconstruction as linear
    gb := Radial(cx, cy, stops...).withSource(source)
    return gb.Build()
```

### Protocol Encoding

Same `AttrHasDynamic` flag triggers reading dynamic data. When a `DynColorDesc` has `Type >= 4` (gradient), the encoding after the fixed 22 bytes is:

```
[fixed 22 bytes: Type, Base, Target, Easing, Speed, Min, Max]
[uint8: stop count N]
[N times:
    float32: position (4 bytes)
    fixed 22 bytes: stop's DynamicColorDesc (always Solid/Pulse/Fade)
]
```

Each stop's color descriptor is always fixed-size (no nested gradients), so the extra bytes per gradient descriptor are predictable: `1 + N * 26`.

Static styles (no `AttrHasDynamic` flag) are encoded identically to today — zero overhead.

### Protocol Decoding

After reading the fixed 22 bytes, if `Type >= 4`:

```go
stopCount := b[0]
b = b[1:]
for i := 0; i < int(stopCount); i++ {
    position := math.Float32frombits(binary.LittleEndian.Uint32(b[:4]))
    b = b[4:]
    // read 22 bytes as DynColorDesc (same as existing decode)
    stopDesc := decodeDynColorDesc(b[:22])
    b = b[22:]
    desc.Stops = append(desc.Stops, GradientStopDesc{Position: position, Color: stopDesc})
}
```

### Widget Changes

None. `BlendInfoLine`, `TabBar`, and all other widgets keep using `color.Linear(...)`, `color.Radial(...)`, and `color.Stop(...)` exactly as today. The `Build()` method now stores a descriptor alongside the closure. Everything flows through the existing `SetDynamicCell` → Painter → Publisher → Protocol → Client path transparently.

### Client Resolution

In `compositeInto`, when a cell has `DynFG.Type >= 4` or `DynBG.Type >= 4`:

```go
dc := color.FromDesc(protocolDescToColor(cell.DynBG))
bg = dc.Resolve(ctx) // gradient resolves spatially + stops resolve temporally
```

`FromDesc` reconstructs the `GradientBuilder` with `DynStop` entries for animated stops. `Build()` creates the `Func` closure. `Resolve(ctx)` computes the gradient with OKLCH interpolation, resolving each stop's DynamicColor at the current `ctx.T`. Animated stops (Pulse) oscillate; static stops are constant.

### Backward Compatibility

Old clients that don't understand gradient descriptors see the static resolved colors in `Cell.Style` (the Painter always stores a resolved snapshot). The `AttrHasDynamic` flag and `Type >= 4` values are simply unknown to old decoders — they should skip the extra bytes based on protocol version negotiation (existing mechanism).

## Files Changed

### texelui

| File | Change |
|------|--------|
| `color/dynamic.go` | Add `DescTypeLinearGrad`, `DescTypeRadialGrad`, `GradientStopDesc`, update `IsAnimated()`, update `FromDesc()` |
| `color/gradient.go` | `Build()` stores desc with stops, remove `DynStop`/`hasDynamicStops`/`resolveStops` (no longer needed as separate helpers) |

### texelation

| File | Change |
|------|--------|
| `protocol/buffer_delta.go` | Encode/decode gradient stops after fixed DynColorDesc bytes |
| `internal/runtime/server/desktop_publisher.go` | `convertCell` copies `Stops` slice from cell descriptor to protocol descriptor |
| `internal/runtime/client/renderer.go` | `protocolDescToColor` copies `Stops` slice |
| `apps/statusbar/blend_info_line.go` | Revert to original single-gradient code (no longer needs split) |
