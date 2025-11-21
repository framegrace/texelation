package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// Checkbox is a toggleable widget that displays a checked or unchecked state.
// Format: [X] Label or [ ] Label
// When focused, shows a cursor: > [X] Label
type Checkbox struct {
	core.BaseWidget
	Label    string
	Checked  bool
	Style    tcell.Style
	OnChange func(checked bool)
}

// NewCheckbox creates a checkbox at the specified position.
// Width is calculated automatically based on label length.
func NewCheckbox(x, y int, label string) *Checkbox {
	c := &Checkbox{
		Label:   label,
		Checked: false,
	}

	// Get default style from theme
	tm := theme.Get()
	fg := tm.GetColor("ui", "text_fg", tcell.ColorWhite)
	bg := tm.GetColor("ui", "surface_bg", tcell.ColorBlack)
	c.Style = tcell.StyleDefault.Foreground(fg).Background(bg)

	// Configure focused style
	focusFg := tm.GetColor("ui", "focus_text_fg", tcell.ColorSilver)
	focusBg := tm.GetColor("ui", "focus_surface_bg", bg)
	c.SetFocusedStyle(tcell.StyleDefault.Foreground(focusFg).Background(focusBg), true)

	c.SetPosition(x, y)

	// Width: "> [X] " + label = 6 + len(label) (includes cursor when focused)
	w := 6 + len(label)
	c.Resize(w, 1)

	// Checkboxes are focusable by default
	c.SetFocusable(true)

	return c
}

// Draw renders the checkbox with its current state.
func (c *Checkbox) Draw(painter *core.Painter) {
	style := c.EffectiveStyle(c.Style)

	// Fill background
	painter.Fill(core.Rect{X: c.Rect.X, Y: c.Rect.Y, W: c.Rect.W, H: 1}, ' ', style)

	// Add cursor indicator when focused
	var cursor string
	if c.IsFocused() {
		cursor = "> "
	} else {
		cursor = "  "
	}

	// Determine checkbox character
	var checkChar string
	if c.Checked {
		checkChar = "[X] "
	} else {
		checkChar = "[ ] "
	}

	// Draw cursor, checkbox indicator, and label
	displayText := cursor + checkChar + c.Label
	painter.DrawText(c.Rect.X, c.Rect.Y, displayText, style)
}

// HandleKey processes keyboard input. Space toggles the checkbox.
func (c *Checkbox) HandleKey(ev *tcell.EventKey) bool {
	if ev.Rune() == ' ' || ev.Key() == tcell.KeyEnter {
		c.toggle()
		return true
	}
	return false
}

// HandleMouse processes mouse input. Click toggles the checkbox.
func (c *Checkbox) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !c.HitTest(x, y) {
		return false
	}

	switch ev.Buttons() {
	case tcell.Button1: // Left mouse button press
		c.toggle()
		return true
	}

	return false
}

// toggle switches the checked state and triggers the OnChange callback.
func (c *Checkbox) toggle() {
	c.Checked = !c.Checked
	if c.OnChange != nil {
		c.OnChange(c.Checked)
	}
}
