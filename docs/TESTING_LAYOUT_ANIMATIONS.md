# Testing Layout Animations (Server-Side)

## Quick Start

Layout animations are **server-side** and run at ~60fps. Configure them in your theme and validate that streamed snapshots/deltas drive the expected motion on the client.

### Configuration

Edit `~/.config/texelation/theme.json`:

```json
{
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 200,
    "easing": "smoothstep"
  }
}
```

### Build and Run

```bash
make clean
make build

# Terminal 1: Start server
./bin/texel-server

# Terminal 2: Start client
./bin/texel-client
```

## What to Test

### 1. Horizontal Split Animation
**Default binding**: `Ctrl+Space` then `s` (split horizontal)

**Expected behavior**:
- New pane grows from a tiny ratio to its final size over ~200ms
- Existing pane shrinks smoothly to make room
- Borders stay aligned; content remains visible
- Snapshots stream during the animation (watch server logs)

### 2. Vertical Split Animation
**Default binding**: `Ctrl+Space` then `v` (split vertical)

**Expected behavior**:
- New pane grows from the side over ~200ms
- Sibling pane shrinks proportionally
- Motion looks continuous even with multiple nested splits

### 3. Pane Close Animation
**Default binding**: `Ctrl+Space` then `x`

**Expected behavior**:
- Closing pane shrinks to near-zero before removal callback fires
- Remaining pane(s) expand smoothly into freed space

### 4. Complex Trees
Create several splits and close panes in different branches. Ensure animations remain smooth and snapshots keep clients in sync.

## Configuration Options

- **duration_ms**: Increase for slower motion (250-320); decrease for snappier (120-180).
- **easing**: Try `"linear"`, `"smoothstep"`, `"ease-in-out"`, or `"spring"` for bounce.
- **enabled**: Set to `false` for instant transitions.
- **min_threshold**: Parsed but currently unused; omit for now.

## Technical Details

- `LayoutTransitionManager` drives animations and broadcasts snapshots/deltas each tick.
- Ticker runs at ~60fps; a grace period skips animations during startup/restore.
- Theme reload (`SIGHUP`) updates duration/easing live.
- Animations end early when a closing pane shrinks to near-zero.

## Troubleshooting

- **Splits are instant**: Ensure `layout_transitions.enabled` is `true` and the server reloaded the theme (`SIGHUP` or restart).
- **Motion is choppy**: Lower `duration_ms` or switch to `"ease-in-out"`; verify server isn’t under heavy load.
- **Too slow**: Drop `duration_ms` to ~150.
- **Small movements feel unnecessary**: Temporarily disable animations; `min_threshold` is not wired up yet.

## See Also

- `docs/LAYOUT_TRANSITIONS_CONFIG.md` – configuration reference
- `texel/layout_transitions.go` – implementation details
