package texelterm

import (
	"os"

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
	wasActive bool
	rect      core.Rect
	refresh   chan<- bool
	w, h      int

	enabled            bool
	pendingAfterScroll bool
	scrollSize         int

	// Shell sync state: when we rewrite the line from the editor, the PTY/shell
	// may in turn adjust it (history navigation, prompt redraw, etc). We track
	// a pending refresh so the next Render can re-seed TextArea from vterm.
	pendingShellSync    bool
	resetCaretFromShell bool

	// Optional test hook: when non-nil, invoked whenever pasteEditorLineToShell
	// rewrites the shell line. Tests can use this to simulate how a shell would
	// update vterm (echoing or transforming the line) without requiring a PTY.
	onShellSync func(text string)

	// lastShellInput remembers the previous input segment seen from vterm so we
	// can detect when the shell has actually updated the line after a rewrite.
	lastShellInput string
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

	// Determine input start column via shell integration; if unknown, do not open editor yet.
	startX := 0
	startKnown := false
	if t != nil {
		t.mu.Lock()
		startKnown = t.inputStartKnown
		if startKnown {
			startX = t.inputStartCol
		}
		t.mu.Unlock()
	}
	if !startKnown {
		// We don't know the prompt/input split yet; do not activate the editor.
		c.deactivate()
		return buf
	}
	if startX < 0 {
		startX = 0
	}
	if startX > cols-1 {
		startX = cols - 1
	}

	// Consider only the input portion for editing.
	rr := []rune(text)
	if startX > len(rr) {
		startX = len(rr)
	}
	shellInput := ""
	if startX <= len(rr) {
		shellInput = string(rr[startX:])
	}

	// When editor is active, the TextArea is the interactive surface, but the
	// underlying shell line remains authoritative. If we've recently pushed
	// a rewritten line into the PTY, allow the next render to re-seed from vterm
	// so shell-side transforms still win. To avoid clobbering freshly-typed
	// characters before the shell has echoed them, only re-seed when we detect
	// a change in the shell's view of the line.
	shellChanged := shellInput != c.lastShellInput
	if c.active && c.ta != nil && len(c.ta.Lines) > 0 && c.pendingShellSync && shellChanged {
		c.ta.Lines = []string{shellInput}
		// For history navigation / boundary transitions, also reset caret to
		// match the shell cursor, using input-start as origin.
		if c.resetCaretFromShell {
			cx := cursorX - startX
			if cx < 0 {
				cx = 0
			}
			if cx > len([]rune(shellInput)) {
				cx = len([]rune(shellInput))
			}
			c.ta.CaretX = cx
			c.ta.CaretY = 0
			c.resetCaretFromShell = false
		}
		c.pendingShellSync = false
	}
	// Track the latest shell input we observed.
	c.lastShellInput = shellInput

	inputText := shellInput
	if c.active && c.ta != nil && len(c.ta.Lines) > 0 {
		inputText = c.ta.Lines[0]
	}

	// Show editor only when input exceeds available columns.
	avail := cols - startX
	if avail < 1 {
		avail = 1
	}
	long := len([]rune(inputText)) > avail
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

	// Precompute initial needed height for potential initial scroll on open.
	initEditorW := cols - startX
	if initEditorW < 1 {
		initEditorW = 1
	}
	initLen := len([]rune(inputText))
	initNeedH := initLen / initEditorW
	if initLen%initEditorW != 0 {
		initNeedH++
	}
	if initNeedH < 1 {
		initNeedH = 1
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
		// If opening at the last screen row, start visual scroll for full initial height.
		if cursorY == rows-1 {
			c.scrollSize = initNeedH
			if c.scrollSize >= rows {
				c.scrollSize = rows - 1
			}
		} else {
			c.scrollSize = 0
		}
	}

	// Determine overlay rect anchored at cursor line; grow down, scroll up if at bottom.
	editorW := cols - startX
	if editorW < 1 {
		editorW = 1
	}
	// Required wrapped rows for input.
	ir := []rune(inputText)
	needH := len(ir) / editorW
	if len(ir)%editorW != 0 {
		needH++
	}
	if needH < 1 {
		needH = 1
	}
	// Visual scroll: adjust overlay Y by current scrollSize.
	oy := cursorY - c.scrollSize
	if oy+needH > rows {
		// Remove special last-line scrolling; simply anchor editor within bounds.
		oy = rows - needH
		if oy < 0 {
			oy = 0
		}
	}
	// Growth detection: if editor height increased, increase visual scroll accordingly.
	prevH := c.rect.H
	c.rect = core.Rect{X: startX, Y: oy, W: editorW, H: needH}
	if prevH > 0 && needH > prevH {
		c.scrollSize += (needH - prevH)
		// Re-adjust overlay Y after scroll change.
		newY := cursorY - c.scrollSize
		if newY+needH > rows {
			newY = rows - needH
		}
		if newY < 0 {
			newY = 0
		}
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

	// Configure TA depending on authority.
	c.ta.SetPosition(c.rect.X, c.rect.Y)
	c.ta.Resize(c.rect.W, c.rect.H)
	c.ta.Style = style
	// Authoritative editor while active.
	c.ta.Focus()

	// Prepare output buffer applying visual scroll (skip top scrollSize rows).
	out := buf
	if c.scrollSize > 0 && c.scrollSize < rows {
		out = make([][]texel.Cell, rows)
		// Base clear style for blank rows at the bottom after scroll.
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
	// Draw overlay.
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
		// Reset visual scroll on close.
		c.scrollSize = 0
		// Clear pending shell sync state.
		c.pendingShellSync = false
		c.resetCaretFromShell = false
		// Mark overlay lines dirty so the base renderer clears any remnants.
		if c.term != nil {
			c.term.mu.Lock()
			if c.term.vterm != nil {
				for y := c.rect.Y; y < c.rect.Y+c.rect.H; y++ {
					c.term.vterm.MarkDirty(y)
				}
			}
			c.term.mu.Unlock()
		}
		if c.refresh != nil {
			select {
			case c.refresh <- true:
			default:
			}
		}
	}
}

// pasteEditorLineToShell rewrites the current shell input line from the TextArea
// buffer via bracketed paste, without sending Enter. This keeps the shell's
// notion of the line in sync while the overlay remains authoritative for caret.
func (c *longLineEditorCard) pasteEditorLineToShell() {
	if c.term == nil || c.term.pty == nil || c.ta == nil {
		if c.onShellSync != nil && c.ta != nil {
			text := ""
			for i, s := range c.ta.Lines {
				if i > 0 {
					text += " "
				}
				text += s
			}
			c.onShellSync(text)
		}
		return
	}
	text := ""
	for i, s := range c.ta.Lines {
		if i > 0 {
			text += " "
		}
		text += s
	}
	// ^A (start of line), ^K (kill to end), then re-type the full line as
	// normal input (no bracketed paste, no Enter). This keeps the shell's
	// internal line buffer consistent in a way that works across basic shells.
	seq := []byte{0x01, 0x0b}
	seq = append(seq, []byte(text)...)
	_, _ = c.term.pty.Write(seq)
	if c.onShellSync != nil {
		c.onShellSync(text)
	}
}

// interceptKey is called by the pipeline control when overlay should be authoritative.
func (c *longLineEditorCard) interceptKey(ev *tcell.EventKey) {
	c.ensureActive()
	if c.ta == nil {
		return
	}

	// Vertical movement across wrapped rows using editor width. When the caret
	// would move beyond the logical line, treat this as a boundary: commit the
	// current editor text into the shell line and forward the arrow key so the
	// shell can perform history navigation, then re-seed from vterm on refresh.
	if c.term != nil && (ev.Key() == tcell.KeyUp || ev.Key() == tcell.KeyDown) {
		cols := 0
		startX := 0
		c.term.mu.Lock()
		if c.term.vterm != nil {
			grid := c.term.vterm.Grid()
			if len(grid) > 0 {
				cols = len(grid[0])
			}
		}
		if c.term.inputStartKnown {
			startX = c.term.inputStartCol
		}
		c.term.mu.Unlock()
		editorW := cols - startX
		if editorW < 1 {
			editorW = 1
		}
		line := []rune("")
		if len(c.ta.Lines) > 0 {
			line = []rune(c.ta.Lines[0])
		}
		nx := c.ta.CaretX
		if ev.Key() == tcell.KeyUp {
			nx -= editorW
		} else {
			nx += editorW
		}
		// Boundary: let shell handle history navigation.
		if nx < 0 || nx > len(line) {
			c.pendingShellSync = true
			c.pasteEditorLineToShell()
			c.resetCaretFromShell = true
			if c.term != nil {
				c.term.HandleKey(ev)
			}
			if c.refresh != nil {
				select {
				case c.refresh <- true:
				default:
				}
			}
			return
		}
		// Intra-overlay motion: keep caret inside TextArea only.
		if nx < 0 {
			nx = 0
		}
		if nx > len(line) {
			nx = len(line)
		}
		c.ta.CaretX = nx
		if c.refresh != nil {
			select {
			case c.refresh <- true:
			default:
			}
		}
		return
	}

	switch ev.Key() {
	case tcell.KeyEsc:
		c.deactivate()
		if c.refresh != nil {
			select {
			case c.refresh <- true:
			default:
			}
		}
		return
	case tcell.KeyCtrlC, tcell.KeyCtrlD:
		// Commit overlay text, then forward the control key to the shell and
		// exit the editor.
		// Shell may redraw or change the line (e.g. cancel), but since we are
		// deactivating the editor we do not track a pending sync.
		c.pasteEditorLineToShell()
		if c.term != nil {
			c.term.HandleKey(ev)
		}
		c.deactivate()
		if c.refresh != nil {
			select {
			case c.refresh <- true:
			default:
			}
		}
		return
	case tcell.KeyEnter:
		// Reset visual scroll before committing so base buffer returns to normal.
		c.scrollSize = 0
		// Treat Enter like a boundary key: sync the editor text into the shell
		// line, then send Enter so the shell executes it.
		c.pasteEditorLineToShell()
		if c.term != nil {
			c.term.HandleKey(ev)
		}
		c.deactivate()
		if c.refresh != nil {
			select {
			case c.refresh <- true:
			default:
			}
		}
		return
	}

	// Let TextArea handle normal editing/navigation keys. After any change,
	// mirror the full logical line into the shell so its state stays in sync.
	handled := c.ta.HandleKey(ev)
	if handled {
		// For normal editing keys, we do not expect the shell to change the
		// line beyond echoing our input, so we avoid marking a pending shell
		// sync; the TextArea remains the source of truth while active.
		c.pasteEditorLineToShell()
	} else {
		// Keys not handled by the TextArea (e.g. shell search/history bindings)
		// should still see the current line in the shell. First sync the text,
		// then forward the key to TexelTerm so it is sent to the PTY.
		// Shell may transform the line (history search, etc.), so track a
		// pending sync and allow the next Render to re-seed from vterm.
		c.pendingShellSync = true
		c.pasteEditorLineToShell()
		if c.term != nil {
			c.resetCaretFromShell = true
			c.term.HandleKey(ev)
		}
	}

	if c.refresh != nil {
		select {
		case c.refresh <- true:
		default:
		}
	}
}

func (c *longLineEditorCard) commitEditor() {
	if c.term == nil || c.term.pty == nil || c.ta == nil {
		return
	}
	// Concatenate lines with spaces
	text := ""
	for i, s := range c.ta.Lines {
		if i > 0 {
			text += " "
		}
		text += s
	}
	// ^A, ^K then bracketed paste text and Enter, mirroring the historical
	// commit behaviour used when finishing the line.
	seq := []byte{0x01, 0x0b}
	seq = append(seq, []byte("\x1b[200~")...)
	seq = append(seq, []byte(text)...)
	seq = append(seq, []byte("\x1b[201~")...)
	seq = append(seq, '\r')
	_, _ = c.term.pty.Write(seq)
}

// (No commit without Enter; boundary pass-through is handled in future steps.)
