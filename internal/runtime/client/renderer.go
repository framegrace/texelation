// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/renderer.go
// Summary: Rendering pipeline for client runtime.
// Usage: Composites pane buffers, applies effects, and renders to tcell screen.

package clientruntime

import (
	"fmt"
	"image"
	"log"
	"os"
	"time"

	"github.com/framegrace/texelui/color"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/protocol"
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

	// Split panes into normal and floating (overlays).
	// Workspace effects apply only to normal panes; floating panels draw on top.
	const floatingZOrder = 100
	var normalPanes, floatingPanes []*client.PaneState
	for _, pane := range panes {
		if pane == nil {
			continue
		}
		if pane.ZOrder >= floatingZOrder {
			floatingPanes = append(floatingPanes, pane)
		} else {
			normalPanes = append(normalPanes, pane)
		}
	}

	// Pass 1: composite normal panes
	hasDynamic := compositeInto(workspaceBuffer, normalPanes, state, width, height)

	// Render images: use Kitty protocol if available, otherwise half-block fallback.
	if state.kitty != nil {
		state.kitty.prepareFrame(state.cache.ImageCache(), panes)
	} else {
		for _, pane := range panes {
			if pane == nil {
				continue
			}
			placements := state.cache.ImageCache().Placements(pane.ID)
			for _, pl := range placements {
				img := state.cache.ImageCache().Get(pl.SurfaceID)
				if img == nil || img.Decoded == nil {
					continue
				}
				renderHalfBlockIntoBuffer(workspaceBuffer, img.Decoded,
					pane.Rect.X+1+pl.X, pane.Rect.Y+1+pl.Y, pl.W, pl.H)
			}
		}
	}

	// Apply workspace effects (tint) only to the non-floating content.
	if state.effects != nil {
		state.effects.ApplyWorkspaceEffects(workspaceBuffer)
	}

	// Pass 2: composite floating panels on top (unaffected by workspace effects).
	if compositeInto(workspaceBuffer, floatingPanes, state, width, height) {
		hasDynamic = true
	}

	if pane, minX, maxX, minY, maxY, ok := state.selectionBounds(); ok {
		applySelectionHighlight(state, workspaceBuffer, pane, minX, maxX, minY, maxY)
	}

	// Apply restart notification overlay if needed
	if state.showRestartNotification && !state.restartNotificationDismissed {
		renderRestartNotification(workspaceBuffer, width, height)
	}

	showWorkspaceBuffer(screen, workspaceBuffer, state.defaultStyle)
	screen.Show()

	state.dynAnimating = hasDynamic

	// Flush Kitty graphics commands after tcell has flushed its cell buffer.
	if state.kitty != nil && state.ttyWriter != nil {
		if err := state.kitty.flush(state.ttyWriter); err != nil {
			log.Printf("kitty flush: %v", err)
		}
	}
}

// compositeInto renders a set of panes into the workspace buffer.
func compositeInto(workspaceBuffer [][]client.Cell, panes []*client.PaneState, state *clientState, screenW, screenH int) bool {
	hasDynamic := false
	animTime := float32(time.Since(state.animStart).Seconds())

	for _, pane := range panes {
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
			if targetY < 0 || targetY >= screenH {
				continue
			}
			row := paneBuffer[rowIdx]
			for col := 0; col < w; col++ {
				targetX := x + col
				if targetX < 0 || targetX >= screenW {
					continue
				}
				cell := row[col]
				style := cell.Style

				// Resolve dynamic colors client-side
				if cell.DynBG.Type >= 2 || cell.DynFG.Type >= 2 {
					ctx := color.ColorContext{
						X: col, Y: rowIdx,
						W: w, H: h,
						PX: x, PY: y,
						PW: w, PH: h,
						SX: targetX, SY: targetY,
						SW: screenW, SH: screenH,
						T: animTime,
					}
					fg, bg, attrs := style.Decompose()
					if cell.DynBG.Type >= 2 {
						bg = color.FromDesc(protocolDescToColor(cell.DynBG)).Resolve(ctx)
					}
					if cell.DynFG.Type >= 2 {
						fg = color.FromDesc(protocolDescToColor(cell.DynFG)).Resolve(ctx)
					}
					style = tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attrs)
					if protocolDescIsAnimated(cell.DynBG) || protocolDescIsAnimated(cell.DynFG) {
						hasDynamic = true
					}
				}

				if zoomOverlay {
					style = applyZoomOverlay(style, 0.2, state)
				}
				workspaceBuffer[targetY][targetX] = client.Cell{Ch: cell.Ch, Style: style}
			}
		}
	}
	return hasDynamic
}

// protocolDescIsAnimated returns true if the descriptor has time-dependent animation
// (Pulse, Fade, or a gradient with animated stops). Static gradients return false.
func protocolDescIsAnimated(d protocol.DynColorDesc) bool {
	if d.Type >= 2 && d.Type <= 3 { // Pulse or Fade
		return true
	}
	for _, s := range d.Stops {
		if s.Color.Type >= 2 && s.Color.Type <= 3 {
			return true
		}
	}
	return false
}

// protocolDescToColor converts a protocol DynColorDesc to a color.DynamicColorDesc.
func protocolDescToColor(d protocol.DynColorDesc) color.DynamicColorDesc {
	desc := color.DynamicColorDesc{
		Type:   d.Type,
		Base:   d.Base,
		Target: d.Target,
		Easing: d.Easing,
		Speed:  d.Speed,
		Min:    d.Min,
		Max:    d.Max,
	}
	if len(d.Stops) > 0 {
		desc.Stops = make([]color.GradientStopDesc, len(d.Stops))
		for i, s := range d.Stops {
			desc.Stops[i] = color.GradientStopDesc{
				Position: s.Position,
				Color: color.DynamicColorDesc{
					Type: s.Color.Type, Base: s.Color.Base, Target: s.Color.Target,
					Easing: s.Color.Easing, Speed: s.Color.Speed, Min: s.Color.Min, Max: s.Color.Max,
				},
			}
		}
	}
	return desc
}

func showWorkspaceBuffer(screen tcell.Screen, buffer [][]client.Cell, defaultStyle tcell.Style) {
	for y, row := range buffer {
		for x, cell := range row {
			ch := cell.Ch
			if ch == 0 {
				ch = ' '
			}
			style := cell.Style
			if style == (tcell.Style{}) {
				style = defaultStyle
			}
			screen.SetContent(x, y, ch, nil, style)
		}
	}
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

// renderHalfBlockIntoBuffer renders an image into the workspace buffer using Unicode half-block characters.
// Each terminal cell encodes two vertical pixels: the upper half-block (U+2580) uses foreground for the
// top pixel and background for the bottom pixel.
func renderHalfBlockIntoBuffer(buf [][]client.Cell, img image.Image, screenX, screenY, w, h int) {
	imgBounds := img.Bounds()
	imgW := imgBounds.Dx()
	imgH := imgBounds.Dy()
	if imgW == 0 || imgH == 0 || w == 0 || h == 0 {
		return
	}
	// Each cell row represents 2 vertical pixels.
	pixW := w
	pixH := h * 2

	for cy := 0; cy < h; cy++ {
		row := screenY + cy
		if row < 0 || row >= len(buf) {
			continue
		}
		for cx := 0; cx < w; cx++ {
			col := screenX + cx
			if col < 0 || col >= len(buf[row]) {
				continue
			}
			topPixY := cy * 2
			botPixY := cy*2 + 1

			topR, topG, topB := sampleImageColor(img, cx, topPixY, pixW, pixH, imgW, imgH)
			botR, botG, botB := sampleImageColor(img, cx, botPixY, pixW, pixH, imgW, imgH)

			style := tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(int32(topR), int32(topG), int32(topB))).
				Background(tcell.NewRGBColor(int32(botR), int32(botG), int32(botB)))

			buf[row][col] = client.Cell{Ch: '\u2580', Style: style}
		}
	}
}

// sampleImageColor maps a cell coordinate to the source image using nearest-neighbor sampling.
func sampleImageColor(img image.Image, cx, py, pixW, pixH, imgW, imgH int) (uint8, uint8, uint8) {
	imgX := cx * imgW / pixW
	imgY := py * imgH / pixH
	if imgX >= imgW {
		imgX = imgW - 1
	}
	if imgY >= imgH {
		imgY = imgH - 1
	}
	bounds := img.Bounds()
	r, g, b, _ := img.At(bounds.Min.X+imgX, bounds.Min.Y+imgY).RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}
