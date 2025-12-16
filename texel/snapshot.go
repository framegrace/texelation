// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/snapshot.go
// Summary: Implements snapshot capabilities for the core desktop engine.
// Usage: Used throughout the project to implement snapshot inside the desktop and panes.

package texel

import (
	"log"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
)

// PaneSnapshot captures the render buffer for a pane along with a stable ID.
type PaneSnapshot struct {
	ID        [16]byte
	Title     string
	Buffer    [][]Cell
	Rect      Rectangle
	AppType   string
	AppConfig map[string]interface{}
}

// Rectangle stores pane position and size in screen coordinates.
type Rectangle struct {
	X      int
	Y      int
	Width  int
	Height int
}

// TreeCapture represents a snapshot of the desktop layout tree.
type TreeCapture struct {
	Panes             []PaneSnapshot
	Root              *TreeNodeCapture            // Deprecated: use WorkspaceRoots
	WorkspaceRoots    map[int]*TreeNodeCapture    // Map of workspace ID to its tree root
	ActiveWorkspaceID int
}

// TreeNodeCapture stores split metadata or references a leaf pane by index.
type TreeNodeCapture struct {
	PaneIndex   int
	Split       SplitType
	SplitRatios []float64
	Children    []*TreeNodeCapture
}

// SnapshotBuffers collects the current buffers for all panes in the active workspace.
func (d *DesktopEngine) SnapshotBuffers() []PaneSnapshot {
	capture := d.CaptureTree()
	return capture.Panes
}

// CaptureTree gathers panes and the layout tree for persistence or transport.
func (d *DesktopEngine) CaptureTree() TreeCapture {
	var capture TreeCapture
	// Default to empty if nothing to capture
	capture.WorkspaceRoots = make(map[int]*TreeNodeCapture)
	
	if len(d.workspaces) == 0 {
		return capture
	}
	
	d.recalculateLayout()
	paneIndex := make(map[*pane]int)
	capture.Panes = make([]PaneSnapshot, 0)
	
	if d.activeWorkspace != nil {
		capture.ActiveWorkspaceID = d.activeWorkspace.id
	}

	// Helper to capture a single tree
	captureWorkspace := func(ws *Workspace) *TreeNodeCapture {
		if ws == nil || ws.tree == nil || ws.tree.Root == nil {
			return nil
		}
		
		var collect func(*Node)
		collect = func(n *Node) {
			if n == nil {
				return
			}
			if len(n.Children) == 0 {
				// Leaf node - should have a pane
				if n.Pane == nil {
					log.Printf("WARNING: CaptureTree found leaf node with nil Pane - tree may be corrupted")
				} else {
					// Check if already captured (shouldn't happen in tree, but safe to check)
					if _, exists := paneIndex[n.Pane]; !exists {
						paneSnap := capturePaneSnapshot(n.Pane)
						paneIndex[n.Pane] = len(capture.Panes)
						capture.Panes = append(capture.Panes, paneSnap)
					}
				}
			}
			for _, child := range n.Children {
				collect(child)
			}
		}
		
		collect(ws.tree.Root)
		return buildTreeCapture(ws.tree.Root, paneIndex)
	}

	// Capture all workspaces
	for id, ws := range d.workspaces {
		if root := captureWorkspace(ws); root != nil {
			capture.WorkspaceRoots[id] = root
		}
	}
	
	// Maintain backward compatibility for Root field (set to active workspace)
	if d.activeWorkspace != nil {
		capture.Root = capture.WorkspaceRoots[d.activeWorkspace.id]
	}

	if status := d.captureStatusPaneSnapshots(); len(status) > 0 {
		capture.Panes = append(capture.Panes, status...)
	}
	if floating := d.captureFloatingPanelSnapshots(); len(floating) > 0 {
		capture.Panes = append(capture.Panes, floating...)
	}
	return capture
}

func capturePaneSnapshot(p *pane) PaneSnapshot {
	buf := p.renderBuffer(false)
	id := p.ID()
	snap := PaneSnapshot{
		ID:     id,
		Title:  p.getTitle(),
		Buffer: buf,
		Rect: Rectangle{
			X:      p.absX0,
			Y:      p.absY0,
			Width:  p.Width(),
			Height: p.Height(),
		},
	}
	// p.app might be nil if capturing during split before attach, or if app crashed
	if p.app != nil {
		if provider, ok := p.app.(SnapshotProvider); ok {
			appType, config := provider.SnapshotMetadata()
			snap.AppType = appType
			snap.AppConfig = cloneAppConfig(config)
		}
	} else {
		// Mark as placeholder
		snap.AppType = "placeholder"
		snap.Title = "Loading..."
	}
	return snap
}

func (d *DesktopEngine) captureStatusPaneSnapshots() []PaneSnapshot {
	if len(d.statusPanes) == 0 {
		return nil
	}
	width, height := d.viewportSize()
	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0
	snaps := make([]PaneSnapshot, 0, len(d.statusPanes))

	for _, sp := range d.statusPanes {
		if sp == nil || sp.app == nil {
			continue
		}
		buf := sp.app.Render()
		rows := len(buf)
		if rows == 0 {
			continue
		}
		maxCols := 0
		for _, row := range buf {
			if len(row) > maxCols {
				maxCols = len(row)
			}
		}
		if maxCols == 0 {
			continue
		}

		rect := Rectangle{}
		switch sp.side {
		case SideTop:
			rect.X = leftOffset
			rect.Y = topOffset
			rect.Width = maxCols
			rect.Height = rows
			topOffset += rect.Height
		case SideBottom:
			rect.X = leftOffset
			rect.Height = rows
			rect.Width = maxCols
			rect.Y = height - bottomOffset - rect.Height
			bottomOffset += rect.Height
		case SideLeft:
			rect.X = leftOffset
			rect.Y = topOffset
			rect.Width = maxCols
			rect.Height = rows
			leftOffset += rect.Width
		case SideRight:
			rect.Width = maxCols
			rect.Height = rows
			rect.X = width - rightOffset - rect.Width
			rect.Y = topOffset
			rightOffset += rect.Width
		}

		if rect.Width <= 0 || rect.Height <= 0 {
			continue
		}

		cloned := cloneBuffer(buf, rect.Height, rect.Width)
		snap := PaneSnapshot{
			ID:     sp.id,
			Title:  sp.app.GetTitle(),
			Buffer: cloned,
			Rect:   rect,
		}
		if provider, ok := sp.app.(SnapshotProvider); ok {
			appType, cfg := provider.SnapshotMetadata()
			snap.AppType = appType
			snap.AppConfig = cloneAppConfig(cfg)
		}
		snaps = append(snaps, snap)
	}
	return snaps
}

func (d *DesktopEngine) captureFloatingPanelSnapshots() []PaneSnapshot {
	if len(d.floatingPanels) == 0 {
		return nil
	}
	snaps := make([]PaneSnapshot, 0, len(d.floatingPanels))

	// Use default style for border
	tm := theme.Get()
	desktopBg := tm.GetSemanticColor("bg.base").TrueColor()
	desktopFg := tm.GetSemanticColor("text.primary").TrueColor()
	borderFg := tm.GetSemanticColor("border.active").TrueColor() // Floating panels are active-like
	defStyle := tcell.StyleDefault.Background(desktopBg).Foreground(desktopFg)
	borderStyle := tcell.StyleDefault.Background(desktopBg).Foreground(borderFg)

	for _, fp := range d.floatingPanels {
		if fp == nil || fp.app == nil {
			continue
		}
		// Render from pipeline (or app as fallback)
		var buf [][]Cell
		if fp.pipeline != nil {
			buf = fp.pipeline.Render()
		} else {
			buf = fp.app.Render()
		}
		if len(buf) == 0 || len(buf[0]) == 0 {
			continue
		}

		// Calculate dimensions with border (padding 1 on each side)
		contentW := fp.width
		contentH := fp.height
		
		// Sanity check buffer size vs requested size, clip if needed
		if len(buf) < contentH {
			contentH = len(buf)
		}
		// Assume uniform width for now
		if len(buf) > 0 && len(buf[0]) < contentW {
			contentW = len(buf[0])
		}

		borderW := contentW + 2
		borderH := contentH + 2

		// Create larger buffer
		out := make([][]Cell, borderH)
		for i := range out {
			out[i] = make([]Cell, borderW)
			for j := range out[i] {
				out[i][j] = Cell{Ch: ' ', Style: defStyle}
			}
		}

		// Draw borders
		for x := 0; x < borderW; x++ {
			out[0][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
			out[borderH-1][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
		}
		for y := 0; y < borderH; y++ {
			out[y][0] = Cell{Ch: tcell.RuneVLine, Style: borderStyle}
			out[y][borderW-1] = Cell{Ch: tcell.RuneVLine, Style: borderStyle}
		}
		out[0][0] = Cell{Ch: tcell.RuneULCorner, Style: borderStyle}
		out[0][borderW-1] = Cell{Ch: tcell.RuneURCorner, Style: borderStyle}
		out[borderH-1][0] = Cell{Ch: tcell.RuneLLCorner, Style: borderStyle}
		out[borderH-1][borderW-1] = Cell{Ch: '╯', Style: borderStyle}

		// Draw Title
		title := fp.app.GetTitle()
		if title != "" && borderW > 4 {
			titleRuneCount := 0
			for range title {
				titleRuneCount++
			}
			
			maxTitleLen := borderW - 4
			if titleRuneCount > maxTitleLen {
				// Truncate
				r := []rune(title)
				title = string(r[:maxTitleLen])
				titleRuneCount = maxTitleLen
			}
			
			// Center title
			startX := (borderW - (titleRuneCount + 2)) / 2
			if startX < 1 { startX = 1 }
			
			titleStr := " " + title + " "
			for i, ch := range titleStr {
				if startX+i < borderW-1 {
					out[0][startX+i] = Cell{Ch: ch, Style: borderStyle}
				}
			}
		}

		// Copy content into center
		for y := 0; y < contentH; y++ {
			if y >= len(buf) { break }
			row := buf[y]
			for x := 0; x < contentW; x++ {
				if x >= len(row) { break }
				out[y+1][x+1] = row[x]
			}
		}

		// Adjust rect to include border
		rect := Rectangle{
			X:      fp.x - 1,
			Y:      fp.y - 1,
			Width:  borderW,
			Height: borderH,
		}

		snap := PaneSnapshot{
			ID:     fp.id,
			Title:  fp.app.GetTitle(),
			Buffer: out,
			Rect:   rect,
		}
		if provider, ok := fp.app.(SnapshotProvider); ok {
			appType, cfg := provider.SnapshotMetadata()
			snap.AppType = appType
			snap.AppConfig = cloneAppConfig(cfg)
		}
		snaps = append(snaps, snap)
	}
	return snaps
}

func cloneBuffer(src [][]Cell, maxRows, maxCols int) [][]Cell {
	rows := maxRows
	if rows > len(src) {
		rows = len(src)
	}
	out := make([][]Cell, rows)
	for y := 0; y < rows; y++ {
		cols := maxCols
		if len(src[y]) < cols {
			cols = len(src[y])
		}
		row := make([]Cell, maxCols)
		copy(row, src[y][:cols])
		out[y] = row
	}
	return out
}

func buildTreeCapture(n *Node, paneIndex map[*pane]int) *TreeNodeCapture {
	if n == nil {
		return nil
	}
	node := &TreeNodeCapture{PaneIndex: -1}
	if len(n.Children) == 0 {
		if idx, ok := paneIndex[n.Pane]; ok {
			node.PaneIndex = idx
		} else if n.Pane != nil {
			// This means the pane exists in the tree but wasn't captured (likely app == nil)
			log.Printf("WARNING: buildTreeCapture - leaf pane '%s' not in paneIndex map, setting PaneIndex=-1 (CORRUPTED TREE)", n.Pane.getTitle())
		}
		return node
	}
	node.Split = n.Split
	node.SplitRatios = make([]float64, len(n.SplitRatios))
	copy(node.SplitRatios, n.SplitRatios)
	node.Children = make([]*TreeNodeCapture, len(n.Children))
	for i, child := range n.Children {
		node.Children[i] = buildTreeCapture(child, paneIndex)
	}
	return node
}

func cloneAppConfig(cfg map[string]interface{}) map[string]interface{} {
	if cfg == nil {
		return nil
	}
	clone := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		clone[k] = v
	}
	return clone
}
