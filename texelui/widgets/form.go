// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/form.go
// Summary: Form layout widget for label/field pairs with focus management.

package widgets

import (
	"sort"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// FormRow represents a single row in a Form.
type FormRow struct {
	Label     *Label      // Optional label (nil for full-width fields)
	Field     core.Widget // The input field widget
	Height    int         // Row height (default 1)
	FullWidth bool        // If true, field spans full width (no label column)
}

// FormConfig holds configuration for a Form widget.
type FormConfig struct {
	PaddingX   int // Horizontal padding (default 2)
	PaddingY   int // Vertical padding (default 1)
	LabelWidth int // Width of label column (default 22)
	RowSpacing int // Vertical spacing between rows (default 0)
}

// DefaultFormConfig returns the default form configuration.
func DefaultFormConfig() FormConfig {
	return FormConfig{
		PaddingX:   2,
		PaddingY:   1,
		LabelWidth: 22,
		RowSpacing: 0,
	}
}

// Form is a layout widget that arranges label/field pairs vertically.
// It manages focus cycling between fields and highlights labels when
// their associated field has focus.
type Form struct {
	core.BaseWidget
	Style  tcell.Style
	Config FormConfig

	rows           []FormRow
	inv            func(core.Rect)
	lastFocusedIdx int // Index of last focused field for focus restoration
}

// NewForm creates a new empty form with default configuration.
// Position defaults to 0,0 and size to 1,1.
// Use SetPosition and Resize to adjust after adding to a layout.
func NewForm() *Form {
	return NewFormWithConfig(DefaultFormConfig())
}

// NewFormWithConfig creates a new form with custom configuration.
func NewFormWithConfig(config FormConfig) *Form {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")
	fg := tm.GetSemanticColor("text.primary")

	f := &Form{
		Style:          tcell.StyleDefault.Background(bg).Foreground(fg),
		Config:         config,
		lastFocusedIdx: -1,
	}
	f.SetPosition(0, 0)
	f.Resize(1, 1)
	f.SetFocusable(true)
	return f
}

// AddRow adds a row to the form.
func (f *Form) AddRow(row FormRow) {
	if row.Height <= 0 {
		row.Height = 1
	}
	f.rows = append(f.rows, row)
	f.layout()
}

// AddField adds a labeled field to the form (convenience method).
func (f *Form) AddField(label string, field core.Widget) {
	f.AddRow(FormRow{
		Label:  NewLabel(label),
		Field:  field,
		Height: 1,
	})
}

// AddFullWidthField adds a field that spans the full width (no label).
func (f *Form) AddFullWidthField(field core.Widget, height int) {
	if height <= 0 {
		height = 1
	}
	f.AddRow(FormRow{
		Field:     field,
		Height:    height,
		FullWidth: true,
	})
}

// AddSpacer adds an empty row for visual separation.
func (f *Form) AddSpacer(height int) {
	if height <= 0 {
		height = 1
	}
	f.AddRow(FormRow{Height: height})
}

// ClearRows removes all rows from the form.
func (f *Form) ClearRows() {
	f.rows = nil
	f.lastFocusedIdx = -1
}

// SetInvalidator implements core.InvalidationAware.
func (f *Form) SetInvalidator(fn func(core.Rect)) {
	f.inv = fn
	for _, row := range f.rows {
		if row.Label != nil {
			row.Label.SetInvalidator(fn)
		}
		if row.Field != nil {
			if ia, ok := row.Field.(core.InvalidationAware); ok {
				ia.SetInvalidator(fn)
			}
		}
	}
}

// Draw renders the form with all rows.
func (f *Form) Draw(painter *core.Painter) {
	style := f.EffectiveStyle(f.Style)
	painter.Fill(f.Rect, ' ', style)

	// Collect all widgets with their draw order
	type drawItem struct {
		widget core.Widget
		z      int
		order  int
	}
	items := make([]drawItem, 0, len(f.rows)*2)

	// Find the currently focused field index for label highlighting
	focusedIdx := f.getFocusedFieldIndex()

	for i, row := range f.rows {
		if row.Label != nil {
			// Highlight label if its associated field is focused
			if focusedIdx == i {
				f.syncLabelFocus(row.Label, true)
			} else {
				f.syncLabelFocus(row.Label, false)
			}
			items = append(items, drawItem{
				widget: row.Label,
				z:      widgetZIndex(row.Label),
				order:  i * 2,
			})
		}
		if row.Field != nil {
			items = append(items, drawItem{
				widget: row.Field,
				z:      widgetZIndex(row.Field),
				order:  i*2 + 1,
			})
		}
	}

	// Sort by z-index, then by order
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].z == items[j].z {
			return items[i].order < items[j].order
		}
		return items[i].z < items[j].z
	})

	for _, item := range items {
		item.widget.Draw(painter)
	}
}

// syncLabelFocus updates the label's visual style based on field focus.
func (f *Form) syncLabelFocus(label *Label, focused bool) {
	tm := theme.Get()
	if focused {
		// Use primary text color (brighter) when focused
		fg := tm.GetSemanticColor("text.primary")
		bg := tm.GetSemanticColor("bg.surface")
		label.Style = tcell.StyleDefault.Foreground(fg).Background(bg).Bold(true)
	} else {
		// Use secondary/dimmed text color when not focused
		fg := tm.GetSemanticColor("text.secondary")
		if fg == tcell.ColorDefault {
			fg = tm.GetSemanticColor("text.primary")
		}
		bg := tm.GetSemanticColor("bg.surface")
		label.Style = tcell.StyleDefault.Foreground(fg).Background(bg).Dim(true)
	}
}

// Resize updates the form size and relays out rows.
func (f *Form) Resize(w, h int) {
	f.BaseWidget.Resize(w, h)
	f.layout()
}

// SetPosition updates the form position and relays out rows.
func (f *Form) SetPosition(x, y int) {
	f.BaseWidget.SetPosition(x, y)
	f.layout()
}

// layout positions all rows within the form.
func (f *Form) layout() {
	x := f.Rect.X + f.Config.PaddingX
	y := f.Rect.Y + f.Config.PaddingY
	maxW := f.Rect.W - (f.Config.PaddingX * 2)
	if maxW < 1 {
		maxW = 1
	}

	for _, row := range f.rows {
		if row.Label != nil {
			row.Label.SetPosition(x, y)
			labelW := f.Config.LabelWidth
			if labelW > maxW {
				labelW = maxW
			}
			row.Label.Resize(labelW, 1)
		}

		if row.Field != nil {
			// Check if field is expanded (e.g., ColorPicker)
			isExpanded := false
			if exp, ok := row.Field.(core.Expandable); ok && exp.IsExpanded() {
				isExpanded = true
			}

			if row.FullWidth || row.Label == nil {
				row.Field.SetPosition(x, y)
				if !isExpanded {
					row.Field.Resize(maxW, row.Height)
				}
			} else {
				fieldX := x + f.Config.LabelWidth + 2
				fieldW := f.Rect.X + f.Rect.W - fieldX - f.Config.PaddingX
				if fieldW < 1 {
					fieldW = 1
				}
				row.Field.SetPosition(fieldX, y)
				if !isExpanded {
					row.Field.Resize(fieldW, row.Height)
				}
			}
		}
		y += row.Height + f.Config.RowSpacing
	}
}

// VisitChildren implements core.ChildContainer.
func (f *Form) VisitChildren(fn func(core.Widget)) {
	for _, row := range f.rows {
		if row.Label != nil {
			fn(row.Label)
		}
		if row.Field != nil {
			fn(row.Field)
		}
	}
}

// WidgetAt implements core.HitTester.
func (f *Form) WidgetAt(x, y int) core.Widget {
	if !f.HitTest(x, y) {
		return nil
	}

	var best core.Widget
	bestZ := -1
	bestOrder := -1

	for i, row := range f.rows {
		if row.Label != nil && row.Label.HitTest(x, y) {
			order := i * 2
			z := widgetZIndex(row.Label)
			if z > bestZ || (z == bestZ && order > bestOrder) {
				best = row.Label
				bestZ = z
				bestOrder = order
			}
		}
		if row.Field != nil && row.Field.HitTest(x, y) {
			order := i*2 + 1
			z := widgetZIndex(row.Field)
			if z > bestZ || (z == bestZ && order > bestOrder) {
				best = row.Field
				bestZ = z
				bestOrder = order
			}
		}
	}
	return best
}

// getFocusableFields returns all focusable field widgets.
func (f *Form) getFocusableFields() []core.Widget {
	var result []core.Widget
	for _, row := range f.rows {
		if row.Field != nil && row.Field.Focusable() {
			result = append(result, row.Field)
		}
	}
	return result
}

// getFocusedFieldIndex returns the row index of the focused field, or -1.
func (f *Form) getFocusedFieldIndex() int {
	for i, row := range f.rows {
		if row.Field != nil {
			if fs, ok := row.Field.(core.FocusState); ok && fs.IsFocused() {
				return i
			}
			// Also check nested focus
			if core.IsDescendantFocused(row.Field) {
				return i
			}
		}
	}
	return -1
}

// Focus focuses the first focusable field, or restores last focused field.
func (f *Form) Focus() {
	f.BaseWidget.Focus()
	fields := f.getFocusableFields()
	if len(fields) == 0 {
		return
	}
	// Try to restore last focused field
	if f.lastFocusedIdx >= 0 && f.lastFocusedIdx < len(fields) {
		fields[f.lastFocusedIdx].Focus()
		return
	}
	// Focus first field
	fields[0].Focus()
	f.lastFocusedIdx = 0
}

// Blur blurs all fields and tracks which one was focused.
func (f *Form) Blur() {
	fields := f.getFocusableFields()
	for i, w := range fields {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			f.lastFocusedIdx = i
			w.Blur()
			break
		}
	}
	f.BaseWidget.Blur()
}

// TrapsFocus returns false - Form doesn't trap focus at boundaries.
func (f *Form) TrapsFocus() bool {
	return false
}

// CycleFocus moves focus to next (forward=true) or previous (forward=false) field.
// Returns true if focus was successfully cycled, false if at boundary.
func (f *Form) CycleFocus(forward bool) bool {
	fields := f.getFocusableFields()
	if len(fields) == 0 {
		return false
	}

	// Find currently focused field (check both direct focus and descendant focus)
	currentIdx := -1
	var focusedField core.Widget
	for i, w := range fields {
		isFocused := false
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			isFocused = true
		}
		// Also check if a descendant is focused (e.g., TextArea inside Border)
		if !isFocused && core.IsDescendantFocused(w) {
			isFocused = true
		}
		if isFocused {
			currentIdx = i
			focusedField = w
			break
		}
	}

	// If nothing focused, focus first/last based on direction
	if currentIdx < 0 {
		if forward {
			fields[0].Focus()
			f.lastFocusedIdx = 0
		} else {
			fields[len(fields)-1].Focus()
			f.lastFocusedIdx = len(fields) - 1
		}
		f.invalidate()
		return true
	}

	var nextIdx int
	if forward {
		nextIdx = currentIdx + 1
		if nextIdx >= len(fields) {
			return false // At boundary, let parent handle
		}
	} else {
		nextIdx = currentIdx - 1
		if nextIdx < 0 {
			return false // At boundary, let parent handle
		}
	}

	focusedField.Blur()
	fields[nextIdx].Focus()
	f.lastFocusedIdx = nextIdx
	f.invalidate()
	return true
}

// HandleKey routes key events to the focused field.
func (f *Form) HandleKey(ev *tcell.EventKey) bool {
	fields := f.getFocusableFields()
	for i, w := range fields {
		// Check both direct focus and descendant focus (e.g., TextArea inside Border)
		isFocused := false
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			isFocused = true
		}
		if !isFocused && core.IsDescendantFocused(w) {
			isFocused = true
		}
		if isFocused {
			// For Tab/Shift-Tab, only forward to nested containers
			isTab := ev.Key() == tcell.KeyTab || ev.Key() == tcell.KeyBacktab
			if isTab {
				if _, isContainer := w.(core.FocusCycler); isContainer {
					if w.HandleKey(ev) {
						return true
					}
				}
				return false // Let parent handle Tab
			}
			// Route other keys to focused field
			if w.HandleKey(ev) {
				f.lastFocusedIdx = i
				return true
			}
			return false
		}
	}
	return false
}

// HandleMouse routes mouse events to fields, handling click-to-focus.
func (f *Form) HandleMouse(ev *tcell.EventMouse) bool {
	_, y := ev.Position()
	buttons := ev.Buttons()
	isPress := buttons&tcell.Button1 != 0
	isWheel := buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0

	// Note: We skip f.HitTest entirely because our position may be offset
	// due to being inside a ScrollPane. Parent already validated the hit.

	// Sort fields by Z-index descending for mouse routing
	type fieldInfo struct {
		field core.Widget
		z     int
		idx   int
		row   int // Row index for position-based matching
	}
	var sortedFields []fieldInfo
	fields := f.getFocusableFields()

	// Build field list with row indices for position matching
	fieldIdx := 0
	for rowIdx, row := range f.rows {
		if row.Field != nil && row.Field.Focusable() {
			sortedFields = append(sortedFields, fieldInfo{
				field: row.Field,
				z:     widgetZIndex(row.Field),
				idx:   fieldIdx,
				row:   rowIdx,
			})
			fieldIdx++
		}
	}
	sort.Slice(sortedFields, func(i, j int) bool {
		return sortedFields[i].z > sortedFields[j].z
	})

	// For wheel events, forward to the focused field (since HitTest is unreliable)
	if isWheel {
		for _, fi := range sortedFields {
			isFocused := false
			if fs, ok := fi.field.(core.FocusState); ok && fs.IsFocused() {
				isFocused = true
			}
			if !isFocused && core.IsDescendantFocused(fi.field) {
				isFocused = true
			}
			if isFocused {
				if ma, ok := fi.field.(core.MouseAware); ok {
					return ma.HandleMouse(ev)
				}
			}
		}
		// No focused field, let parent handle
		return false
	}

	// For click events, find field by checking which row contains the Y coordinate
	// Use relative Y position since absolute positions may be offset by scroll
	relY := y - f.Rect.Y - f.Config.PaddingY
	if relY < 0 {
		return false
	}

	// Find which row the click is in (iterate in row order, not z-order)
	rowY := 0
	for rowIdx, row := range f.rows {
		rowEnd := rowY + row.Height + f.Config.RowSpacing
		if relY >= rowY && relY < rowEnd {
			// Click is in this row - find the corresponding field
			if row.Field != nil && row.Field.Focusable() {
				// Find field index for lastFocusedIdx
				fieldIdx := 0
				for i := 0; i < rowIdx; i++ {
					if f.rows[i].Field != nil && f.rows[i].Field.Focusable() {
						fieldIdx++
					}
				}

				if isPress {
					// Blur currently focused field
					for _, w := range fields {
						if fs, ok := w.(core.FocusState); ok && fs.IsFocused() && w != row.Field {
							w.Blur()
						}
					}
					row.Field.Focus()
					f.lastFocusedIdx = fieldIdx
					f.invalidate()
				}
				if ma, ok := row.Field.(core.MouseAware); ok {
					return ma.HandleMouse(ev)
				}
				return true
			}
			// Row has no focusable field (spacer?), but we're in it
			return false
		}
		rowY = rowEnd
	}

	return false
}

// ContentHeight returns the total height needed to display all rows.
func (f *Form) ContentHeight() int {
	height := f.Config.PaddingY
	for _, row := range f.rows {
		height += row.Height + f.Config.RowSpacing
	}
	height += f.Config.PaddingY
	return height
}

// invalidate marks the form as needing redraw.
func (f *Form) invalidate() {
	if f.inv != nil {
		f.inv(f.Rect)
	}
}

// widgetZIndex returns the z-index of a widget.
func widgetZIndex(w core.Widget) int {
	if zi, ok := w.(core.ZIndexer); ok {
		return zi.ZIndex()
	}
	return 0
}
