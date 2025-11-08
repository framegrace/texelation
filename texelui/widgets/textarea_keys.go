package widgets

import (
    "github.com/gdamore/tcell/v2"
)

// HandleKey implements basic editing and movement (selection disabled).
func (t *TextArea) HandleKey(ev *tcell.EventKey) bool {
    // ESC not handled (no selection state)
    if ev.Key() == tcell.KeyEsc {
        return false
    }

    if ev.Modifiers()&tcell.ModCtrl != 0 {
        if ev.Rune() == 'v' && t.clip != "" {
            t.insertText(t.clip)
            return true
        }
    }

    switch ev.Key() {
    case tcell.KeyInsert:
        // Toggle insert/replace mode
        t.replaceMode = !t.replaceMode
        t.invalidateCaretAt(t.CaretX, t.CaretY)
        return true
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
        r := ev.Rune()
        line := []rune(t.Lines[t.CaretY])
        if t.CaretX < 0 {
            t.CaretX = 0
        }
        if t.CaretX > len(line) {
            t.CaretX = len(line)
        }
        if t.replaceMode && t.CaretX < len(line) {
            // Overwrite current character
            line[t.CaretX] = r
            t.Lines[t.CaretY] = string(line)
            t.CaretX++
        } else {
            // Insert mode (default)
            line = append(line[:t.CaretX], append([]rune{r}, line[t.CaretX:]...)...)
            t.Lines[t.CaretY] = string(line)
            t.CaretX++
        }
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
