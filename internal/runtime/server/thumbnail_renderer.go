// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/thumbnail_renderer.go
// Summary: Composes per-pane buffer snapshots into a single workspace
// cell grid for thumbnail rendering. The composer is the server-only
// glue between the desktop's authoritative pane state and the shared
// internal/thumbnail render primitive.

package server

import (
	"image"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/internal/thumbnail"
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

// RenderSessionThumbnail renders the desktop's current pane buffers to
// an image via the shared primitive. Returns (nil, false) when the
// desktop has no renderable content.
//
// We pull from desktop.SnapshotBuffers (the authoritative pane state)
// rather than publisher.prevBuffers because the latter is per-publisher
// — the picker's own connection installs its own empty publisher into
// the sink, which would replace the live session's populated one and
// produce blank thumbnails. SnapshotBuffers has no such ambiguity:
// the desktop is single-instance.
//
// The id parameter is informational (used by callers to key the
// on-disk PNG file). Once F.2 lands and per-session pane state becomes
// a thing, this method may need to switch on id; for F.1 the active
// desktop is the only thing we render.
//
// Implements the ThumbnailRenderer interface declared in thumbnail.go.
func (s *DesktopSink) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	if s == nil {
		return nil, false
	}
	desktop := s.Desktop()
	if desktop == nil {
		return nil, false
	}
	snaps := desktop.SnapshotBuffers()
	if len(snaps) == 0 {
		return nil, false
	}
	maxX, maxY := 0, 0
	panes := make([]paneRender, 0, len(snaps))
	for _, ps := range snaps {
		if len(ps.Buffer) == 0 {
			continue
		}
		// texel.Cell == texelcore.Cell via the alias in
		// texel/core_aliases.go, so the slice types are identical.
		coreRows := make([][]texelcore.Cell, len(ps.Buffer))
		for y, row := range ps.Buffer {
			coreRows[y] = ([]texelcore.Cell)(row)
		}
		_ = texel.Cell{} // keep texel import live; alias is referenced
		px, py := ps.Rect.X, ps.Rect.Y
		pw, ph := ps.Rect.Width, ps.Rect.Height
		panes = append(panes, paneRender{x: px, y: py, w: pw, h: ph, rows: coreRows})
		if right := px + pw; right > maxX {
			maxX = right
		}
		if bottom := py + ph; bottom > maxY {
			maxY = bottom
		}
	}
	if len(panes) == 0 || maxX <= 0 || maxY <= 0 {
		return nil, false
	}
	grid := composePaneGrid(maxX, maxY, panes)
	img, err := thumbnail.RenderGrid(grid)
	if err != nil {
		return nil, false
	}
	return img, true
}
