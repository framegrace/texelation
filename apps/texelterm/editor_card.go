package texelterm

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

// longLineEditorCard overlays a TextArea when the current line exceeds the viewport width
// (or caret moves beyond it) and forwards keys to the underlying terminal app.
// It renders on top of the buffer and consumes no events from the pipeline by design —
// the underlying app still receives keys to keep the PTY authoritative.
type longLineEditorCard struct {
	term      *TexelTerm
	ta        *widgets.TextArea
	active    bool
	capture   bool
	wasActive bool
	rect      core.Rect
	refresh   chan<- bool
	w, h      int

	enabled bool
}

func newLongLineEditorCard(term *TexelTerm) *longLineEditorCard {
	cfg := theme.Get()
	enabled := cfg.GetBool("texelterm", "longline_overlay_enabled", true)
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

	// Decide draw/capture: draw while line is long OR caret past edge; capture only if caret past edge
	long := len([]rune(text)) > cols
	c.capture = cursorX >= cols
	shouldDraw := long || c.capture
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
		c.active = true
		c.wasActive = true
	}

	// Determine overlay rect (2 rows) above or below the cursor
	const overlayH = 2
	oy := cursorY + 1
	if oy+overlayH > rows {
		oy = cursorY - overlayH
		if oy < 0 {
			oy = 0
		}
	}
	c.rect = core.Rect{X: 0, Y: oy, W: cols, H: overlayH}

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
	if c.capture {
		// authoritative: keep local edits; focus to draw caret
		c.ta.Focus()
	} else {
		// terminal authoritative and overlay visible: mirror text/caret, blur to hide caret
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
		c.ta.Blur()
	}

	// Draw overlay
	p := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: cols, H: rows})
	p.Fill(c.rect, ' ', style)
	c.ta.Draw(p)
	return buf
}

// shouldCapture returns true if overlay should be authoritative based on caret position.
func (c *longLineEditorCard) shouldCapture() bool {
	if !c.enabled || c.term == nil || c.term.vterm == nil || c.w <= 0 {
		return false
	}
	v := c.term.vterm
	cursorX, _ := v.Cursor()
	cols := c.w
	return cursorX >= cols
}

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
	}
}

// interceptKey is called by the pipeline control when overlay should be authoritative.
func (c *longLineEditorCard) interceptKey(ev *tcell.EventKey) {
	c.ensureActive()
	if c.ta != nil {
		_ = c.ta.HandleKey(ev)
		// Also forward to terminal so vterm stays in sync; this lets caret/length
		// reflect the same edits and allows authority to switch back naturally.
		if c.term != nil {
			c.term.HandleKey(ev)
		}
		if c.refresh != nil {
			select {
			case c.refresh <- true:
			default:
			}
		}
	}
}
