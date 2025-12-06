# Testing Layout Animations (Client-Side)

## Quick Start

Layout animations are **client-side** and run at 60fps for smooth transitions.

### Configuration

Edit `~/.config/texelation/theme.json`:

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

### Build and Run

```bash
# Build both server and client
make clean
make build

# Terminal 1: Start server
./bin/texel-server

# Terminal 2: Start client
./bin/texel-client
```

Or use the Makefile shortcuts:
```bash
# Terminal 1
make server

# Terminal 2
make client
```

## What to Test

### 1. Horizontal Split Animation
**Default binding**: `Ctrl+Space` then `s` (split horizontal)

**Expected behavior**:
- New pane should **smoothly slide in from bottom** over 200ms
- Existing pane should **smoothly shrink** to make room
- Both panes should move in sync
- Content renders correctly throughout animation

### 2. Vertical Split Animation
**Default binding**: `Ctrl+Space` then `v` (split vertical)

**Expected behavior**:
- New pane should **smoothly slide in from right** over 200ms
- Existing pane should **smoothly shrink** to make room
- Smooth coordinated motion
- Content visible during entire animation

### 3. Multiple Splits
Try creating a complex layout:
```
1. Start with one pane
2. Split horizontally → watch bottom pane animate in
3. Split the top pane vertically → watch right pane animate in
4. Split again → watch the smooth transitions
```

**Expected**: Each new pane should animate smoothly regardless of tree complexity.

## Configuration Options

### Duration
Change animation speed:
```json
{
  "layout_transitions": {
    "duration_ms": 100  // Fast
    "duration_ms": 200  // Balanced (default)
    "duration_ms": 300  // Slow
  }
}
```

### Easing Functions
Try different timing curves:
```json
{
  "layout_transitions": {
    "easing": "linear"       // Constant speed
    "easing": "smoothstep"   // Smooth start/end (default)
    "easing": "ease-in"      // Slow start
    "easing": "ease-out"     // Slow end
    "easing": "ease-in-out"  // Slow start and end
  }
}
```

### Disable Animations
For instant splits:
```json
{
  "layout_transitions": {
    "enabled": false
  }
}
```

## Technical Details

### How It Works (Client-Side)
1. **Server** performs split instantly, sends ONE tree snapshot with final layout
2. **Client** detects layout changes between old and new snapshots
3. **Client** animates locally at 60fps for configured duration
4. **Interpolation** smoothly transitions pane positions/sizes
5. **Rendering** uses animated coordinates during transition

### Benefits vs Server-Side
- ✅ **Lower latency**: No waiting for network round-trips
- ✅ **Less bandwidth**: 1 snapshot message instead of ~12 deltas
- ✅ **Smoother**: Client controls precise frame timing
- ✅ **Better performance**: No server CPU for animation calculations

### Animation Flow
```
Server Split → Tree Snapshot Sent
                ↓
Client Receives → Detects Changes
                ↓
Start Animation → 60fps interpolation loop
                ↓
Render Frames → Use animated layouts
                ↓
Complete (200ms) → Commit final layout
```

### Performance
- Runs at **60fps** (16ms per frame)
- Minimal CPU impact (simple lerp calculations)
- No allocations in hot path
- Animations stop automatically when complete
- Only animates changes above min_threshold

## Troubleshooting

**Problem**: Splits are still instant
- Check `~/.config/texelation/theme.json` has `enabled: true`
- Verify you restarted the client after changing config
- Check for JSON syntax errors in theme file

**Problem**: Animations feel jerky
- Reduce `duration_ms` (try 150 or 100)
- Try different easing function (e.g., `"ease-out"`)
- Check system isn't under heavy load

**Problem**: Animations too slow
- Decrease `duration_ms` (try 150 or 100)
- Try `"ease-out"` easing for faster finish

**Problem**: Small movements are annoying
- Increase `min_threshold` to 5 or 6
- This filters out minor layout adjustments

## Advanced Configuration

### Fine-Tuned Example
```json
{
  "desktop": {
    "default_fg": "#E0E0E0",
    "default_bg": "#1E1E1E"
  },
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 180,
    "easing": "ease-out",
    "min_threshold": 4
  },
  "effects": {
    "bindings": [
      {"event": "pane.active", "target": "pane", "effect": "fadeTint"}
    ]
  }
}
```

### Disable Only for Testing
```json
{
  "layout_transitions": {
    "enabled": false
  }
}
```

## See Also

- [LAYOUT_TRANSITIONS_CONFIG.md](LAYOUT_TRANSITIONS_CONFIG.md) - Full configuration reference
- [THEMING_GUIDE.md](THEMING_GUIDE.md) - Theme system overview
