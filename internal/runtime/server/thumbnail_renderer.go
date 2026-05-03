// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/thumbnail_renderer.go
// Summary: Composes per-pane buffer snapshots into a single workspace
// cell grid for thumbnail rendering. The composer is the server-only
// glue between the publisher's authoritative state and the shared
// internal/thumbnail render primitive.

package server

import (
	"image"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/internal/thumbnail"
	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

// paneRender is the per-pane input to composePaneGrid. Coordinates are
// workspace-relative; rows[y][x] is a cell.
type paneRender struct {
	x, y int
	w, h int
	rows [][]texelcore.Cell
}

// composePaneGrid paints pane buffers onto a (workspaceW × workspaceH)
// cell grid. Out-of-bounds cells are dropped silently. The grid is
// initialised with zero-value Cells (Ch=0, default style) which the
// renderer treats as blanks.
func composePaneGrid(workspaceW, workspaceH int, panes []paneRender) [][]texelcore.Cell {
	grid := make([][]texelcore.Cell, workspaceH)
	for y := 0; y < workspaceH; y++ {
		grid[y] = make([]texelcore.Cell, workspaceW)
	}
	for _, p := range panes {
		for ry, row := range p.rows {
			absY := p.y + ry
			if absY < 0 || absY >= workspaceH {
				continue
			}
			for rx, cell := range row {
				absX := p.x + rx
				if absX < 0 || absX >= workspaceW {
					continue
				}
				grid[absY][absX] = cell
			}
		}
	}
	return grid
}

// workspaceBounds derives (w, h) from the union of pane rects in a
// geometry snapshot. We bound-box rather than expose a separate
// ViewportSize getter on the engine — the geometry snapshot already
// has the data, and a separate getter would be a wider engine change
// than F.1 needs.
func workspaceBounds(snap protocol.TreeSnapshot) (int, int) {
	maxX, maxY := 0, 0
	for _, p := range snap.Panes {
		if right := int(p.X) + int(p.Width); right > maxX {
			maxX = right
		}
		if bottom := int(p.Y) + int(p.Height); bottom > maxY {
			maxY = bottom
		}
	}
	return maxX, maxY
}

// RenderSessionThumbnail extracts pane buffers from the publisher
// (which already maintains them for diff generation) and renders the
// composed grid to an image via the shared primitive. Returns
// (nil, false) when the session has no renderable content (no panes,
// empty publisher state, geometry-snapshot failure).
//
// Implements the ThumbnailRenderer interface declared in thumbnail.go.
func (s *DesktopSink) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	if s == nil {
		return nil, false
	}
	pub := s.Publisher()
	desktop := s.Desktop()
	if pub == nil || desktop == nil {
		return nil, false
	}
	geom, err := s.GeometrySnapshot()
	if err != nil {
		return nil, false
	}
	workspaceW, workspaceH := workspaceBounds(geom)
	if workspaceW <= 0 || workspaceH <= 0 {
		return nil, false
	}
	panes := make([]paneRender, 0, len(geom.Panes))
	for _, p := range geom.Panes {
		buf := pub.PrevBufferFor(p.PaneID)
		if len(buf) == 0 {
			continue
		}
		// texel.Cell == texelcore.Cell via the alias in
		// texel/core_aliases.go, so the slice types are identical.
		coreRows := make([][]texelcore.Cell, len(buf))
		for y, row := range buf {
			coreRows[y] = ([]texelcore.Cell)(row)
			_ = texel.Cell{} // keep texel import live; alias is referenced
		}
		panes = append(panes, paneRender{
			x:    int(p.X),
			y:    int(p.Y),
			w:    int(p.Width),
			h:    int(p.Height),
			rows: coreRows,
		})
	}
	if len(panes) == 0 {
		return nil, false
	}
	grid := composePaneGrid(workspaceW, workspaceH, panes)
	img, err := thumbnail.RenderGrid(grid)
	if err != nil {
		return nil, false
	}
	return img, true
}
