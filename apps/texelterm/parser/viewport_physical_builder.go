// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_physical_builder.go
// Summary: PhysicalLineBuilder converts logical lines to physical lines.
//
// Architecture:
//
//	PhysicalLineBuilder converts LogicalLines into displayable PhysicalLines
//	at a given width. It leverages LogicalLine.ActiveWrapToWidth() which
//	handles dual-layer rendering (original cells vs overlay content) and
//	supports synthetic line visibility.

package parser

// PhysicalLineBuilder converts logical lines to physical lines for display.
type PhysicalLineBuilder struct {
	width       int
	showOverlay bool
}

// NewPhysicalLineBuilder creates a builder with the specified display width.
func NewPhysicalLineBuilder(width int) *PhysicalLineBuilder {
	if width <= 0 {
		width = DefaultWidth
	}
	return &PhysicalLineBuilder{width: width, showOverlay: true}
}

// SetShowOverlay sets whether to render overlay content when available.
func (b *PhysicalLineBuilder) SetShowOverlay(show bool) {
	b.showOverlay = show
}

// ShowOverlay returns whether overlay content is rendered.
func (b *PhysicalLineBuilder) ShowOverlay() bool {
	return b.showOverlay
}

// BuildLine converts a single logical line to physical lines.
// Returns nil for synthetic lines when showOverlay is false (hidden in original view).
func (b *PhysicalLineBuilder) BuildLine(line *LogicalLine, globalIdx int64) []PhysicalLine {
	if line == nil {
		return []PhysicalLine{{
			Cells:        make([]Cell, 0),
			LogicalIndex: int(globalIdx),
			Offset:       0,
		}}
	}

	physical := line.ActiveWrapToWidth(b.width, b.showOverlay)
	if physical == nil {
		return nil
	}

	for i := range physical {
		physical[i].LogicalIndex = int(globalIdx)
	}

	return physical
}

// BuildRange converts a range of logical lines to physical lines.
// Consecutive logical lines where the previous line's last cell has Wrapped=true
// are joined into a single long logical line before wrapping at the current width.
// This enables automatic reflow when the terminal is resized.
// Skips lines that produce nil (synthetic lines hidden in original view).
func (b *PhysicalLineBuilder) BuildRange(lines []*LogicalLine, startGlobalIdx int64) []PhysicalLine {
	if len(lines) == 0 {
		return nil
	}

	result := make([]PhysicalLine, 0, len(lines)*2)

	i := 0
	for i < len(lines) {
		line := lines[i]
		globalIdx := startGlobalIdx + int64(i)

		// Check if this line starts a wrap chain (joinable for reflow)
		if line != nil && !line.Synthetic && line.FixedWidth == 0 && line.Overlay == nil &&
			len(line.Cells) > 0 && line.Cells[len(line.Cells)-1].Wrapped {

			// Accumulate wrapped lines into one combined logical line
			combined := make([]Cell, 0, len(line.Cells)*2)
			combined = append(combined, line.Cells...)
			// Clear Wrapped flag on the copied cell (not the original)
			combined[len(combined)-1].Wrapped = false

			j := i + 1
			for j < len(lines) {
				next := lines[j]
				if next == nil || next.Synthetic || next.FixedWidth > 0 || next.Overlay != nil {
					break
				}
				combined = append(combined, next.Cells...)
				if len(next.Cells) > 0 && next.Cells[len(next.Cells)-1].Wrapped {
					// Clear Wrapped flag on copied cell and continue chain
					combined[len(combined)-1].Wrapped = false
					j++
				} else {
					// End of wrap chain
					j++
					break
				}
			}

			// Build physical lines from the combined content
			combinedLine := &LogicalLine{Cells: combined}
			physical := b.BuildLine(combinedLine, globalIdx)
			if physical != nil {
				result = append(result, physical...)
			}
			i = j
		} else {
			// Normal line (or nil/synthetic/fixed) - build directly
			physical := b.BuildLine(line, globalIdx)
			if physical != nil {
				result = append(result, physical...)
			}
			i++
		}
	}

	return result
}

// SetWidth updates the display width for future builds.
func (b *PhysicalLineBuilder) SetWidth(width int) {
	if width <= 0 {
		width = DefaultWidth
	}
	b.width = width
}

// Width returns the current display width.
func (b *PhysicalLineBuilder) Width() int {
	return b.width
}
