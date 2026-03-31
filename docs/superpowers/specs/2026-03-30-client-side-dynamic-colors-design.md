# Client-Side DynamicColor Resolution

**Date**: 2026-03-30
**Status**: Approved

## Overview

Migrate DynamicColor animation from server-side rendering (which generates per-frame buffer deltas flooding the client) to client-side resolution. The server sends a serializable descriptor of the animation intent. The client reconstructs and resolves the DynamicColor locally each frame with zero protocol overhead for static cells.

## Problem

Animated DynamicColors (e.g. tab pulsation) cause the server to produce different cell content every frame (~60fps). Each frame generates a BufferDelta sent over the protocol. The client renders each delta, starving keyboard input. Profiling showed the FPS counter alone caused 85% of all allocations (6.7GB) via cloneBuffer and 51% CPU in GC.

## Design

### DynamicColorDesc — Serializable Descriptor

```go
// In texelui/color/dynamic.go
type DynamicColorDesc struct {
    Type   uint8   // 0=none, 1=solid, 2=pulse, 3=fade
    Base   uint32  // RGB packed base color
    Target uint32  // RGB packed target (for fade/gradient)
    Easing uint8   // index into shared easing table
    Speed  float32 // oscillations/sec (pulse) or duration in seconds (fade)
    Min    float32 // min scale factor (pulse: 0.7)
    Max    float32 // max scale factor (pulse: 1.0)
}
```

Type constants:
- `0` — none (zero value, cell is static)
- `1` — solid (static color, used for non-animated DynamicColors that were set via `Solid()`)
- `2` — pulse (oscillate brightness between Min and Max scale of Base, at Speed Hz)
- `3` — fade (interpolate from Base to Target over Speed seconds, one-shot)

Easing indices map to `texelui/animation` functions: `0=linear, 1=smoothstep, 2=smootherstep, 3=ease-in-quad, 4=ease-out-quad, 5=ease-in-out-quad, 6=ease-in-cubic, 7=ease-out-cubic, 8=ease-in-out-cubic`. Table is shared code, both server and client reference the same mapping.

### Cell Extension

```go
// In texelui/core/cell.go
type Cell struct {
    Ch    rune
    Style tcell.Style
    DynFG color.DynamicColorDesc // zero Type = use Style fg
    DynBG color.DynamicColorDesc // zero Type = use Style bg
}
```

Zero-value `DynamicColorDesc` (Type=0) means the cell is static. Existing code creating cells with `Cell{Ch: 'x', Style: s}` works unchanged. Only cells with animated colors carry descriptors.

### Named DynamicColor Constructors

Replace raw `Func()` closures with named constructors that store descriptors:

```go
// In texelui/color/dynamic.go

// Pulse creates an oscillating brightness animation.
func Pulse(base tcell.Color, min, max, speedHz float32) DynamicColor

// Fade creates a one-shot color transition.
func Fade(from, to tcell.Color, easing string, durationSec float32) DynamicColor

// Solid remains unchanged — returns a static DynamicColor.
func Solid(c tcell.Color) DynamicColor
```

These constructors store the `DynamicColorDesc` internally AND create the `Func` closure for direct use. `Describe()` returns the stored descriptor. `FromDesc(desc)` reconstructs a working `DynamicColor` from a descriptor.

The raw `Func()` constructor remains available for standalone use but cannot be serialized — `Describe()` returns a solid descriptor with the last resolved color.

### Painter Changes

When `Painter.SetDynamicCell(x, y, ch, dynamicStyle)` is called:
1. Resolve the static color as today → store in `Cell.Style`
2. Call `dynamicStyle.FG.Describe()` → store in `Cell.DynFG` (if non-static)
3. Call `dynamicStyle.BG.Describe()` → store in `Cell.DynBG` (if non-static)

Static DynamicColors (`Solid()`) produce `Type=1` descriptors. The protocol can treat `Type=1` same as `Type=0` (no animation needed) to avoid unnecessary encoding. Only `Type >= 2` carries meaningful animation data.

### Protocol Extension

`StyleEntry` in `protocol/messages.go` gets optional dynamic descriptors:

```go
type StyleEntry struct {
    AttrFlags uint16
    FgModel   ColorModel
    FgValue   uint32
    BgModel   ColorModel
    BgValue   uint32
    DynFG     DynamicColorDesc // zero = static
    DynBG     DynamicColorDesc // zero = static
}
```

Encoding: static styles (DynFG.Type <= 1 and DynBG.Type <= 1) are encoded identically to today — zero overhead. Animated styles carry the descriptor bytes after the existing fields. A flag bit in `AttrFlags` indicates presence of dynamic data.

Backward compatibility: old clients ignore the extra bytes (protocol version check). They see the static resolved colors.

### Server-Side Changes

1. **UIManager**: Add `ClientSideAnimations bool` flag. When true, `scheduleAnimationRefreshLocked` does NOT fire — the client handles animation refresh. Standalone mode keeps it false (UIManager drives animation directly).

2. **Publisher**: `convertStyle()` copies `DynFG`/`DynBG` from `Cell` to `StyleEntry`. No other changes — the publisher already sends whatever cells contain.

3. **Status bar**: Replace `makePulse()` closure with `color.Pulse(base, 0.7, 1.0, 6)`. The tab pulsation uses the named constructor. Server resolves it once per frame (static snapshot); client animates.

### Client-Side Resolution

In `internal/runtime/client/renderer.go`, during cell compositing:

```go
for col := 0; col < w; col++ {
    cell := row[col]
    style := cell.Style

    // Resolve dynamic colors if present
    if cell.DynBG.Type >= 2 {
        ctx := color.ColorContext{
            X: col, Y: rowIdx,
            W: w, H: h,
            PX: pane.Rect.X, PY: pane.Rect.Y,
            PW: pane.Rect.Width, PH: pane.Rect.Height,
            SX: targetX, SY: targetY,
            SW: screenWidth, SH: screenHeight,
            T: animTime,
        }
        bg := color.FromDesc(cell.DynBG).Resolve(ctx)
        fg, _, attrs := style.Decompose()
        if cell.DynFG.Type >= 2 {
            fg = color.FromDesc(cell.DynFG).Resolve(ctx)
        }
        style = tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attrs)
        hasDynamic = true
    } else if cell.DynFG.Type >= 2 {
        ctx := color.ColorContext{...}
        fg := color.FromDesc(cell.DynFG).Resolve(ctx)
        _, bg, attrs := style.Decompose()
        style = tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attrs)
        hasDynamic = true
    }

    workspaceBuffer[targetY][targetX] = client.Cell{Ch: cell.Ch, Style: style}
}
```

After compositing all panes, if `hasDynamic` is true, request another frame:
```go
if hasDynamic {
    select {
    case renderCh <- struct{}{}:
    default:
    }
}
```

The `ColorContext` is fully populated from information the client already has:
- Cell position within pane (col, rowIdx, w, h)
- Pane position on screen (from TreeSnapshot pane geometry)
- Screen coordinates (targetX/Y computed during compositing)
- Screen size (from tcell)
- Animation time (wall clock since client start)

No context transfer from the server needed.

### Standalone Mode (UIManager)

No changes needed. UIManager's `scheduleAnimationRefreshLocked` continues to drive animation directly. The `Painter` stores both resolved color and descriptor on cells. The UIManager renders from its own buffer, resolving DynamicColors via `Func` closures as today.

The named constructors (`Pulse`, `Fade`) create working `Func` closures, so standalone apps use them identically. The `DynamicColorDesc` is simply unused in standalone mode.

### Easing Table

Shared mapping in `texelui/animation` (or `texelui/color`):

```go
var EasingByIndex = []EasingFunc{
    EaseLinear,       // 0
    EaseSmoothstep,   // 1
    EaseSmootherstep, // 2
    EaseInQuad,       // 3
    EaseOutQuad,      // 4
    EaseInOutQuad,    // 5
    EaseInCubic,      // 6
    EaseOutCubic,     // 7
    EaseInOutCubic,   // 8
}

var EasingByName = map[string]uint8{
    "linear": 0, "smoothstep": 1, "smootherstep": 2,
    "ease-in-quad": 3, "ease-out-quad": 4, "ease-in-out-quad": 5,
    "ease-in-cubic": 6, "ease-out-cubic": 7, "ease-in-out-cubic": 8,
}
```

New easings are added by appending to both tables — no protocol change needed.

## Files Changed

### texelui

| File | Change |
|------|--------|
| `color/dynamic.go` | Add `DynamicColorDesc`, `Describe()`, `FromDesc()`, `Pulse()`, `Fade()` constructors |
| `color/easing_table.go` | New: shared easing index ↔ name ↔ function mapping |
| `core/cell.go` | Add `DynFG`, `DynBG DynamicColorDesc` fields to `Cell` |
| `core/painter.go` | Store descriptors on cells in `SetDynamicCell` |
| `core/uimanager.go` | Add `ClientSideAnimations` flag to suppress animation refresh |

### texelation

| File | Change |
|------|--------|
| `protocol/messages.go` | Add `DynFG`, `DynBG` to `StyleEntry`, encode/decode with flag bit |
| `internal/runtime/server/desktop_publisher.go` | `convertStyle` copies dynamic descriptors |
| `internal/runtime/client/renderer.go` | Resolve dynamic cells with `ColorContext` each frame, request frames when animated |
| `apps/statusbar/statusbar.go` | Replace `makePulse()` with `color.Pulse()` |
| `cmd/texel-server/main.go` | Set `UIManager.ClientSideAnimations = true` on status bar |

## Migration

1. Apps using `color.Func()` for animations should migrate to `color.Pulse()` / `color.Fade()` for protocol support
2. `color.Func()` continues to work in standalone mode — just can't be serialized
3. Existing `color.Solid()` cells are unaffected (zero overhead)
4. Protocol is backward compatible — old clients see static colors
