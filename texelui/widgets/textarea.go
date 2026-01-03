package widgets

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/scroll"
)

// TextArea is a multiline text editor with automatic scrolling.
// It internally uses a ScrollPane for scroll management.
type TextArea struct {
	core.BaseWidget
	Lines      []string
	CaretX     int
	CaretY     int
	Style      tcell.Style
	CaretStyle tcell.Style

	// Optional change callback - called when text content changes
	OnChange func(text string)
	// Optional blur callback
	OnBlur func(text string)

	// local clipboard (paste only for now)
	clip string
	// invalidation callback
	inv func(core.Rect)
	// insert vs replace mode: false=insert (default), true=replace (overwrite)
	replaceMode bool

	// Internal scroll pane for scrolling support
	scrollPane *scroll.ScrollPane
	// Internal content widget
	content *textAreaContent
}

// textAreaContent is the internal widget that renders text content.
// It's managed by the parent TextArea and wrapped in a ScrollPane.
type textAreaContent struct {
	core.BaseWidget
	parent *TextArea
	Style  tcell.Style
}

func NewTextArea(x, y, w, h int) *TextArea {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")
	fg := tm.GetSemanticColor("text.primary")
	caret := tm.GetSemanticColor("caret")

	ta := &TextArea{
		Lines:      []string{""},
		Style:      tcell.StyleDefault.Background(bg).Foreground(fg),
		CaretStyle: tcell.StyleDefault.Foreground(caret),
	}

	// Create internal content widget
	ta.content = &textAreaContent{
		parent: ta,
		Style:  ta.Style,
	}
	ta.content.SetFocusable(false)

	// Create internal scroll pane
	ta.scrollPane = scroll.NewScrollPane(0, 0, w, h, ta.Style)
	ta.scrollPane.SetChild(ta.content)
	ta.scrollPane.SetFocusable(false) // TextArea handles focus

	// Enable focused styling
	ta.SetFocusedStyle(tcell.StyleDefault.Background(bg).Foreground(fg), true)
	ta.SetPosition(x, y)
	ta.Resize(w, h)
	ta.SetFocusable(true)

	return ta
}

// updateContentSize recalculates the content height and updates the scroll pane.
func (t *TextArea) updateContentSize() {
	if t.content == nil || t.scrollPane == nil {
		return
	}
	textWidth := t.textWidth()
	naturalHeight := t.totalVisualRows(textWidth)
	t.content.Resize(textWidth, naturalHeight)
	t.scrollPane.SetContentHeight(naturalHeight)
}

// textWidth returns the width available for text (excluding scrollbar space).
func (t *TextArea) textWidth() int {
	// ScrollPane reserves 1 column for scrollbar when needed
	w := t.Rect.W - 1
	if w < 1 {
		w = 1
	}
	return w
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (t *TextArea) SetInvalidator(fn func(core.Rect)) {
	t.inv = fn
	if t.scrollPane != nil {
		t.scrollPane.SetInvalidator(fn)
	}
}

// Resize updates the widget size.
func (t *TextArea) Resize(w, h int) {
	t.BaseWidget.Resize(w, h)
	if t.scrollPane != nil {
		t.scrollPane.Resize(w, h)
		t.updateContentSize()
	}
	t.ensureCaretVisible()
}

// SetPosition updates the widget position.
func (t *TextArea) SetPosition(x, y int) {
	t.BaseWidget.SetPosition(x, y)
	if t.scrollPane != nil {
		t.scrollPane.SetPosition(x, y)
	}
}

// GetKeyHints implements core.KeyHintsProvider.
func (t *TextArea) GetKeyHints() []core.KeyHint {
	return []core.KeyHint{
		{Key: "↑↓←→", Label: "Move"},
		{Key: "PgUp/Dn", Label: "Page"},
		{Key: "Ins", Label: "Mode"},
	}
}

// IsMultiline implements core.MultilineWidget.
func (t *TextArea) IsMultiline() bool {
	return true
}

// Blur removes focus and triggers the OnBlur callback if set.
func (t *TextArea) Blur() {
	wasFocused := t.IsFocused()
	t.BaseWidget.Blur()
	if wasFocused {
		// Scroll to top when losing focus
		t.scrollTo(0)
		t.invalidate()
		if t.OnBlur != nil {
			t.OnBlur(t.Text())
		}
	}
}

// Text returns all lines joined with newlines.
func (t *TextArea) Text() string {
	return strings.Join(t.Lines, "\n")
}

// SetText replaces the content with the given text.
func (t *TextArea) SetText(text string) {
	if text == "" {
		t.Lines = []string{""}
	} else {
		t.Lines = strings.Split(text, "\n")
	}
	t.CaretX = 0
	t.CaretY = 0
	t.updateContentSize()
	t.scrollTo(0)
	t.onChange()
	t.invalidate()
}

// onChange triggers the OnChange callback if set.
func (t *TextArea) onChange() {
	if t.OnChange != nil {
		t.OnChange(t.Text())
	}
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

// ensureCaretVisible scrolls to make the caret visible.
func (t *TextArea) ensureCaretVisible() {
	if t.scrollPane == nil {
		return
	}
	_, vcy := t.caretVisualPos()
	t.scrollPane.ScrollTo(vcy)
}

// scrollTo scrolls to the given offset.
func (t *TextArea) scrollTo(offset int) {
	if t.scrollPane != nil {
		// Use ScrollBy to set absolute position
		current := t.scrollPane.ScrollOffset()
		t.scrollPane.ScrollBy(offset - current)
	}
}

// scrollOffset returns the current scroll offset.
func (t *TextArea) scrollOffset() int {
	if t.scrollPane != nil {
		return t.scrollPane.ScrollOffset()
	}
	return 0
}

func (t *TextArea) Draw(p *core.Painter) {
	if t.scrollPane == nil {
		return
	}
	// Update content before drawing
	t.updateContentSize()
	// Draw via scroll pane
	t.scrollPane.Draw(p)
}

// HandleKey processes keyboard input.
func (t *TextArea) HandleKey(ev *tcell.EventKey) bool {
	// Only handle keys when focused
	if !t.IsFocused() {
		return false
	}

	// ESC not handled
	if ev.Key() == tcell.KeyEsc {
		return false
	}

	// Handle Ctrl key combinations
	if ev.Modifiers()&tcell.ModCtrl != 0 {
		switch ev.Key() {
		case tcell.KeyHome:
			// Ctrl+Home: go to beginning
			t.CaretY = 0
			t.CaretX = 0
			t.clampCaret()
			t.ensureCaretVisible()
			t.invalidate()
			return true
		case tcell.KeyEnd:
			// Ctrl+End: go to end
			t.CaretY = len(t.Lines) - 1
			if t.CaretY < 0 {
				t.CaretY = 0
			}
			t.CaretX = len([]rune(t.Lines[t.CaretY]))
			t.clampCaret()
			t.ensureCaretVisible()
			t.invalidate()
			return true
		}
		if ev.Rune() == 'v' && t.clip != "" {
			t.insertText(t.clip)
			return true
		}
	}

	switch ev.Key() {
	case tcell.KeyPgUp:
		t.scrollPane.ScrollBy(-t.Rect.H)
		t.invalidate()
		return true
	case tcell.KeyPgDn:
		t.scrollPane.ScrollBy(t.Rect.H)
		t.invalidate()
		return true
	case tcell.KeyInsert:
		t.replaceMode = !t.replaceMode
		t.invalidate()
		return true
	case tcell.KeyLeft:
		t.CaretX--
		if t.CaretX < 0 && t.CaretY > 0 {
			t.CaretY--
			t.CaretX = len([]rune(t.Lines[t.CaretY]))
		}
	case tcell.KeyRight:
		maxX := len([]rune(t.Lines[t.CaretY]))
		t.CaretX++
		if t.CaretX > maxX && t.CaretY < len(t.Lines)-1 {
			t.CaretY++
			t.CaretX = 0
		}
	case tcell.KeyUp:
		t.CaretY--
	case tcell.KeyDown:
		t.CaretY++
	case tcell.KeyHome:
		t.CaretX = 0
	case tcell.KeyEnd:
		t.CaretX = len([]rune(t.Lines[t.CaretY]))
	case tcell.KeyEnter:
		t.insertNewline()
		return true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if t.CaretX > 0 {
			line := []rune(t.Lines[t.CaretY])
			t.Lines[t.CaretY] = string(append(line[:t.CaretX-1], line[t.CaretX:]...))
			t.CaretX--
			t.updateContentSize()
			t.onChange()
			t.ensureCaretVisible()
			t.invalidate()
			return true
		} else if t.CaretY > 0 {
			prev := t.Lines[t.CaretY-1]
			cur := t.Lines[t.CaretY]
			t.CaretX = len([]rune(prev))
			t.Lines[t.CaretY-1] = prev + cur
			t.Lines = append(t.Lines[:t.CaretY], t.Lines[t.CaretY+1:]...)
			t.CaretY--
			t.updateContentSize()
			t.onChange()
			t.ensureCaretVisible()
			t.invalidate()
			return true
		}
		return false
	case tcell.KeyDelete:
		if t.CaretY >= 0 && t.CaretY < len(t.Lines) {
			line := []rune(t.Lines[t.CaretY])
			if t.CaretX >= 0 && t.CaretX < len(line) {
				t.Lines[t.CaretY] = string(append(line[:t.CaretX], line[t.CaretX+1:]...))
				t.updateContentSize()
				t.onChange()
				t.invalidate()
				return true
			} else if t.CaretY < len(t.Lines)-1 {
				// Delete at end of line - join with next line
				t.Lines[t.CaretY] = string(line) + t.Lines[t.CaretY+1]
				t.Lines = append(t.Lines[:t.CaretY+1], t.Lines[t.CaretY+2:]...)
				t.updateContentSize()
				t.onChange()
				t.invalidate()
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
			line[t.CaretX] = r
			t.Lines[t.CaretY] = string(line)
			t.CaretX++
		} else {
			line = append(line[:t.CaretX], append([]rune{r}, line[t.CaretX:]...)...)
			t.Lines[t.CaretY] = string(line)
			t.CaretX++
		}
		t.updateContentSize()
		t.onChange()
		t.ensureCaretVisible()
		t.invalidate()
		return true
	default:
		return false
	}
	t.clampCaret()
	t.ensureCaretVisible()
	t.invalidate()
	return true
}

// HandleMouse processes mouse input.
func (t *TextArea) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !t.HitTest(x, y) {
		return false
	}

	btn := ev.Buttons()
	lx := x - t.Rect.X

	// Handle wheel events - forward to scroll pane
	if btn&(tcell.WheelUp|tcell.WheelDown) != 0 {
		if t.scrollPane != nil {
			t.scrollPane.HandleMouse(ev)
			t.invalidate()
			return true
		}
	}

	// Scrollbar area is the rightmost column
	scrollbarX := t.Rect.W - 1
	if lx == scrollbarX && t.scrollPane != nil {
		// Forward scrollbar clicks to scroll pane
		if t.scrollPane.HandleMouse(ev) {
			t.invalidate()
			return true
		}
	}

	// Handle text area clicks (not on scrollbar)
	ly := y - t.Rect.Y

	if btn&tcell.Button1 != 0 {
		// Click to position caret
		vrow := t.scrollOffset() + ly
		li, start := t.visualRowToLogical(vrow)
		t.CaretY = li
		segLen := t.segmentLen(li, start)
		dx := lx
		if dx > segLen {
			dx = segLen
		}
		t.CaretX = start + dx
		t.clampCaret()
		t.invalidate()
		return true
	}

	return false
}

// caretVisualPos returns the caret position in visual coordinates.
func (t *TextArea) caretVisualPos() (int, int) {
	textWidth := t.textWidth()
	if textWidth <= 0 {
		return 0, 0
	}
	vrow := 0
	for li := 0; li < len(t.Lines) && li < t.CaretY; li++ {
		r := []rune(t.Lines[li])
		n := (len(r) + textWidth - 1) / textWidth
		if n == 0 {
			n = 1
		}
		vrow += n
	}
	cx := t.CaretX
	if cx < 0 {
		cx = 0
	}
	r := []rune("")
	if t.CaretY >= 0 && t.CaretY < len(t.Lines) {
		r = []rune(t.Lines[t.CaretY])
	}
	if cx > len(r) {
		cx = len(r)
	}
	vrow += cx / textWidth
	vx := cx % textWidth
	return vx, vrow
}

// totalVisualRows calculates total wrapped rows.
func (t *TextArea) totalVisualRows(textWidth int) int {
	if textWidth <= 0 {
		return len(t.Lines)
	}
	total := 0
	for _, line := range t.Lines {
		r := []rune(line)
		if len(r) == 0 {
			total++
		} else {
			total += (len(r) + textWidth - 1) / textWidth
		}
	}
	return total
}

// visualRowToLogical maps visual row to logical line and offset.
func (t *TextArea) visualRowToLogical(vrow int) (int, int) {
	textWidth := t.textWidth()
	if textWidth <= 0 {
		return 0, 0
	}
	row := 0
	for li := 0; li < len(t.Lines); li++ {
		r := []rune(t.Lines[li])
		n := (len(r) + textWidth - 1) / textWidth
		if n == 0 {
			n = 1
		}
		if vrow < row+n {
			segIdx := vrow - row
			start := segIdx * textWidth
			if start > len(r) {
				start = len(r)
			}
			return li, start
		}
		row += n
	}
	li := len(t.Lines) - 1
	if li < 0 {
		li = 0
	}
	r := []rune("")
	if li < len(t.Lines) {
		r = []rune(t.Lines[li])
	}
	return li, len(r)
}

func (t *TextArea) segmentLen(li, start int) int {
	textWidth := t.textWidth()
	if li < 0 || li >= len(t.Lines) || textWidth <= 0 {
		return 0
	}
	r := []rune(t.Lines[li])
	if start < 0 {
		start = 0
	}
	if start > len(r) {
		return 0
	}
	end := start + textWidth
	if end > len(r) {
		end = len(r)
	}
	return end - start
}

func (t *TextArea) insertText(s string) {
	for _, r := range s {
		if r == '\n' {
			t.insertNewline()
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
	t.updateContentSize()
	t.ensureCaretVisible()
	t.onChange()
	t.invalidate()
}

func (t *TextArea) insertNewline() {
	line := t.Lines[t.CaretY]
	runes := []rune(line)
	if t.CaretX > len(runes) {
		t.CaretX = len(runes)
	}
	head := runes[:t.CaretX]
	tail := runes[t.CaretX:]
	t.Lines[t.CaretY] = string(head)
	t.Lines = append(t.Lines[:t.CaretY+1], append([]string{""}, t.Lines[t.CaretY+1:]...)...)
	t.Lines[t.CaretY+1] = string(tail)
	t.CaretY++
	t.CaretX = 0
	t.clampCaret()
	t.updateContentSize()
	t.ensureCaretVisible()
	t.onChange()
	t.invalidate()
}

func (t *TextArea) invalidate() {
	if t.inv != nil {
		t.inv(t.Rect)
	}
}

// NaturalHeight returns total visual rows (for external use).
func (t *TextArea) NaturalHeight() int {
	return t.totalVisualRows(t.textWidth())
}

// textAreaContent Draw implementation
func (c *textAreaContent) Draw(p *core.Painter) {
	if c.parent == nil {
		return
	}
	t := c.parent
	base := t.Style
	if t.IsFocused() {
		base = t.EffectiveStyle(t.Style)
	}

	// Fill background
	p.Fill(c.Rect, ' ', base)

	textWidth := c.Rect.W
	if textWidth <= 0 {
		return
	}

	// Draw all lines (ScrollPane clips to viewport)
	globalRow := 0
	for li := 0; li < len(t.Lines); li++ {
		r := []rune(t.Lines[li])
		if len(r) == 0 {
			globalRow++
			continue
		}
		for start := 0; start < len(r); start += textWidth {
			end := start + textWidth
			if end > len(r) {
				end = len(r)
			}
			row := globalRow
			col := 0
			for i := start; i < end && col < textWidth; i++ {
				p.SetCell(c.Rect.X+col, c.Rect.Y+row, r[i], base)
				col++
			}
			globalRow++
		}
	}

	// Draw caret
	if t.IsFocused() {
		cx, cy := t.caretVisualPos()
		if cx >= 0 && cy >= 0 && cx < textWidth && cy < c.Rect.H {
			ch := ' '
			if t.CaretY >= 0 && t.CaretY < len(t.Lines) {
				line := []rune(t.Lines[t.CaretY])
				if t.CaretX >= 0 && t.CaretX < len(line) {
					ch = line[t.CaretX]
				}
			}
			fg, bg, _ := base.Decompose()
			var caretStyle tcell.Style
			if t.replaceMode {
				caretStyle = tcell.StyleDefault.Background(bg).Foreground(fg).Underline(true)
			} else {
				caretStyle = tcell.StyleDefault.Background(fg).Foreground(bg)
			}
			p.SetCell(c.Rect.X+cx, c.Rect.Y+cy, ch, caretStyle)
		}
	}
}
