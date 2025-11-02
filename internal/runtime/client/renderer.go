// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/renderer.go
// Summary: Rendering pipeline for client runtime.
// Usage: Composites pane buffers, applies effects, and renders to tcell screen.

package clientruntime

import (
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

func render(state *uiState, screen tcell.Screen) {
	width, height := screen.Size()
	screen.SetStyle(state.defaultStyle)
	screen.Clear()

	if state.effects != nil {
		state.effects.Update(time.Now())
	}

	workspaceBuffer := make([][]client.Cell, height)
	for y := 0; y < height; y++ {
		row := make([]client.Cell, width)
		for x := range row {
			row[x] = client.Cell{Ch: ' ', Style: state.defaultStyle}
		}
		workspaceBuffer[y] = row
	}

	panes := state.cache.SortedPanes()
	for _, pane := range panes {
		if pane == nil {
			continue
		}
		x := pane.Rect.X
		y := pane.Rect.Y
		w := pane.Rect.Width
		h := pane.Rect.Height
		if w <= 0 || h <= 0 {
			continue
		}

		paneBuffer := make([][]client.Cell, h)
		for rowIdx := 0; rowIdx < h; rowIdx++ {
			row := make([]client.Cell, w)
			source := pane.RowCells(rowIdx)
			for col := 0; col < w; col++ {
				cell := client.Cell{Ch: ' ', Style: state.defaultStyle}
				if source != nil && col < len(source) {
					cell = source[col]
					if cell.Ch == 0 {
						cell.Ch = ' '
					}
					if cell.Style == (tcell.Style{}) {
						cell.Style = state.defaultStyle
					}
				}
				row[col] = cell
			}
			paneBuffer[rowIdx] = row
		}

		if state.effects != nil {
			state.effects.ApplyPaneEffects(pane, paneBuffer)
		}

		zoomOverlay := state.zoomed && pane.ID == state.zoomedPane
		for rowIdx := 0; rowIdx < h; rowIdx++ {
			targetY := y + rowIdx
			if targetY < 0 || targetY >= height {
				continue
			}
			row := paneBuffer[rowIdx]
			for col := 0; col < w; col++ {
				targetX := x + col
				if targetX < 0 || targetX >= width {
					continue
				}
				cell := row[col]
				style := cell.Style
				if zoomOverlay {
					style = applyZoomOverlay(style, 0.2, state)
				}
				workspaceBuffer[targetY][targetX] = client.Cell{Ch: cell.Ch, Style: style}
			}
		}
	}

	if state.effects != nil {
		state.effects.ApplyWorkspaceEffects(workspaceBuffer)
	}

	if pane, minX, maxX, minY, maxY, ok := state.selectionBounds(); ok {
		applySelectionHighlight(state, workspaceBuffer, pane, minX, maxX, minY, maxY)
	}

	for y, row := range workspaceBuffer {
		for x, cell := range row {
			ch := cell.Ch
			if ch == 0 {
				ch = ' '
			}
			style := cell.Style
			if style == (tcell.Style{}) {
				style = state.defaultStyle
			}
			screen.SetContent(x, y, ch, nil, style)
		}
	}

	screen.Show()
}

func applyZoomOverlay(style tcell.Style, intensity float32, state *uiState) tcell.Style {
	if intensity <= 0 {
		return style
	}
	fg, bg, attrs := style.Decompose()
	if !fg.Valid() {
		fg = state.defaultFg
		if !fg.Valid() {
			fg = tcell.ColorWhite
		}
	}
	if !bg.Valid() {
		bg = state.defaultBg
		if !bg.Valid() {
			bg = state.desktopBg
			if !bg.Valid() {
				bg = tcell.ColorBlack
			}
		}
	}
	outline := tcell.NewRGBColor(120, 200, 255)
	blendedFg := blendColor(fg, outline, intensity/2)
	blendedBg := blendColor(bg, outline, intensity/1.5)
	return tcell.StyleDefault.Foreground(blendedFg).
		Background(blendedBg).
		Bold(true).
		Underline(attrs&tcell.AttrUnderline != 0).
		Reverse(attrs&tcell.AttrReverse != 0).
		Blink(attrs&tcell.AttrBlink != 0).
		Dim(attrs&tcell.AttrDim != 0).
		Italic(attrs&tcell.AttrItalic != 0)
}

func blendColor(base, overlay tcell.Color, intensity float32) tcell.Color {
	if !overlay.Valid() || intensity <= 0 {
		return base
	}
	if !base.Valid() {
		return overlay
	}
	br, bg, bb := base.RGB()
	or, og, ob := overlay.RGB()
	blend := func(bc, oc int32) int32 {
		return int32(float32(bc)*(1-intensity) + float32(oc)*intensity)
	}
	return tcell.NewRGBColor(blend(br, or), blend(bg, og), blend(bb, ob))
}

func applySelectionHighlight(state *uiState, buffer [][]client.Cell, pane *client.PaneState, minX, maxX, minY, maxY int) {
	if len(buffer) == 0 {
		return
	}
	bgColor := state.selectionBg
	if !bgColor.Valid() {
		bgColor = tcell.NewRGBColor(232, 217, 255)
	}
	fgColor := state.selectionFg
	if !fgColor.Valid() {
		fgColor = tcell.ColorBlack
	}
	paneX0, paneY0, paneX1, paneY1 := 0, 0, 0, 0
	if pane != nil {
		paneX0 = pane.Rect.X
		paneY0 = pane.Rect.Y
		paneX1 = pane.Rect.X + pane.Rect.Width
		paneY1 = pane.Rect.Y + pane.Rect.Height
	}
	for y := minY; y < maxY; y++ {
		if y < 0 || y >= len(buffer) {
			continue
		}
		if pane != nil && (y < paneY0 || y >= paneY1) {
			continue
		}
		row := buffer[y]
		for x := minX; x < maxX; x++ {
			if x < 0 || x >= len(row) {
				continue
			}
			if pane != nil && (x < paneX0 || x >= paneX1) {
				continue
			}
			cell := row[x]
			style := cell.Style
			if style == (tcell.Style{}) {
				style = state.defaultStyle
			}
			style = style.Background(bgColor).Foreground(fgColor)
			row[x] = client.Cell{Ch: cell.Ch, Style: style}
		}
	}
}
