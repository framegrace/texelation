// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_physical_builder.go
// Summary: PhysicalLineBuilder converts logical lines to physical lines.
//
// Architecture:
//
//	PhysicalLineBuilder is a stateless service that converts LogicalLines
//	into displayable PhysicalLines at a given width. It leverages the
//	existing LogicalLine.WrapToWidth() method which already handles:
//
//	  - Normal line wrapping
//	  - Fixed-width lines (ClipOrPadToWidth)
//	  - Empty lines
//
//	This component provides a clean interface for the ViewportCache
//	and ViewportWindow to build physical line representations.

package parser

// PhysicalLineBuilder converts logical lines to physical lines for display.
// This is a stateless service - all methods are pure functions.
type PhysicalLineBuilder struct {
	width int
}

// NewPhysicalLineBuilder creates a builder with the specified display width.
func NewPhysicalLineBuilder(width int) *PhysicalLineBuilder {
	if width <= 0 {
		width = DefaultWidth
	}
	return &PhysicalLineBuilder{width: width}
}

// BuildLine converts a single logical line to one or more physical lines.
// Returns at least one line (nil/empty logical lines produce one empty physical line).
// The globalIdx is stored in each PhysicalLine.LogicalIndex for coordinate conversion.
func (b *PhysicalLineBuilder) BuildLine(line *LogicalLine, globalIdx int64) []PhysicalLine {
	if line == nil {
		// Nil line produces one empty physical line
		return []PhysicalLine{{
			Cells:        make([]Cell, 0),
			LogicalIndex: int(globalIdx),
			Offset:       0,
		}}
	}

	// Delegate to LogicalLine's existing wrapping logic
	// This handles normal wrapping and fixed-width (ClipOrPadToWidth) automatically
	physical := line.WrapToWidth(b.width)

	// Set LogicalIndex on each physical line for coordinate conversion
	for i := range physical {
		physical[i].LogicalIndex = int(globalIdx)
	}

	return physical
}

// BuildRange converts a range of logical lines to physical lines.
// Returns a flat list of all physical lines in order.
// Lines in the slice are expected to start at startGlobalIdx and be contiguous.
func (b *PhysicalLineBuilder) BuildRange(lines []*LogicalLine, startGlobalIdx int64) []PhysicalLine {
	if len(lines) == 0 {
		return nil
	}

	// Estimate capacity: assume average 2 physical lines per logical line
	result := make([]PhysicalLine, 0, len(lines)*2)

	for i, line := range lines {
		globalIdx := startGlobalIdx + int64(i)
		physical := b.BuildLine(line, globalIdx)
		result = append(result, physical...)
	}

	return result
}

// SetWidth updates the display width for future builds.
// This triggers reflow on next build.
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
