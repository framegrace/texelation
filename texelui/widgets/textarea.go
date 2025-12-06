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
	// local clipboard (paste only for now)
	clip string
	// invalidation callback
	inv func(core.Rect)
    // mouse state
    mouseDown bool
    // insert vs replace mode: false=insert (default), true=replace (overwrite)
    replaceMode bool
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
    // Enable focused styling using theme defaults (falls back to text colors)
    fbg := tm.GetColor("ui", "focus_text_bg", bg)
    ffg := tm.GetColor("ui", "focus_text_fg", fg)
    ta.SetFocusedStyle(tcell.StyleDefault.Background(fbg).Foreground(ffg), true)
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
    // With wrapping, horizontal offset is unused
    t.OffX = 0
    // Ensure caret's visual row is within the viewport
    _, vcy := t.caretVisualPos()
    if vcy < t.OffY {
        t.OffY = vcy
    }
    if vcy >= t.OffY+t.Rect.H {
        t.OffY = vcy - t.Rect.H + 1
    }
    if t.OffY < 0 {
        t.OffY = 0
    }
}

func (t *TextArea) Draw(p *core.Painter) {
    // fill background
    base := t.EffectiveStyle(t.Style)
    p.Fill(t.Rect, ' ', base)
    // draw wrapped content
    if t.Rect.W <= 0 || t.Rect.H <= 0 { return }
    globalRow := 0
    drawnRows := 0
    for li := 0; li < len(t.Lines) && drawnRows < t.Rect.H; li++ {
        r := []rune(t.Lines[li])
        if len(r) == 0 {
            // empty line still occupies one visual row
            if globalRow >= t.OffY {
                // nothing to draw beyond background
                drawnRows++
            }
            globalRow++
            continue
        }
        for start := 0; start < len(r) && drawnRows < t.Rect.H; start += t.Rect.W {
            end := start + t.Rect.W
            if end > len(r) { end = len(r) }
            if globalRow >= t.OffY {
                row := drawnRows
                col := 0
                for i := start; i < end && col < t.Rect.W; i++ {
                    p.SetCell(t.Rect.X+col, t.Rect.Y+row, r[i], base)
                    col++
                }
                drawnRows++
            }
            globalRow++
        }
    }
    // caret: draw underlying rune; reverse in insert mode, underline in replace mode
    if t.IsFocused() {
        cx, cy := t.caretVisualPos()
        cy = cy - t.OffY
        if cx >= 0 && cy >= 0 && cx < t.Rect.W && cy < t.Rect.H {
            ch := ' '
            if t.CaretY >= 0 && t.CaretY < len(t.Lines) {
                line := []rune(t.Lines[t.CaretY])
                if t.CaretX >= 0 && t.CaretX < len(line) {
                    ch = line[t.CaretX]
                }
            }
            // Determine current cell style (no selection styling)
            baseStyle := base
            fg, bg, _ := baseStyle.Decompose()
            var caretStyle tcell.Style
            if t.replaceMode {
                // Underline caret in replace mode
                caretStyle = tcell.StyleDefault.Background(bg).Foreground(fg).Underline(true)
            } else {
                // Reverse video caret in insert mode
                caretStyle = tcell.StyleDefault.Background(fg).Foreground(bg)
            }
            p.SetCell(t.Rect.X+cx, t.Rect.Y+cy, ch, caretStyle)
        }
    }
}

// caretVisualPos returns the caret position in visual (wrapped) coordinates (x within width, y as visual row index).
func (t *TextArea) caretVisualPos() (int, int) {
    if t.Rect.W <= 0 { return 0, 0 }
    vrow := 0
    for li := 0; li < len(t.Lines) && li < t.CaretY; li++ {
        r := []rune(t.Lines[li])
        // number of wrapped rows for this line (at least 1)
        n := (len(r) + t.Rect.W - 1) / t.Rect.W
        if n == 0 { n = 1 }
        vrow += n
    }
    // for current line
    cx := t.CaretX
    if cx < 0 { cx = 0 }
    r := []rune("")
    if t.CaretY >=0 && t.CaretY < len(t.Lines) { r = []rune(t.Lines[t.CaretY]) }
    if cx > len(r) { cx = len(r) }
    vrow += cx / max(1, t.Rect.W)
    vx := cx % max(1, t.Rect.W)
    return vx, vrow
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
        // simple click-to-caret on wrapped layout
        vrow := t.OffY + ly
        // Map visual row to logical line and offset
        li, start := t.visualRowToLogical(vrow)
        t.CaretY = li
        // clamp x to segment length
        segLen := t.segmentLen(li, start)
        dx := lx
        if dx > segLen { dx = segLen }
        t.CaretX = start + dx
        t.clampCaret()
        t.ensureVisible()
        t.invalidateViewport()
        return true
    }
    return false
}

// visualRowToLogical maps a visual wrapped row index to (logical line, start rune offset of that segment).
func (t *TextArea) visualRowToLogical(vrow int) (int, int) {
    if t.Rect.W <= 0 { return 0, 0 }
    row := 0
    for li := 0; li < len(t.Lines); li++ {
        r := []rune(t.Lines[li])
        n := (len(r) + t.Rect.W - 1) / t.Rect.W
        if n == 0 { n = 1 }
        if vrow < row+n {
            // within this logical line
            segIdx := vrow - row
            start := segIdx * t.Rect.W
            if start > len(r) { start = len(r) }
            return li, start
        }
        row += n
    }
    // past end: clamp to last line end
    li := len(t.Lines) - 1
    if li < 0 { li = 0 }
    r := []rune("")
    if li < len(t.Lines) { r = []rune(t.Lines[li]) }
    return li, len(r)
}

func (t *TextArea) segmentLen(li, start int) int {
    if li < 0 || li >= len(t.Lines) || t.Rect.W <= 0 { return 0 }
    r := []rune(t.Lines[li])
    if start < 0 { start = 0 }
    if start > len(r) { return 0 }
    end := start + t.Rect.W
    if end > len(r) { end = len(r) }
    return end - start
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
