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
    // selection state
    selActive    bool
    selSX, selSY int
    selEX, selEY int
    // selection direction for Shift+Arrow: -1 (left/up), +1 (right/down), 0 (none)
    selDir int
	// local clipboard
	clip string
	// invalidation callback
	inv func(core.Rect)
    // mouse state for click/drag selection
    mouseDown bool
}

func NewTextArea(x, y, w, h int) *TextArea {
	tm := theme.Get()
    bg := tm.GetColor("ui", "text_bg", tcell.ColorBlack)
    fg := tm.GetColor("ui", "text_fg", tcell.ColorWhite)
    // Default caret color: slightly greyer than text
    caret := tm.GetColor("ui", "caret_fg", tcell.ColorSilver)
	ta := &TextArea{
		Lines:      []string{""},
        Style:      tcell.StyleDefault.Background(bg).Foreground(fg),
        CaretStyle: tcell.StyleDefault.Foreground(caret),
	}
	ta.SetPosition(x, y)
	ta.Resize(w, h)
	ta.SetFocusable(true)
	return ta
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (t *TextArea) SetInvalidator(fn func(core.Rect)) { t.inv = fn }

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
		// selection style (invert)
		fg, bg, _ := t.Style.Decompose()
		selStyle := tcell.StyleDefault.Background(fg).Foreground(bg)
		for cx := t.OffX; cx < len(visible) && col < t.Rect.W; cx++ {
			style := t.Style
			if t.isSelected(cx, ly) {
				style = selStyle
			}
			p.SetCell(t.Rect.X+col, t.Rect.Y+row, visible[cx], style)
			col++
		}
	}
    // caret: draw underlying rune with reversed video (swap fg/bg)
    if t.IsFocused() {
		cx := t.CaretX - t.OffX
		cy := t.CaretY - t.OffY
		if cx >= 0 && cy >= 0 && cx < t.Rect.W && cy < t.Rect.H {
			ch := ' '
			if t.CaretY >= 0 && t.CaretY < len(t.Lines) {
				line := []rune(t.Lines[t.CaretY])
				if t.CaretX >= 0 && t.CaretX < len(line) {
					ch = line[t.CaretX]
				}
			}
            // Determine current cell style (selected or normal)
            baseStyle := t.Style
            if t.CaretY >= 0 && t.CaretY < len(t.Lines) {
                if t.isSelected(t.CaretX, t.CaretY) {
                    fg, bg, _ := t.Style.Decompose()
                    baseStyle = tcell.StyleDefault.Background(fg).Foreground(bg)
                }
            }
            fg, bg, _ := baseStyle.Decompose()
            // Reverse: swap fg and bg of the effective cell style
            caretStyle := tcell.StyleDefault.Background(fg).Foreground(bg)
            p.SetCell(t.Rect.X+cx, t.Rect.Y+cy, ch, caretStyle)
        }
    }
}

/*
	func (t *TextArea) HandleKeyOld(ev *tcell.EventKey) bool {
		// ESC clears selection
		if ev.Key() == tcell.KeyEsc {
			if t.hasSelection() {
				t.clearSelection()
				t.invalidateViewport()
				return true
			}
			return false
		}
		prevCX, prevCY := t.CaretX, t.CaretY
		// clipboard shortcuts
		if ev.Modifiers()&tcell.ModCtrl != 0 {
			switch ev.Rune() {
			case 'c':
				t.clip = t.getSelectedText()
				return true
			case 'x':
				t.clip = t.getSelectedText()
				t.deleteSelection()
				t.clampCaret()
				t.ensureVisible()
				return true
			case 'v':
				if t.clip != "" {
					t.insertText(t.clip)
					return true
				}
			}
		}
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
			if t.hasSelection() {
				t.deleteSelection()
				// Update selection after movement keys
				switch ev.Key() {
				case tcell.KeyLeft, tcell.KeyRight, tcell.KeyUp, tcell.KeyDown, tcell.KeyHome, tcell.KeyEnd:
					if ev.Modifiers()&tcell.ModShift != 0 {
						if !t.selActive {
							t.selActive = true
							t.selSX, t.selSY = prevCX, prevCY
	        }
	    case tcell.KeyDelete:
	        if t.hasSelection() {
	            t.deleteSelection()
	            t.clampCaret(); t.ensureVisible(); t.invalidateViewport()
	            return true
	        }
	        // Delete char at caret
	        if t.CaretY >= 0 && t.CaretY < len(t.Lines) {
	            line := []rune(t.Lines[t.CaretY])
	            if t.CaretX >= 0 && t.CaretX < len(line) {
	                t.Lines[t.CaretY] = string(append(line[:t.CaretX], line[t.CaretX+1:]...))
	                t.invalidateViewport()
	                return true
	            }
	        }
						t.selEX, t.selEY = t.CaretX, t.CaretY
					} else {
						t.clearSelection()
					}
					// Ensure selection visuals update immediately
					t.invalidateViewport()
				}
				t.clampCaret()
				t.ensureVisible()
				// Invalidate: if selection active, redraw viewport; else only caret move
				if t.hasSelection() {
					t.invalidateViewport()
				} else {
					t.invalidateCaretAt(prevCX, prevCY)
					t.invalidateCaretAt(t.CaretX, t.CaretY)
				}
				return true
			}
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
			if t.hasSelection() {
				t.deleteSelection()
			}
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
		// Update selection after movement keys
		switch ev.Key() {
		case tcell.KeyLeft, tcell.KeyRight, tcell.KeyUp, tcell.KeyDown, tcell.KeyHome, tcell.KeyEnd:
			if ev.Modifiers()&tcell.ModShift != 0 {
				if !t.selActive {
					t.selActive = true
					t.selSX, t.selSY = prevCX, prevCY
				}
				t.selEX, t.selEY = t.CaretX, t.CaretY
			} else {
				t.clearSelection()
			}
			t.invalidateViewport()
		}
		t.clampCaret()
		t.ensureVisible()
		return true
	}

// Mouse-aware implementation for selection and scrolling.
*/
func (t *TextArea) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	lx := x - t.Rect.X
	ly := y - t.Rect.Y
	if lx < 0 || ly < 0 || lx >= t.Rect.W || ly >= t.Rect.H {
		return false
	}
	btn := ev.Buttons()
	if btn&(tcell.WheelUp|tcell.WheelDown) != 0 {
		if btn&tcell.WheelUp != 0 && t.OffY > 0 {
			t.OffY--
		}
		if btn&tcell.WheelDown != 0 {
			t.OffY++
		}
		t.invalidateViewport()
		return true
	}
	if btn&tcell.Button1 != 0 {
		// press/drag
		if !t.mouseDown {
			t.mouseDown = true
			t.CaretY = t.OffY + ly
			if t.CaretY >= len(t.Lines) {
				t.CaretY = len(t.Lines) - 1
			}
			t.CaretX = t.OffX + lx
			t.startSelection()
		}
		t.extendSelection()
		t.clampCaret()
		t.ensureVisible()
		t.invalidateViewport()
		return true
	}
	// release: if no selection range, clear selection
	if t.mouseDown && btn&tcell.Button1 == 0 {
		t.mouseDown = false
		if !t.hasSelection() {
			t.clearSelection()
			t.invalidateViewport()
		}
		return true
	}
	return false
}

func (t *TextArea) startSelection() {
	t.selActive = true
	t.selSX, t.selSY = t.CaretX, t.CaretY
	t.selEX, t.selEY = t.CaretX, t.CaretY
}
func (t *TextArea) extendSelection() {
	if !t.selActive {
		t.startSelection()
		return
	}
	// Update raw end (exclusive handled by consumers where needed)
	t.selEX, t.selEY = t.CaretX, t.CaretY
}
func (t *TextArea) clearSelection() { t.selActive = false; t.selDir = 0 }
func (t *TextArea) hasSelection() bool {
    // Under the new semantics, a single-cell selection (sx==ex, sy==ey) is valid.
    return t.selActive
}

// SelectedRange returns the current selection start and end on the same line
// for debugging/tests. If no selection or multi-line, returns (-1,-1).
func (t *TextArea) SelectedRange() (int, int) {
    if !t.hasSelection() || t.selSY != t.selEY {
        return -1, -1
    }
    sx, _, ex, _ := t.selSX, t.selSY, t.selEX, t.selEY
    if sx > ex { sx, ex = ex, sx }
    return sx, ex
}

func (t *TextArea) isSelected(cx, cy int) bool {
    if !t.hasSelection() {
        return false
    }
    sx, sy, ex, ey := t.selSX, t.selSY, t.selEX, t.selEY
    // Normalize order so (sx,sy) -> (ex,ey) is forward
    if ey < sy || (ey == sy && ex < sx) {
        sx, sy, ex, ey = ex, ey, sx, sy
    }
    if cy < sy || cy > ey {
        return false
    }
    if sy == ey {
        // Single-line selection uses inclusive end [sx, ex]
        return cx >= sx && cx <= ex
    }
    if cy == sy {
        return cx >= sx
    }
    if cy == ey {
        return cx < ex
    }
    return true
}

func (t *TextArea) getSelectedText() string {
	if !t.hasSelection() {
		return ""
	}
	sx, sy, ex, ey := t.selSX, t.selSY, t.selEX, t.selEY
	if sy > ey || (sy == ey && sx > ex) {
		sx, sy, ex, ey = ex, ey, sx, sy
	}
    if sy == ey {
        r := []rune(t.Lines[sy])
        if ex >= len(r) {
            ex = len(r) - 1
        }
        if sx < 0 { sx = 0 }
        if sx > len(r) { sx = len(r) }
        if ex < sx { return "" }
        // Inclusive end: slice [sx:ex+1]
        return string(r[sx : ex+1])
    }
	out := ""
	r := []rune(t.Lines[sy])
	out += string(r[sx:]) + "\n"
	for yy := sy + 1; yy < ey; yy++ {
		out += t.Lines[yy] + "\n"
	}
    rr := []rune(t.Lines[ey])
    if ex >= len(rr) {
        ex = len(rr) - 1
    }
    if ex >= 0 {
        // Inclusive end on the last line
        out += string(rr[:ex+1])
    }
    return out
}

func (t *TextArea) deleteSelection() {
    if !t.hasSelection() {
        return
    }
    sx, sy, ex, ey := t.selSX, t.selSY, t.selEX, t.selEY
    // Normalize order so (sx,sy) -> (ex,ey) is forward
    if ey < sy || (ey == sy && ex < sx) {
        sx, sy, ex, ey = ex, ey, sx, sy
    }
    if sy == ey {
        r := []rune(t.Lines[sy])
        if ex >= len(r) {
            ex = len(r) - 1
        }
        if sx < 0 {
            sx = 0
        }
        if sx > len(r) {
            sx = len(r)
        }
        if ex < sx {
            t.clearSelection()
            t.invalidateViewport()
            return
        }
        // Remove inclusive [sx, ex]
        t.Lines[sy] = string(append(r[:sx], r[ex+1:]...))
        t.CaretX, t.CaretY = sx, sy
        t.clearSelection()
        t.invalidateViewport()
        return
    }
    head := []rune(t.Lines[sy])
    tail := []rune(t.Lines[ey])
    if ex >= len(tail) {
        ex = len(tail) - 1
    }
    if sx < 0 {
        sx = 0
    }
    if sx > len(head) {
        sx = len(head)
    }
    if ex < 0 { ex = -1 }
    // Remove inclusive [sx, ex] across lines: keep left head[:sx] + right tail[ex+1:]
    newHead := string(head[:sx]) + string(tail[ex+1:])
    t.Lines = append(t.Lines[:sy+1], t.Lines[ey+1:]...)
    t.Lines[sy] = newHead
    t.CaretX, t.CaretY = sx, sy
    t.clearSelection()
    t.invalidateViewport()
}

func (t *TextArea) insertText(s string) {
	for _, r := range s {
		if r == '\n' {
			line := t.Lines[t.CaretY]
			head := []rune(line)[:t.CaretX]
			tail := []rune(line)[t.CaretX:]
			t.Lines[t.CaretY] = string(head)
			t.Lines = append(t.Lines[:t.CaretY+1], append([]string{""}, t.Lines[t.CaretY+1:]...)...)
			t.Lines[t.CaretY+1] = string(tail)
			t.CaretY++
			t.CaretX = 0
		} else {
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
		}
	}
	t.clampCaret()
	t.ensureVisible()
	t.invalidateViewport()
}
func (t *TextArea) invalidateViewport() {
	if t.inv == nil {
		return
	}
	t.inv(t.Rect)
}
func (t *TextArea) invalidateCaretAt(cx, cy int) {
    if t.inv == nil {
        return
    }
    vx := cx - t.OffX
    vy := cy - t.OffY
    if vx < 0 || vy < 0 || vx >= t.Rect.W || vy >= t.Rect.H {
        return
    }
    t.inv(core.Rect{X: t.Rect.X + vx, Y: t.Rect.Y + vy, W: 1, H: 1})
}
