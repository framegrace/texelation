// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker/semantic.go
// Summary: Semantic color selection mode.

package colorpicker

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// SemanticPicker allows selection from semantic color names.
type SemanticPicker struct {
	semanticNames []string
	selectedIdx   int
}

// NewSemanticPicker creates a semantic color picker.
func NewSemanticPicker() *SemanticPicker {
	return &SemanticPicker{
		semanticNames: []string{
			"accent",
			"accent_secondary",
			"bg.base",
			"bg.mantle",
			"bg.crust",
			"bg.surface",
			"text.primary",
			"text.secondary",
			"text.muted",
			"text.inverse",
			"text.accent",
			"action.primary",
			"action.success",
			"action.warning",
			"action.danger",
			"selection",
			"border.active",
			"border.inactive",
			"border.resizing",
			"border.focus",
			"caret",
		},
		selectedIdx: 0,
	}
}

func (sp *SemanticPicker) Draw(painter *core.Painter, rect core.Rect) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	// Fill background
	painter.Fill(rect, ' ', baseStyle)

	// Calculate scroll offset to center selected item
	maxVisible := rect.H
	startIdx := 0

	if sp.selectedIdx >= maxVisible/2 {
		startIdx = sp.selectedIdx - maxVisible/2
	}
	if startIdx+maxVisible > len(sp.semanticNames) {
		startIdx = len(sp.semanticNames) - maxVisible
	}
	if startIdx < 0 {
		startIdx = 0
	}

	y := rect.Y
	for i := startIdx; i < len(sp.semanticNames) && y < rect.Y+rect.H; i++ {
		name := sp.semanticNames[i]
		color := tm.GetSemanticColor(name)

		style := baseStyle
		if i == sp.selectedIdx {
			style = style.Reverse(true)
		}

		// Draw: [██] semantic.name
		x := rect.X
		painter.SetCell(x, y, '[', style)
		x++
		painter.SetCell(x, y, '█', tcell.StyleDefault.Foreground(color).Background(bg))
		x++
		painter.SetCell(x, y, '█', tcell.StyleDefault.Foreground(color).Background(bg))
		x++
		painter.SetCell(x, y, ']', style)
		x += 2

		// Truncate name if needed
		displayName := name
		maxLen := rect.W - 6
		if len(displayName) > maxLen && maxLen > 0 {
			displayName = displayName[:maxLen]
		}
		painter.DrawText(x, y, displayName, style)

		y++
	}

	// Draw scroll indicators if needed
	if startIdx > 0 {
		painter.SetCell(rect.X+rect.W-1, rect.Y, '▲', baseStyle)
	}
	if startIdx+maxVisible < len(sp.semanticNames) {
		painter.SetCell(rect.X+rect.W-1, rect.Y+rect.H-1, '▼', baseStyle)
	}
}

func (sp *SemanticPicker) HandleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyUp:
		if sp.selectedIdx > 0 {
			sp.selectedIdx--
		}
		return true
	case tcell.KeyDown:
		if sp.selectedIdx < len(sp.semanticNames)-1 {
			sp.selectedIdx++
		}
		return true
	case tcell.KeyHome:
		sp.selectedIdx = 0
		return true
	case tcell.KeyEnd:
		sp.selectedIdx = len(sp.semanticNames) - 1
		return true
	case tcell.KeyPgUp:
		sp.selectedIdx -= 5
		if sp.selectedIdx < 0 {
			sp.selectedIdx = 0
		}
		return true
	case tcell.KeyPgDn:
		sp.selectedIdx += 5
		if sp.selectedIdx >= len(sp.semanticNames) {
			sp.selectedIdx = len(sp.semanticNames) - 1
		}
		return true
	}
	return false
}

func (sp *SemanticPicker) HandleMouse(ev *tcell.EventMouse, rect core.Rect) bool {
	x, y := ev.Position()
	if x < rect.X || y < rect.Y || x >= rect.X+rect.W || y >= rect.Y+rect.H {
		return false
	}

	if ev.Buttons() == tcell.Button1 {
		// Calculate which item was clicked
		maxVisible := rect.H
		startIdx := 0

		if sp.selectedIdx >= maxVisible/2 {
			startIdx = sp.selectedIdx - maxVisible/2
		}
		if startIdx+maxVisible > len(sp.semanticNames) {
			startIdx = len(sp.semanticNames) - maxVisible
		}
		if startIdx < 0 {
			startIdx = 0
		}

		relY := y - rect.Y
		clickedIdx := startIdx + relY
		if clickedIdx >= 0 && clickedIdx < len(sp.semanticNames) {
			sp.selectedIdx = clickedIdx
			return true
		}
	}

	// Handle scroll wheel
	if ev.Buttons()&tcell.WheelUp != 0 {
		if sp.selectedIdx > 0 {
			sp.selectedIdx--
		}
		return true
	}
	if ev.Buttons()&tcell.WheelDown != 0 {
		if sp.selectedIdx < len(sp.semanticNames)-1 {
			sp.selectedIdx++
		}
		return true
	}

	return false
}

func (sp *SemanticPicker) GetResult() PickerResult {
	tm := theme.Get()
	name := sp.semanticNames[sp.selectedIdx]
	color := tm.GetSemanticColor(name)
	return MakeResult(color, name)
}

func (sp *SemanticPicker) PreferredSize() (int, int) {
	// Width: "[██] border.inactive" = ~25 chars
	// Height: show ~10 items
	return 28, 10
}

func (sp *SemanticPicker) SetColor(color tcell.Color) {
	// Try to find matching semantic color
	tm := theme.Get()
	for i, name := range sp.semanticNames {
		if tm.GetSemanticColor(name) == color {
			sp.selectedIdx = i
			return
		}
	}
}

// ResetFocus is a no-op for semantic picker (single focus area).
func (sp *SemanticPicker) ResetFocus() {
	// No internal tab stops to reset
}
