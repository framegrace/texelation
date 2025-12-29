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
	Style tcell.Style
	rows  []formRow
	inv   func(core.Rect)
}

func newFormPane(x, y, w, h int) *formPane {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")
	fg := tm.GetSemanticColor("text.primary")
	p := &formPane{
		Style: tcell.StyleDefault.Background(bg).Foreground(fg),
	}
	p.SetPosition(x, y)
	p.Resize(w, h)
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
			if row.fullWidth || row.label == nil {
				row.field.SetPosition(x, y)
				row.field.Resize(maxW, row.height)
			} else {
				fieldX := x + labelWidth + 2
				fieldW := p.Rect.X + p.Rect.W - fieldX - formPaddingX
				if fieldW < 1 {
					fieldW = 1
				}
				row.field.SetPosition(fieldX, y)
				row.field.Resize(fieldW, row.height)
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
