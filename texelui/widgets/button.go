package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// Button is a clickable widget that triggers an action when activated.
// It can be activated by mouse click or keyboard (Enter/Space).
type Button struct {
	core.BaseWidget
	Text    string
	Style   tcell.Style
	OnClick func()

	// Visual state
	pressed bool

	// Invalidation callback
	inv func(core.Rect)
}

// NewButton creates a button at the specified position and size.
// If w is 0, the button will size to fit the text plus padding.
// If h is 0, the button will default to height 1.
func NewButton(x, y, w, h int, text string) *Button {
	b := &Button{
		Text: text,
	}

	// Get default style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.inverse")
	bg := tm.GetSemanticColor("action.primary")
	b.Style = tcell.StyleDefault.Foreground(fg).Background(bg)

	// Configure focused style
	focusFg := tm.GetSemanticColor("text.inverse")
	focusBg := tm.GetSemanticColor("border.focus")
	b.SetFocusedStyle(tcell.StyleDefault.Foreground(focusFg).Background(focusBg), true)

	b.SetPosition(x, y)

	// Auto-size if dimensions are 0
	if w == 0 {
		// Add padding: [ Text ]
		w = len(text) + 4
	}
	if h == 0 {
		h = 1
	}
	b.Resize(w, h)

	// Buttons are focusable by default
	b.SetFocusable(true)

	return b
}

// Draw renders the button with text centered and optional brackets.
func (b *Button) Draw(painter *core.Painter) {
	style := b.EffectiveStyle(b.Style)

	// Invert colors when pressed for visual feedback
	if b.pressed {
		fg, bg, attr := style.Decompose()
		style = tcell.StyleDefault.Foreground(bg).Background(fg).Attributes(attr)
	}

	// Fill background
	painter.Fill(core.Rect{X: b.Rect.X, Y: b.Rect.Y, W: b.Rect.W, H: b.Rect.H}, ' ', style)

	if b.Text == "" {
		return
	}

	// Format text with brackets: [ Text ]
	displayText := "[ " + b.Text + " ]"
	textLen := len(displayText)
	if textLen > b.Rect.W {
		// Truncate if too long
		displayText = displayText[:b.Rect.W]
		textLen = b.Rect.W
	}

	// Center text horizontally and vertically
	x := b.Rect.X + (b.Rect.W-textLen)/2
	y := b.Rect.Y + b.Rect.H/2

	painter.DrawText(x, y, displayText, style)
}

// HandleKey processes keyboard input. Enter or Space activates the button.
func (b *Button) HandleKey(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEnter || ev.Rune() == ' ' {
		b.activate()
		return true
	}
	return false
}

// HandleMouse processes mouse input. Click activates the button.
func (b *Button) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !b.HitTest(x, y) {
		b.pressed = false
		return false
	}

	switch ev.Buttons() {
	case tcell.Button1: // Left mouse button
		b.pressed = true
		return true
	case tcell.ButtonNone: // Mouse release
		if b.pressed {
			b.pressed = false
			b.activate()
			return true
		}
	}

	return false
}

// activate triggers the OnClick callback if set.
func (b *Button) activate() {
	if b.OnClick != nil {
		b.OnClick()
	}
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (b *Button) SetInvalidator(fn func(core.Rect)) { b.inv = fn }

// invalidate marks the widget as needing redraw.
func (b *Button) invalidate() {
	if b.inv != nil {
		b.inv(b.Rect)
	}
}
