// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/primitives/grid.go
// Summary: 2D grid widget with cell selection and keyboard/mouse navigation.

package primitives

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// GridItem represents a single cell in a Grid.
type GridItem struct {
	Text  string
	Value interface{} // Optional data payload
}

// GridCellRenderer is a custom rendering function for grid cells.
// If nil, the default rendering is used (text with selection highlight).
type GridCellRenderer func(p *core.Painter, rect core.Rect, item GridItem, selected bool)

// Grid is a 2D grid widget with cell selection and navigation.
// It dynamically calculates the number of columns based on available width
// and supports 2D keyboard navigation, Tab sequential navigation, and mouse clicks.
type Grid struct {
	core.BaseWidget
	Items        []GridItem
	SelectedIdx  int
	MinCellWidth int // Minimum width per cell (used for column calculation)
	MaxCols      int // Maximum number of columns (0 = unlimited)
	OnChange     func(int) // Called when selection changes

	// Custom rendering (optional)
	RenderCell GridCellRenderer

	// Internal state (computed during Draw)
	cols      int
	cellWidth int
	inv       func(core.Rect)
}

// NewGrid creates a new grid at the specified position and size.
func NewGrid(x, y, w, h int) *Grid {
	g := &Grid{
		Items:        []GridItem{},
		SelectedIdx:  0,
		MinCellWidth: 10,
		MaxCols:      0, // Unlimited
		cols:         1,
	}

	g.SetPosition(x, y)
	g.Resize(w, h)
	g.SetFocusable(true)

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	g.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	return g
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (g *Grid) SetInvalidator(fn func(core.Rect)) {
	g.inv = fn
}

// SetItems replaces the grid items.
func (g *Grid) SetItems(items []GridItem) {
	g.Items = items
	// Clamp selection to valid range
	if g.SelectedIdx >= len(items) {
		g.SelectedIdx = len(items) - 1
	}
	if g.SelectedIdx < 0 {
		g.SelectedIdx = 0
	}
	g.invalidate()
}

// SetSelected changes the selected cell by index.
func (g *Grid) SetSelected(idx int) {
	if idx < 0 || idx >= len(g.Items) {
		return
	}
	if idx == g.SelectedIdx {
		return
	}
	g.SelectedIdx = idx
	g.invalidate()
	if g.OnChange != nil {
		g.OnChange(idx)
	}
}

// SelectedItem returns the currently selected item, or nil if none.
func (g *Grid) SelectedItem() *GridItem {
	if g.SelectedIdx >= 0 && g.SelectedIdx < len(g.Items) {
		return &g.Items[g.SelectedIdx]
	}
	return nil
}

// Columns returns the calculated number of columns.
func (g *Grid) Columns() int {
	return g.cols
}

// calculateLayout computes grid layout based on available width.
func (g *Grid) calculateLayout() {
	if g.Rect.W <= 0 || len(g.Items) == 0 {
		g.cols = 1
		g.cellWidth = g.Rect.W
		return
	}

	// Find longest item text
	maxTextLen := 0
	for _, item := range g.Items {
		if len(item.Text) > maxTextLen {
			maxTextLen = len(item.Text)
		}
	}

	// Calculate cell width (at least MinCellWidth, at least longest text + padding)
	cellWidth := g.MinCellWidth
	if maxTextLen+2 > cellWidth { // +2 for minimal padding
		cellWidth = maxTextLen + 2
	}

	// Calculate number of columns
	cols := g.Rect.W / cellWidth
	if cols < 1 {
		cols = 1
	}
	if g.MaxCols > 0 && cols > g.MaxCols {
		cols = g.MaxCols
	}

	// Recalculate cell width to use available space evenly
	cellWidth = g.Rect.W / cols

	g.cols = cols
	g.cellWidth = cellWidth
}

// Draw renders the grid.
func (g *Grid) Draw(painter *core.Painter) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	// Fill background
	painter.Fill(g.Rect, ' ', baseStyle)

	if len(g.Items) == 0 {
		return
	}

	// Calculate layout
	g.calculateLayout()

	// Draw items in grid
	col := 0
	x := g.Rect.X
	y := g.Rect.Y

	for i, item := range g.Items {
		if y >= g.Rect.Y+g.Rect.H {
			break // No more rows available
		}

		selected := i == g.SelectedIdx

		cellRect := core.Rect{
			X: x,
			Y: y,
			W: g.cellWidth,
			H: 1,
		}

		if g.RenderCell != nil {
			// Custom rendering
			g.RenderCell(painter, cellRect, item, selected)
		} else {
			// Default rendering
			g.drawDefaultCell(painter, cellRect, item, selected, baseStyle)
		}

		col++
		if col >= g.cols {
			col = 0
			x = g.Rect.X
			y++
		} else {
			x += g.cellWidth
		}
	}
}

// drawDefaultCell renders a grid cell with default styling.
func (g *Grid) drawDefaultCell(painter *core.Painter, rect core.Rect, item GridItem, selected bool, baseStyle tcell.Style) {
	style := baseStyle
	if selected {
		style = style.Reverse(true)
	}

	// Fill cell background
	painter.Fill(rect, ' ', style)

	// Draw text (truncate if needed)
	text := item.Text
	maxLen := rect.W
	if len(text) > maxLen && maxLen > 0 {
		text = text[:maxLen]
	}

	painter.DrawText(rect.X, rect.Y, text, style)
}

// HandleKey processes keyboard input for grid navigation.
func (g *Grid) HandleKey(ev *tcell.EventKey) bool {
	if len(g.Items) == 0 {
		return false
	}

	// Ensure layout is calculated
	if g.cols < 1 {
		g.cols = 1
	}

	currentRow := g.SelectedIdx / g.cols
	currentCol := g.SelectedIdx % g.cols
	totalRows := (len(g.Items) + g.cols - 1) / g.cols

	switch ev.Key() {
	case tcell.KeyUp:
		if currentRow > 0 {
			newIdx := g.SelectedIdx - g.cols
			if newIdx >= 0 {
				g.SetSelected(newIdx)
				return true
			}
		}
		return false

	case tcell.KeyDown:
		if currentRow < totalRows-1 {
			newIdx := g.SelectedIdx + g.cols
			if newIdx >= len(g.Items) {
				// Move to last item if going down from partial last row
				newIdx = len(g.Items) - 1
			}
			if newIdx != g.SelectedIdx {
				g.SetSelected(newIdx)
				return true
			}
		}
		return false

	case tcell.KeyLeft:
		if currentCol > 0 {
			g.SetSelected(g.SelectedIdx - 1)
			return true
		}
		return false

	case tcell.KeyRight:
		if currentCol < g.cols-1 && g.SelectedIdx < len(g.Items)-1 {
			g.SetSelected(g.SelectedIdx + 1)
			return true
		}
		return false

	case tcell.KeyHome:
		if g.SelectedIdx != 0 {
			g.SetSelected(0)
			return true
		}
		return false

	case tcell.KeyEnd:
		lastIdx := len(g.Items) - 1
		if g.SelectedIdx != lastIdx {
			g.SetSelected(lastIdx)
			return true
		}
		return false

	case tcell.KeyTab:
		// Tab: left-to-right, top-to-bottom sequential navigation
		if ev.Modifiers()&tcell.ModShift != 0 {
			// Shift+Tab: go backwards
			if g.SelectedIdx > 0 {
				g.SetSelected(g.SelectedIdx - 1)
				return true
			}
		} else {
			// Tab: go forwards
			if g.SelectedIdx < len(g.Items)-1 {
				g.SetSelected(g.SelectedIdx + 1)
				return true
			}
		}
		// At boundary, let parent handle
		return false
	}

	return false
}

// HandleMouse processes mouse input for cell selection.
func (g *Grid) HandleMouse(ev *tcell.EventMouse) bool {
	if len(g.Items) == 0 {
		return false
	}

	x, y := ev.Position()
	if !g.HitTest(x, y) {
		return false
	}

	if ev.Buttons() != tcell.Button1 {
		return false
	}

	// Ensure layout is calculated
	if g.cols < 1 || g.cellWidth < 1 {
		g.calculateLayout()
	}

	// Calculate which cell was clicked
	relX := x - g.Rect.X
	relY := y - g.Rect.Y

	clickedCol := relX / g.cellWidth
	if clickedCol >= g.cols {
		clickedCol = g.cols - 1
	}

	clickedRow := relY
	clickedIdx := clickedRow*g.cols + clickedCol

	if clickedIdx >= 0 && clickedIdx < len(g.Items) {
		if clickedIdx != g.SelectedIdx {
			g.SetSelected(clickedIdx)
		}
		return true
	}

	return false
}

// invalidate marks the widget as needing redraw.
func (g *Grid) invalidate() {
	if g.inv != nil {
		g.inv(g.Rect)
	}
}
