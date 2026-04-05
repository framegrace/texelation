# Text Renderer: Cell Grid to Image

## Overview

A new `texelui/graphics/textrender` package that renders a `[][]core.Cell` grid to an `*image.RGBA` using a real monospace font. This enables image-based screensaver effects (sent via Kitty graphics protocol, rendered on GPU) and future features like minimap thumbnails.

## Motivation

Screensaver effects currently update every terminal cell via escape sequences, consuming significant CPU in both the client (tcell formatting) and the host terminal (parsing/rendering). Rendering effects as images and displaying them via the Kitty graphics protocol shifts the work to the GPU, reducing CPU usage dramatically.

## Package

`texelui/graphics/textrender`

## Public API

### Types

```go
type Config struct {
    FontPath   string // path to TTF/OTF file (required)
    CellWidth  int    // cell width in pixels (0 = derive from font metrics)
    CellHeight int    // cell height in pixels (0 = derive from font metrics)
}

type Renderer struct { /* unexported fields */ }
```

### Functions

```go
// New creates a renderer by loading the font and computing cell metrics.
func New(cfg Config) (*Renderer, error)

// Render draws a cell grid to an RGBA image.
func (r *Renderer) Render(grid [][]core.Cell) *image.RGBA

// DetectFont reads terminal config files to find the user's font,
// then resolves the font name to a file path via fc-match.
// Checks Ghostty (~/.config/ghostty/config) and Kitty (~/.config/kitty/kitty.conf).
// Returns the file path or an error if no font could be detected.
func DetectFont() (string, error)

// QueryCellSize writes a terminal query and parses the response to get
// the cell dimensions in pixels. Times out after 100ms.
// Returns an error if the terminal doesn't respond (caller falls back to font metrics).
func QueryCellSize(w io.Writer, r io.Reader) (width, height int, err error)
```

## Rendering Pipeline

`Render(grid)` performs these steps:

1. Create `image.NewRGBA(cols * cellW, rows * cellH)`
2. For each cell in the grid:
   - `Decompose()` the `tcell.Style` to get FG, BG, and attributes
   - Fill the cell rectangle with the BG color
   - Draw the character glyph in FG color using `opentype.Face`
   - Handle attributes:
     - **bold**: bold font face, or synthetic bold (draw glyph twice offset by 1px)
     - **dim**: reduce FG alpha
     - **italic**: italic font face if available
     - **reverse**: swap FG and BG before drawing
     - **underline**: draw 1px line at cell bottom
3. Return the `*image.RGBA`

One `opentype.Face` is loaded at the configured cell height. The font file's own glyphs are used directly — Nerd Font icons, braille, CJK, emoji all render through the same code path. If the user's font has the glyph, it renders correctly.

## Font Detection

`DetectFont()` searches for the user's terminal font:

1. **Ghostty**: parse `~/.config/ghostty/config` for `font-family = <name>`
2. **Kitty**: parse `~/.config/kitty/kitty.conf` for `font_family <name>`
3. **Resolve**: run `fc-match "<name>" --format=%{file}` to get the TTF/OTF path
4. Return the path or error

Users can override via `texelation.json` config (font path setting). The auto-detection is a convenience for initial setup.

## Cell Size Detection

`QueryCellSize(w, r)` queries the terminal:

1. Write `\x1b[16t]` to the terminal
2. Read response `\x1b[6;<cellH>;<cellW>t`
3. Parse and return `(cellW, cellH)`
4. Timeout after 100ms → return error

When the query fails, the caller falls back to deriving cell dimensions from font metrics (glyph advance width, line height).

## Dependencies

New dependencies for `texelui`:
- `golang.org/x/image/font` — font interfaces
- `golang.org/x/image/font/opentype` — TTF/OTF loading and face creation

## Testing

### `TestRenderBasicGrid`
Create a 20x5 grid with colored cells and text. Render to image. Write PNG to `testdata/output.png`. Assert image dimensions equal `cols*cellW x rows*cellH`. Visual inspection of the PNG for correctness.

Uses `fc-match` to find any monospace font on the system. Skips with `t.Skip("no monospace font found")` if unavailable.

### `TestDetectFontGhostty` / `TestDetectFontKitty`
Parse sample config strings (test fixtures, not filesystem). Verify correct font name extraction.

### `TestQueryCellSize`
Mock reader/writer with a canned `\x1b[6;16;8t` response. Verify parsing returns `(8, 16)`.

## Out of Scope

- Integration with screensaver effects (separate spec)
- Integration with Kitty graphics protocol (consumers handle this)
- Minimap rendering (future consumer of this package)
- Font fallback chains (single font file for now)
