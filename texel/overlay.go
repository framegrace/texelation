// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/overlay.go
// Summary: Utility for compositing buffers (used by apps for overlays).
// Usage: Apps can use this to composite tview or other overlays on their buffers.
// Notes: Apps manage their own overlays; this is just a helper function.

package texel

// CompositeBuffers overlays the top buffer onto the base buffer.
// Cells with rune(0) in the top buffer are treated as transparent.
// This is a utility function that apps can use internally.
func CompositeBuffers(base, overlay [][]Cell) [][]Cell {
	if len(overlay) == 0 || len(overlay[0]) == 0 {
		return base
	}

	// Create result buffer with base dimensions
	height := len(base)
	if height == 0 {
		return base
	}
	width := len(base[0])

	result := make([][]Cell, height)
	for y := 0; y < height; y++ {
		result[y] = make([]Cell, width)
		copy(result[y], base[y])
	}

	// Overlay the top buffer
	overlayHeight := len(overlay)
	for y := 0; y < overlayHeight && y < height; y++ {
		overlayWidth := len(overlay[y])
		for x := 0; x < overlayWidth && x < width; x++ {
			// Skip transparent cells (rune 0 = transparent)
			if overlay[y][x].Ch != rune(0) {
				result[y][x] = overlay[y][x]
			}
		}
	}

	return result
}
