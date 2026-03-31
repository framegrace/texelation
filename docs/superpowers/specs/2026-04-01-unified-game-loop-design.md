# Unified Game Loop — Fixed-Timestep Client Rendering

**Date**: 2026-04-01
**Status**: Approved

## Overview

Replace the client render loop's competing timing mechanisms (animation ticker, `requestFrame()` chains, drain logic, `pendingRender`/`AfterFunc` scheduling) with a single fixed-timestep game loop. Animations and effects advance by a constant delta each tick. Data-driven events (deltas, input) render immediately without advancing time.

## Problem

The current render loop has two timing mechanisms that don't coordinate:
1. `animTicker` — a `time.Ticker` for DynamicColor animations
2. `requestFrame()` — one-shot `AfterFunc(16ms)` chains for effects

These compete through `renderCh` with drain logic and frame limiting, causing unpredictable frame rates, timer races, and complex control flow (`pendingRender`, `AfterFunc` rescheduling, drain loops).

## Design

### Loop Structure

Two paths trigger rendering:

1. **Tick path** (60fps when animating) — advances time by fixed `dt`, updates effects, renders
2. **Data path** (event-driven) — delta/snapshot/input arrives, renders immediately with zero time advance

```
const dt = 16 * time.Millisecond   // ~60fps

var tickAccum float64  // accumulated time in seconds (high precision)
var ticker *time.Ticker

for {
    animating := state.dynAnimating || effects.HasActiveAnimations()
    if animating && ticker == nil:  start ticker
    if !animating && ticker != nil: stop ticker

    select {
    case <-tickerCh:
        tickAccum += dt.Seconds()
        ctx.T = float32(tickAccum)
        ctx.DT = float32(dt.Seconds())
        effects.Update(dt)
        render(state, screen)

    case <-renderCh:
        ctx.DT = 0   // no time advance
        effects.Update(0)
        render(state, screen)

    case ev := <-events:
        handle(ev)

    case <-doneCh:
        return
    }
}
```

### Delta-Based Time

Instead of passing absolute wall-clock time, pass the frame delta `dt` to all animation consumers:

- **Tick path**: `dt = 16.667ms` (constant)
- **Data path**: `dt = 0` (render current state, don't advance animations)

Each consumer accumulates time internally from deltas. The loop maintains a master accumulator (`tickAccum float64`) for `ctx.T`.

### ColorContext Extension

```go
type ColorContext struct {
    X, Y   int
    W, H   int
    PX, PY int
    PW, PH int
    SX, SY int
    SW, SH int
    T      float32  // accumulated time in seconds (from deltas)
    DT     float32  // delta this frame in seconds (0 for data-driven renders)
}
```

Existing animation math (`sin(T * speedHz)`) works unchanged — `T` accumulates from deltas instead of wall clock.

### Effects Manager Changes

**Remove:**
- `requestFrame()` method and `frameTimer *time.Timer`
- `frameMu sync.Mutex` (only used by requestFrame)
- `RequestFrame()` export
- `renderCh` field (effects no longer send to it)

**Change:**
- `Update(now time.Time)` → `Update(dt time.Duration)`
- Each effect's `Update(dt)` advances its internal timeline by `dt`
- `HandleTrigger` no longer calls `requestFrame()`

**Add:**
- `HasActiveAnimations() bool` — returns true if any pane OR workspace effect is active
- Wake signal: when an effect activates (0→1 active), send to a `wakeCh chan struct{}` so the loop starts the ticker. Single buffered channel, non-blocking send.

### Animation Timeline Changes

The `texelui/animation.Timeline` currently uses `time.Time` for `startTime` and computes elapsed via `now.Sub(startTime)`. With delta-based updates:

- `Update(dt)` advances each animation's elapsed time by `dt`
- `AnimateTo` records the current elapsed time as start, not wall clock
- `computeValue` uses accumulated elapsed, not `now.Sub(startTime)`

This is a **future change** in texelui. For now, the effects manager converts: it maintains a synthetic clock (`effectsClock time.Time`) that advances by `dt` each update, and passes it to `Timeline.Update(effectsClock)`. The Timeline sees evenly-spaced timestamps. No Timeline API changes needed.

### Render Function Changes

- `render(state, screen)` signature unchanged
- `ctx.T` read from `state.tickAccum` (float32 cast of the float64 accumulator)
- `ctx.DT` read from `state.frameDT`
- `state.animStart` removed

### What Gets Removed

| Component | Location | Reason |
|-----------|----------|--------|
| `animTicker` | `app.go` | Replaced by unified ticker |
| `pendingRender` | `app.go` | No more frame limiting needed |
| `drainLoop` | `app.go` | No competing signals to drain |
| `AfterFunc` scheduling | `app.go` | No deferred renders |
| `requestFrame()` | `manager.go` | Ticker drives frames |
| `frameTimer` | `manager.go` | Removed with requestFrame |
| `frameMu` | `manager.go` | Removed with requestFrame |
| `RequestFrame()` | `manager.go` | Removed with requestFrame |
| `renderCh` on Manager | `manager.go` | Effects use wakeCh instead |
| `AttachRenderChannel()` | `manager.go` | Replaced by wake channel |
| `state.animStart` | `client_state.go` | Replaced by tickAccum |

### What Stays

- `renderCh` on the loop — data-driven wake-ups (deltas, snapshots)
- `fullRender` / `incrementalComposite` / `diffAndShow` — rendering logic unchanged
- `dynAnimating` flag — set by renderer, checked by loop
- `HasActivePaneEffects()` / `HasActiveWorkspaceEffects()` — used by renderer for full-render fallback
- `PaneStateTriggerTimestamp()` / `FinishInitialization()` — initial connect snapping

### Ticker Lifecycle

- **Start**: `dynAnimating` becomes true OR effects wake signal received
- **Stop**: `!dynAnimating && !effects.HasActiveAnimations()` — checked each loop iteration
- **Idle**: no ticker running, no CPU. Only `renderCh`, `events`, and `wakeCh` channels active in select.

### PaneStateTriggerTimestamp Adaptation

Currently uses wall-clock past timestamp for init snapping. With delta-based effects, the synthetic `effectsClock` starts at a past value and the init triggers use it. After `FinishInitialization()`, triggers use the current `effectsClock`. Same behavior, different clock source.

### Files Changed

| File | Change |
|------|--------|
| `internal/runtime/client/app.go` | New game loop replacing old select with unified ticker |
| `internal/runtime/client/renderer.go` | Use `state.tickAccum` for `ctx.T`, remove `animStart` usage |
| `internal/runtime/client/client_state.go` | Remove `animStart`, add `tickAccum float64`, `frameDT float32` |
| `internal/effects/manager.go` | Remove `requestFrame`/`frameTimer`/`frameMu`/`renderCh`, `Update(dt)`, add `HasActiveAnimations`, wake channel |
| `texelui/color/dynamic.go` | Add `DT float32` to `ColorContext` |

### Future: Unified Tick in texelui

The fixed-timestep animation tick is a generic concept that could live in texelui rather than texelation. A future project could move the tick loop into the UI library so both standalone texelui apps and the texelation client share the same timing infrastructure. Standalone apps would get deterministic animations for free. This spec scopes to texelation client only — the texelui migration is a separate project.

### Expected Impact

- **Idle**: zero CPU (no ticker, no timers)
- **Animating**: exactly 60 ticks/second, deterministic time advance
- **Typing**: immediate render on delta, no latency added
- **System lag**: animations catch up smoothly (multiple ticks if needed) rather than jumping
- **Testability**: advance clock manually — "after 10 ticks, fade is at 50%"
