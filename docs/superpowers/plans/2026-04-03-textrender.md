# Text Renderer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a `texelui/graphics/textrender` package that renders a `[][]core.Cell` grid to an `*image.RGBA` using a real monospace font.

**Architecture:** Load a TTF/OTF font via `golang.org/x/image/font/opentype`, create a face at the terminal's cell pixel size, then iterate the cell grid drawing BG rectangles and FG glyphs. Font detection reads Ghostty/Kitty config files and resolves via `fc-match`. Cell pixel size is queried from the terminal or derived from font metrics.

**Tech Stack:** Go, `golang.org/x/image/font/opentype`, `image/draw` (stdlib), `os/exec` for `fc-match`

---

### Task 1: Add `golang.org/x/image` dependency to texelui

**Files:**
- Modify: `texelui/go.mod`
- Modify: `texelui/go.sum`

- [ ] **Step 1: Add the dependency**

```bash
cd /home/marc/projects/texel/texelui
go get golang.org/x/image@latest
```

- [ ] **Step 2: Verify it resolves**

```bash
go mod tidy
go build ./...
```

Expected: builds with no errors, `golang.org/x/image` appears in `go.mod`.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "Add golang.org/x/image dependency for text rendering"
```

---

### Task 2: Font detection — parse Ghostty and Kitty configs

**Files:**
- Create: `texelui/graphics/textrender/fontdetect.go`
- Create: `texelui/graphics/textrender/fontdetect_test.go`

- [ ] **Step 1: Write tests for config parsing**

```go
// textrender/fontdetect_test.go
package textrender

import "testing"

func TestParseGhosttyConfig(t *testing.T) {
	config := `
# Some comment
font-size = 14
font-family = JetBrainsMono Nerd Font
window-padding-x = 2
`
	name := parseGhosttyFont(config)
	if name != "JetBrainsMono Nerd Font" {
		t.Fatalf("expected 'JetBrainsMono Nerd Font', got %q", name)
	}
}

func TestParseGhosttyConfig_Missing(t *testing.T) {
	config := `font-size = 14`
	name := parseGhosttyFont(config)
	if name != "" {
		t.Fatalf("expected empty, got %q", name)
	}
}

func TestParseKittyConfig(t *testing.T) {
	config := `
# kitty config
font_size 14.0
font_family FiraCode Nerd Font Mono
bold_font auto
`
	name := parseKittyFont(config)
	if name != "FiraCode Nerd Font Mono" {
		t.Fatalf("expected 'FiraCode Nerd Font Mono', got %q", name)
	}
}

func TestParseKittyConfig_Missing(t *testing.T) {
	config := `font_size 14.0`
	name := parseKittyFont(config)
	if name != "" {
		t.Fatalf("expected empty, got %q", name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/marc/projects/texel/texelui
go test ./graphics/textrender/ -v -run TestParse
```

Expected: FAIL — package/functions don't exist yet.

- [ ] **Step 3: Implement config parsing**

```go
// textrender/fontdetect.go
package textrender

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// parseGhosttyFont extracts the font-family value from a Ghostty config string.
func parseGhosttyFont(config string) string {
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "font-family" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// parseKittyFont extracts the font_family value from a Kitty config string.
func parseKittyFont(config string) string {
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) == 2 && strings.TrimSpace(fields[0]) == "font_family" {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}

// resolveFontPath uses fc-match to resolve a font family name to a file path.
func resolveFontPath(family string) (string, error) {
	out, err := exec.Command("fc-match", family, "--format=%{file}").Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", os.ErrNotExist
	}
	return path, nil
}

// DetectFont reads terminal config files to find the user's font,
// then resolves the font name to a file path via fc-match.
// Checks Ghostty (~/.config/ghostty/config) and Kitty (~/.config/kitty/kitty.conf).
func DetectFont() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Try Ghostty
	if data, err := os.ReadFile(filepath.Join(home, ".config", "ghostty", "config")); err == nil {
		if name := parseGhosttyFont(string(data)); name != "" {
			if path, err := resolveFontPath(name); err == nil {
				return path, nil
			}
		}
	}

	// Try Kitty
	if data, err := os.ReadFile(filepath.Join(home, ".config", "kitty", "kitty.conf")); err == nil {
		if name := parseKittyFont(string(data)); name != "" {
			if path, err := resolveFontPath(name); err == nil {
				return path, nil
			}
		}
	}

	return "", os.ErrNotExist
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./graphics/textrender/ -v -run TestParse
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add graphics/textrender/fontdetect.go graphics/textrender/fontdetect_test.go
git commit -m "Add font detection for Ghostty and Kitty configs"
```

---

### Task 3: Terminal cell size query

**Files:**
- Create: `texelui/graphics/textrender/cellsize.go`
- Create: `texelui/graphics/textrender/cellsize_test.go`

- [ ] **Step 1: Write test for cell size parsing**

```go
// textrender/cellsize_test.go
package textrender

import (
	"bytes"
	"testing"
	"time"
)

func TestParseCellSizeResponse(t *testing.T) {
	tests := []struct {
		name           string
		response       string
		wantW, wantH   int
		wantErr        bool
	}{
		{"standard", "\x1b[6;20;10t", 10, 20, false},
		{"typical", "\x1b[6;16;8t", 8, 16, false},
		{"malformed", "\x1b[6;t", 0, 0, true},
		{"wrong code", "\x1b[5;16;8t", 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h, err := parseCellSizeResponse([]byte(tt.response))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if w != tt.wantW || h != tt.wantH {
				t.Fatalf("got (%d, %d), want (%d, %d)", w, h, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestQueryCellSize_Mock(t *testing.T) {
	// Simulate a terminal that responds to \x1b[16t] with \x1b[6;16;8t
	var written bytes.Buffer
	response := bytes.NewBufferString("\x1b[6;16;8t")

	w, h, err := QueryCellSize(&written, response, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 8 || h != 16 {
		t.Fatalf("got (%d, %d), want (8, 16)", w, h)
	}
	if !bytes.Contains(written.Bytes(), []byte("\x1b[16t")) {
		t.Fatal("expected query to be written")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./graphics/textrender/ -v -run "TestParseCellSize|TestQueryCellSize"
```

Expected: FAIL — functions don't exist.

- [ ] **Step 3: Implement cell size query**

```go
// textrender/cellsize.go
package textrender

import (
	"fmt"
	"io"
	"time"
)

// parseCellSizeResponse parses a terminal response to \x1b[16t].
// Expected format: \x1b[6;<height>;<width>t
func parseCellSizeResponse(data []byte) (width, height int, err error) {
	var code, h, w int
	// Skip \x1b[
	s := string(data)
	if len(s) < 4 || s[0] != '\x1b' || s[1] != '[' {
		return 0, 0, fmt.Errorf("invalid response prefix")
	}
	s = s[2:]
	// Remove trailing 't'
	if s[len(s)-1] != 't' {
		return 0, 0, fmt.Errorf("invalid response suffix")
	}
	s = s[:len(s)-1]
	n, err := fmt.Sscanf(s, "%d;%d;%d", &code, &h, &w)
	if err != nil || n != 3 {
		return 0, 0, fmt.Errorf("failed to parse response: %q", string(data))
	}
	if code != 6 {
		return 0, 0, fmt.Errorf("unexpected response code %d", code)
	}
	return w, h, nil
}

// QueryCellSize sends a terminal query to determine cell pixel dimensions.
// Writes \x1b[16t and reads the response \x1b[6;<h>;<w>t.
// Returns an error if the terminal doesn't respond within the timeout.
func QueryCellSize(w io.Writer, r io.Reader, timeout time.Duration) (width, height int, err error) {
	if _, err := w.Write([]byte("\x1b[16t")); err != nil {
		return 0, 0, err
	}

	buf := make([]byte, 64)
	done := make(chan struct{})
	var n int
	var readErr error

	go func() {
		n, readErr = r.Read(buf)
		close(done)
	}()

	select {
	case <-done:
		if readErr != nil {
			return 0, 0, readErr
		}
		return parseCellSizeResponse(buf[:n])
	case <-time.After(timeout):
		return 0, 0, fmt.Errorf("terminal did not respond within %v", timeout)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./graphics/textrender/ -v -run "TestParseCellSize|TestQueryCellSize"
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add graphics/textrender/cellsize.go graphics/textrender/cellsize_test.go
git commit -m "Add terminal cell size query and parsing"
```

---

### Task 4: Core renderer — font loading and cell metrics

**Files:**
- Create: `texelui/graphics/textrender/renderer.go`
- Create: `texelui/graphics/textrender/renderer_test.go`

- [ ] **Step 1: Write test for renderer creation**

```go
// textrender/renderer_test.go
package textrender

import (
	"os/exec"
	"testing"
)

func findTestFont(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("fc-match", "monospace", "--format=%{file}").Output()
	if err != nil {
		t.Skip("fc-match not available, skipping font test")
	}
	path := string(out)
	if path == "" {
		t.Skip("no monospace font found")
	}
	return path
}

func TestNewRenderer(t *testing.T) {
	fontPath := findTestFont(t)
	r, err := New(Config{FontPath: fontPath})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if r.cellW <= 0 || r.cellH <= 0 {
		t.Fatalf("invalid cell dimensions: %dx%d", r.cellW, r.cellH)
	}
	t.Logf("Cell dimensions from font metrics: %dx%d", r.cellW, r.cellH)
}

func TestNewRenderer_WithExplicitSize(t *testing.T) {
	fontPath := findTestFont(t)
	r, err := New(Config{FontPath: fontPath, CellWidth: 10, CellHeight: 20})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if r.cellW != 10 || r.cellH != 20 {
		t.Fatalf("expected 10x20, got %dx%d", r.cellW, r.cellH)
	}
}

func TestNewRenderer_BadPath(t *testing.T) {
	_, err := New(Config{FontPath: "/nonexistent/font.ttf"})
	if err == nil {
		t.Fatal("expected error for bad font path")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./graphics/textrender/ -v -run TestNewRenderer
```

Expected: FAIL — `New` function doesn't exist.

- [ ] **Step 3: Implement renderer with font loading**

```go
// textrender/renderer.go
package textrender

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

// Config configures the text renderer.
type Config struct {
	FontPath   string // path to TTF/OTF file (required)
	CellWidth  int    // cell width in pixels (0 = derive from font metrics)
	CellHeight int    // cell height in pixels (0 = derive from font metrics)
}

// Renderer renders a cell grid to an RGBA image using a monospace font.
type Renderer struct {
	face  font.Face
	cellW int
	cellH int
	asc   int // font ascent in pixels (baseline offset from top of cell)
}

// New creates a renderer by loading the font and computing cell metrics.
func New(cfg Config) (*Renderer, error) {
	data, err := os.ReadFile(cfg.FontPath)
	if err != nil {
		return nil, fmt.Errorf("read font: %w", err)
	}

	parsed, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse font: %w", err)
	}

	// Default font size: 14pt at 72 DPI yields typical terminal cell dimensions.
	size := float64(14)

	// If explicit cell height is given, derive font size from it.
	// A cell's height is roughly lineHeight = ascent + descent + leading.
	// We iterate to find the size that produces the requested cell height.
	if cfg.CellHeight > 0 {
		size = float64(cfg.CellHeight) * 0.75 // initial guess
	}

	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("create face: %w", err)
	}

	metrics := face.Metrics()
	asc := metrics.Ascent.Ceil()
	desc := metrics.Descent.Ceil()

	cellW := cfg.CellWidth
	cellH := cfg.CellHeight

	if cellH <= 0 {
		cellH = asc + desc
	}
	if cellW <= 0 {
		// Measure the advance of 'M' for monospace width
		adv, ok := face.GlyphAdvance('M')
		if !ok {
			adv, _ = face.GlyphAdvance(' ')
		}
		cellW = adv.Ceil()
		if cellW <= 0 {
			cellW = int(math.Ceil(size * 0.6))
		}
	}

	// If explicit cell height was given and differs from font metrics,
	// re-create the face at the correct size.
	if cfg.CellHeight > 0 {
		fontCellH := asc + desc
		if fontCellH != cfg.CellHeight {
			adjustedSize := size * float64(cfg.CellHeight) / float64(fontCellH)
			face2, err := opentype.NewFace(parsed, &opentype.FaceOptions{
				Size:    adjustedSize,
				DPI:     72,
				Hinting: font.HintingFull,
			})
			if err == nil {
				face = face2
				m2 := face.Metrics()
				asc = m2.Ascent.Ceil()
			}
		}
	}

	return &Renderer{
		face:  face,
		cellW: cellW,
		cellH: cellH,
		asc:   asc,
	}, nil
}

// Render draws a cell grid to an RGBA image.
func (r *Renderer) Render(grid [][]core.Cell) *image.RGBA {
	rows := len(grid)
	if rows == 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}
	cols := len(grid[0])

	img := image.NewRGBA(image.Rect(0, 0, cols*r.cellW, rows*r.cellH))

	for y, row := range grid {
		for x, cell := range row {
			r.drawCell(img, x, y, cell)
		}
	}

	return img
}

// drawCell renders a single cell into the image at grid position (gx, gy).
func (r *Renderer) drawCell(img *image.RGBA, gx, gy int, cell core.Cell) {
	px := gx * r.cellW
	py := gy * r.cellH

	fg, bg, attrs := cell.Style.Decompose()

	// Handle reverse attribute
	if attrs&tcell.AttrReverse != 0 {
		fg, bg = bg, fg
	}

	// Resolve default colors
	bgColor := tcellToRGBA(bg, color.RGBA{0, 0, 0, 255})
	fgColor := tcellToRGBA(fg, color.RGBA{204, 204, 204, 255})

	// Handle dim attribute
	if attrs&tcell.AttrDim != 0 {
		fgColor.R = uint8(float64(fgColor.R) * 0.6)
		fgColor.G = uint8(float64(fgColor.G) * 0.6)
		fgColor.B = uint8(float64(fgColor.B) * 0.6)
	}

	// Fill background
	cellRect := image.Rect(px, py, px+r.cellW, py+r.cellH)
	draw.Draw(img, cellRect, image.NewUniform(bgColor), image.Point{}, draw.Src)

	// Draw character
	ch := cell.Ch
	if ch == 0 || ch == ' ' {
		// Space or empty — background only, skip glyph
	} else {
		d := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(fgColor),
			Face: r.face,
			Dot:  fixed.P(px, py+r.asc),
		}
		d.DrawString(string(ch))
	}

	// Draw underline
	if attrs&tcell.AttrUnderline != 0 {
		uy := py + r.cellH - 1
		for ux := px; ux < px+r.cellW; ux++ {
			img.SetRGBA(ux, uy, fgColor)
		}
	}
}

// tcellToRGBA converts a tcell.Color to an RGBA value.
// Returns fallback if the color is default or invalid.
func tcellToRGBA(c tcell.Color, fallback color.RGBA) color.RGBA {
	if c == tcell.ColorDefault {
		return fallback
	}
	tc := c.TrueColor()
	if !tc.Valid() {
		return fallback
	}
	r, g, b := tc.RGB()
	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./graphics/textrender/ -v -run TestNewRenderer
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add graphics/textrender/renderer.go graphics/textrender/renderer_test.go
git commit -m "Add core text renderer with font loading and cell rendering"
```

---

### Task 5: Render a test grid and write PNG for visual inspection

**Files:**
- Modify: `texelui/graphics/textrender/renderer_test.go`

- [ ] **Step 1: Write test that renders a grid and writes PNG**

Add to `renderer_test.go`:

```go
import (
	"image/png"
	"os"

	"github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

func TestRenderBasicGrid(t *testing.T) {
	fontPath := findTestFont(t)
	r, err := New(Config{FontPath: fontPath})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Build a 30x5 grid with various styles
	cols, rows := 30, 5
	grid := make([][]core.Cell, rows)
	for y := range grid {
		grid[y] = make([]core.Cell, cols)
		for x := range grid[y] {
			grid[y][x] = core.Cell{Ch: ' ', Style: tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(204, 204, 204)).
				Background(tcell.NewRGBColor(30, 30, 46))}
		}
	}

	// Row 0: "Hello, World!" in green on dark
	msg := "Hello, World!"
	for i, ch := range msg {
		if i < cols {
			grid[0][i] = core.Cell{Ch: ch, Style: tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(0, 230, 0)).
				Background(tcell.NewRGBColor(30, 30, 46))}
		}
	}

	// Row 1: colored blocks
	colors := []tcell.Color{
		tcell.NewRGBColor(255, 0, 0),
		tcell.NewRGBColor(0, 255, 0),
		tcell.NewRGBColor(0, 0, 255),
		tcell.NewRGBColor(255, 255, 0),
		tcell.NewRGBColor(255, 0, 255),
	}
	for x := 0; x < cols; x++ {
		grid[1][x] = core.Cell{Ch: '#', Style: tcell.StyleDefault.
			Foreground(colors[x%len(colors)]).
			Background(tcell.NewRGBColor(30, 30, 46))}
	}

	// Row 2: bold text
	bold := "BOLD TEXT"
	for i, ch := range bold {
		if i < cols {
			grid[2][i] = core.Cell{Ch: ch, Style: tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(255, 255, 255)).
				Background(tcell.NewRGBColor(30, 30, 46)).
				Bold(true)}
		}
	}

	// Row 3: underlined text
	underline := "underlined"
	for i, ch := range underline {
		if i < cols {
			grid[3][i] = core.Cell{Ch: ch, Style: tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(200, 200, 200)).
				Background(tcell.NewRGBColor(30, 30, 46)).
				Underline(true)}
		}
	}

	// Row 4: reversed text
	rev := "reversed"
	for i, ch := range rev {
		if i < cols {
			grid[4][i] = core.Cell{Ch: ch, Style: tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(200, 200, 200)).
				Background(tcell.NewRGBColor(30, 30, 46)).
				Reverse(true)}
		}
	}

	img := r.Render(grid)

	// Verify dimensions
	wantW := cols * r.cellW
	wantH := rows * r.cellH
	if img.Bounds().Dx() != wantW || img.Bounds().Dy() != wantH {
		t.Fatalf("image size %dx%d, want %dx%d",
			img.Bounds().Dx(), img.Bounds().Dy(), wantW, wantH)
	}

	// Write PNG for visual inspection
	os.MkdirAll("testdata", 0o755)
	f, err := os.Create("testdata/output.png")
	if err != nil {
		t.Fatalf("create output: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	t.Logf("Wrote testdata/output.png (%dx%d) — inspect visually", wantW, wantH)
}
```

- [ ] **Step 2: Run the test**

```bash
go test ./graphics/textrender/ -v -run TestRenderBasicGrid
```

Expected: PASS, writes `graphics/textrender/testdata/output.png`.

- [ ] **Step 3: Visually inspect the PNG**

Open `texelui/graphics/textrender/testdata/output.png` in an image viewer. Verify:
- Row 0: green text "Hello, World!" on dark background
- Row 1: colored '#' characters
- Row 2: bold white text
- Row 3: underlined text with 1px line at bottom
- Row 4: reversed colors (light BG, dark FG)

- [ ] **Step 4: Add testdata to gitignore**

```bash
echo "testdata/" >> graphics/textrender/.gitignore
```

- [ ] **Step 5: Commit**

```bash
git add graphics/textrender/renderer_test.go graphics/textrender/.gitignore
git commit -m "Add visual render test with PNG output"
```

---

### Task 6: Add DetectFont integration test

**Files:**
- Modify: `texelui/graphics/textrender/fontdetect_test.go`

- [ ] **Step 1: Add integration test for DetectFont**

Add to `fontdetect_test.go`:

```go
func TestDetectFont_Integration(t *testing.T) {
	path, err := DetectFont()
	if err != nil {
		t.Skipf("DetectFont() not available in this environment: %v", err)
	}
	if path == "" {
		t.Fatal("DetectFont() returned empty path")
	}
	t.Logf("Detected font: %s", path)

	// Verify the file exists and is readable
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("font file not accessible: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("font file is empty")
	}
}
```

- [ ] **Step 2: Run the test**

```bash
go test ./graphics/textrender/ -v -run TestDetectFont_Integration
```

Expected: PASS (if Ghostty/Kitty config exists) or SKIP.

- [ ] **Step 3: Commit**

```bash
git add graphics/textrender/fontdetect_test.go
git commit -m "Add DetectFont integration test"
```
