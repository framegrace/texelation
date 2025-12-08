# Layout Transition Animations (Server-Side)

## Overview

Layout transitions animate smooth changes when panes are split or closed. Animations run on the **server**: `LayoutTransitionManager` interpolates split ratios at ~60fps, recalculates layout, and streams snapshots/deltas to connected clients during the transition.

## Configuration

Add the `layout_transitions` section to your theme JSON file:

```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 200,
    "easing": "smoothstep"
  }
}
```

### Configuration Options

#### `enabled` (boolean, default: `true`)
Enable or disable layout transition animations.
- `true`: Smooth animations when layout changes
- `false`: Instant layout updates (no animation)

#### `duration_ms` (number, default: `300`)
Animation duration in milliseconds.
- **Range**: 50-1000ms recommended
- **Examples**:
  - `100`: Fast, snappy animations
  - `200`: Smooth, balanced
  - `300`: Default, deliberate motion

#### `easing` (string, default: `"smoothstep"`)
Easing function for animation timing.

Available options:
- `"linear"`: Constant speed throughout
- `"smoothstep"`: Smooth acceleration and deceleration (default)
- `"ease-in-out"`: Slow start/end, fast middle
- `"spring"`: Bouncy overshoot

#### `min_threshold` (reserved)
This key is parsed from the theme but **not applied yet**. Keep it omitted until the threshold fast-path is implemented.

## Examples

### Disabled (Instant Splits)
```json
{
  "layout_transitions": { "enabled": false }
}
```

### Fast & Snappy
```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 120,
    "easing": "ease-in-out"
  }
}
```

### Slow & Smooth
```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 320,
    "easing": "smoothstep"
  }
}
```

## How It Works

1. Workspace split/close operations seed start and target ratios.
2. `LayoutTransitionManager` lerps ratios each tick using the configured easing curve.
3. After each tick it recalculates layout and broadcasts a snapshot/delta to clients.
4. When the animation completes (or a removal shrinks to near-zero), callbacks fire and final ratios are committed.

## Technical Details

- Ticker runs at ~60fps (16ms).
- Grace period skips animations during startup/restore to avoid jank.
- Theme reload (`SIGHUP`) updates the animator config live.
- Animations stop automatically when complete; `min_threshold` is currently ignored.

## Troubleshooting

- **Animations feel too fast**: Increase `duration_ms` (try 250-320).
- **Animations feel sluggish**: Decrease `duration_ms` (try 150) or use `"ease-in-out"`.
- **Small movements feel unnecessary**: Temporarily set `enabled: false` until the min-threshold guard is implemented.
- **Want instant splits**: Set `enabled: false`.
