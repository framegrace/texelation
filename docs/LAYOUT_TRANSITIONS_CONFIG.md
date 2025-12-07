# Layout Transition Animations (Client-Side)

## Overview

Layout transitions animate smooth changes when panes are split, removed, or resized. These animations run entirely **client-side** for maximum performance and minimal network overhead.

## Configuration

Add the `layout_transitions` section to your theme JSON file:

```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 200,
    "easing": "smoothstep",
    "min_threshold": 3
  }
}
```

### Configuration Options

#### `enabled` (boolean, default: `true`)
Enable or disable layout transition animations.
- `true`: Smooth animations when layout changes
- `false`: Instant layout updates (no animation)

#### `duration_ms` (number, default: `200`)
Animation duration in milliseconds.
- **Range**: 50-1000ms recommended
- **Examples**:
  - `100`: Fast, snappy animations
  - `200`: Smooth, balanced (recommended)
  - `300`: Slow, deliberate animations

#### `easing` (string, default: `"smoothstep"`)
Easing function for animation timing.

Available options:
- `"linear"`: Constant speed throughout
- `"smoothstep"`: Smooth acceleration and deceleration (recommended)
- `"ease-in"`: Start slow, end fast
- `"ease-out"`: Start fast, end slow
- `"ease-in-out"`: Slow start and end, fast middle

#### `min_threshold` (number, default: `3`)
Minimum size change (in cells) to trigger animation.
- Changes smaller than this threshold are instant
- Prevents jittery animations on minor adjustments
- **Range**: 1-10 cells recommended

## Examples

### Disabled (Instant Splits)
```json
{
  "layout_transitions": {
    "enabled": false
  }
}
```

### Fast & Snappy
```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 100,
    "easing": "ease-out",
    "min_threshold": 2
  }
}
```

### Slow & Smooth
```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 300,
    "easing": "smoothstep",
    "min_threshold": 5
  }
}
```

### Linear (No Easing)
```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 200,
    "easing": "linear",
    "min_threshold": 3
  }
}
```

## Complete Theme Example

```json
{
  "desktop": {
    "default_fg": "#E0E0E0",
    "default_bg": "#1E1E1E"
  },
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 200,
    "easing": "smoothstep",
    "min_threshold": 3
  },
  "effects": {
    "bindings": [
      {"event": "pane.active", "target": "pane", "effect": "fadeTint"},
      {"event": "workspace.control", "target": "workspace", "effect": "rainbow"}
    ]
  }
}
```

## How It Works

1. **Server sends ONE snapshot** with final layout after split/close
2. **Client detects changes** between old and new tree snapshots
3. **Client animates locally** at 60fps for the configured duration
4. **No network overhead** during animation (all computation client-side)

### Benefits vs Server-Side Animation
- ✅ **Lower latency**: No waiting for network round-trips
- ✅ **Better performance**: ~1 message instead of ~12 per animation
- ✅ **Smoother**: Client controls frame timing precisely
- ✅ **Consistent**: Matches how visual effects work

## Technical Details

- Runs at 60fps (16ms per frame)
- Uses smoothstep easing by default for natural motion
- Interpolates pane positions and sizes independently
- Automatically stops when animation completes
- Minimal CPU impact (single interpolation pass per frame)

## Troubleshooting

**Animations feel too fast:**
- Increase `duration_ms` (try 250 or 300)

**Animations feel sluggish:**
- Decrease `duration_ms` (try 150 or 100)
- Try `"ease-out"` easing for faster finish

**Small movements are annoying:**
- Increase `min_threshold` (try 5 or 6)

**Want instant splits:**
- Set `enabled: false`
