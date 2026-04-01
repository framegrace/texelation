# Render CPU Optimizations

**Date**: 2026-04-01
**Status**: Approved

## Overview

Targeted CPU optimizations across the rendering pipeline: gradient computation, DynamicColor resolution caching, buffer allocation pooling, and effects manager overhead reduction.

## Optimizations

### 1. Cache DynamicColor Reconstruction (renderer)

`FromDesc(protocolDescToColor(cell.DynBG)).Resolve(ctx)` runs per-cell per-frame. Most cells share a few unique descriptors (one gradient, one Pulse). Cache the `color.DynamicColor` by descriptor identity using a small fixed-size array in the compositing functions. Linear scan of ~8 entries beats a map for small N.

Key type for cache: the fixed fields of `DynColorDesc` (Type, Base, Target, Easing, Speed, Min, Max) as a struct. For gradients, also match stop count + first stop type (sufficient to distinguish unique gradients in practice).

Cache scope: per-render call (cleared each frame). No invalidation needed.

### 2. Pre-compute Gradient Trig (texelui)

`Build()` closures compute `math.Sin(rad)` and `math.Cos(rad)` per-cell. Pre-compute in `Build()` and capture in closure. Also pre-compute radians from angle.

### 3. Pre-allocate resolveStops Buffers (texelui)

Dynamic gradient closures allocate `[]ColorStop` and `[]oklchStop` per-cell via `resolveStops()`. Pre-allocate in `Build()` and reuse in closure. Thread-safe because each DynamicColor is resolved in one goroutine.

### 4. Pre-compute normalizedCoords (texelui)

`normalizedCoords()` has a switch + `math.Max` per-cell. Specialize per coord source in `Build()`.

### 5. Inline clampFloat (texelui)

Function call per-cell for a trivial 3-line operation. Inline at call sites.

### 6. Pre-compute Pulse Constants (texelui)

`mid` and `amp` computed from `min`/`max` every call. Capture in closure.

### 7. Two-Buffer Swap for Diff (renderer)

`render()` copies entire `prevBuffer` for diffing. Use two buffers and swap pointers. Eliminates full-screen memcpy per incremental render.

### 8. Pool fullRender Workspace Buffer (renderer)

`fullRender()` allocates a new `workspaceBuffer` every call. Keep on `clientState` and reuse.

### 9. Cache Effect Active State (effects manager)

`HasActiveAnimations()`/`HasActivePaneEffects()`/`HasActiveWorkspaceEffects()` iterate all effects every frame. Cache the result during `Update()`.

### 10. Reduce Manager Lock/Alloc (effects manager)

`Update()`/`Apply*()` copy effect slices every call. Effects don't change at runtime — use direct iteration under RLock without copying. Combine redundant lock acquisitions in `HandleTrigger`.

### 11. Pool Screensaver Fade Buffer (effects)

Screensaver fade allocates full-screen buffer copy every frame. Pool it on the effect struct.

### 12. Keyflash Fallback Colors (effects)

Fallback colors computed per-cell. Move outside loop.

## Files Changed

### texelui
| File | Changes |
|------|---------|
| `color/gradient.go` | Pre-compute trig, inline clamp, pre-alloc resolveStops, specialize normalizedCoords |
| `color/dynamic.go` | Pre-compute Pulse mid/amp |

### texelation
| File | Changes |
|------|---------|
| `internal/runtime/client/renderer.go` | DynColor cache, two-buffer swap, pool workspace buffer |
| `internal/runtime/client/client_state.go` | Add workspace buffer pool, second buffer |
| `internal/effects/manager.go` | Cache active state, reduce lock/alloc |
| `internal/effects/screensaver_fade.go` | Pool snapshot buffer |
| `internal/effects/keyflash.go` | Move fallback colors out of loop |
