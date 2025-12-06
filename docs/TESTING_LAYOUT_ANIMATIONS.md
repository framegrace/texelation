# Testing Layout Animations (Phase 2)

## Quick Start

Layout animations are now **enabled by default** in `texel/workspace.go:93`.

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

### 2. Vertical Split Animation
**Default binding**: `Ctrl+Space` then `v` (split vertical)

**Expected behavior**:
- New pane should **smoothly slide in from right** over 200ms
- Existing pane should **smoothly shrink** to make room
- Smooth coordinated motion

### 3. Multiple Splits
Try creating a complex layout:
```
1. Start with one pane
2. Split horizontally → watch bottom pane animate in
3. Split the top pane vertically → watch right pane animate in
4. Split again → watch the smooth transitions
```

**Expected**: Each new pane should animate smoothly regardless of tree complexity.

## Technical Details

### Animation Parameters
- **Duration**: 200ms (configured in `texel/tree.go:49`)
- **Easing**: Smoothstep (from `effects.Timeline`)
- **Weight range**: 0.0 (hidden) → 1.0 (full size)

### How It Works
1. New pane starts at weight factor 0 (instant)
2. Timeline animates weight 0 → 1.0 over 200ms
3. Every frame: `Tree.Resize()` updates animations and applies weights
4. Layout ratios are multiplied by weight factors
5. Result: smooth size transitions

### Logs to Watch
If you want to see the animation in action, check the server logs for:
```
resizeNode: Processing [horizontal|vertical] split (effective ratios: [0.XX 0.YY])
```

The ratios will change each frame during animation:
- Start: `[1.0 0.0]` (old pane full size, new pane hidden)
- Mid:   `[0.7 0.3]` (transitioning)
- End:   `[0.5 0.5]` (equal split)

## Disabling Animations

To disable and return to instant splits:

**Option 1**: Comment out line in `texel/workspace.go:93`
```go
// w.tree.SetLayoutAnimationEnabled(true)
```

**Option 2**: Change to false
```go
w.tree.SetLayoutAnimationEnabled(false)
```

Then rebuild.

## Recent Fixes

**Issue**: New pane showed only borders during animation, content appeared only after animation finished.
**Fix** (commit e0aeb90): Workspace now continuously refreshes at 60fps during active animations, ensuring app content is rendered throughout the animation.

## Known Limitations (Current Phase)

- ✅ Split animations working
- ✅ Content renders during animation (fixed!)
- ❌ Pane close animations not implemented yet (instant removal)
- ❌ No visual effects on split (glow, highlight) - Phase 3
- ❌ No emit of TriggerPaneSplit events yet - Phase 3

## Performance

Animations run at **60fps** with negligible CPU impact:
- Single `Update()` call per frame
- Cached weight values used during layout
- No allocations in hot path
- Animations stop automatically when complete

## Troubleshooting

**Problem**: Splits are still instant
- Check that line 93 in `workspace.go` enables animations
- Verify you rebuilt after making changes
- Check server logs for animation-related output

**Problem**: Animations feel jerky
- This indicates frame timing issues (not animation system)
- Check if client is getting consistent frame updates
- Monitor CPU usage

**Problem**: Weird layout glitches during animation
- Check logs for "effective ratios" - should always sum to ~1.0
- File a bug with reproduction steps

## Next Steps (Phase 3)

Future enhancements will add:
1. Visual effects on split (e.g., new pane glows during entrance)
2. Animated pane removal (shrink to 0 before removal)
3. Custom animation durations per operation
4. Configurable easing functions
