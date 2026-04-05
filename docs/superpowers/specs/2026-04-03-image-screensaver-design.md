# Image-Based Screensaver Effects

## Overview

Upgrade screensaver effects to render as Kitty graphics protocol images instead of cell-by-cell escape sequences. When the terminal supports Kitty graphics, effects automatically use the image path (GPU-rendered by the terminal). Terminals without Kitty support fall back to the existing cell-based path. Same config, same effect IDs, transparent upgrade.

## Motivation

Cell-based screensaver effects generate ~11,000 escape sequences per frame, consuming significant CPU in both the client (tcell formatting) and the host terminal (parsing/rendering). Image-based rendering shifts the work to the GPU: one image transfer per frame, terminal composites it on the GPU.

## Architecture

The image screensaver is a client-side concern. The server is unaware of it.

### Flow

1. **Screensaver activates** → check `GraphicsKitty` capability
2. If available: create `textrender.Renderer` + `ImageSurface` (via texelui `GraphicsProvider`). Render workspace `[][]Cell` snapshot to `*image.RGBA`.
3. **Each frame** → call effect's `ApplyImage(*image.RGBA, frame)`. Effect modifies pixels. Only changed regions are re-transmitted via Kitty region update protocol.
4. **Deactivation** → delete the Kitty surface, resume cell-based rendering.
5. **No Kitty support** → cell-based path runs unchanged.

### Integration Points

**screensaverFade** (existing wrapper in `internal/effects/screensaver_fade.go`):
- Gains an image mode path alongside the existing cell-based path.
- On activation: detects Kitty capability, creates renderer and surface, renders snapshot.
- On each frame: calls `ApplyImage` on the inner effect instead of `ApplyWorkspace`.
- On deactivation: deletes surface, resumes cell rendering.
- During fade transitions: pixel-level blending on the image (same concept as cell-based blending).

**Effect interface** (in `internal/effects/interfaces.go`):
```go
type ImageEffect interface {
    ApplyImage(img *image.RGBA, frame int)
    ImageDirtyRect() image.Rectangle
}
```
Effects that implement `ImageEffect` get the image path. Effects that don't fall back to cell-based. This is how effects upgrade transparently.

**Client renderer** (`internal/runtime/client/renderer.go`):
- When the image screensaver is active, `fullRender` skips compositing and `diffAndShow` — the Kitty surface covers the screen.
- Normal rendering resumes on deactivation.

### Kitty Surface Lifecycle

One full-screen `ImageSurface` is created when the screensaver activates. It persists across frames — only changed regions are re-transmitted via Kitty's region update protocol. The surface is deleted when the screensaver deactivates.

Reuses the existing texelui Kitty infrastructure: `graphics/kitty.go` (`ImageSurface.Buffer()`, `Update()`, `Place()`), APC chunking, surface ID management.

## Crypt Image Effect (v1)

The first effect ported to image-based rendering:

- **On first frame**: receives the rendered snapshot image. Identifies text regions by scanning for non-background pixels.
- **Each frame**: for ~40% of text cells (same shimmer rate as cell-based), replaces glyph pixels with a random braille character rendered via `textrender.Renderer`. Uses position-based `cellHash` for deterministic-per-position selection with per-frame re-rolling.
- **ImageDirtyRect**: returns the bounding box of changed cells. Kitty's PNG compression handles unchanged pixels within the rect efficiently.
- The `textrender.Renderer` is created once at screensaver activation and reused for all glyph rendering.

## Resolution Configuration

New optional field in `texelation.json` screensaver section:

```json
"screensaver": {
    "effect": "crypt",
    "enabled": true,
    "resolution": 1.0
}
```

`resolution` is a multiplier on the terminal's native pixel size:
- `1.0` (default): full resolution, pixel-accurate text rendering
- `0.5`: half resolution, less image data, slightly blurry
- Values below 1.0 reduce image transfer bandwidth proportionally

The `textrender.Renderer` is created with `CellWidth` and `CellHeight` scaled by this factor. The Kitty surface is placed at the same cell coordinates — the terminal scales the image to fit.

## Capability Detection

The existing `graphics.DetectCapability()` in texelui already returns `GraphicsKitty` for Ghostty, Kitty, and WezTerm. The screensaver fade wrapper checks this at activation time and chooses the rendering path. No new detection code needed.

## Future Effects

Once the image pipeline works with crypt, porting matrix and rainbow is straightforward — they implement `ImageEffect` and manipulate pixels instead of cells. New image-native effects (blur, dissolve, pixelate, glow) become possible since they operate on pixel data.

## Testing

**Unit test** (`TestCryptImageEffect`): Create a small grid, render to image via textrender, apply crypt image effect for N frames, verify text-region pixels changed and background pixels unchanged.

**Integration test** (`TestImageScreensaverLifecycle`): Mock `GraphicsProvider`, activate screensaver, verify surface created, run a few frames, deactivate, verify surface deleted.

**Manual test**: Run screensaver, compare CPU usage between cell-based and image-based crypt. Use Ctrl+S screenshot to capture pre-effect state for visual comparison.

## Out of Scope

- Server-side image rendering
- Porting matrix and rainbow effects (follow-up work)
- New image-native effects like blur/glow (follow-up work)
- Cell size terminal query integration (already built in textrender, wired by consumer)
