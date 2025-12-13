# Visual Effects Architecture

This document describes how Texelation's visual effects system works - the registry, triggers, rendering pipeline, and configuration.

## Overview

Visual effects are client-side overlays that modify the rendered buffer in response to events. Effects run on the **client** (not server), applied after receiving pane/workspace buffers but before rendering to the terminal.

```
┌─────────────────────────────────────────────────────────────────┐
│                    EFFECTS PIPELINE                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Server sends buffer data                                       │
│         │                                                       │
│         ▼                                                       │
│  Client receives MsgBufferDelta / MsgTreeSnapshot               │
│         │                                                       │
│         ▼                                                       │
│  EffectManager.HandleTrigger() (state changes)                  │
│         │                                                       │
│         ▼                                                       │
│  EffectManager.Update(now) (advance animations)                 │
│         │                                                       │
│         ▼                                                       │
│  EffectManager.ApplyPaneEffects() (per-pane overlays)           │
│         │                                                       │
│         ▼                                                       │
│  EffectManager.ApplyWorkspaceEffects() (global overlays)        │
│         │                                                       │
│         ▼                                                       │
│  Render to terminal (tcell)                                     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Core Components

### Effect Interface

Every effect implements this interface (`internal/effects/interfaces.go`):

```go
type Effect interface {
    ID() string                                           // Unique identifier
    Active() bool                                         // Currently rendering?
    Update(now time.Time)                                 // Advance animations
    HandleTrigger(trigger EffectTrigger)                  // React to events
    ApplyPane(pane *PaneState, buffer [][]Cell)          // Modify pane buffer
    ApplyWorkspace(buffer [][]Cell)                       // Modify workspace buffer
}
```

### Registry

Effects register themselves at import time (`internal/effects/registry.go`):

```go
func init() {
    effects.Register("fadeTint", NewFadeTint)
    effects.Register("rainbow", NewRainbow)
    effects.Register("flash", NewFlash)
}
```

The registry maps effect IDs to factory functions that create configured instances.

### Effect Manager

The Manager coordinates all active effects (`internal/effects/manager.go`):

- **Bindings** - Maps trigger types to effects that respond to them
- **Update loop** - Advances animation timelines each frame
- **Apply methods** - Runs effects against buffers before rendering
- **Frame requests** - Triggers re-renders when animations are active

### Triggers

Events that effects can respond to (`internal/effects/events.go`):

| Trigger | Description |
|---------|-------------|
| `TriggerPaneActive` | Pane gained/lost focus |
| `TriggerPaneResizing` | Pane resize started/ended |
| `TriggerPaneKey` | Key pressed in pane |
| `TriggerWorkspaceControl` | Control mode toggled |
| `TriggerWorkspaceKey` | Key pressed in workspace |
| `TriggerWorkspaceSwitch` | Changed workspaces |
| `TriggerWorkspaceZoom` | Zoom toggled |

## Built-in Effects

### fadeTint

Dims inactive panes to highlight the focused one.

- **Target**: Pane
- **Trigger**: `pane.active`
- **Behavior**: Inactive panes get a color overlay that fades in/out

### rainbow

Animated color gradient across the workspace.

- **Target**: Workspace
- **Trigger**: `workspace.control`
- **Behavior**: Shows rainbow animation when control mode is active

### flash

Brief flash overlay, used for visual bell.

- **Target**: Workspace
- **Trigger**: Custom (via control bus)
- **Behavior**: Quick white flash that fades out

## Configuration

Effects are configured in `~/.config/texelation/theme.json`:

```json
{
  "effects": {
    "bindings": [
      {
        "event": "pane.active",
        "target": "pane",
        "effect": "fadeTint",
        "params": {
          "duration_ms": 200,
          "color": "#404040",
          "intensity": 0.3
        }
      },
      {
        "event": "workspace.control",
        "target": "workspace",
        "effect": "rainbow",
        "params": {
          "mix": 0.6
        }
      }
    ]
  }
}
```

### Binding Fields

| Field | Description |
|-------|-------------|
| `event` | Trigger type (`pane.active`, `workspace.control`, etc.) |
| `target` | Where to apply (`pane` or `workspace`) |
| `effect` | Effect ID from registry |
| `params` | Effect-specific configuration |

### Common Parameters

| Parameter | Description |
|-----------|-------------|
| `duration_ms` | Animation duration in milliseconds |
| `color` | Overlay color (hex string) |
| `intensity` | Effect strength (0.0 - 1.0) |
| `mix` | Color blend ratio (0.0 - 1.0) |

## Animation System

### Timeline

Effects use a shared Timeline helper (`internal/effects/timeline.go`) for smooth animations:

```go
// Animate a value from current to target over duration
timeline.AnimateTo("intensity", 1.0, 200*time.Millisecond, now)

// Get current interpolated value
value := timeline.Get("intensity", now)

// Check if still animating
if timeline.IsAnimating("intensity", now) {
    // Request another frame
}
```

All Timeline methods take explicit `time.Time` to prevent jitter from multiple `time.Now()` calls per frame.

### Base Classes

Pre-built base structs simplify effect implementation:

**PaneEffectBase** - For per-pane effects:
- Tracks animation state per pane ID
- Provides `Animate(paneID, target, now)` and `GetCached(paneID)`

**WorkspaceEffectBase** - For global effects:
- Single animation state for entire workspace
- Provides `Animate(target, now)` and `GetCached()`

## Card Pipeline Integration

Effects can also run in the card pipeline via `EffectCard` (`texel/cards/effect_card.go`):

```go
flash, _ := cards.NewEffectCard("flash", effects.EffectConfig{
    "duration_ms":   100,
    "color":         "#FFFFFF",
    "max_intensity": 0.75,
    "trigger_type":  "workspace.control",
})
pipeline := cards.NewPipeline(nil, cards.WrapApp(app), flash)
```

The card adapter:
- Runs effect's Timeline via internal ticker (~60fps)
- Registers with control bus as `effects.<id>`
- Converts between `texel.Cell` and `client.Cell` types

## Rendering Flow

1. **Server** sends buffer updates via `MsgBufferDelta`
2. **Client** applies deltas to `BufferCache`
3. **Triggers fire** when state changes (focus, control mode, etc.)
4. **Effects handle triggers** - start/stop animations
5. **Update loop** advances all animation timelines
6. **Apply pane effects** - modify each pane's buffer copy
7. **Apply workspace effects** - modify composited workspace buffer
8. **Render** - write final buffer to terminal

## Files

| File | Purpose |
|------|---------|
| `internal/effects/interfaces.go` | Effect interface definition |
| `internal/effects/registry.go` | Effect factory registration |
| `internal/effects/manager.go` | Coordinates effects and triggers |
| `internal/effects/events.go` | Trigger types and EffectTrigger struct |
| `internal/effects/timeline.go` | Animation timing helper |
| `internal/effects/base.go` | PaneEffectBase, WorkspaceEffectBase |
| `internal/effects/helpers.go` | Color blending utilities |
| `internal/effects/fadetint.go` | FadeTint effect |
| `internal/effects/rainbow.go` | Rainbow effect |
| `internal/effects/keyflash.go` | Flash effect |
| `texel/cards/effect_card.go` | Card pipeline adapter |

## Design Rationale

**Why client-side?**
- Effects are visual overlays, not authoritative state
- Reduces server load and network traffic
- Client controls its own render loop timing
- Different clients could have different effects

**Why a registry?**
- Effects can be added without modifying core code
- Theme configuration references effects by ID
- Card pipeline can instantiate effects dynamically
- Clean separation between effect logic and wiring

**Why triggers instead of polling?**
- Efficient - effects only activate when needed
- Predictable - clear cause/effect relationship
- Extensible - new trigger types can be added easily
