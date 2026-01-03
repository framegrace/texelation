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
	"texelation/texelui/primitives"
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
	expanded  bool
	cursorPos int
	filtered  []string // Filtered items based on Text
	inv       func(core.Rect)

	// Dropdown list widget
	list *primitives.ScrollableList
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

	// Create dropdown list (position will be set when expanded)
	cb.list = primitives.NewScrollableList(x, y+1, w, 8)
	cb.list.RenderItem = cb.renderDropdownItem
	cb.syncListItems()

	return cb
}

// SetInvalidator sets the invalidation callback.
func (cb *ComboBox) SetInvalidator(fn func(core.Rect)) {
	cb.inv = fn
	if cb.list != nil {
		cb.list.SetInvalidator(fn)
	}
}

// GetKeyHints implements core.KeyHintsProvider.
func (cb *ComboBox) GetKeyHints() []core.KeyHint {
	if cb.expanded {
		return []core.KeyHint{
			{Key: "↑↓", Label: "Navigate"},
			{Key: "Enter", Label: "Select"},
			{Key: "Esc", Label: "Close"},
		}
	}
	if cb.Editable {
		return []core.KeyHint{
			{Key: "↑↓", Label: "Open"},
			{Key: "Tab", Label: "Complete"},
			{Key: "←→", Label: "Move"},
		}
	}
	return []core.KeyHint{
		{Key: "↑↓/Space", Label: "Open"},
	}
}

// syncListItems updates the ScrollableList items from filtered.
func (cb *ComboBox) syncListItems() {
	items := make([]primitives.ListItem, len(cb.filtered))
	for i, s := range cb.filtered {
		items[i] = primitives.ListItem{Text: s, Value: s}
	}
	cb.list.SetItems(items)

	// Select the item matching cb.Text if present
	for i, s := range cb.filtered {
		if s == cb.Text {
			cb.list.SetSelected(i)
			break
		}
	}
}

// renderDropdownItem renders a dropdown item with proper styling.
func (cb *ComboBox) renderDropdownItem(p *core.Painter, rect core.Rect, item primitives.ListItem, selected bool) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	commitBg := tm.GetSemanticColor("accent")

	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	isCommitted := item.Text == cb.Text

	style := baseStyle
	if isCommitted {
		// Committed selection (item matching cb.Text) - accent background
		style = tcell.StyleDefault.Foreground(bg).Background(commitBg)
	} else if selected {
		// Active highlight (keyboard navigation) - reverse fg/bg
		style = baseStyle.Reverse(true)
	}

	// Fill row
	p.Fill(rect, ' ', style)

	// Draw item text
	text := item.Text
	maxLen := rect.W
	if len(text) > maxLen && maxLen > 0 {
		text = text[:maxLen]
	}
	p.DrawText(rect.X, rect.Y, text, style)
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
	cb.syncListItems()
}

// selectCurrentValue shows all items and selects the current Text value.
func (cb *ComboBox) selectCurrentValue() {
	// Show all items when opening dropdown (don't filter)
	cb.filtered = cb.Items
	cb.syncListItems()
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
	btnStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	focused := cb.IsFocused()
	if focused {
		// Add underline to show the input field extent when focused
		baseStyle = baseStyle.Underline(true)
		dimStyle = dimStyle.Underline(true)
		btnStyle = tcell.StyleDefault.Foreground(accentFg).Background(bg)
	}

	// Fill background (with underline when focused for input area)
	inputWidth := cb.Rect.W - 3 // Reserve 3 chars for button " ▼ "
	p.Fill(core.Rect{X: cb.Rect.X, Y: cb.Rect.Y, W: inputWidth, H: 1}, ' ', baseStyle)
	// Fill button area without underline
	p.Fill(core.Rect{X: cb.Rect.X + inputWidth, Y: cb.Rect.Y, W: 3, H: 1}, ' ', btnStyle)

	x := cb.Rect.X
	y := cb.Rect.Y

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
		placeholderStyle := tcell.StyleDefault.Foreground(dimFg).Background(bg)
		for i, ch := range cb.Placeholder {
			if i >= inputWidth {
				break
			}
			p.SetCell(x+i, y, ch, placeholderStyle)
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
	bg := tm.GetSemanticColor("bg.surface")
	borderFg := tm.GetSemanticColor("border.default")
	borderStyle := tcell.StyleDefault.Foreground(borderFg).Background(bg)

	dr := cb.dropdownRect()

	// Dropdown is shifted 1 char left so item text aligns with input text
	boxX := dr.X - 1
	boxW := dr.W + 1

	// The dropdown has a top border at dr.Y, list from dr.Y+1, bottom border at dr.Y+dr.H+1
	topY := dr.Y
	contentY := dr.Y + 1
	bottomY := dr.Y + dr.H + 1

	// Draw top border with normal corners
	for x := boxX; x < boxX+boxW; x++ {
		p.SetCell(x, topY, '─', borderStyle)
	}
	p.SetCell(boxX, topY, '╭', borderStyle)
	p.SetCell(boxX+boxW-1, topY, '╮', borderStyle)

	// Draw bottom border
	for x := boxX; x < boxX+boxW; x++ {
		p.SetCell(x, bottomY, '─', borderStyle)
	}
	p.SetCell(boxX, bottomY, '╰', borderStyle)
	p.SetCell(boxX+boxW-1, bottomY, '╯', borderStyle)

	// Draw side borders
	for row := 0; row < dr.H; row++ {
		p.SetCell(boxX, contentY+row, '│', borderStyle)
		p.SetCell(boxX+boxW-1, contentY+row, '│', borderStyle)
	}

	// Position and draw the list inside the borders
	cb.list.SetPosition(boxX+1, contentY)
	cb.list.Resize(boxW-2, dr.H)
	cb.list.Draw(p)
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
			// Select current item from dropdown
			item := cb.list.SelectedItem()
			if item != nil {
				cb.Text = item.Text
				cb.cursorPos = len(cb.Text)
			}
			cb.expanded = false
			cb.updateFilter()
			cb.invalidate()
			if cb.OnChange != nil {
				cb.OnChange(cb.Text)
			}
		} else if !cb.expanded && cb.Editable {
			// Editable: Accept autocomplete or current value
			autocomplete := cb.autocompleteMatch()
			if autocomplete != "" && len(autocomplete) > len(cb.Text) {
				cb.Text = autocomplete
				cb.cursorPos = len(cb.Text)
				cb.updateFilter()
				cb.invalidate()
				if cb.OnChange != nil {
					cb.OnChange(cb.Text)
				}
			}
		}
		// Return true to signal handled; UIManager advances focus if AdvanceFocusOnEnter is set
		return true

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

	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn:
		if cb.expanded {
			// Delegate to list for navigation
			if cb.list.HandleKey(ev) {
				cb.invalidate()
			}
			return true
		} else if len(cb.filtered) > 0 {
			// Open dropdown and preselect current value
			cb.expanded = true
			cb.selectCurrentValue()
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyHome:
		if cb.expanded {
			cb.list.HandleKey(ev)
			cb.invalidate()
			return true
		}
		if cb.Editable && cb.cursorPos > 0 {
			cb.cursorPos = 0
			cb.invalidate()
			return true
		}
		return false

	case tcell.KeyEnd:
		if cb.expanded {
			cb.list.HandleKey(ev)
			cb.invalidate()
			return true
		}
		if cb.Editable && cb.cursorPos < len(cb.Text) {
			cb.cursorPos = len(cb.Text)
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
			// Non-editable: open dropdown on any key and preselect current value
			cb.expanded = true
			cb.selectCurrentValue()
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
	buttons := ev.Buttons()

	// Check if click is on the widget or dropdown
	inMainRect := cb.Rect.Contains(x, y)
	dr := cb.dropdownRect()
	// Dropdown box is shifted 1 char left
	boxX := dr.X - 1
	boxW := dr.W + 1
	contentY := dr.Y + 1
	// Dropdown has: top border at dr.Y, content from dr.Y+1 to dr.Y+dr.H, bottom border at dr.Y+dr.H+1
	inDropdown := cb.expanded && x >= boxX && x < boxX+boxW && y >= dr.Y && y < dr.Y+dr.H+2
	// Check if in list content area specifically
	inListArea := cb.expanded && x >= boxX+1 && x < boxX+boxW-1 && y >= contentY && y < contentY+dr.H

	if !inMainRect && !inDropdown {
		if cb.expanded {
			cb.expanded = false
			cb.invalidate()
		}
		return false
	}

	// Delegate to list for dropdown interactions (scrollbar, wheel, clicks)
	if cb.expanded && inListArea {
		// Update list position for hit testing
		cb.list.SetPosition(boxX+1, contentY)
		cb.list.Resize(boxW-2, dr.H)

		// Handle wheel and scrollbar
		if buttons&(tcell.WheelUp|tcell.WheelDown) != 0 {
			if cb.list.HandleMouse(ev) {
				cb.invalidate()
			}
			return true
		}

		// Handle clicks on list items
		if buttons == tcell.Button1 {
			oldIdx := cb.list.SelectedIdx
			if cb.list.HandleMouse(ev) {
				// If selection changed, commit the selection
				if cb.list.SelectedIdx != oldIdx || true { // Always commit on click
					item := cb.list.SelectedItem()
					if item != nil {
						cb.Text = item.Text
						cb.cursorPos = len(cb.Text)
						cb.expanded = false
						cb.updateFilter()
						cb.invalidate()
						if cb.OnChange != nil {
							cb.OnChange(cb.Text)
						}
					}
				}
			}
			return true
		}
	}

	// Handle wheel on dropdown border area (route to list)
	if cb.expanded && inDropdown && buttons&(tcell.WheelUp|tcell.WheelDown) != 0 {
		cb.list.SetPosition(boxX+1, contentY)
		cb.list.Resize(boxW-2, dr.H)
		if cb.list.HandleMouse(ev) {
			cb.invalidate()
		}
		return true
	}

	// Only handle left clicks for other areas
	if buttons != tcell.Button1 {
		return true
	}

	// Click on main area
	if inMainRect {
		btnX := cb.Rect.X + cb.Rect.W - 3
		if x >= btnX {
			// Click on button - toggle dropdown
			if !cb.expanded {
				cb.expanded = true
				cb.selectCurrentValue()
			} else {
				cb.expanded = false
			}
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

	return true
}

// HitTest checks if a point is within the combo box bounds.
func (cb *ComboBox) HitTest(x, y int) bool {
	if cb.Rect.Contains(x, y) {
		return true
	}
	if cb.expanded {
		dr := cb.dropdownRect()
		// Dropdown box is shifted 1 char left
		boxX := dr.X - 1
		boxW := dr.W + 1
		// Dropdown includes: top border at dr.Y, content, bottom border at dr.Y+dr.H+1
		if x >= boxX && x < boxX+boxW && y >= dr.Y && y < dr.Y+dr.H+2 {
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

// Blur removes focus and closes the dropdown.
func (cb *ComboBox) Blur() {
	cb.BaseWidget.Blur()
	if cb.expanded {
		cb.expanded = false
		cb.invalidate()
	}
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
			// Dropdown is shifted 1 char left and 1 char wider
			r.X = dr.X - 1
			r.W = dr.W + 1
			// Main (1) + top border (1) + content (dr.H) + bottom border (1)
			r.H = 1 + 1 + dr.H + 1
		}
		cb.inv(r)
	}
}
