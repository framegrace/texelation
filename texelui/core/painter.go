package core

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// Painter writes into a [][]texel.Cell target with clipping.
type Painter struct {
	buf  [][]texel.Cell
	clip Rect
}

func NewPainter(buf [][]texel.Cell, clip Rect) *Painter {
	return &Painter{buf: buf, clip: clip}
}

func (p *Painter) Size() (int, int) {
	if p.buf == nil {
		return 0, 0
	}
	h := len(p.buf)
	w := 0
	if h > 0 {
		w = len(p.buf[0])
	}
	return w, h
}

func (p *Painter) SetCell(x, y int, ch rune, style tcell.Style) {
	if p.buf == nil {
		return
	}
	if x < p.clip.X || y < p.clip.Y || x >= p.clip.X+p.clip.W || y >= p.clip.Y+p.clip.H {
		return
	}
	if y >= 0 && y < len(p.buf) && x >= 0 && len(p.buf) > 0 && x < len(p.buf[y]) {
		p.buf[y][x] = texel.Cell{Ch: ch, Style: style}
	}
}

func (p *Painter) Fill(rect Rect, ch rune, style tcell.Style) {
	for yy := rect.Y; yy < rect.Y+rect.H; yy++ {
		for xx := rect.X; xx < rect.X+rect.W; xx++ {
			p.SetCell(xx, yy, ch, style)
		}
	}
}

func (p *Painter) DrawText(x, y int, s string, style tcell.Style) {
	xx := x
	for _, r := range s {
		p.SetCell(xx, y, r, style)
		xx++
	}
}

func (p *Painter) DrawBorder(rect Rect, style tcell.Style, charset [6]rune) {
	if rect.W <= 1 || rect.H <= 1 {
		return
	}
	x0, y0 := rect.X, rect.Y
	x1, y1 := rect.X+rect.W-1, rect.Y+rect.H-1
	h, v := charset[0], charset[1]
	tl, tr, bl, br := charset[2], charset[3], charset[4], charset[5]
	for x := x0 + 1; x < x1; x++ {
		p.SetCell(x, y0, h, style)
		p.SetCell(x, y1, h, style)
	}
	for y := y0 + 1; y < y1; y++ {
		p.SetCell(x0, y, v, style)
		p.SetCell(x1, y, v, style)
	}
	p.SetCell(x0, y0, tl, style)
	p.SetCell(x1, y0, tr, style)
	p.SetCell(x0, y1, bl, style)
	p.SetCell(x1, y1, br, style)
}

// WithClip returns a new Painter that clips to the intersection of the
// current clip and the given rectangle. Useful for scrollable containers.
// If the intersection is empty or the rectangle has non-positive dimensions,
// returns a painter with an empty clip (no output will be rendered).
func (p *Painter) WithClip(rect Rect) *Painter {
	// Calculate intersection of current clip and new rect
	left := max(p.clip.X, rect.X)
	top := max(p.clip.Y, rect.Y)
	right := min(p.clip.X+p.clip.W, rect.X+rect.W)
	bottom := min(p.clip.Y+p.clip.H, rect.Y+rect.H)

	// If no valid intersection, return painter with zero-size clip
	if left >= right || top >= bottom {
		return &Painter{buf: p.buf, clip: Rect{}}
	}

	return &Painter{
		buf: p.buf,
		clip: Rect{
			X: left,
			Y: top,
			W: right - left,
			H: bottom - top,
		},
	}
}
