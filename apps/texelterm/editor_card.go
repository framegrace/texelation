package texelterm

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
    "os"
)

// longLineEditorCard overlays a TextArea when the current line exceeds the viewport width
// (or caret moves beyond it) and forwards keys to the underlying terminal app.
// It renders on top of the buffer and consumes no events from the pipeline by design —
// the underlying app still receives keys to keep the PTY authoritative.
type longLineEditorCard struct {
	term      *TexelTerm
	ta        *widgets.TextArea
	active    bool
	wasActive bool
	rect      core.Rect
	refresh   chan<- bool
	w, h      int

    enabled bool
    pendingAfterScroll bool
    scrollSize int
}

func newLongLineEditorCard(term *TexelTerm) *longLineEditorCard {
    cfg := theme.Get()
    enabled := cfg.GetBool("texelterm", "longline_overlay_enabled", true)
    if v := os.Getenv("TEXEL_LONG_LINE_ENABLED"); v == "0" || v == "false" {
        enabled = false
    }
    return &longLineEditorCard{term: term, enabled: enabled}
}

func (c *longLineEditorCard) Run() error                        { return nil }
func (c *longLineEditorCard) Stop()                             {}
func (c *longLineEditorCard) Resize(cols, rows int)             { c.w, c.h = cols, rows }
func (c *longLineEditorCard) SetRefreshNotifier(ch chan<- bool) { c.refresh = ch }
func (c *longLineEditorCard) HandleMessage(texel.Message)       {}

func (c *longLineEditorCard) HandleKey(ev *tcell.EventKey) {
	// Note: card no longer relies on pipeline delivery; authority switching is handled via control func.
	if !c.enabled {
		return
	}
	// If we are active and have a TA, handle locally and request refresh.
	if c.active && c.ta != nil {
		// Safety: if capture no longer desired, ignore keys here
		if !c.shouldCapture() {
			return
		}
		_ = c.ta.HandleKey(ev)
		if c.refresh != nil {
			select {
			case c.refresh <- true:
			default:
			}
		}
	}
}

func (c *longLineEditorCard) Render(input [][]texel.Cell) [][]texel.Cell {
	// Pass-through if no input or not enabled
	buf := input
	if buf == nil {
		return nil
	}
	if !c.enabled {
		c.active = false
		return buf
	}
	t := c.term
	t.mu.Lock()
	v := t.vterm
	cols := 0
	rows := 0
	if len(buf) > 0 {
		rows = len(buf)
		cols = len(buf[0])
	}
	cursorX, cursorY := 0, 0
	if v != nil {
		cursorX, cursorY = v.Cursor()
	}
	top := 0
	if v != nil {
		top = v.VisibleTop()
	}
	lineIdx := top + cursorY
	var text string
	if v != nil && v.HistoryLength() > 0 {
		if cells := v.HistoryLineCopy(lineIdx); cells != nil {
			// Use raw runes (no trim) so trailing spaces remain visible
			text = string(cellsToRunes(cells))
		}
	}
	t.mu.Unlock()

	if cols == 0 || rows == 0 {
		c.active = false
		return buf
	}

    // Determine input start column via shell integration; if unknown, do not open editor yet
    startX := 0
    startKnown := false
    if t != nil {
        t.mu.Lock()
        startKnown = t.inputStartKnown
        if startKnown { startX = t.inputStartCol }
        t.mu.Unlock()
    }
    if !startKnown {
        // We don't know the prompt/input split yet; do not activate the editor.
        c.deactivate()
        return buf
    }
    if startX < 0 { startX = 0 }
    if startX > cols-1 { startX = cols-1 }

    // Consider only the input portion for editing. When editor is active, use the
    // TextArea buffer (we don't forward keys to the terminal while editing),
    // otherwise seed from the terminal line.
    rr := []rune(text)
    if startX > len(rr) { startX = len(rr) }
    inputText := ""
    if c.active && c.ta != nil && len(c.ta.Lines) > 0 {
        inputText = c.ta.Lines[0]
    } else {
        inputText = string(rr[startX:])
    }

    // Show editor only when input exceeds available columns
    avail := cols - startX
    if avail < 1 { avail = 1 }
    long := len([]rune(inputText)) > avail
    c.capture = long
    shouldDraw := long
	if !shouldDraw {
		c.deactivate()
		if c.ta != nil {
			c.ta.Lines = []string{text}
			cx := cursorX
			if cx < 0 {
				cx = 0
			}
			if cx > len([]rune(text)) {
				cx = len([]rune(text))
			}
			c.ta.CaretX = cx
			c.ta.CaretY = 0
		}
		return buf
	}
    if !c.active || c.ta == nil {
        if c.ta == nil {
            c.ta = widgets.NewTextArea(0, 0, 0, 0)
            c.ta.SetFocusable(true)
        }
        c.ta.Lines = []string{inputText}
        cx := cursorX - startX
        if cx < 0 {
            cx = 0
        }
        if cx > len([]rune(inputText)) {
            cx = len([]rune(inputText))
        }
        c.ta.CaretX = cx
        c.ta.CaretY = 0
        c.active = true
        c.wasActive = true
        // If opening at the last screen row, start visual scroll (skip one row)
        if cursorY == rows-1 {
            c.scrollSize = 1
        } else {
            c.scrollSize = 0
        }
    }

    // Determine overlay rect anchored at cursor line; grow down, scroll up if at bottom
    editorW := cols - startX
    if editorW < 1 { editorW = 1 }
    // Required wrapped rows for input
    ir := []rune(inputText)
    needH := len(ir) / editorW
    if len(ir)%editorW != 0 { needH++ }
    if needH < 1 { needH = 1 }
    // Visual scroll: adjust overlay Y by current scrollSize
    oy := cursorY - c.scrollSize
    if oy+needH > rows {
        // Remove special last-line scrolling; simply anchor editor within bounds
        oy = rows - needH
        if oy < 0 { oy = 0 }
    }
    // Growth detection: if editor height increased, increase visual scroll accordingly
    prevH := c.rect.H
    c.rect = core.Rect{X: startX, Y: oy, W: editorW, H: needH}
    if prevH > 0 && needH > prevH {
        c.scrollSize += (needH - prevH)
        // Re-adjust overlay Y after scroll change
        newY := cursorY - c.scrollSize
        if newY+needH > rows { newY = rows - needH }
        if newY < 0 { newY = 0 }
        c.rect.Y = newY
    }

	// Initialize TA if needed (already handled on activation)
	if c.ta == nil {
		c.ta = widgets.NewTextArea(0, 0, 0, 0)
		c.ta.SetFocusable(true)
	}
	// Theme
	cfg := theme.Get()
	bg := cfg.GetColor("texelterm", "longline_overlay_bg", tcell.NewRGBColor(56, 58, 70))
	fg := cfg.GetColor("texelterm", "longline_overlay_fg", tcell.ColorWhite)
	style := tcell.StyleDefault.Background(bg).Foreground(fg)

	// Configure TA depending on authority
	c.ta.SetPosition(c.rect.X, c.rect.Y)
	c.ta.Resize(c.rect.W, c.rect.H)
	c.ta.Style = style
    // Authoritative editor while active
    c.ta.Focus()

    // Prepare output buffer applying visual scroll (skip top scrollSize rows)
    out := buf
    if c.scrollSize > 0 && c.scrollSize < rows {
        out = make([][]texel.Cell, rows)
        // Base clear style for blank rows at the bottom after scroll
        tm := theme.Get()
        clrBg := tm.GetColor("ui", "surface_bg", tcell.ColorBlack)
        clrFg := tm.GetColor("ui", "surface_fg", tcell.ColorWhite)
        clearStyle := tcell.StyleDefault.Background(clrBg).Foreground(clrFg)
        for y := 0; y < rows; y++ {
            out[y] = make([]texel.Cell, cols)
            sy := y + c.scrollSize
            if sy < rows {
                copy(out[y], buf[sy])
            } else {
                for x := 0; x < cols; x++ {
                    out[y][x] = texel.Cell{Ch: ' ', Style: clearStyle}
                }
            }
        }
    }
    // Draw overlay
    p := core.NewPainter(out, core.Rect{X: 0, Y: 0, W: cols, H: rows})
    p.Fill(c.rect, ' ', style)
    c.ta.Draw(p)
    return out
}

// shouldCapture returns true if overlay should be authoritative based on caret position.
func (c *longLineEditorCard) shouldCapture() bool { return c.enabled && c.active }

// ensureActive seeds the overlay TA from the current terminal line if not already active.
func (c *longLineEditorCard) ensureActive() {
	if c.active {
		return
	}
	if !c.enabled || c.term == nil || c.term.vterm == nil {
		return
	}
	v := c.term.vterm
	cursorX, cursorY := v.Cursor()
	top := v.VisibleTop()
	lineIdx := top + cursorY
	text := ""
	if v.HistoryLength() > 0 {
		if cells := v.HistoryLineCopy(lineIdx); cells != nil {
			text = string(cellsToRunes(cells))
		}
	}
	if c.ta == nil {
		c.ta = widgets.NewTextArea(0, 0, 0, 0)
		c.ta.SetFocusable(true)
	}
	c.ta.Lines = []string{text}
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorX > len([]rune(text)) {
		cursorX = len([]rune(text))
	}
	c.ta.CaretX = cursorX
	c.ta.CaretY = 0
	// No prompt detection; keep ReadOnlyPrefix at default (0)
	c.active = true
}

// deactivate immediately disables overlay and blurs the TextArea
func (c *longLineEditorCard) deactivate() {
    if c.active {
        c.active = false
        if c.ta != nil {
            c.ta.Blur()
        }
        // Mark overlay lines dirty so the base renderer clears any remnants
        if c.term != nil {
            c.term.mu.Lock()
            if c.term.vterm != nil {
                for y := c.rect.Y; y < c.rect.Y+c.rect.H; y++ {
                    c.term.vterm.MarkDirty(y)
                }
            }
            c.term.mu.Unlock()
        }
        if c.refresh != nil { select { case c.refresh <- true: default: } }
    }
}

// interceptKey is called by the pipeline control when overlay should be authoritative.
func (c *longLineEditorCard) interceptKey(ev *tcell.EventKey) {
    c.ensureActive()
    if c.ta == nil { return }
    // Vertical movement across wrapped rows using editor width
    if c.term != nil && (ev.Key() == tcell.KeyUp || ev.Key() == tcell.KeyDown) {
        cols := 0
        c.term.mu.Lock()
        if c.term.vterm != nil {
            grid := c.term.vterm.Grid()
            if len(grid) > 0 { cols = len(grid[0]) }
        }
        startX := 0
        if c.term.inputStartKnown { startX = c.term.inputStartCol }
        c.term.mu.Unlock()
        editorW := cols - startX
        if editorW < 1 { editorW = 1 }
        line := []rune("")
        if len(c.ta.Lines) > 0 { line = []rune(c.ta.Lines[0]) }
        nx := c.ta.CaretX
        if ev.Key() == tcell.KeyUp {
            nx -= editorW
        } else {
            nx += editorW
        }
        if nx < 0 { nx = 0 }
        if nx > len(line) { nx = len(line) }
        c.ta.CaretX = nx
        if c.refresh != nil { select { case c.refresh <- true: default: } }
        return
    }
    switch ev.Key() {
    case tcell.KeyEsc:
        c.deactivate()
        if c.refresh != nil { select { case c.refresh <- true: default: } }
        return
    case tcell.KeyCtrlC, tcell.KeyCtrlD:
        if c.term != nil { c.term.HandleKey(ev) }
        c.deactivate()
        if c.refresh != nil { select { case c.refresh <- true: default: } }
        return
    case tcell.KeyEnter:
        // Reset visual scroll before committing so base buffer returns to normal
        c.scrollSize = 0
        c.commitEditor()
        c.deactivate()
        if c.refresh != nil { select { case c.refresh <- true: default: } }
        return
    }
    _ = c.ta.HandleKey(ev)
    if c.refresh != nil { select { case c.refresh <- true: default: } }
}

func (c *longLineEditorCard) commitEditor() {
    if c.term == nil || c.term.pty == nil || c.ta == nil { return }
    // Concatenate lines with spaces
    text := ""
    for i, s := range c.ta.Lines { if i > 0 { text += " " }; text += s }
    // ^A, ^K then bracketed paste text and Enter
    seq := []byte{0x01, 0x0b}
    seq = append(seq, []byte("\x1b[200~")...)
    seq = append(seq, []byte(text)...)
    seq = append(seq, []byte("\x1b[201~")...)
    seq = append(seq, '\r')
    _, _ = c.term.pty.Write(seq)
}
