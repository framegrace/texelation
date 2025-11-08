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
        // Clamp and ensure visibility before invalidation so redraw reflects new viewport
        t.clampCaret()
        t.ensureVisible()
        t.invalidateViewport()
        return true
    }
    t.clampCaret()
    t.ensureVisible()
    return true
}
