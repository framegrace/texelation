// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/configeditor/form.go
// Summary: Form layout helpers for the config editor.

package configeditor

import (
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

const (
	formPaddingX = 2
	formPaddingY = 1
	labelWidth   = 22
	rowSpacing   = 0
)

type formRow struct {
	label     *widgets.Label
	field     core.Widget
	height    int
	fullWidth bool
}

type formPane struct {
	core.BaseWidget
	Style          tcell.Style
	rows           []formRow
	inv            func(core.Rect)
	lastFocusedIdx int // Index of last focused field for focus restoration
}

func newFormPane(x, y, w, h int) *formPane {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")
	fg := tm.GetSemanticColor("text.primary")
	p := &formPane{
		Style:          tcell.StyleDefault.Background(bg).Foreground(fg),
		lastFocusedIdx: -1,
	}
	p.SetPosition(x, y)
	p.Resize(w, h)
	p.SetFocusable(true)
	return p
}

func (p *formPane) AddRow(row formRow) {
	if row.height <= 0 {
		row.height = 1
	}
	p.rows = append(p.rows, row)
}

func (p *formPane) SetInvalidator(fn func(core.Rect)) {
	p.inv = fn
	for _, row := range p.rows {
		if row.label != nil {
			row.label.SetInvalidator(fn)
		}
		if row.field != nil {
			if ia, ok := row.field.(core.InvalidationAware); ok {
				ia.SetInvalidator(fn)
			}
		}
	}
}

func (p *formPane) Draw(painter *core.Painter) {
	style := p.EffectiveStyle(p.Style)
	painter.Fill(p.Rect, ' ', style)
	items := make([]drawItem, 0, len(p.rows)*2)
	for i, row := range p.rows {
		if row.label != nil {
			items = append(items, drawItem{
				widget: row.label,
				z:      widgetZ(row.label),
				order:  i * 2,
			})
		}
		if row.field != nil {
			items = append(items, drawItem{
				widget: row.field,
				z:      widgetZ(row.field),
				order:  i*2 + 1,
			})
		}
	}
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

func (p *formPane) Resize(w, h int) {
	p.BaseWidget.Resize(w, h)
	p.layout()
}

func (p *formPane) SetPosition(x, y int) {
	p.BaseWidget.SetPosition(x, y)
	p.layout()
}

func (p *formPane) layout() {
	x := p.Rect.X + formPaddingX
	y := p.Rect.Y + formPaddingY
	maxW := p.Rect.W - (formPaddingX * 2)
	if maxW < 1 {
		maxW = 1
	}

	for _, row := range p.rows {
		if row.label != nil {
			row.label.SetPosition(x, y)
			row.label.Resize(minInt(labelWidth, maxW), 1)
		}
		if row.field != nil {
			// Check if field is expanded (e.g., expanded ColorPicker).
			// Expanded widgets manage their own size, so skip resizing them.
			isExpanded := false
			if exp, ok := row.field.(core.Expandable); ok && exp.IsExpanded() {
				isExpanded = true
			}

			if row.fullWidth || row.label == nil {
				row.field.SetPosition(x, y)
				if !isExpanded {
					row.field.Resize(maxW, row.height)
				}
			} else {
				fieldX := x + labelWidth + 2
				fieldW := p.Rect.X + p.Rect.W - fieldX - formPaddingX
				if fieldW < 1 {
					fieldW = 1
				}
				row.field.SetPosition(fieldX, y)
				if !isExpanded {
					row.field.Resize(fieldW, row.height)
				}
			}
		}
		y += row.height + rowSpacing
	}
}

func (p *formPane) VisitChildren(f func(core.Widget)) {
	for _, row := range p.rows {
		if row.label != nil {
			f(row.label)
		}
		if row.field != nil {
			f(row.field)
		}
	}
}

func (p *formPane) WidgetAt(x, y int) core.Widget {
	if !p.HitTest(x, y) {
		return nil
	}
	var best core.Widget
	bestZ := -1
	bestOrder := -1
	for i, row := range p.rows {
		if row.label != nil && row.label.HitTest(x, y) {
			order := i * 2
			z := widgetZ(row.label)
			if z > bestZ || (z == bestZ && order > bestOrder) {
				best = row.label
				bestZ = z
				bestOrder = order
			}
		}
		if row.field != nil && row.field.HitTest(x, y) {
			order := i*2 + 1
			z := widgetZ(row.field)
			if z > bestZ || (z == bestZ && order > bestOrder) {
				best = row.field
				bestZ = z
				bestOrder = order
			}
		}
	}
	return best
}

// getFocusableFields returns all focusable field widgets.
func (p *formPane) getFocusableFields() []core.Widget {
	var result []core.Widget
	for _, row := range p.rows {
		if row.field != nil && row.field.Focusable() {
			result = append(result, row.field)
		}
	}
	return result
}

// Focus focuses the first focusable field, or restores last focused field.
func (p *formPane) Focus() {
	p.BaseWidget.Focus()
	fields := p.getFocusableFields()
	if len(fields) == 0 {
		return
	}
	// Try to restore last focused field
	if p.lastFocusedIdx >= 0 && p.lastFocusedIdx < len(fields) {
		fields[p.lastFocusedIdx].Focus()
		return
	}
	// Focus first field
	fields[0].Focus()
	p.lastFocusedIdx = 0
}

// Blur blurs all fields and tracks which one was focused.
func (p *formPane) Blur() {
	fields := p.getFocusableFields()
	for i, w := range fields {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			p.lastFocusedIdx = i
			w.Blur()
			break
		}
	}
	p.BaseWidget.Blur()
}

// TrapsFocus returns false - formPane doesn't trap focus at boundaries.
func (p *formPane) TrapsFocus() bool {
	return false
}

// CycleFocus moves focus to next (forward=true) or previous (forward=false) field.
// Returns true if focus was successfully cycled, false if at boundary.
func (p *formPane) CycleFocus(forward bool) bool {
	fields := p.getFocusableFields()
	if len(fields) == 0 {
		return false
	}

	// Find currently focused field
	currentIdx := -1
	var focusedField core.Widget
	for i, w := range fields {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			currentIdx = i
			focusedField = w
			break
		}
	}

	// If nothing focused, focus first/last based on direction
	if currentIdx < 0 {
		if forward {
			fields[0].Focus()
			p.lastFocusedIdx = 0
		} else {
			fields[len(fields)-1].Focus()
			p.lastFocusedIdx = len(fields) - 1
		}
		if p.inv != nil {
			p.inv(p.Rect)
		}
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
	p.lastFocusedIdx = nextIdx
	if p.inv != nil {
		p.inv(p.Rect)
	}
	return true
}

// HandleKey routes key events to the focused field.
func (p *formPane) HandleKey(ev *tcell.EventKey) bool {
	fields := p.getFocusableFields()
	for i, w := range fields {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
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
				p.lastFocusedIdx = i
				return true
			}
			return false
		}
	}
	return false
}

// HandleMouse routes mouse events to fields, handling click-to-focus.
func (p *formPane) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !p.HitTest(x, y) {
		return false
	}

	buttons := ev.Buttons()
	isPress := buttons&tcell.Button1 != 0
	isWheel := buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0

	// Sort fields by Z-index descending for mouse routing
	type fieldInfo struct {
		field core.Widget
		z     int
		idx   int
	}
	var sortedFields []fieldInfo
	fields := p.getFocusableFields()
	for i, f := range fields {
		sortedFields = append(sortedFields, fieldInfo{field: f, z: widgetZ(f), idx: i})
	}
	sort.Slice(sortedFields, func(i, j int) bool {
		return sortedFields[i].z > sortedFields[j].z
	})

	// Check fields in Z-order
	for _, fi := range sortedFields {
		if fi.field.HitTest(x, y) {
			// Focus the clicked field on button press
			if isPress {
				// Blur currently focused field
				for _, w := range fields {
					if fs, ok := w.(core.FocusState); ok && fs.IsFocused() && w != fi.field {
						w.Blur()
					}
				}
				fi.field.Focus()
				p.lastFocusedIdx = fi.idx
				if p.inv != nil {
					p.inv(p.Rect)
				}
			}
			if ma, ok := fi.field.(core.MouseAware); ok {
				return ma.HandleMouse(ev)
			}
			return !isWheel
		}
	}
	return !isWheel
}

// ContentHeight returns the total height needed to display all rows.
func (p *formPane) ContentHeight() int {
	height := formPaddingY
	for _, row := range p.rows {
		height += row.height + rowSpacing
	}
	height += formPaddingY
	return height
}

type drawItem struct {
	widget core.Widget
	z      int
	order  int
}

func widgetZ(w core.Widget) int {
	if zi, ok := w.(core.ZIndexer); ok {
		return zi.ZIndex()
	}
	return 0
}

func keysSorted(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func humanLabel(value string) string {
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, ".", " ")
	words := strings.Fields(value)
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
