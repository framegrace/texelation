# Image-Based Screensaver Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade screensaver effects to render as Kitty graphics protocol images when available, falling back to cell-based rendering on terminals without Kitty support.

**Architecture:** The `screensaverFade` wrapper detects Kitty capability on activation. If available, it creates a `textrender.Renderer` to snapshot the workspace into an `*image.RGBA`, then manages a `kittyOutput` surface for the screensaver's lifetime. The inner effect implements the `ImageEffect` interface to manipulate pixels instead of cells. Only changed regions are re-transmitted. The cell-based path is untouched — same config, same effect IDs.

**Tech Stack:** Go, `texelui/graphics/textrender`, Kitty graphics protocol (APC sequences), `image/png`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/effects/interfaces.go` | Add `ImageEffect` interface |
| `internal/effects/screensaver_fade.go` | Image mode lifecycle: detect capability, create renderer/surface, delegate to `ImageEffect`, manage Kitty output |
| `internal/effects/screensaver_config.go` | Add `Resolution` field |
| `internal/effects/crypt.go` | Implement `ImageEffect` for crypt |
| `internal/effects/events.go` | Add fields to `EffectTrigger` for Kitty capability and workspace buffer |
| `internal/runtime/client/client_state.go` | Pass Kitty capability + workspace buffer + ttyWriter to screensaver trigger |
| `internal/runtime/client/renderer.go` | Skip cell rendering when image screensaver is active |

---

### Task 1: Add ImageEffect interface

**Files:**
- Modify: `internal/effects/interfaces.go`

- [ ] **Step 1: Add the ImageEffect interface**

Add after the `FrameSkipper` interface in `interfaces.go`:

```go
// ImageEffect is an optional interface for effects that can render to an image
// instead of manipulating cell buffers. When the terminal supports Kitty graphics,
// screensaverFade checks for this interface and uses the image path if available.
type ImageEffect interface {
	// ApplyImage modifies the image in-place. Called once per frame.
	ApplyImage(img *image.RGBA, frame int)
	// ImageDirtyRect returns the region that changed in the last ApplyImage call.
	// The screensaver uses this to optimize Kitty image transfers.
	ImageDirtyRect() image.Rectangle
}
```

Add `"image"` to the imports.

- [ ] **Step 2: Verify it builds**

```bash
cd /home/marc/projects/texel/texelation
go build ./internal/effects/
```

Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/effects/interfaces.go
git commit -m "Add ImageEffect interface for image-based screensaver effects"
```

---

### Task 2: Add resolution to screensaver config

**Files:**
- Modify: `internal/effects/screensaver_config.go`
- Modify: `internal/effects/screensaver_config_test.go`

- [ ] **Step 1: Write test for resolution parsing**

Add to `screensaver_config_test.go`:

```go
func TestParseScreensaverConfig_Resolution(t *testing.T) {
	section := map[string]interface{}{
		"enabled":    true,
		"resolution": float64(0.5),
	}
	cfg := ParseScreensaverConfig(section)
	if cfg.Resolution != 0.5 {
		t.Fatalf("expected resolution 0.5, got %f", cfg.Resolution)
	}
}

func TestParseScreensaverConfig_ResolutionDefault(t *testing.T) {
	cfg := ParseScreensaverConfig(nil)
	if cfg.Resolution != 1.0 {
		t.Fatalf("expected default resolution 1.0, got %f", cfg.Resolution)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/effects/ -v -run TestParseScreensaverConfig_Resolution
```

Expected: FAIL — `Resolution` field doesn't exist.

- [ ] **Step 3: Add Resolution field and parsing**

In `screensaver_config.go`, add `Resolution float64` to `ScreensaverConfig`:

```go
type ScreensaverConfig struct {
	Enabled     bool
	Timeout     time.Duration
	EffectID    string
	FadeStyle   string
	FadeIn      time.Duration
	FadeOut     time.Duration
	LockEnabled bool
	LockTimeout time.Duration
	Resolution  float64
}
```

In `ParseScreensaverConfig`, set default and parse:

```go
cfg := ScreensaverConfig{
	// ... existing defaults ...
	Resolution:  1.0,
}
```

Add parsing after `lock_timeout_minutes`:

```go
if v, ok := section["resolution"].(float64); ok && v > 0 {
	cfg.Resolution = v
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/effects/ -v -run TestParseScreensaverConfig
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/effects/screensaver_config.go internal/effects/screensaver_config_test.go
git commit -m "Add resolution field to screensaver config"
```

---

### Task 3: Add Kitty capability and workspace buffer to EffectTrigger

**Files:**
- Modify: `internal/effects/events.go`

- [ ] **Step 1: Add fields to EffectTrigger**

Add these fields to the `EffectTrigger` struct in `events.go`:

```go
type EffectTrigger struct {
	// ... existing fields ...

	// Image screensaver fields (set by client when triggering screensaver)
	HasKitty        bool              // true if terminal supports Kitty graphics
	WorkspaceBuffer [][]client.Cell   // current workspace content for image snapshot
	ScreenWidth     int               // terminal width in cells
	ScreenHeight    int               // terminal height in cells
	TtyWriter       io.Writer         // TTY for Kitty APC output (nil if no Kitty)
	Resolution      float64           // image resolution multiplier
}
```

Add `"io"` to imports.

- [ ] **Step 2: Verify it builds**

```bash
go build ./internal/effects/
```

Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/effects/events.go
git commit -m "Add Kitty capability and workspace buffer to EffectTrigger"
```

---

### Task 4: Pass Kitty info in screensaver trigger from client

**Files:**
- Modify: `internal/runtime/client/client_state.go`
- Modify: `internal/runtime/client/renderer.go`

- [ ] **Step 1: Add imageScreensaverActive flag to clientState**

In `client_state.go`, add to the `clientState` struct:

```go
	// Image screensaver state
	imageScreensaverActive bool
```

- [ ] **Step 2: Pass Kitty info in the screensaver OnActivate trigger**

In `client_state.go`, in the `applyEffectConfig` function, modify the `OnActivate` callback (around line 218-224):

```go
OnActivate: func() {
	// Build workspace buffer for image snapshot
	width, height := 0, 0
	if s.prevBuffer != nil {
		height = len(s.prevBuffer)
		if height > 0 {
			width = len(s.prevBuffer[0])
		}
	}
	// Copy prevBuffer so the snapshot is stable
	var wsBuf [][]client.Cell
	if width > 0 && height > 0 {
		wsBuf = make([][]client.Cell, height)
		for y := 0; y < height; y++ {
			wsBuf[y] = make([]client.Cell, width)
			copy(wsBuf[y], s.prevBuffer[y])
		}
	}
	mgr.HandleTrigger(effects.EffectTrigger{
		Type:            effects.TriggerScreensaver,
		Active:          true,
		FadeIn:          fadeIn,
		FadeOut:         fadeOut,
		HasKitty:        s.kitty != nil,
		WorkspaceBuffer: wsBuf,
		ScreenWidth:     width,
		ScreenHeight:    height,
		TtyWriter:       s.ttyWriter,
		Resolution:      ssCfg.Resolution,
	})
	s.imageScreensaverActive = s.kitty != nil
},
```

Also update `OnDeactivate`:

```go
OnDeactivate: func() {
	mgr.HandleTrigger(effects.EffectTrigger{
		Type:     effects.TriggerScreensaver,
		Active:   false,
		FadeIn:   fadeIn,
		FadeOut:  fadeOut,
		HasKitty: s.kitty != nil,
		TtyWriter: s.ttyWriter,
	})
	s.imageScreensaverActive = false
},
```

- [ ] **Step 3: Skip cell rendering when image screensaver is active**

In `renderer.go`, in the `render` function (around line 263), add an early return before the `needsFull` logic:

```go
func render(state *clientState, screen tcell.Screen) {
	// When the image screensaver is active, the Kitty surface covers the screen.
	// Skip all cell-based rendering — the screensaver effect handles output
	// directly via Kitty APC sequences.
	if state.imageScreensaverActive {
		return
	}

	width, height := screen.Size()
	// ... rest of existing code ...
```

- [ ] **Step 4: Verify it builds**

```bash
go build ./...
```

Expected: builds with no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/client_state.go internal/runtime/client/renderer.go
git commit -m "Pass Kitty capability to screensaver trigger, skip cell rendering when image active"
```

---

### Task 5: Image mode in screensaverFade

**Files:**
- Modify: `internal/effects/screensaver_fade.go`

This is the core task. The screensaverFade wrapper gains image mode: on activation with Kitty, it creates a textrender.Renderer, snapshots the workspace to an image, creates a Kitty surface, and delegates to ImageEffect each frame.

- [ ] **Step 1: Add image state fields to screensaverFade**

Add these fields to the `screensaverFade` struct:

```go
type screensaverFade struct {
	inner       Effect
	effectIDs   []string
	blender     fadeBlender
	timeline    *Timeline
	active      bool
	fadingOut   bool
	fadeOut      time.Duration
	snapshotBuf [][]client.Cell

	// Image mode state (active when terminal supports Kitty graphics)
	imageMode    bool
	imageImg     *image.RGBA       // the rendered image being modified by effect
	snapshotImg  *image.RGBA       // original snapshot for fade blending
	kittyOut     *kittyImageOutput // manages Kitty APC output for the image
	imageFrame   int               // frame counter
}
```

Add imports: `"image"`, `"image/png"`, `"bytes"`, `"fmt"`, `"io"`, `"encoding/base64"`, `"github.com/framegrace/texelui/graphics/textrender"`.

- [ ] **Step 2: Create kittyImageOutput helper**

Add a small helper type in `screensaver_fade.go` that wraps the TTY writer for image output. This is simpler than the full client-side kittyOutput since it only handles one image:

```go
// kittyImageOutput manages a single full-screen Kitty image surface.
type kittyImageOutput struct {
	w         io.Writer
	surfaceID uint32
	placed    bool
	cols, rows int // terminal size in cells
}

func newKittyImageOutput(w io.Writer, surfaceID uint32, cols, rows int) *kittyImageOutput {
	return &kittyImageOutput{w: w, surfaceID: surfaceID, cols: cols, rows: rows}
}

func (k *kittyImageOutput) transmitAndPlace(img *image.RGBA) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	chunks := kittyChunkString(encoded, 4096)
	for i, chunk := range chunks {
		more := 1
		if i == len(chunks)-1 {
			more = 0
		}
		var err error
		if i == 0 {
			_, err = fmt.Fprintf(k.w,
				"\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\",
				k.surfaceID, more, chunk)
		} else {
			_, err = fmt.Fprintf(k.w, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
		if err != nil {
			return err
		}
	}
	// Place at top-left, covering full terminal
	if !k.placed {
		_, err := fmt.Fprintf(k.w,
			"\x1b[1;1H\x1b_Ga=p,i=%d,p=%d,c=%d,r=%d,z=1000,q=2;\x1b\\",
			k.surfaceID, k.surfaceID, k.cols, k.rows)
		if err != nil {
			return err
		}
		k.placed = true
	}
	return nil
}

func (k *kittyImageOutput) delete() {
	if k.w != nil {
		fmt.Fprintf(k.w, "\x1b_Ga=d,d=i,i=%d,q=2;\x1b\\", k.surfaceID)
	}
}

func kittyChunkString(s string, n int) []string {
	if len(s) <= n {
		return []string{s}
	}
	var chunks []string
	for len(s) > n {
		chunks = append(chunks, s[:n])
		s = s[n:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}
```

- [ ] **Step 3: Modify HandleTrigger for image mode activation**

Update the `HandleTrigger` method to detect Kitty and set up image mode:

```go
func (e *screensaverFade) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerScreensaver {
		if e.inner != nil {
			e.inner.HandleTrigger(trigger)
		}
		return
	}

	fadeIn := trigger.FadeIn
	if fadeIn <= 0 {
		fadeIn = 5 * time.Second
	}

	if trigger.Active {
		e.active = true
		e.fadingOut = false
		e.timeline.Reset("fade")
		e.timeline.AnimateTo("fade", 1.0, fadeIn, trigger.Timestamp)
		e.blender.Reset()

		// Random mode: pick a new effect each activation.
		if len(e.effectIDs) > 0 {
			id := e.effectIDs[rand.Intn(len(e.effectIDs))]
			if eff, err := CreateEffect(id, nil); err == nil {
				e.inner = eff
			}
		}
		e.inner.HandleTrigger(trigger)

		// Check if we can use image mode
		if imgEff, ok := e.inner.(ImageEffect); ok && trigger.HasKitty && trigger.TtyWriter != nil && len(trigger.WorkspaceBuffer) > 0 {
			e.setupImageMode(imgEff, trigger)
		} else {
			e.imageMode = false
		}
	} else {
		e.active = false
		e.fadingOut = true
		fadeOut := trigger.FadeOut
		if fadeOut <= 0 {
			fadeOut = defaultFadeOut
		}
		e.fadeOut = fadeOut
		e.timeline.AnimateTo("fade", 0.0, fadeOut, trigger.Timestamp)

		// Clean up image mode immediately on deactivate
		if e.imageMode {
			e.cleanupImageMode()
		}
	}
}
```

- [ ] **Step 4: Add setupImageMode and cleanupImageMode**

```go
func (e *screensaverFade) setupImageMode(imgEff ImageEffect, trigger EffectTrigger) {
	// Detect font for rendering
	fontPath, err := textrender.DetectFont()
	if err != nil {
		e.imageMode = false
		return
	}

	// Determine cell size — use resolution multiplier
	cellW := 8  // fallback defaults
	cellH := 16
	if trigger.Resolution > 0 && trigger.Resolution != 1.0 {
		cellW = int(float64(cellW) * trigger.Resolution)
		cellH = int(float64(cellH) * trigger.Resolution)
	}

	renderer, err := textrender.New(textrender.Config{
		FontPath:   fontPath,
		CellWidth:  cellW,
		CellHeight: cellH,
	})
	if err != nil {
		e.imageMode = false
		return
	}

	// Convert client.Cell buffer to core.Cell for textrender
	coreGrid := make([][]texelcore.Cell, len(trigger.WorkspaceBuffer))
	for y, row := range trigger.WorkspaceBuffer {
		coreGrid[y] = make([]texelcore.Cell, len(row))
		for x, cell := range row {
			coreGrid[y][x] = texelcore.Cell{
				Ch:    cell.Ch,
				Style: cell.Style,
			}
		}
	}

	// Render snapshot
	img := renderer.Render(coreGrid)

	// Clone for the original snapshot (used during fade blending)
	snapshot := image.NewRGBA(img.Bounds())
	copy(snapshot.Pix, img.Pix)

	e.imageMode = true
	e.imageImg = img
	e.snapshotImg = snapshot
	e.imageFrame = 0
	e.kittyOut = newKittyImageOutput(trigger.TtyWriter, 9999, trigger.ScreenWidth, trigger.ScreenHeight)
}

func (e *screensaverFade) cleanupImageMode() {
	if e.kittyOut != nil {
		e.kittyOut.delete()
		e.kittyOut = nil
	}
	e.imageMode = false
	e.imageImg = nil
	e.snapshotImg = nil
}
```

Add import for `texelcore "github.com/framegrace/texelui/core"`.

- [ ] **Step 5: Modify ApplyWorkspace for image mode**

Update `ApplyWorkspace` to handle image mode:

```go
func (e *screensaverFade) ApplyWorkspace(buffer [][]client.Cell) {
	if !e.active && !e.fadingOut {
		return
	}

	// Image mode: delegate to ApplyImage and transmit via Kitty
	if e.imageMode {
		e.applyImageMode()
		return
	}

	// Cell mode: existing logic unchanged
	intensity := e.timeline.GetCached("fade")
	if intensity <= 0 {
		return
	}
	if e.inner == nil {
		return
	}
	if intensity >= 1.0 {
		e.inner.ApplyWorkspace(buffer)
		return
	}
	if len(e.snapshotBuf) != len(buffer) {
		e.snapshotBuf = make([][]client.Cell, len(buffer))
	}
	for y := range buffer {
		if len(e.snapshotBuf[y]) != len(buffer[y]) {
			e.snapshotBuf[y] = make([]client.Cell, len(buffer[y]))
		}
		copy(e.snapshotBuf[y], buffer[y])
	}
	e.inner.ApplyWorkspace(buffer)
	e.blender.Blend(e.snapshotBuf, buffer, float32(intensity))
}

func (e *screensaverFade) applyImageMode() {
	imgEff, ok := e.inner.(ImageEffect)
	if !ok || e.imageImg == nil || e.kittyOut == nil {
		return
	}

	e.imageFrame++
	imgEff.ApplyImage(e.imageImg, e.imageFrame)

	// Transmit updated image
	if err := e.kittyOut.transmitAndPlace(e.imageImg); err != nil {
		// Kitty output failed — fall back to cell mode next frame
		e.cleanupImageMode()
	}
}
```

- [ ] **Step 6: Update FrameSkip for image mode**

In the `FrameSkip` method, image mode can use its own skip rate:

```go
func (e *screensaverFade) FrameSkip() int {
	if e.timeline.GetCached("fade") < 1.0 {
		return 1
	}
	if fs, ok := e.inner.(FrameSkipper); ok {
		return fs.FrameSkip()
	}
	return 1
}
```

No change needed — the inner effect's FrameSkip already applies.

- [ ] **Step 7: Update Update() to clean up after fade-out in image mode**

In the `Update` method, clean up image mode when fade-out completes:

```go
func (e *screensaverFade) Update(now time.Time) {
	e.timeline.Update(now)
	if e.inner != nil {
		e.inner.Update(now)
	}
	if e.fadingOut && e.timeline.GetCached("fade") <= 0 {
		e.fadingOut = false
		if e.imageMode {
			e.cleanupImageMode()
		}
		if e.inner != nil {
			e.inner.HandleTrigger(EffectTrigger{
				Type:      TriggerScreensaver,
				Active:    false,
				Timestamp: now,
			})
		}
	}
}
```

- [ ] **Step 8: Verify it builds**

```bash
go build ./...
```

Expected: builds with no errors.

- [ ] **Step 9: Commit**

```bash
git add internal/effects/screensaver_fade.go
git commit -m "Add image mode to screensaverFade: Kitty surface lifecycle and rendering"
```

---

### Task 6: Implement ImageEffect on crypt

**Files:**
- Modify: `internal/effects/crypt.go`

- [ ] **Step 1: Add image state fields to cryptEffect**

```go
type cryptEffect struct {
	active    bool
	charTable [256]rune
	frame     uint32

	// Image mode state
	renderer  *textrender.Renderer
	cellW     int
	cellH     int
	imgW      int // image width in pixels
	imgH      int // image height in pixels
	gridCols  int // grid width in cells
	gridRows  int // grid height in cells
	bgColors  [][]color.RGBA // per-cell background color from snapshot
	dirtyRect image.Rectangle
}
```

Add imports: `"image"`, `"image/color"`, `"image/draw"`, `"github.com/framegrace/texelui/graphics/textrender"`.

- [ ] **Step 2: Implement ApplyImage**

```go
func (e *cryptEffect) ApplyImage(img *image.RGBA, frame int) {
	if !e.active {
		return
	}

	// First frame: initialize from image dimensions
	if e.renderer == nil {
		e.initImageMode(img)
	}

	e.frame++
	frameU8 := uint8(e.frame)
	bounds := img.Bounds()

	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := 0, 0

	for gy := 0; gy < e.gridRows; gy++ {
		for gx := 0; gx < e.gridCols; gx++ {
			// Check if this cell has text (non-background pixels)
			if !e.cellHasText(img, gx, gy) {
				continue
			}

			h := cellHash(gx, gy)
			// ~40% shimmer rate
			if uint8(h+frameU8) >= 100 {
				continue
			}

			// Re-roll braille char
			e.charTable[h] = rune(brailleBase + rand.Intn(brailleCount))

			// Draw the replacement braille char into the cell
			e.drawBrailleCell(img, gx, gy, h)

			// Track dirty region
			px := gx * e.cellW
			py := gy * e.cellH
			if px < minX { minX = px }
			if py < minY { minY = py }
			ex := px + e.cellW
			ey := py + e.cellH
			if ex > maxX { maxX = ex }
			if ey > maxY { maxY = ey }
		}
	}

	if maxX > minX && maxY > minY {
		e.dirtyRect = image.Rect(minX, minY, maxX, maxY)
	} else {
		e.dirtyRect = image.Rectangle{}
	}
}

func (e *cryptEffect) ImageDirtyRect() image.Rectangle {
	return e.dirtyRect
}
```

- [ ] **Step 3: Add helper methods**

```go
func (e *cryptEffect) initImageMode(img *image.RGBA) {
	fontPath, err := textrender.DetectFont()
	if err != nil {
		return
	}

	bounds := img.Bounds()
	e.imgW = bounds.Dx()
	e.imgH = bounds.Dy()

	// Estimate cell dimensions from image/grid ratio
	// The image was rendered by textrender, so we can detect cell size
	// by trying common sizes or using the renderer's metrics
	renderer, err := textrender.New(textrender.Config{FontPath: fontPath})
	if err != nil {
		return
	}
	e.renderer = renderer

	// Get cell dimensions from a test render
	testGrid := make([][]texelcore.Cell, 1)
	testGrid[0] = []texelcore.Cell{{Ch: 'M', Style: tcell.StyleDefault}}
	testImg := renderer.Render(testGrid)
	e.cellW = testImg.Bounds().Dx()
	e.cellH = testImg.Bounds().Dy()

	if e.cellW <= 0 || e.cellH <= 0 {
		e.renderer = nil
		return
	}

	e.gridCols = e.imgW / e.cellW
	e.gridRows = e.imgH / e.cellH

	// Sample background colors from the snapshot
	e.bgColors = make([][]color.RGBA, e.gridRows)
	for gy := 0; gy < e.gridRows; gy++ {
		e.bgColors[gy] = make([]color.RGBA, e.gridCols)
		for gx := 0; gx < e.gridCols; gx++ {
			// Sample from corner of cell (least likely to have glyph pixels)
			px := gx * e.cellW
			py := gy * e.cellH
			e.bgColors[gy][gx] = img.RGBAAt(px, py)
		}
	}
}

func (e *cryptEffect) cellHasText(img *image.RGBA, gx, gy int) bool {
	px := gx * e.cellW
	py := gy * e.cellH
	bg := e.bgColors[gy][gx]

	// Check a few sample pixels in the cell for non-background content
	for dy := 2; dy < e.cellH-2 && dy < e.cellH; dy += 3 {
		for dx := 1; dx < e.cellW-1 && dx < e.cellW; dx += 2 {
			c := img.RGBAAt(px+dx, py+dy)
			if c != bg {
				return true
			}
		}
	}
	return false
}

func (e *cryptEffect) drawBrailleCell(img *image.RGBA, gx, gy int, h uint8) {
	px := gx * e.cellW
	py := gy * e.cellH
	bg := e.bgColors[gy][gx]
	ch := e.charTable[h]

	// Clear cell to background
	cellRect := image.Rect(px, py, px+e.cellW, py+e.cellH)
	draw.Draw(img, cellRect, image.NewUniform(bg), image.Point{}, draw.Src)

	// Render the braille character using textrender
	// Create a 1-cell grid and render it, then copy pixels
	grid := [][]texelcore.Cell{{
		{Ch: ch, Style: tcell.StyleDefault.
			Foreground(tcell.NewRGBColor(int32(bg.R), int32(bg.G), int32(bg.B))).
			Background(tcell.NewRGBColor(int32(bg.R), int32(bg.G), int32(bg.B)))},
	}}

	// Use the original cell's foreground color instead
	// Sample a non-background pixel for the FG color
	fg := e.sampleFgColor(img, gx, gy, bg)
	grid[0][0].Style = tcell.StyleDefault.
		Foreground(tcell.NewRGBColor(int32(fg.R), int32(fg.G), int32(fg.B))).
		Background(tcell.NewRGBColor(int32(bg.R), int32(bg.G), int32(bg.B)))

	charImg := e.renderer.Render(grid)

	// Copy rendered glyph into the main image
	draw.Draw(img, cellRect, charImg, image.Point{}, draw.Src)
}

func (e *cryptEffect) sampleFgColor(img *image.RGBA, gx, gy int, bg color.RGBA) color.RGBA {
	px := gx * e.cellW
	py := gy * e.cellH
	for dy := 2; dy < e.cellH-2; dy++ {
		for dx := 1; dx < e.cellW-1; dx++ {
			c := img.RGBAAt(px+dx, py+dy)
			if c != bg {
				return c
			}
		}
	}
	return color.RGBA{204, 204, 204, 255} // default gray
}
```

Add import for `texelcore "github.com/framegrace/texelui/core"` and `"github.com/gdamore/tcell/v2"`.

- [ ] **Step 4: Verify it builds**

```bash
go build ./...
```

Expected: builds with no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/effects/crypt.go
git commit -m "Implement ImageEffect on crypt for image-based screensaver rendering"
```

---

### Task 7: Integration test

**Files:**
- Create: `internal/effects/screensaver_image_test.go`

- [ ] **Step 1: Write test for image mode lifecycle**

```go
package effects

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/framegrace/texelation/client"
	"github.com/gdamore/tcell/v2"
)

func TestImageScreensaver_CryptActivation(t *testing.T) {
	// Create a crypt effect and verify it implements ImageEffect
	eff, err := CreateEffect("crypt", nil)
	if err != nil {
		t.Fatalf("create crypt: %v", err)
	}
	if _, ok := eff.(ImageEffect); !ok {
		t.Fatal("crypt does not implement ImageEffect")
	}
}

func TestImageScreensaver_ApplyImage(t *testing.T) {
	eff, _ := CreateEffect("crypt", nil)
	imgEff := eff.(ImageEffect)

	// Activate
	eff.HandleTrigger(EffectTrigger{
		Type:   TriggerScreensaver,
		Active: true,
	})

	// Create a test image (10x10 cells at 8x16 = 80x160 pixels)
	img := image.NewRGBA(image.Rect(0, 0, 80, 160))

	// Fill with a background color
	bg := image.NewUniform(image.Black)
	for y := 0; y < 160; y++ {
		for x := 0; x < 80; x++ {
			img.SetRGBA(x, y, image.Black.RGBA.(interface{ RGBA() (uint8, uint8, uint8, uint8) }).(image.Black))
		}
	}

	// Draw some "text" pixels (non-black) in a few cells
	for x := 10; x < 20; x++ {
		for y := 20; y < 30; y++ {
			img.Set(x, y, image.White)
		}
	}

	// Apply image effect — should not panic
	imgEff.ApplyImage(img, 1)
	imgEff.ApplyImage(img, 2)

	t.Log("ApplyImage completed without panic")
}

func TestImageScreensaver_FadeLifecycle(t *testing.T) {
	eff, _ := CreateEffect("crypt", nil)
	fade := NewScreensaverFade(eff, "dissolve").(*screensaverFade)

	// Build a workspace buffer
	cols, rows := 20, 5
	wsBuf := make([][]client.Cell, rows)
	for y := range wsBuf {
		wsBuf[y] = make([]client.Cell, cols)
		for x := range wsBuf[y] {
			wsBuf[y][x] = client.Cell{
				Ch:    'A',
				Style: tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack),
			}
		}
	}

	var ttyBuf bytes.Buffer

	// Activate with Kitty
	fade.HandleTrigger(EffectTrigger{
		Type:            TriggerScreensaver,
		Active:          true,
		HasKitty:        true,
		WorkspaceBuffer: wsBuf,
		ScreenWidth:     cols,
		ScreenHeight:    rows,
		TtyWriter:       &ttyBuf,
		Resolution:      1.0,
		Timestamp:       time.Now(),
		FadeIn:          100 * time.Millisecond,
	})

	// Image mode may or may not activate depending on font availability
	if fade.imageMode {
		t.Log("Image mode activated successfully")

		// Simulate a frame
		fade.ApplyWorkspace(wsBuf)

		if ttyBuf.Len() == 0 {
			t.Fatal("expected Kitty APC output")
		}
		t.Logf("Kitty output: %d bytes", ttyBuf.Len())

		// Deactivate
		fade.HandleTrigger(EffectTrigger{
			Type:      TriggerScreensaver,
			Active:    false,
			HasKitty:  true,
			TtyWriter: &ttyBuf,
			Timestamp: time.Now(),
		})

		if fade.imageMode {
			t.Fatal("image mode should be off after deactivation")
		}
	} else {
		t.Skip("Image mode not available (font detection failed) — cell mode test only")
	}
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/effects/ -v -run TestImageScreensaver
```

Expected: PASS (or SKIP if font detection unavailable).

- [ ] **Step 3: Commit**

```bash
git add internal/effects/screensaver_image_test.go
git commit -m "Add integration tests for image-based screensaver lifecycle"
```

---

### Task 8: Manual testing and CPU comparison

- [ ] **Step 1: Build**

```bash
make build
```

- [ ] **Step 2: Test image screensaver**

Start `./bin/texelation`, wait for crypt screensaver to activate. Verify:
- Kitty image appears covering the screen
- Braille characters shimmer over text regions
- Status bar shows CPU usage (should be significantly lower than cell-based)
- Press any key to deactivate — Kitty image should disappear, normal rendering resumes

- [ ] **Step 3: Compare CPU with cell-based**

In `texelation.json`, temporarily disable Kitty by setting `TERM_PROGRAM` to something unknown, or test with a non-Kitty terminal. Compare CPU usage.

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "Fix issues found during manual testing"
```
