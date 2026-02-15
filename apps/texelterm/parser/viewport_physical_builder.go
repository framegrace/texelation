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
	return &PhysicalLineBuilder{width: width}
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
// Skips lines that produce nil (synthetic lines hidden in original view).
func (b *PhysicalLineBuilder) BuildRange(lines []*LogicalLine, startGlobalIdx int64) []PhysicalLine {
	if len(lines) == 0 {
		return nil
	}

	result := make([]PhysicalLine, 0, len(lines)*2)

	for i, line := range lines {
		globalIdx := startGlobalIdx + int64(i)
		physical := b.BuildLine(line, globalIdx)
		if physical != nil {
			result = append(result, physical...)
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
