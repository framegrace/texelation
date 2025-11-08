package widgets

import (
    "github.com/gdamore/tcell/v2"
)

// HandleKey implements keyboard editing, selection, and clipboard operations.
func (t *TextArea) HandleKey(ev *tcell.EventKey) bool {
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
			t.invalidateViewport()
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
        if ev.Modifiers()&tcell.ModShift != 0 {
            t.handleShiftArrow(-1, 0)
            return true
        }
        t.clearSelection(); t.selDir = 0
        t.CaretX--
    case tcell.KeyRight:
        if ev.Modifiers()&tcell.ModShift != 0 {
            t.handleShiftArrow(1, 0)
            return true
        }
        t.clearSelection(); t.selDir = 0
        t.CaretX++
    case tcell.KeyUp:
        if ev.Modifiers()&tcell.ModShift != 0 {
            t.handleShiftArrow(0, -1)
            return true
        }
        t.clearSelection(); t.selDir = 0
        t.CaretY--
    case tcell.KeyDown:
        if ev.Modifiers()&tcell.ModShift != 0 {
            t.handleShiftArrow(0, 1)
            return true
        }
        t.clearSelection(); t.selDir = 0
        t.CaretY++
    case tcell.KeyHome:
        if ev.Modifiers()&tcell.ModShift != 0 {
            if !t.selActive {
                t.selActive = true
                t.selSX, t.selSY = prevCX, prevCY
            }
            t.CaretX = 0
            t.clampCaret(); t.ensureVisible()
            t.selEX, t.selEY = t.CaretX, t.CaretY
            t.invalidateViewport()
            return true
        }
        t.clearSelection(); t.selDir = 0
        t.CaretX = 0
    case tcell.KeyEnd:
        if ev.Modifiers()&tcell.ModShift != 0 {
            if !t.selActive {
                t.selActive = true
                t.selSX, t.selSY = prevCX, prevCY
            }
            t.CaretX = 1 << 30
            t.clampCaret(); t.ensureVisible()
            t.selEX, t.selEY = t.CaretX, t.CaretY
            t.invalidateViewport()
            return true
        }
        t.clearSelection(); t.selDir = 0
        t.CaretX = 1 << 30
	case tcell.KeyEnter:
		line := t.Lines[t.CaretY]
		head := []rune(line)[:t.CaretX]
		tail := []rune(line)[t.CaretX:]
		t.Lines[t.CaretY] = string(head)
		t.Lines = append(t.Lines[:t.CaretY+1], append([]string{""}, t.Lines[t.CaretY+1:]...)...)
		t.Lines[t.CaretY+1] = string(tail)
		t.CaretY++
		t.CaretX = 0
		t.invalidateViewport()
		return true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if t.hasSelection() {
			t.deleteSelection()
			t.clampCaret()
			t.ensureVisible()
			t.invalidateViewport()
			return true
		}
		if t.CaretX > 0 {
			line := []rune(t.Lines[t.CaretY])
			t.Lines[t.CaretY] = string(append(line[:t.CaretX-1], line[t.CaretX:]...))
			t.CaretX--
			t.invalidateViewport()
			return true
		} else if t.CaretY > 0 {
			prev := t.Lines[t.CaretY-1]
			cur := t.Lines[t.CaretY]
			t.CaretX = len([]rune(prev))
			t.Lines[t.CaretY-1] = prev + cur
			t.Lines = append(t.Lines[:t.CaretY], t.Lines[t.CaretY+1:]...)
			t.CaretY--
			t.invalidateViewport()
			return true
		}
		return false
	case tcell.KeyDelete:
		if t.hasSelection() {
			t.deleteSelection()
			t.clampCaret()
			t.ensureVisible()
			t.invalidateViewport()
			return true
		}
		if t.CaretY >= 0 && t.CaretY < len(t.Lines) {
			line := []rune(t.Lines[t.CaretY])
			if t.CaretX >= 0 && t.CaretX < len(line) {
				t.Lines[t.CaretY] = string(append(line[:t.CaretX], line[t.CaretX+1:]...))
				t.invalidateViewport()
				return true
			}
		}
		return false
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
		t.invalidateViewport()
		return true
    default:
        // Not handled
        return false
    }
    t.clampCaret()
    t.ensureVisible()
    return true
}

// handleShiftArrow applies the selection algorithm requested:
// - On first Shift+Arrow: set both selection ends to current char, move caret.
// - On continued Shift+Arrow in same direction: set end to current position, then move caret.
// - On opposite direction: move caret first, then set end to new position.
// dx,dy in {-1,0,1}; horizontal uses dx; vertical uses dy.
func (t *TextArea) handleShiftArrow(dx, dy int) {
    dir := 0
    if dx < 0 || dy < 0 { dir = -1 }
    if dx > 0 || dy > 0 { dir = 1 }

    if !t.selActive {
        t.selActive = true
        t.selSX, t.selSY = t.CaretX, t.CaretY
        t.selEX, t.selEY = t.CaretX, t.CaretY
        t.selDir = dir
        // move caret
        t.moveCaretBy(dx, dy)
        t.clampCaret(); t.ensureVisible(); t.invalidateViewport()
        return
    }
    if t.selDir == 0 || t.selDir == dir {
        // same direction: snapshot end at current caret, then move
        t.selEX, t.selEY = t.CaretX, t.CaretY
        t.moveCaretBy(dx, dy)
    } else {
        // opposite: move first, then set end to new caret
        t.moveCaretBy(dx, dy)
        t.selEX, t.selEY = t.CaretX, t.CaretY
        t.selDir = dir
    }
    t.clampCaret(); t.ensureVisible(); t.invalidateViewport()
}

func (t *TextArea) moveCaretBy(dx, dy int) {
    if dy != 0 {
        t.CaretY += dy
    }
    if dx != 0 {
        t.CaretX += dx
    }
}
