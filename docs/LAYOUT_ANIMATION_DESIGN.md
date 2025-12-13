# Layout Animation Architecture

This document describes how Texelation animates pane splits and closes with smooth transitions.

## Overview

Layout transitions run entirely on the **server**. When panes split or close, the server animates the split ratios over time, broadcasting tree snapshots at ~60fps. Clients simply render each snapshot as it arrives.

```
┌─────────────────────────────────────────────────────────────────┐
│                    LAYOUT ANIMATION FLOW                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  User Action (split/close)                                      │
│         │                                                       │
│         ▼                                                       │
│  LayoutTransitionManager                                        │
│    - Sets start and target ratios                               │
│    - Starts 60fps ticker                                        │
│         │                                                       │
│         ▼ (each tick)                                           │
│  Interpolate ratios using easing function                       │
│         │                                                       │
│         ▼                                                       │
│  recalculateLayout() → broadcast snapshot to clients            │
│         │                                                       │
│         ▼ (animation complete)                                  │
│  Execute callback (e.g., remove pane after close animation)     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## How It Works

### Split Animation

When a pane splits:
1. New pane starts at 1% of the space, existing pane at 99%
2. Ratios animate toward final values (e.g., 50/50)
3. Each frame updates the tree and broadcasts to clients

```
Start: [0.99, 0.01]  →  End: [0.50, 0.50]
```

### Close Animation

When a pane closes:
1. Closing pane shrinks from current size toward 1%
2. Sibling panes grow to fill the space
3. After animation completes, pane is actually removed from tree

```
Start: [0.50, 0.50]  →  End: [0.99, 0.01]  →  Remove pane
```

## Configuration

Configure in `~/.config/texelation/theme.json`:

```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 200,
    "easing": "smoothstep"
  }
}
```

### Options

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `true` | Enable/disable animations |
| `duration_ms` | `300` | Animation duration in milliseconds |
| `easing` | `"smoothstep"` | Easing function for timing |

### Easing Functions

- `linear` - Constant speed
- `smoothstep` - Smooth acceleration and deceleration (default)
- `ease-in-out` - Slow start/end, fast middle
- `spring` - Bouncy overshoot effect

### Hot Reload

Configuration reloads on `SIGHUP`:
```bash
kill -HUP $(pidof texel-server)
```

## Implementation

### Core Components

**LayoutTransitionManager** (`texel/layout_transitions.go`):
- Manages active animations
- Runs 60fps ticker during animations
- Interpolates ratios using configured easing
- Triggers layout recalculation and broadcast each frame

**Timeline** (`internal/effects/timeline.go`):
- Shared animation primitive used by both effects and layout
- All methods take explicit `time.Time` parameter for frame synchronization
- Prevents jitter from multiple `time.Now()` calls per frame

### Integration Points

- `texel/workspace.go` - Calls `AnimateSplit()` and `AnimateRemoval()`
- `texel/desktop_engine_core.go` - Initializes manager, parses theme config
- `texel/tree.go` - Stores animated `SplitRatios` in tree nodes

### Grace Period

During server startup and snapshot restore, a grace period skips animations to avoid visual noise. This ensures restored layouts appear instantly.

## Files

- `texel/layout_transitions.go` - LayoutTransitionManager (~200 lines)
- `texel/workspace.go` - Split/close hooks into animator
- `texel/desktop_engine_core.go` - Config parsing and initialization
- `internal/effects/timeline.go` - Shared animation timeline

## Design Rationale

**Why server-side?**
- Server owns authoritative tree state
- Clients remain stateless thin renderers
- Borders render correctly at each animated position
- Reuses existing snapshot broadcast mechanism

**Why not an Effect?**
- Layout animation modifies tree structure, not visual overlay
- Keeps separation of concerns (effects = visual, layout = structural)
- Simpler implementation with fewer moving parts

## Future Enhancements

- Interrupt animations when new split/close arrives mid-flight
- Animate workspace switches (fade/slide)
- Animate pane swaps
