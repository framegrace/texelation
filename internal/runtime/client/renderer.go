// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/renderer.go
// Summary: Rendering pipeline for client runtime.
// Usage: Composites pane buffers, applies effects, and renders to tcell screen.

package clientruntime

import (
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
)

func debugLogRender(msg string) {
	if f, err := os.OpenFile("/tmp/layout_anim_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05.000"), msg)
		f.Close()
	}
}

func render(state *clientState, screen tcell.Screen) {
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
			for col := 0; col < w && col < len(source); col++ {
				cell := source[col]
				if cell.Ch == 0 {
					cell.Ch = ' '
				}
				if cell.Style == (tcell.Style{}) {
					cell.Style = state.defaultStyle
				}
				row[col] = cell
			}
			// Fill any remaining cells with default
			for col := 0; col < w; col++ {
				if row[col].Ch == 0 {
					row[col] = client.Cell{Ch: ' ', Style: state.defaultStyle}
				}
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

	// Apply restart notification overlay if needed
	if state.showRestartNotification && !state.restartNotificationDismissed {
		renderRestartNotification(workspaceBuffer, width, height)
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

// renderRestartNotification overlays a centered modal notification
func renderRestartNotification(buffer [][]client.Cell, width, height int) {
	// Modal dimensions
	modalWidth := 52
	modalHeight := 9

	// Center the modal
	startX := (width - modalWidth) / 2
	startY := (height - modalHeight) / 2

	if startX < 0 || startY < 0 || width == 0 || height == 0 {
		return // Screen too small
	}

	// Apply semi-transparent backdrop (dim the entire screen)
	dimStyle := tcell.StyleDefault.Dim(true)
	for y := 0; y < len(buffer); y++ {
		for x := 0; x < len(buffer[y]); x++ {
			buffer[y][x].Style = dimStyle
		}
	}

	// Modal box styles
	boxBg := tcell.NewRGBColor(45, 45, 55)
	borderStyle := tcell.StyleDefault.
		Background(boxBg).
		Foreground(tcell.NewRGBColor(100, 100, 120))
	titleStyle := tcell.StyleDefault.
		Background(boxBg).
		Foreground(tcell.NewRGBColor(255, 200, 100)).
		Bold(true)
	textStyle := tcell.StyleDefault.
		Background(boxBg).
		Foreground(tcell.ColorWhite)
	hintStyle := tcell.StyleDefault.
		Background(boxBg).
		Foreground(tcell.NewRGBColor(150, 150, 170))

	// Modal content
	lines := []struct {
		text  string
		style tcell.Style
	}{
		{"", borderStyle},
		{"   Server Restart Notification   ", titleStyle},
		{"", textStyle},
		{" The texelation server was unresponsive ", textStyle},
		{" and has been automatically restarted.  ", textStyle},
		{"", textStyle},
		{" Your session has been restored.        ", textStyle},
		{"", textStyle},
		{"       Press any key to continue        ", hintStyle},
	}

	// Draw modal box
	for dy := 0; dy < modalHeight && startY+dy < len(buffer); dy++ {
		y := startY + dy
		if y < 0 || y >= len(buffer) {
			continue
		}

		for dx := 0; dx < modalWidth && startX+dx < len(buffer[y]); dx++ {
			x := startX + dx
			if x < 0 || x >= len(buffer[y]) {
				continue
			}

			ch := ' '
			style := textStyle

			// Border characters
			if dy == 0 {
				if dx == 0 {
					ch = '╭'
					style = borderStyle
				} else if dx == modalWidth-1 {
					ch = '╮'
					style = borderStyle
				} else {
					ch = '─'
					style = borderStyle
				}
			} else if dy == modalHeight-1 {
				if dx == 0 {
					ch = '╰'
					style = borderStyle
				} else if dx == modalWidth-1 {
					ch = '╯'
					style = borderStyle
				} else {
					ch = '─'
					style = borderStyle
				}
			} else if dx == 0 || dx == modalWidth-1 {
				ch = '│'
				style = borderStyle
			} else if dy < len(lines) {
				// Content
				contentX := dx - 1 // Adjust for left border
				if contentX >= 0 && contentX < len(lines[dy].text) {
					ch = rune(lines[dy].text[contentX])
					style = lines[dy].style
				} else {
					style = textStyle
				}
			}

			buffer[y][x] = client.Cell{Ch: ch, Style: style}
		}
	}
}

func applyZoomOverlay(style tcell.Style, intensity float32, state *clientState) tcell.Style {
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

func applySelectionHighlight(state *clientState, buffer [][]client.Cell, pane *client.PaneState, minX, maxX, minY, maxY int) {
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
