package widgets

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/scroll"
)

// TextArea is a multiline text editor with internal scrolling.
// It has a fixed outer size and contains an internal ScrollPane
// that handles scrolling of the text content.
type TextArea struct {
	core.BaseWidget
	Style      tcell.Style
	CaretStyle tcell.Style

	// Optional change callback - called when text content changes
	OnChange func(text string)
	// Optional blur callback
	OnBlur func(text string)

	// Internal components
	scrollPane *scroll.ScrollPane
	content    *textAreaContent

	// invalidation callback
	inv func(core.Rect)
}

// textAreaContent is the internal widget that holds the actual text.
// It grows as lines are added.
type textAreaContent struct {
	core.BaseWidget
	parent *TextArea

	Lines []string
	CaretX int
	CaretY int

	// local clipboard (paste only for now)
	clip string
	// insert vs replace mode: false=insert (default), true=replace (overwrite)
	replaceMode bool
	// Width used for wrapping calculations
	wrapWidth int
}

// NewTextArea creates a multi-line text input widget.
// Position defaults to 0,0 and size defaults to 20x4. Use SetPosition() and Resize() to configure.
func NewTextArea() *TextArea {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")
	fg := tm.GetSemanticColor("text.primary")
	caret := tm.GetSemanticColor("caret")

	ta := &TextArea{
		Style:      tcell.StyleDefault.Background(bg).Foreground(fg),
		CaretStyle: tcell.StyleDefault.Foreground(caret),
	}

	// Create internal content
	// Note: content is NOT focusable - TextArea is the focusable widget,
	// and content inherits focus state from parent.
	ta.content = &textAreaContent{
		parent:    ta,
		Lines:     []string{""},
		wrapWidth: 20,
	}
	ta.content.SetFocusedStyle(tcell.StyleDefault.Background(bg).Foreground(fg), true)

	// Create internal ScrollPane
	ta.scrollPane = scroll.NewScrollPane()
	ta.scrollPane.SetChild(ta.content)

	// Enable focused styling
	ta.SetFocusedStyle(tcell.StyleDefault.Background(bg).Foreground(fg), true)
	ta.Resize(20, 4) // Default size
	ta.SetFocusable(true)

	return ta
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (t *TextArea) SetInvalidator(fn func(core.Rect)) {
	t.inv = fn
	if t.scrollPane != nil {
		t.scrollPane.SetInvalidator(fn)
	}
}

// SetPosition sets the position of the TextArea.
func (t *TextArea) SetPosition(x, y int) {
	t.BaseWidget.SetPosition(x, y)
	if t.scrollPane != nil {
		t.scrollPane.SetPosition(x, y)
	}
}

// Resize updates the widget size and internal components.
func (t *TextArea) Resize(w, h int) {
	t.BaseWidget.Resize(w, h)
	if t.scrollPane != nil {
		t.scrollPane.Resize(w, h)
	}
	if t.content != nil {
		// Reserve 1 char for scrollbar to prevent text from rendering under it
		t.content.wrapWidth = w - 1
		if t.content.wrapWidth < 1 {
			t.content.wrapWidth = 1
		}
		// Update content height based on text
		contentH := t.content.totalVisualRows()
		t.content.Resize(w, contentH)
		if t.scrollPane != nil {
			t.scrollPane.SetContentHeight(contentH)
		}
	}
}

// GetKeyHints implements core.KeyHintsProvider.
func (t *TextArea) GetKeyHints() []core.KeyHint {
	return []core.KeyHint{
		{Key: "↑↓←→", Label: "Move"},
		{Key: "Ins", Label: "Mode"},
	}
}

// IsMultiline implements core.MultilineWidget.
func (t *TextArea) IsMultiline() bool {
	return true
}

// Focus focuses the TextArea.
func (t *TextArea) Focus() {
	t.BaseWidget.Focus()
	// Note: content inherits focus from parent, no need to focus it separately
}

// Blur removes focus and triggers the OnBlur callback if set.
func (t *TextArea) Blur() {
	wasFocused := t.IsFocused()
	t.BaseWidget.Blur()
	// Note: content inherits focus from parent, no need to blur it separately
	if wasFocused {
		t.invalidate()
		if t.OnBlur != nil {
			t.OnBlur(t.Text())
		}
	}
}

// Text returns all lines joined with newlines.
func (t *TextArea) Text() string {
	if t.content == nil {
		return ""
	}
	return strings.Join(t.content.Lines, "\n")
}

// SetText replaces the content with the given text.
func (t *TextArea) SetText(text string) {
	if t.content == nil {
		return
	}
	if text == "" {
		t.content.Lines = []string{""}
	} else {
		t.content.Lines = strings.Split(text, "\n")
	}
	t.content.CaretX = 0
	t.content.CaretY = 0
	// Update content height
	contentH := t.content.totalVisualRows()
	t.content.Resize(t.content.wrapWidth, contentH)
	if t.scrollPane != nil {
		t.scrollPane.SetContentHeight(contentH)
	}
	t.onChange()
	t.invalidate()
}

// onChange triggers the OnChange callback if set.
func (t *TextArea) onChange() {
	if t.OnChange != nil {
		t.OnChange(t.Text())
	}
}

// Draw renders the TextArea by drawing its internal ScrollPane.
func (t *TextArea) Draw(p *core.Painter) {
	if t.scrollPane != nil {
		t.scrollPane.Draw(p)
	}
}

// HandleKey processes keyboard input.
// Routes through the internal ScrollPane so it can handle PgUp/PgDown.
func (t *TextArea) HandleKey(ev *tcell.EventKey) bool {
	if t.scrollPane != nil {
		return t.scrollPane.HandleKey(ev)
	}
	return false
}

// HandleMouse processes mouse input by delegating to the internal ScrollPane.
func (t *TextArea) HandleMouse(ev *tcell.EventMouse) bool {
	if t.scrollPane != nil {
		return t.scrollPane.HandleMouse(ev)
	}
	return false
}

// VisitChildren implements core.ChildContainer for recursive operations.
func (t *TextArea) VisitChildren(f func(core.Widget)) {
	if t.scrollPane != nil {
		f(t.scrollPane)
	}
}

// WidgetAt returns the TextArea itself (not internal components).
// The internal ScrollPane/content structure is an implementation detail
// and should not be exposed to UIManager's focus tracking.
func (t *TextArea) WidgetAt(x, y int) core.Widget {
	if t.HitTest(x, y) {
		return t
	}
	return nil
}

func (t *TextArea) invalidate() {
	if t.inv != nil {
		t.inv(t.Rect)
	}
}

// updateContentSize recalculates and updates the content size.
func (t *TextArea) updateContentSize() {
	if t.content == nil {
		return
	}
	contentH := t.content.totalVisualRows()
	t.content.Resize(t.content.wrapWidth, contentH)
	if t.scrollPane != nil {
		t.scrollPane.SetContentHeight(contentH)
	}
}

// ============================================================================
// textAreaContent methods
// ============================================================================

func (c *textAreaContent) Draw(p *core.Painter) {
	base := c.parent.Style
	if c.parent.IsFocused() {
		// Use parent's EffectiveStyle since parent holds the focus, not content
		base = c.parent.EffectiveStyle(c.parent.Style)
	}

	// Fill background
	p.Fill(c.Rect, ' ', base)

	textWidth := c.wrapWidth
	if textWidth <= 0 {
		return
	}

	// Draw all lines
	globalRow := 0
	for li := 0; li < len(c.Lines); li++ {
		r := []rune(c.Lines[li])
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
	if c.parent.IsFocused() {
		cx, cy := c.caretVisualPos()
		if cx >= 0 && cy >= 0 && cx < textWidth && cy < c.Rect.H {
			ch := ' '
			if c.CaretY >= 0 && c.CaretY < len(c.Lines) {
				line := []rune(c.Lines[c.CaretY])
				if c.CaretX >= 0 && c.CaretX < len(line) {
					ch = line[c.CaretX]
				}
			}
			fg, bg, _ := base.Decompose()
			var caretStyle tcell.Style
			if c.replaceMode {
				caretStyle = tcell.StyleDefault.Background(bg).Foreground(fg).Underline(true)
			} else {
				caretStyle = tcell.StyleDefault.Background(fg).Foreground(bg)
			}
			p.SetCell(c.Rect.X+cx, c.Rect.Y+cy, ch, caretStyle)
		}
	}
}

// HandleKey processes keyboard input for the content.
func (c *textAreaContent) HandleKey(ev *tcell.EventKey) bool {
	// Only handle keys when parent is focused
	if !c.parent.IsFocused() {
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
			c.CaretY = 0
			c.CaretX = 0
			c.clampCaret()
			c.parent.scrollPane.ScrollToTop()
			c.parent.invalidate()
			return true
		case tcell.KeyEnd:
			// Ctrl+End: go to end
			c.CaretY = len(c.Lines) - 1
			if c.CaretY < 0 {
				c.CaretY = 0
			}
			c.CaretX = len([]rune(c.Lines[c.CaretY]))
			c.clampCaret()
			c.parent.scrollPane.ScrollToBottom()
			c.parent.invalidate()
			return true
		}
		if ev.Rune() == 'v' && c.clip != "" {
			c.insertText(c.clip)
			return true
		}
	}

	switch ev.Key() {
	case tcell.KeyInsert:
		c.replaceMode = !c.replaceMode
		c.parent.invalidate()
		return true
	case tcell.KeyLeft:
		c.CaretX--
		if c.CaretX < 0 && c.CaretY > 0 {
			c.CaretY--
			c.CaretX = len([]rune(c.Lines[c.CaretY]))
		}
	case tcell.KeyRight:
		maxX := len([]rune(c.Lines[c.CaretY]))
		c.CaretX++
		if c.CaretX > maxX && c.CaretY < len(c.Lines)-1 {
			c.CaretY++
			c.CaretX = 0
		}
	case tcell.KeyUp:
		c.CaretY--
	case tcell.KeyDown:
		c.CaretY++
	case tcell.KeyHome:
		c.CaretX = 0
	case tcell.KeyEnd:
		c.CaretX = len([]rune(c.Lines[c.CaretY]))
	case tcell.KeyEnter:
		c.insertNewline()
		return true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if c.CaretX > 0 {
			line := []rune(c.Lines[c.CaretY])
			c.Lines[c.CaretY] = string(append(line[:c.CaretX-1], line[c.CaretX:]...))
			c.CaretX--
			c.parent.updateContentSize()
			c.parent.onChange()
			c.parent.invalidate()
			return true
		} else if c.CaretY > 0 {
			prev := c.Lines[c.CaretY-1]
			cur := c.Lines[c.CaretY]
			c.CaretX = len([]rune(prev))
			c.Lines[c.CaretY-1] = prev + cur
			c.Lines = append(c.Lines[:c.CaretY], c.Lines[c.CaretY+1:]...)
			c.CaretY--
			c.parent.updateContentSize()
			c.parent.onChange()
			c.parent.invalidate()
			return true
		}
		return false
	case tcell.KeyDelete:
		if c.CaretY >= 0 && c.CaretY < len(c.Lines) {
			line := []rune(c.Lines[c.CaretY])
			if c.CaretX >= 0 && c.CaretX < len(line) {
				c.Lines[c.CaretY] = string(append(line[:c.CaretX], line[c.CaretX+1:]...))
				c.parent.updateContentSize()
				c.parent.onChange()
				c.parent.invalidate()
				return true
			} else if c.CaretY < len(c.Lines)-1 {
				// Delete at end of line - join with next line
				c.Lines[c.CaretY] = string(line) + c.Lines[c.CaretY+1]
				c.Lines = append(c.Lines[:c.CaretY+1], c.Lines[c.CaretY+2:]...)
				c.parent.updateContentSize()
				c.parent.onChange()
				c.parent.invalidate()
				return true
			}
		}
		return false
	case tcell.KeyRune:
		r := ev.Rune()
		line := []rune(c.Lines[c.CaretY])
		if c.CaretX < 0 {
			c.CaretX = 0
		}
		if c.CaretX > len(line) {
			c.CaretX = len(line)
		}
		if c.replaceMode && c.CaretX < len(line) {
			line[c.CaretX] = r
			c.Lines[c.CaretY] = string(line)
			c.CaretX++
		} else {
			line = append(line[:c.CaretX], append([]rune{r}, line[c.CaretX:]...)...)
			c.Lines[c.CaretY] = string(line)
			c.CaretX++
		}
		c.parent.updateContentSize()
		c.parent.onChange()
		c.parent.invalidate()
		return true
	default:
		return false
	}
	c.clampCaret()
	c.ensureCaretVisible()
	c.parent.invalidate()
	return true
}

// HandleMouse processes mouse input for the content.
func (c *textAreaContent) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	btn := ev.Buttons()

	// Don't handle wheel events - let parent ScrollPane handle them
	if btn&(tcell.WheelUp|tcell.WheelDown) != 0 {
		return false
	}

	// For clicks, use relative position
	lx := x - c.Rect.X
	ly := y - c.Rect.Y

	if btn&tcell.Button1 != 0 {
		// Click to position caret
		li, start := c.visualRowToLogical(ly)
		c.CaretY = li
		segLen := c.segmentLen(li, start)
		dx := lx
		if dx > segLen {
			dx = segLen
		}
		c.CaretX = start + dx
		c.clampCaret()
		c.parent.invalidate()
		return true
	}

	return false
}

func (c *textAreaContent) clampCaret() {
	if c.CaretY < 0 {
		c.CaretY = 0
	}
	if c.CaretY >= len(c.Lines) {
		c.CaretY = len(c.Lines) - 1
	}
	if c.CaretY < 0 {
		c.CaretY = 0
	}
	maxX := len([]rune(c.Lines[c.CaretY]))
	if c.CaretX < 0 {
		c.CaretX = 0
	}
	if c.CaretX > maxX {
		c.CaretX = maxX
	}
}

// ensureCaretVisible scrolls the view to keep the caret visible.
func (c *textAreaContent) ensureCaretVisible() {
	if c.parent.scrollPane == nil {
		return
	}
	_, cy := c.caretVisualPos()
	c.parent.scrollPane.EnsureVisible(cy)
}

// caretVisualPos returns the caret position in visual coordinates.
func (c *textAreaContent) caretVisualPos() (int, int) {
	textWidth := c.wrapWidth
	if textWidth <= 0 {
		return 0, 0
	}
	vrow := 0
	for li := 0; li < len(c.Lines) && li < c.CaretY; li++ {
		r := []rune(c.Lines[li])
		n := (len(r) + textWidth - 1) / textWidth
		if n == 0 {
			n = 1
		}
		vrow += n
	}
	cx := c.CaretX
	if cx < 0 {
		cx = 0
	}
	r := []rune("")
	if c.CaretY >= 0 && c.CaretY < len(c.Lines) {
		r = []rune(c.Lines[c.CaretY])
	}
	if cx > len(r) {
		cx = len(r)
	}
	vrow += cx / textWidth
	vx := cx % textWidth
	return vx, vrow
}

// totalVisualRows calculates total wrapped rows.
func (c *textAreaContent) totalVisualRows() int {
	textWidth := c.wrapWidth
	if textWidth <= 0 {
		return len(c.Lines)
	}
	total := 0
	for _, line := range c.Lines {
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
func (c *textAreaContent) visualRowToLogical(vrow int) (int, int) {
	textWidth := c.wrapWidth
	if textWidth <= 0 {
		return 0, 0
	}
	row := 0
	for li := 0; li < len(c.Lines); li++ {
		r := []rune(c.Lines[li])
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
	li := len(c.Lines) - 1
	if li < 0 {
		li = 0
	}
	r := []rune("")
	if li < len(c.Lines) {
		r = []rune(c.Lines[li])
	}
	return li, len(r)
}

func (c *textAreaContent) segmentLen(li, start int) int {
	textWidth := c.wrapWidth
	if li < 0 || li >= len(c.Lines) || textWidth <= 0 {
		return 0
	}
	r := []rune(c.Lines[li])
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

func (c *textAreaContent) insertText(s string) {
	for _, r := range s {
		if r == '\n' {
			c.insertNewline()
		} else {
			line := []rune(c.Lines[c.CaretY])
			if c.CaretX < 0 {
				c.CaretX = 0
			}
			if c.CaretX > len(line) {
				c.CaretX = len(line)
			}
			line = append(line[:c.CaretX], append([]rune{r}, line[c.CaretX:]...)...)
			c.Lines[c.CaretY] = string(line)
			c.CaretX++
		}
	}
	c.clampCaret()
	c.parent.updateContentSize()
	c.parent.onChange()
	c.parent.invalidate()
}

func (c *textAreaContent) insertNewline() {
	line := c.Lines[c.CaretY]
	runes := []rune(line)
	if c.CaretX > len(runes) {
		c.CaretX = len(runes)
	}
	head := runes[:c.CaretX]
	tail := runes[c.CaretX:]
	c.Lines[c.CaretY] = string(head)
	c.Lines = append(c.Lines[:c.CaretY+1], append([]string{""}, c.Lines[c.CaretY+1:]...)...)
	c.Lines[c.CaretY+1] = string(tail)
	c.CaretY++
	c.CaretX = 0
	c.clampCaret()
	c.parent.updateContentSize()
	c.ensureCaretVisible()
	c.parent.onChange()
	c.parent.invalidate()
}
