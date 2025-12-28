# Rendering

How TexelUI draws widgets to the screen.

## Rendering Pipeline

```
  Widget State Changes
         │
         ▼
  widget.invalidate()
         │
         ▼
  UIManager.Invalidate(rect)
         │
         ▼
  Add rect to dirty list
         │
         ▼
  Request refresh
         │
         ▼
  UIManager.Render()
         │
    ┌────┴────┐
    │ Empty   │──Yes──▶ Full frame render
    │ dirty?  │
    └────┬────┘
         │ No
         ▼
  Merge overlapping rects
         │
         ▼
  For each merged rect:
  ├── Create clipped Painter
  ├── Fill with background
  └── Draw intersecting widgets
         │
         ▼
  Return buffer
         │
         ▼
  Runtime displays buffer
```

## The Render Buffer

UIManager maintains a 2D cell buffer:

```go
buf [][]texel.Cell
```

Each cell contains:

```go
type Cell struct {
    Ch    rune        // Character to display
    Style tcell.Style // Foreground, background, attributes
}
```

The buffer is sized to match the current dimensions:

```go
func (u *UIManager) ensureBufferLocked() {
    if u.buf != nil && len(u.buf) == u.H && len(u.buf[0]) == u.W {
        return  // Already correct size
    }
    // Reallocate
    u.buf = make([][]texel.Cell, u.H)
    for y := 0; y < u.H; y++ {
        row := make([]texel.Cell, u.W)
        for x := 0; x < u.W; x++ {
            row[x] = texel.Cell{Ch: ' ', Style: u.bgStyle}
        }
        u.buf[y] = row
    }
}
```

## The Painter

Widgets draw using a `Painter` with automatic clipping:

```go
type Painter struct {
    buf  [][]texel.Cell  // Target buffer
    clip Rect            // Clipping rectangle
}
```

### SetCell

Draw a single character:

```go
func (p *Painter) SetCell(x, y int, ch rune, style tcell.Style) {
    // Check clip bounds
    if x < p.clip.X || y < p.clip.Y ||
       x >= p.clip.X+p.clip.W || y >= p.clip.Y+p.clip.H {
        return  // Outside clip, ignore
    }
    // Check buffer bounds
    if y >= 0 && y < len(p.buf) && x >= 0 && x < len(p.buf[y]) {
        p.buf[y][x] = texel.Cell{Ch: ch, Style: style}
    }
}
```

### Fill

Fill a rectangle with a character:

```go
func (p *Painter) Fill(rect Rect, ch rune, style tcell.Style) {
    for yy := rect.Y; yy < rect.Y+rect.H; yy++ {
        for xx := rect.X; xx < rect.X+rect.W; xx++ {
            p.SetCell(xx, yy, ch, style)
        }
    }
}
```

### DrawText

Draw a string horizontally:

```go
func (p *Painter) DrawText(x, y int, s string, style tcell.Style) {
    xx := x
    for _, r := range s {
        p.SetCell(xx, y, r, style)
        xx++
    }
}
```

### DrawBorder

Draw a box border:

```go
func (p *Painter) DrawBorder(rect Rect, style tcell.Style, charset [6]rune) {
    // charset: [horizontal, vertical, top-left, top-right, bottom-left, bottom-right]
    h, v := charset[0], charset[1]
    tl, tr, bl, br := charset[2], charset[3], charset[4], charset[5]

    // Top and bottom edges
    for x := rect.X + 1; x < rect.X+rect.W-1; x++ {
        p.SetCell(x, rect.Y, h, style)
        p.SetCell(x, rect.Y+rect.H-1, h, style)
    }
    // Left and right edges
    for y := rect.Y + 1; y < rect.Y+rect.H-1; y++ {
        p.SetCell(rect.X, y, v, style)
        p.SetCell(rect.X+rect.W-1, y, v, style)
    }
    // Corners
    p.SetCell(rect.X, rect.Y, tl, style)
    p.SetCell(rect.X+rect.W-1, rect.Y, tr, style)
    p.SetCell(rect.X, rect.Y+rect.H-1, bl, style)
    p.SetCell(rect.X+rect.W-1, rect.Y+rect.H-1, br, style)
}
```

## Dirty Regions

### What Are Dirty Regions?

A "dirty region" is an area of the screen that needs to be redrawn.

Instead of redrawing the entire screen every frame, TexelUI tracks which parts changed:

```
┌────────────────────────────────────────┐
│                                        │
│   Old text               New text      │
│   ████████  ──────────▶  ▓▓▓▓▓▓▓▓     │
│                          (dirty)       │
│                                        │
└────────────────────────────────────────┘
```

### Invalidation

Widgets mark themselves dirty when their state changes:

```go
type MyWidget struct {
    core.BaseWidget
    inv  func(core.Rect)  // Injected by UIManager
    text string
}

func (w *MyWidget) SetInvalidator(fn func(core.Rect)) {
    w.inv = fn
}

func (w *MyWidget) invalidate() {
    if w.inv != nil {
        w.inv(w.Rect)  // Mark our rectangle dirty
    }
}

func (w *MyWidget) SetText(s string) {
    w.text = s
    w.invalidate()  // Text changed, need redraw
}
```

### Rectangle Merging

To avoid many small draw passes, overlapping rectangles are merged:

```go
func mergeRects(in []Rect) []Rect {
    out := make([]Rect, 0, len(in))
    // Copy valid rects
    for _, r := range in {
        if r.W > 0 && r.H > 0 {
            out = append(out, r)
        }
    }
    // Iteratively merge overlapping/adjacent rects
    changed := true
    for changed {
        changed = false
        for i := 0; i < len(out); i++ {
            for j := i + 1; j < len(out); j++ {
                if rectsTouchOrOverlap(out[i], out[j]) {
                    out[i] = union(out[i], out[j])
                    out = append(out[:j], out[j+1:]...)
                    changed = true
                    break
                }
            }
        }
    }
    return out
}
```

```
Before merge:           After merge:
┌──────┐                ┌─────────────┐
│ Rect1│ ┌──────┐       │             │
│      │ │ Rect2│   ──▶ │  Merged     │
└──────┘ │      │       │             │
         └──────┘       └─────────────┘
```

## The Render Method

```go
func (u *UIManager) Render() [][]texel.Cell {
    u.mu.Lock()
    defer u.mu.Unlock()

    // 1. Ensure buffer is correctly sized
    u.ensureBufferLocked()

    // 2. Get and clear dirty list
    u.dirtyMu.Lock()
    dirtyCopy := u.dirty
    u.dirty = nil
    u.dirtyMu.Unlock()

    // 3. Get widgets sorted by z-index
    sorted := u.sortedWidgetsLocked()

    // 4. If no dirty regions, full frame render
    if len(dirtyCopy) == 0 {
        full := Rect{X: 0, Y: 0, W: u.W, H: u.H}
        p := NewPainter(u.buf, full)
        p.Fill(full, ' ', u.bgStyle)
        for _, w := range sorted {
            w.Draw(p)
        }
        return u.buf
    }

    // 5. Merge dirty rects
    merged := mergeRects(dirtyCopy)

    // 6. Render each dirty region
    for _, clip := range merged {
        // Clamp to screen bounds
        if clip.X < 0 { clip.W += clip.X; clip.X = 0 }
        if clip.Y < 0 { clip.H += clip.Y; clip.Y = 0 }
        if clip.X+clip.W > u.W { clip.W = u.W - clip.X }
        if clip.Y+clip.H > u.H { clip.H = u.H - clip.Y }
        if clip.W <= 0 || clip.H <= 0 { continue }

        // Create clipped painter
        p := NewPainter(u.buf, clip)

        // Clear region
        p.Fill(clip, ' ', u.bgStyle)

        // Draw widgets that intersect this region
        for _, w := range sorted {
            wx, wy := w.Position()
            ww, wh := w.Size()
            wr := Rect{X: wx, Y: wy, W: ww, H: wh}
            if rectsOverlap(wr, clip) {
                w.Draw(p)
            }
        }
    }

    return u.buf
}
```

## Z-Ordering

Widgets are drawn in z-index order (lowest first):

```go
func (u *UIManager) sortedWidgetsLocked() []Widget {
    sorted := make([]Widget, len(u.widgets))
    copy(sorted, u.widgets)
    sort.SliceStable(sorted, func(i, j int) bool {
        return getZIndex(sorted[i]) < getZIndex(sorted[j])
    })
    return sorted
}

func getZIndex(w Widget) int {
    if zi, ok := w.(ZIndexer); ok {
        return zi.ZIndex()
    }
    return 0
}
```

```
Z-Index 0:  ┌──────────────────────┐
            │     Background       │
            │                      │
            └──────────────────────┘

Z-Index 1:       ┌────────────┐
                 │   Dialog   │
                 │            │
                 └────────────┘

Z-Index 100:          ┌─────────┐
                      │ Tooltip │
                      └─────────┘
```

## Drawing Best Practices

### 1. Use EffectiveStyle

```go
func (w *MyWidget) Draw(p *core.Painter) {
    // Automatically uses focus style when focused
    style := w.EffectiveStyle(w.Style)
    // Draw with style...
}
```

### 2. Fill Background First

```go
func (w *MyWidget) Draw(p *core.Painter) {
    style := w.EffectiveStyle(w.Style)

    // Fill background to clear old content
    p.Fill(w.Rect, ' ', style)

    // Then draw content
    p.DrawText(w.Rect.X, w.Rect.Y, w.text, style)
}
```

### 3. Respect Widget Bounds

```go
func (w *MyWidget) Draw(p *core.Painter) {
    // Truncate text if too long
    text := w.text
    if len(text) > w.Rect.W {
        text = text[:w.Rect.W]
    }
    p.DrawText(w.Rect.X, w.Rect.Y, text, style)
}
```

### 4. Handle Edge Cases

```go
func (w *MyWidget) Draw(p *core.Painter) {
    // Don't draw if zero-sized
    if w.Rect.W <= 0 || w.Rect.H <= 0 {
        return
    }

    // Handle minimum size requirements
    if w.Rect.W < 3 {
        // Can't fit content, draw placeholder
        p.Fill(w.Rect, '?', style)
        return
    }

    // Normal drawing...
}
```

### 5. Invalidate Appropriately

```go
func (w *MyWidget) SetValue(v int) {
    if w.value == v {
        return  // No change, skip invalidation
    }
    w.value = v
    w.invalidate()
}
```

## Performance Tips

1. **Minimize invalidations** - Only invalidate when necessary
2. **Use smallest rects** - Invalidate only the changed area
3. **Avoid full redraws** - Let dirty tracking do its job
4. **Cache expensive calculations** - Don't recalculate in Draw()
5. **Batch state changes** - Multiple changes, one invalidation

## What's Next?

- [Theming](theming.md) - Styling with semantic colors
- [Widget Interface](widget-interface.md) - Widget contracts
- [Custom Widget Tutorial](../tutorials/custom-widget.md) - Build your own
