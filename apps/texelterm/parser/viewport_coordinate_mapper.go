// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_coordinate_mapper.go
// Summary: CoordinateMapper converts between viewport and content coordinates.
//
// Architecture:
//
//	CoordinateMapper handles the translation between two coordinate systems:
//
//	1. Viewport coordinates (row, col):
//	   - row: 0-based row within the visible viewport (0 = top)
//	   - col: 0-based column within the row
//
//	2. Content coordinates (globalLineIdx, charOffset):
//	   - globalLineIdx: MemoryBuffer's global line index
//	   - charOffset: character position within the logical line
//
//	This conversion is essential for:
//	  - Mouse click handling (viewport -> content)
//	  - Cursor positioning (content -> viewport)
//	  - Selection rendering
//	  - Search result highlighting

package parser

// CoordinateMapper converts between viewport and content coordinate systems.
// Thread-safety must be managed by the caller (ViewportWindow).
type CoordinateMapper struct {
	reader  ContentReader
	builder *PhysicalLineBuilder
	scroll  *ScrollManager
}

// NewCoordinateMapper creates a new coordinate mapper with the given dependencies.
func NewCoordinateMapper(reader ContentReader, builder *PhysicalLineBuilder, scroll *ScrollManager) *CoordinateMapper {
	return &CoordinateMapper{
		reader:  reader,
		builder: builder,
		scroll:  scroll,
	}
}

// ViewportToContent converts viewport coordinates (row, col) to content coordinates.
// Returns (globalLineIdx, charOffset, ok).
// ok is false if coordinates are out of bounds.
func (cm *CoordinateMapper) ViewportToContent(viewportRow, col, viewportHeight int) (int64, int, bool) {
	if viewportRow < 0 || col < 0 || viewportHeight <= 0 {
		return 0, 0, false
	}

	// Get the visible range of logical lines
	startGlobal, endGlobal := cm.scroll.VisibleRange(viewportHeight)

	// Build physical lines for this range
	lines := cm.reader.GetLineRange(startGlobal, endGlobal)
	physical := cm.builder.BuildRange(lines, startGlobal)

	// Calculate which physical line corresponds to the viewport row
	// We need to account for scroll offset within the physical lines
	totalPhysical := int64(len(physical))
	physicalEnd := totalPhysical
	physicalStart := max(physicalEnd-int64(viewportHeight), 0)

	// The viewport row maps to physical[physicalStart + viewportRow]
	physIdx := int(physicalStart) + viewportRow
	if physIdx < 0 || physIdx >= len(physical) {
		return 0, 0, false
	}

	pl := physical[physIdx]
	charOffset := pl.Offset + col

	// Clamp charOffset to actual line length
	line := cm.reader.GetLine(int64(pl.LogicalIndex))
	if line != nil && charOffset > len(line.Cells) {
		charOffset = len(line.Cells)
	}

	return int64(pl.LogicalIndex), charOffset, true
}

// ContentToViewport converts content coordinates to viewport coordinates.
// Returns (row, col, visible).
// visible is false if the content is not currently on screen.
func (cm *CoordinateMapper) ContentToViewport(globalLineIdx int64, charOffset, viewportHeight int) (int, int, bool) {
	if viewportHeight <= 0 {
		return 0, 0, false
	}

	// Get the visible range of logical lines
	startGlobal, endGlobal := cm.scroll.VisibleRange(viewportHeight)

	// Check if the line is in the visible range
	if globalLineIdx < startGlobal || globalLineIdx >= endGlobal {
		return 0, 0, false
	}

	// Build physical lines for this range
	lines := cm.reader.GetLineRange(startGlobal, endGlobal)
	physical := cm.builder.BuildRange(lines, startGlobal)

	// Calculate the physical line window we're showing
	totalPhysical := int64(len(physical))
	physicalEnd := totalPhysical
	physicalStart := max(physicalEnd-int64(viewportHeight), 0)

	width := cm.builder.Width()

	// Find the physical line that contains our character offset
	for i := int(physicalStart); i < len(physical) && i < int(physicalEnd); i++ {
		pl := physical[i]
		if int64(pl.LogicalIndex) == globalLineIdx {
			// Check if charOffset falls within this physical line
			if charOffset >= pl.Offset && charOffset < pl.Offset+width {
				viewportRow := i - int(physicalStart)
				col := charOffset - pl.Offset
				return viewportRow, col, true
			}
			// Check for end of line (charOffset at last position)
			if charOffset == pl.Offset+len(pl.Cells) && len(pl.Cells) < width {
				viewportRow := i - int(physicalStart)
				col := len(pl.Cells)
				return viewportRow, col, true
			}
		}
	}

	return 0, 0, false
}

// PhysicalLineAt returns metadata about the physical line at the given viewport row.
// Returns (globalLineIdx, charOffset, ok).
// This is useful for determining which logical line a row belongs to.
func (cm *CoordinateMapper) PhysicalLineAt(viewportRow, viewportHeight int) (globalLineIdx int64, charOffset int, ok bool) {
	return cm.ViewportToContent(viewportRow, 0, viewportHeight)
}
