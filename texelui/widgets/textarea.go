package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// TextArea is a minimal multiline text editor with viewport.
type TextArea struct {
	core.BaseWidget
	Lines      []string
	CaretX     int
	CaretY     int
	OffX       int
	OffY       int
	Style      tcell.Style
	CaretStyle tcell.Style
}

func NewTextArea(x, y, w, h int) *TextArea {
	tm := theme.Get()
	bg := tm.GetColor("ui", "text_bg", tcell.ColorBlack)
	fg := tm.GetColor("ui", "text_fg", tcell.ColorWhite)
	caret := tm.GetColor("ui", "caret_fg", fg)
	ta := &TextArea{
		Lines:      []string{""},
		Style:      tcell.StyleDefault.Background(bg).Foreground(fg),
		CaretStyle: tcell.StyleDefault.Background(fg).Foreground(caret),
	}
	ta.SetPosition(x, y)
	ta.Resize(w, h)
	ta.SetFocusable(true)
	return ta
}

func (t *TextArea) clampCaret() {
	if t.CaretY < 0 {
		t.CaretY = 0
	}
	if t.CaretY >= len(t.Lines) {
		t.CaretY = len(t.Lines) - 1
	}
	if t.CaretY < 0 {
		t.CaretY = 0
	}
	maxX := len([]rune(t.Lines[t.CaretY]))
	if t.CaretX < 0 {
		t.CaretX = 0
	}
	if t.CaretX > maxX {
		t.CaretX = maxX
	}
}

func (t *TextArea) ensureVisible() {
	// horizontal
	if t.CaretX < t.OffX {
		t.OffX = t.CaretX
	}
	if t.CaretX >= t.OffX+t.Rect.W {
		t.OffX = t.CaretX - t.Rect.W + 1
	}
	if t.OffX < 0 {
		t.OffX = 0
	}
	// vertical
	if t.CaretY < t.OffY {
		t.OffY = t.CaretY
	}
	if t.CaretY >= t.OffY+t.Rect.H {
		t.OffY = t.CaretY - t.Rect.H + 1
	}
	if t.OffY < 0 {
		t.OffY = 0
	}
}

func (t *TextArea) Draw(p *core.Painter) {
	// fill background
	p.Fill(t.Rect, ' ', t.Style)
	// draw visible lines
	for row := 0; row < t.Rect.H; row++ {
		ly := t.OffY + row
		if ly >= len(t.Lines) {
			break
		}
		visible := []rune(t.Lines[ly])
		// column window
		col := 0
		for cx := t.OffX; cx < len(visible) && col < t.Rect.W; cx++ {
			p.SetCell(t.Rect.X+col, t.Rect.Y+row, visible[cx], t.Style)
			col++
		}
	}
	// caret (invert style of cell at caret when focused)
	if t.IsFocused() {
		cx := t.CaretX - t.OffX
		cy := t.CaretY - t.OffY
		if cx >= 0 && cy >= 0 && cx < t.Rect.W && cy < t.Rect.H {
			// draw space with caret style at caret location
			p.SetCell(t.Rect.X+cx, t.Rect.Y+cy, ' ', t.CaretStyle)
		}
	}
}

func (t *TextArea) HandleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyLeft:
		t.CaretX--
	case tcell.KeyRight:
		t.CaretX++
	case tcell.KeyUp:
		t.CaretY--
	case tcell.KeyDown:
		t.CaretY++
	case tcell.KeyHome:
		t.CaretX = 0
	case tcell.KeyEnd:
		t.CaretX = 1 << 30
	case tcell.KeyEnter:
		// split line
		line := t.Lines[t.CaretY]
		head := []rune(line)[:t.CaretX]
		tail := []rune(line)[t.CaretX:]
		t.Lines[t.CaretY] = string(head)
		t.Lines = append(t.Lines[:t.CaretY+1], append([]string{""}, t.Lines[t.CaretY+1:]...)...)
		t.Lines[t.CaretY+1] = string(tail)
		t.CaretY++
		t.CaretX = 0
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if t.CaretX > 0 {
			line := []rune(t.Lines[t.CaretY])
			t.Lines[t.CaretY] = string(append(line[:t.CaretX-1], line[t.CaretX:]...))
			t.CaretX--
		} else if t.CaretY > 0 {
			// join with previous
			prev := t.Lines[t.CaretY-1]
			cur := t.Lines[t.CaretY]
			t.CaretX = len([]rune(prev))
			t.Lines[t.CaretY-1] = prev + cur
			t.Lines = append(t.Lines[:t.CaretY], t.Lines[t.CaretY+1:]...)
			t.CaretY--
		}
	case tcell.KeyRune:
		r := ev.Rune()
		line := []rune(t.Lines[t.CaretY])
		if t.CaretX < 0 {
			t.CaretX = 0
		}
		if t.CaretX > len(line) {
			t.CaretX = len(line)
		}
		line = append(line[:t.CaretX], append([]rune{r}, line[t.CaretX:]...)...)
		t.Lines[t.CaretY] = string(line)
		t.CaretX++
	default:
		return false
	}
	t.clampCaret()
	t.ensureVisible()
	return true
}
