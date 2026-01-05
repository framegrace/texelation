package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// Checkbox is a toggleable widget that displays a checked or unchecked state.
// Format: [X] Label or [ ] Label
type Checkbox struct {
	core.BaseWidget
	Label    string
	Checked  bool
	Style    tcell.Style
	OnChange func(checked bool)

	// Invalidation callback
	inv func(core.Rect)
}

// NewCheckbox creates a checkbox with the specified label.
// Position defaults to 0,0 and width is calculated automatically based on label length.
// Use SetPosition to adjust after adding to a layout.
func NewCheckbox(label string) *Checkbox {
	c := &Checkbox{
		Label:   label,
		Checked: false,
	}

	// Get default style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	c.Style = tcell.StyleDefault.Foreground(fg).Background(bg)

	// Configure focused style - reverse colors for clear visibility
	c.SetFocusedStyle(tcell.StyleDefault.Foreground(bg).Background(fg), true)

	c.SetPosition(0, 0)

	// Width: "[X] " + label = 4 + len(label)
	w := 4 + len(label)
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

	// Determine checkbox character
	var checkChar string
	if c.Checked {
		checkChar = "[X] "
	} else {
		checkChar = "[ ] "
	}

	// Draw checkbox indicator and label
	displayText := checkChar + c.Label
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
	c.invalidate()
	if c.OnChange != nil {
		c.OnChange(c.Checked)
	}
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (c *Checkbox) SetInvalidator(fn func(core.Rect)) { c.inv = fn }

// GetKeyHints implements core.KeyHintsProvider.
func (c *Checkbox) GetKeyHints() []core.KeyHint {
	return []core.KeyHint{
		{Key: "Space", Label: "Toggle"},
	}
}

// invalidate marks the widget as needing redraw.
func (c *Checkbox) invalidate() {
	if c.inv != nil {
		c.inv(c.Rect)
	}
}
