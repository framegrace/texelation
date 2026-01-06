// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/primitives/grid.go
// Summary: 2D grid widget with cell selection, keyboard/mouse navigation, and scrolling.

package primitives

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/scroll"
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
// and supports 2D keyboard navigation, Tab sequential navigation, mouse clicks,
// and automatic vertical scrolling when content overflows.
type Grid struct {
	core.BaseWidget
	Items        []GridItem
	SelectedIdx  int
	MinCellWidth int       // Minimum width per cell (used for column calculation)
	MaxCols      int       // Maximum number of columns (0 = unlimited)
	OnChange     func(int) // Called when selection changes

	// Custom rendering (optional)
	RenderCell GridCellRenderer

	// Internal state
	cols       int
	cellWidth  int
	scrollPane *scroll.ScrollPane
	content    *gridContent
	inv        func(core.Rect)
}

// gridContent is the internal widget that renders grid items.
// It's wrapped by ScrollPane for scrolling support.
type gridContent struct {
	core.BaseWidget
	parent *Grid
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

	// Create internal content widget
	g.content = &gridContent{parent: g}

	// Create scroll pane wrapping the content
	g.scrollPane = scroll.NewScrollPane()
	g.scrollPane.SetChild(g.content)

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
	g.scrollPane.SetInvalidator(fn)
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
	// Recalculate layout and update scroll pane content height
	g.calculateLayout()
	g.updateScrollPaneContentHeight()
	g.ensureSelectedVisible()
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
	g.ensureSelectedVisible()
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

// Resize updates the grid size and recalculates layout.
func (g *Grid) Resize(w, h int) {
	g.BaseWidget.Resize(w, h)
	g.scrollPane.SetPosition(g.Rect.X, g.Rect.Y)
	g.scrollPane.Resize(w, h)
	g.calculateLayout()
	// Update content size and scroll pane content height
	g.content.Resize(w, g.contentHeight())
	g.updateScrollPaneContentHeight()
}

// SetPosition updates the grid position.
func (g *Grid) SetPosition(x, y int) {
	g.BaseWidget.SetPosition(x, y)
	g.scrollPane.SetPosition(x, y)
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

// contentHeight returns the number of rows needed for all items.
func (g *Grid) contentHeight() int {
	if len(g.Items) == 0 || g.cols < 1 {
		return 0
	}
	return (len(g.Items) + g.cols - 1) / g.cols
}

// updateScrollPaneContentHeight updates the scroll pane's content height.
func (g *Grid) updateScrollPaneContentHeight() {
	g.scrollPane.SetContentHeight(g.contentHeight())
}

// ensureSelectedVisible scrolls to make the selected item visible.
func (g *Grid) ensureSelectedVisible() {
	if len(g.Items) == 0 || g.cols < 1 {
		return
	}
	selectedRow := g.SelectedIdx / g.cols
	g.scrollPane.EnsureVisible(selectedRow)
}

// Draw renders the grid via the scroll pane.
func (g *Grid) Draw(painter *core.Painter) {
	// Ensure layout is calculated
	g.calculateLayout()
	g.content.Resize(g.Rect.W, g.contentHeight())
	g.scrollPane.Draw(painter)
}

// ContentHeight implements scroll.ContentHeightProvider for gridContent.
func (gc *gridContent) ContentHeight() int {
	return gc.parent.contentHeight()
}

// HandlePageNavigation implements scroll.PageNavigator for selection-based page navigation.
// This moves the selected item by a page-worth of rows rather than just scrolling the viewport.
func (gc *gridContent) HandlePageNavigation(direction int, pageSize int) bool {
	g := gc.parent
	if len(g.Items) == 0 || g.cols < 1 {
		return false
	}

	currentRow := g.SelectedIdx / g.cols
	currentCol := g.SelectedIdx % g.cols
	totalRows := (len(g.Items) + g.cols - 1) / g.cols

	// Calculate target row
	targetRow := currentRow + (direction * pageSize)

	// Clamp to valid range
	if targetRow < 0 {
		targetRow = 0
	}
	if targetRow >= totalRows {
		targetRow = totalRows - 1
	}

	// Calculate target index (same column, different row)
	targetIdx := targetRow*g.cols + currentCol

	// Clamp to valid item index (handle partial last row)
	if targetIdx >= len(g.Items) {
		targetIdx = len(g.Items) - 1
	}
	if targetIdx < 0 {
		targetIdx = 0
	}

	// Only act if we're actually moving
	if targetIdx == g.SelectedIdx {
		return false
	}

	g.SetSelected(targetIdx)
	return true
}

// Draw renders the grid items.
func (gc *gridContent) Draw(painter *core.Painter) {
	g := gc.parent
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	if len(g.Items) == 0 {
		return
	}

	// Get scroll offset from parent's scroll pane
	scrollOffset := g.scrollPane.ScrollOffset()

	// Draw items in grid, accounting for scroll offset
	// Note: Use g.Rect (parent's rect) for screen positions since ScrollPane
	// manages clipping. gc.Rect is adjusted by ScrollPane during Draw which
	// we don't want to use here.
	for i, item := range g.Items {
		row := i / g.cols
		col := i % g.cols

		// Skip rows above viewport
		if row < scrollOffset {
			continue
		}
		// Stop if below viewport
		if row >= scrollOffset+g.Rect.H {
			break
		}

		// Calculate screen position relative to parent's viewport
		x := g.Rect.X + col*g.cellWidth
		y := g.Rect.Y + (row - scrollOffset)

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
			gc.drawDefaultCell(painter, cellRect, item, selected, baseStyle)
		}
	}
}

// drawDefaultCell renders a grid cell with default styling.
func (gc *gridContent) drawDefaultCell(painter *core.Painter, rect core.Rect, item GridItem, selected bool, baseStyle tcell.Style) {
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

	case tcell.KeyPgUp, tcell.KeyPgDn:
		// Let scroll pane handle page up/down - it will delegate to our
		// gridContent.HandlePageNavigation for selection-based navigation
		return g.scrollPane.HandleKey(ev)

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

// HandleMouse processes mouse input for cell selection and scrolling.
func (g *Grid) HandleMouse(ev *tcell.EventMouse) bool {
	if len(g.Items) == 0 {
		return false
	}

	x, y := ev.Position()
	if !g.HitTest(x, y) {
		return false
	}

	buttons := ev.Buttons()

	// Handle wheel scrolling via scroll pane
	if buttons&(tcell.WheelUp|tcell.WheelDown) != 0 {
		if g.scrollPane.HandleMouse(ev) {
			g.invalidate()
			return true
		}
		return false
	}

	if buttons != tcell.Button1 {
		return false
	}

	// Ensure layout is calculated
	if g.cols < 1 || g.cellWidth < 1 {
		g.calculateLayout()
	}

	// Calculate which cell was clicked, accounting for scroll offset
	scrollOffset := g.scrollPane.ScrollOffset()
	relX := x - g.Rect.X
	relY := y - g.Rect.Y

	clickedCol := relX / g.cellWidth
	if clickedCol >= g.cols {
		clickedCol = g.cols - 1
	}

	clickedRow := relY + scrollOffset
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
