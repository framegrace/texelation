// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/combobox.go
// Summary: ComboBox widget combining text input with dropdown list selection.

package widgets

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// ComboBox combines a text input with a dropdown list.
// It supports filtering, autocomplete display, and optional editing.
type ComboBox struct {
	core.BaseWidget

	// Items is the list of available options
	Items []string

	// Text is the current text value (may or may not be in Items)
	Text string

	// Editable determines if the user can type custom values
	Editable bool

	// Placeholder shown when Text is empty
	Placeholder string

	// OnChange is called when the value changes
	OnChange func(string)

	// Internal state
	expanded     bool
	cursorPos    int
	scrollOffset int
	selectedIdx  int      // Index in filtered list
	filtered     []string // Filtered items based on Text
	inv          func(core.Rect)
}

// NewComboBox creates a new combo box at the specified position.
func NewComboBox(x, y, w int, items []string, editable bool) *ComboBox {
	cb := &ComboBox{
		Items:    items,
		Editable: editable,
		filtered: items,
	}
	cb.SetPosition(x, y)
	cb.Resize(w, 1)
	cb.SetFocusable(true)

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	cb.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	return cb
}

// SetInvalidator sets the invalidation callback.
func (cb *ComboBox) SetInvalidator(fn func(core.Rect)) {
	cb.inv = fn
}

// SetValue sets the current text value.
func (cb *ComboBox) SetValue(text string) {
	cb.Text = text
	cb.cursorPos = len(text)
	cb.updateFilter()
	cb.invalidate()
}

// Value returns the current text value.
func (cb *ComboBox) Value() string {
	return cb.Text
}

// dropdownRect returns the rectangle for the dropdown list.
func (cb *ComboBox) dropdownRect() core.Rect {
	maxHeight := 8
	if len(cb.filtered) < maxHeight {
		maxHeight = len(cb.filtered)
	}
	if maxHeight < 1 {
		maxHeight = 1
	}
	return core.Rect{
		X: cb.Rect.X,
		Y: cb.Rect.Y + 1,
		W: cb.Rect.W,
		H: maxHeight,
	}
}

// updateFilter updates the filtered list based on current text.
func (cb *ComboBox) updateFilter() {
	// Non-editable combos don't filter - always show all items
	if !cb.Editable || cb.Text == "" {
		cb.filtered = cb.Items
	} else {
		cb.filtered = nil
		lower := strings.ToLower(cb.Text)
		for _, item := range cb.Items {
			if strings.HasPrefix(strings.ToLower(item), lower) {
				cb.filtered = append(cb.filtered, item)
			}
		}
	}
	// Reset selection if out of bounds
	if cb.selectedIdx >= len(cb.filtered) {
		cb.selectedIdx = 0
	}
	// Adjust scroll
	cb.ensureSelectedVisible()
}

// ensureSelectedVisible adjusts scroll to keep selection visible.
func (cb *ComboBox) ensureSelectedVisible() {
	dr := cb.dropdownRect()
	if cb.selectedIdx < cb.scrollOffset {
		cb.scrollOffset = cb.selectedIdx
	} else if cb.selectedIdx >= cb.scrollOffset+dr.H {
		cb.scrollOffset = cb.selectedIdx - dr.H + 1
	}
}

// autocompleteMatch returns the best matching item for autocomplete.
func (cb *ComboBox) autocompleteMatch() string {
	if cb.Text == "" || len(cb.filtered) == 0 {
		return ""
	}
	// Return first filtered item as autocomplete suggestion
	return cb.filtered[0]
}

// Draw renders the combo box.
func (cb *ComboBox) Draw(p *core.Painter) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	dimFg := tm.GetSemanticColor("text.muted")
	accentFg := tm.GetSemanticColor("accent")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)
	dimStyle := tcell.StyleDefault.Foreground(dimFg).Background(bg)
	btnStyle := baseStyle

	focused := cb.IsFocused()
	if focused {
		btnStyle = tcell.StyleDefault.Foreground(accentFg).Background(bg)
	}

	// Fill background
	p.Fill(cb.Rect, ' ', baseStyle)

	x := cb.Rect.X
	y := cb.Rect.Y
	inputWidth := cb.Rect.W - 3 // Reserve 3 chars for button " ▼ "

	// Draw text input area
	displayText := cb.Text
	autocomplete := cb.autocompleteMatch()

	// Draw the typed text
	for i, ch := range displayText {
		if i >= inputWidth {
			break
		}
		p.SetCell(x+i, y, ch, baseStyle)
	}

	// Draw autocomplete suggestion (dimmed)
	if !cb.expanded && len(displayText) > 0 && len(autocomplete) > len(displayText) {
		suffix := autocomplete[len(displayText):]
		startX := x + len(displayText)
		for i, ch := range suffix {
			if startX+i >= x+inputWidth {
				break
			}
			p.SetCell(startX+i, y, ch, dimStyle)
		}
	}

	// Draw placeholder if empty
	if displayText == "" && cb.Placeholder != "" && !focused {
		for i, ch := range cb.Placeholder {
			if i >= inputWidth {
				break
			}
			p.SetCell(x+i, y, ch, dimStyle)
		}
	}

	// Draw cursor if focused and editable
	if focused && cb.Editable {
		cursorX := x + cb.cursorPos
		if cursorX < x+inputWidth {
			cursorStyle := baseStyle.Reverse(true)
			ch := ' '
			if cb.cursorPos < len(cb.Text) {
				ch = rune(cb.Text[cb.cursorPos])
			} else if !cb.expanded && len(autocomplete) > len(cb.Text) {
				// Show autocomplete char under cursor
				ch = rune(autocomplete[cb.cursorPos])
			}
			p.SetCell(cursorX, y, ch, cursorStyle)
		}
	}

	// Draw dropdown button
	btnX := cb.Rect.X + cb.Rect.W - 3
	btnChar := '▼'
	if cb.expanded {
		btnChar = '▲'
	}
	p.SetCell(btnX, y, ' ', btnStyle)
	p.SetCell(btnX+1, y, btnChar, btnStyle)
	p.SetCell(btnX+2, y, ' ', btnStyle)

	// Draw dropdown if expanded
	if cb.expanded {
		cb.drawDropdown(p)
	}
}

// drawDropdown renders the dropdown list.
func (cb *ComboBox) drawDropdown(p *core.Painter) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	selFg := tm.GetSemanticColor("text.primary")
	selBg := tm.GetSemanticColor("bg.selection")
	borderFg := tm.GetSemanticColor("border.default")

	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)
	selStyle := tcell.StyleDefault.Foreground(selFg).Background(selBg)
	borderStyle := tcell.StyleDefault.Foreground(borderFg).Background(bg)

	dr := cb.dropdownRect()

	// The dropdown has a top border at dr.Y, items from dr.Y+1, bottom border at dr.Y+dr.H+1
	topY := dr.Y
	contentY := dr.Y + 1
	bottomY := dr.Y + dr.H + 1

	// Fill background for content area
	contentRect := core.Rect{X: dr.X, Y: contentY, W: dr.W, H: dr.H}
	p.Fill(contentRect, ' ', baseStyle)

	// Draw top border
	for x := dr.X; x < dr.X+dr.W; x++ {
		p.SetCell(x, topY, '─', borderStyle)
	}
	p.SetCell(dr.X, topY, '├', borderStyle)
	p.SetCell(dr.X+dr.W-1, topY, '┤', borderStyle)

	// Draw bottom border
	for x := dr.X; x < dr.X+dr.W; x++ {
		p.SetCell(x, bottomY, '─', borderStyle)
	}
	p.SetCell(dr.X, bottomY, '╰', borderStyle)
	p.SetCell(dr.X+dr.W-1, bottomY, '╯', borderStyle)

	// Draw side borders
	for row := 0; row < dr.H; row++ {
		p.SetCell(dr.X, contentY+row, '│', borderStyle)
		p.SetCell(dr.X+dr.W-1, contentY+row, '│', borderStyle)
	}

	// Draw items
	for i := 0; i < dr.H; i++ {
		itemIdx := cb.scrollOffset + i
		if itemIdx >= len(cb.filtered) {
			break
		}
		item := cb.filtered[itemIdx]
		isSelected := itemIdx == cb.selectedIdx

		style := baseStyle
		if isSelected {
			style = selStyle
		}

		// Fill row
		for x := dr.X + 1; x < dr.X+dr.W-1; x++ {
			p.SetCell(x, contentY+i, ' ', style)
		}

		// Draw item text
		for j, ch := range item {
			if j >= dr.W-2 {
				break
			}
			p.SetCell(dr.X+1+j, contentY+i, ch, style)
		}
	}

	// Draw scroll indicators if needed
	if cb.scrollOffset > 0 {
		p.SetCell(dr.X+dr.W-2, contentY, '▲', baseStyle)
	}
	if cb.scrollOffset+dr.H < len(cb.filtered) {
		p.SetCell(dr.X+dr.W-2, contentY+dr.H-1, '▼', baseStyle)
	}
}

// HandleKey processes keyboard input.
func (cb *ComboBox) HandleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEsc:
		if cb.expanded {
			cb.expanded = false
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyEnter:
		if cb.expanded && len(cb.filtered) > 0 {
			// Select current item
			cb.Text = cb.filtered[cb.selectedIdx]
			cb.cursorPos = len(cb.Text)
			cb.expanded = false
			cb.updateFilter()
			cb.invalidate()
			if cb.OnChange != nil {
				cb.OnChange(cb.Text)
			}
			return true
		} else if !cb.expanded {
			// Accept autocomplete or current value
			autocomplete := cb.autocompleteMatch()
			if autocomplete != "" && len(autocomplete) > len(cb.Text) {
				cb.Text = autocomplete
				cb.cursorPos = len(cb.Text)
				cb.updateFilter()
			}
			cb.invalidate()
			if cb.OnChange != nil {
				cb.OnChange(cb.Text)
			}
			return true
		}
		return false

	case tcell.KeyTab:
		// Accept autocomplete on Tab
		if !cb.expanded {
			autocomplete := cb.autocompleteMatch()
			if autocomplete != "" && len(autocomplete) > len(cb.Text) {
				cb.Text = autocomplete
				cb.cursorPos = len(cb.Text)
				cb.updateFilter()
				cb.invalidate()
				return true
			}
		}
		return false

	case tcell.KeyUp:
		if cb.expanded {
			if cb.selectedIdx > 0 {
				cb.selectedIdx--
				cb.ensureSelectedVisible()
				cb.invalidate()
			}
			return true
		} else if len(cb.filtered) > 0 {
			// Open dropdown and select last matching
			cb.expanded = true
			cb.selectedIdx = 0
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyDown:
		if cb.expanded {
			if cb.selectedIdx < len(cb.filtered)-1 {
				cb.selectedIdx++
				cb.ensureSelectedVisible()
				cb.invalidate()
			}
			return true
		} else if len(cb.filtered) > 0 {
			// Open dropdown
			cb.expanded = true
			cb.selectedIdx = 0
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyLeft:
		if cb.Editable && cb.cursorPos > 0 {
			cb.cursorPos--
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyRight:
		if cb.Editable {
			autocomplete := cb.autocompleteMatch()
			maxPos := len(cb.Text)
			if !cb.expanded && len(autocomplete) > len(cb.Text) {
				// Accept one char from autocomplete
				cb.Text = autocomplete[:len(cb.Text)+1]
				cb.cursorPos = len(cb.Text)
				cb.updateFilter()
				cb.invalidate()
				return true
			} else if cb.cursorPos < maxPos {
				cb.cursorPos++
				cb.invalidate()
				return true
			}
		}
		return false

	case tcell.KeyHome:
		if cb.Editable && cb.cursorPos > 0 {
			cb.cursorPos = 0
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyEnd:
		if cb.Editable && cb.cursorPos < len(cb.Text) {
			cb.cursorPos = len(cb.Text)
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if cb.Editable && cb.cursorPos > 0 {
			cb.Text = cb.Text[:cb.cursorPos-1] + cb.Text[cb.cursorPos:]
			cb.cursorPos--
			cb.updateFilter()
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyDelete:
		if cb.Editable && cb.cursorPos < len(cb.Text) {
			cb.Text = cb.Text[:cb.cursorPos] + cb.Text[cb.cursorPos+1:]
			cb.updateFilter()
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyRune:
		if cb.Editable {
			ch := ev.Rune()
			cb.Text = cb.Text[:cb.cursorPos] + string(ch) + cb.Text[cb.cursorPos:]
			cb.cursorPos++
			cb.updateFilter()
			cb.invalidate()
			return true
		} else if !cb.expanded {
			// Non-editable: open dropdown on any key
			cb.expanded = true
			cb.invalidate()
			return true
		}
		return false
	}

	return false
}

// HandleMouse processes mouse input.
func (cb *ComboBox) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()

	// Check if click is on the widget or dropdown
	inMain := cb.HitTest(x, y)
	dr := cb.dropdownRect()
	// Dropdown has: top border at dr.Y, content from dr.Y+1 to dr.Y+dr.H, bottom border at dr.Y+dr.H+1
	inDropdown := cb.expanded && x >= dr.X && x < dr.X+dr.W && y >= dr.Y && y < dr.Y+dr.H+2

	if !inMain && !inDropdown {
		if cb.expanded {
			cb.expanded = false
			cb.invalidate()
		}
		return false
	}

	if ev.Buttons() != tcell.Button1 {
		return true
	}

	// Click on main area
	if inMain {
		btnX := cb.Rect.X + cb.Rect.W - 3
		if x >= btnX {
			// Click on button - toggle dropdown
			cb.expanded = !cb.expanded
			cb.invalidate()
			return true
		} else if cb.Editable {
			// Click on text area - position cursor
			cb.cursorPos = x - cb.Rect.X
			if cb.cursorPos > len(cb.Text) {
				cb.cursorPos = len(cb.Text)
			}
			cb.invalidate()
			return true
		}
	}

	// Click on dropdown content area (between top and bottom borders)
	contentY := dr.Y + 1
	if inDropdown && y >= contentY && y < contentY+dr.H {
		idx := cb.scrollOffset + (y - contentY)
		if idx >= 0 && idx < len(cb.filtered) {
			cb.Text = cb.filtered[idx]
			cb.cursorPos = len(cb.Text)
			cb.expanded = false
			cb.updateFilter()
			cb.invalidate()
			if cb.OnChange != nil {
				cb.OnChange(cb.Text)
			}
		}
		return true
	}

	return true
}

// HitTest checks if a point is within the combo box bounds.
func (cb *ComboBox) HitTest(x, y int) bool {
	if cb.Rect.Contains(x, y) {
		return true
	}
	if cb.expanded {
		dr := cb.dropdownRect()
		// Dropdown includes: top border at dr.Y, content, bottom border at dr.Y+dr.H+1
		if x >= dr.X && x < dr.X+dr.W && y >= dr.Y && y < dr.Y+dr.H+2 {
			return true
		}
	}
	return false
}

// IsModal returns true when the combo box is expanded.
func (cb *ComboBox) IsModal() bool {
	return cb.expanded
}

// DismissModal collapses the dropdown.
func (cb *ComboBox) DismissModal() {
	cb.expanded = false
	cb.invalidate()
}

// ZIndex returns higher z-index when expanded.
func (cb *ComboBox) ZIndex() int {
	if cb.expanded {
		return 100
	}
	return 0
}

// invalidate marks the widget as needing redraw.
func (cb *ComboBox) invalidate() {
	if cb.inv != nil {
		// Invalidate main rect plus dropdown area
		r := cb.Rect
		if cb.expanded {
			dr := cb.dropdownRect()
			// Main (1) + top border (1) + content (dr.H) + bottom border (1)
			r.H = 1 + 1 + dr.H + 1
		}
		cb.inv(r)
	}
}
