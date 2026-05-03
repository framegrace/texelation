// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker_ascii.go
// Summary: Pure-function tree-snapshot to box-drawing characters.
// Used as the fallback render path for thumbnails on terminals
// without graphics support, and as the placeholder while a Kitty
// thumbnail fetch is in flight.

package boot

import "github.com/framegrace/texelation/protocol"

const (
	asciiMinW = 8
	asciiMinH = 4
)

// renderASCIILayoutGrid returns a (h × w) rune matrix containing the
// box-drawing representation of root. Sub-minimum-size rects collapse
// to a single bordered box; nil roots also fall back to a single box
// so callers don't have to special-case missing layouts.
func renderASCIILayoutGrid(w, h int, root *protocol.TreeNodeSnapshot) [][]rune {
	grid := makeBlankGrid(w, h)
	if root == nil || w < asciiMinW || h < asciiMinH {
		drawSingleBox(grid, 0, 0, w, h)
		return grid
	}
	drawNode(grid, 0, 0, w, h, root)
	return grid
}

func makeBlankGrid(w, h int) [][]rune {
	grid := make([][]rune, h)
	for y := 0; y < h; y++ {
		grid[y] = make([]rune, w)
		for x := 0; x < w; x++ {
			grid[y][x] = ' '
		}
	}
	return grid
}

func drawNode(grid [][]rune, x, y, w, h int, n *protocol.TreeNodeSnapshot) {
	if n.Split == protocol.SplitNone || len(n.Children) < 2 || w < asciiMinW || h < asciiMinH {
		drawSingleBox(grid, x, y, w, h)
		return
	}
	ratios := normaliseRatios(n.SplitRatios, len(n.Children))
	if n.Split == protocol.SplitHorizontal {
		// Walk children in order, allocating rows per ratio. Each
		// child shares its bottom border with the next child's top
		// border (the -1 / +1 dance), so the visual divider sits on
		// a single row and shows '─' from each side's drawSingleBox.
		cursorY := y
		remaining := h
		for i, child := range n.Children {
			var sliceH int
			if i == len(n.Children)-1 {
				sliceH = remaining
			} else {
				sliceH = int(float32(h) * ratios[i])
				if sliceH < asciiMinH {
					sliceH = asciiMinH
				}
				if sliceH > remaining-asciiMinH*(len(n.Children)-i-1) {
					sliceH = remaining - asciiMinH*(len(n.Children)-i-1)
				}
			}
			drawNode(grid, x, cursorY, w, sliceH, &child)
			cursorY += sliceH - 1
			remaining -= sliceH - 1
		}
		return
	}
	// Vertical split: walk children left-to-right.
	cursorX := x
	remaining := w
	for i, child := range n.Children {
		var sliceW int
		if i == len(n.Children)-1 {
			sliceW = remaining
		} else {
			sliceW = int(float32(w) * ratios[i])
			if sliceW < asciiMinW {
				sliceW = asciiMinW
			}
			if sliceW > remaining-asciiMinW*(len(n.Children)-i-1) {
				sliceW = remaining - asciiMinW*(len(n.Children)-i-1)
			}
		}
		drawNode(grid, cursorX, y, sliceW, h, &child)
		cursorX += sliceW - 1
		remaining -= sliceW - 1
	}
}

// normaliseRatios returns a slice of len(childCount) ratios summing to
// ~1.0. If the input is missing/short or doesn't sum, it falls back to
// equal allocation so n-way splits never produce zero-width children.
func normaliseRatios(ratios []float32, count int) []float32 {
	out := make([]float32, count)
	if len(ratios) >= count {
		var sum float32
		for i := 0; i < count; i++ {
			out[i] = ratios[i]
			sum += ratios[i]
		}
		if sum > 0 {
			for i := range out {
				out[i] /= sum
			}
			return out
		}
	}
	for i := range out {
		out[i] = 1.0 / float32(count)
	}
	return out
}

func drawSingleBox(grid [][]rune, x, y, w, h int) {
	if w < 2 || h < 2 {
		return
	}
	maxY := y + h - 1
	maxX := x + w - 1
	for cy := y; cy <= maxY; cy++ {
		for cx := x; cx <= maxX; cx++ {
			if cy < 0 || cy >= len(grid) || cx < 0 || cx >= len(grid[cy]) {
				continue
			}
			switch {
			case cy == y && cx == x:
				grid[cy][cx] = '┌'
			case cy == y && cx == maxX:
				grid[cy][cx] = '┐'
			case cy == maxY && cx == x:
				grid[cy][cx] = '└'
			case cy == maxY && cx == maxX:
				grid[cy][cx] = '┘'
			case cy == y || cy == maxY:
				if grid[cy][cx] == ' ' {
					grid[cy][cx] = '─'
				}
			case cx == x || cx == maxX:
				if grid[cy][cx] == ' ' {
					grid[cy][cx] = '│'
				}
			}
		}
	}
}
