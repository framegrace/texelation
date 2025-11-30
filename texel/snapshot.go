// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/snapshot.go
// Summary: Implements snapshot capabilities for the core desktop engine.
// Usage: Used throughout the project to implement snapshot inside the desktop and panes.

package texel

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
	Panes []PaneSnapshot
	Root  *TreeNodeCapture
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
	if d.activeWorkspace == nil || d.activeWorkspace.tree == nil || d.activeWorkspace.tree.Root == nil {
		return capture
	}
	d.recalculateLayout()
	paneIndex := make(map[*pane]int)
	capture.Panes = make([]PaneSnapshot, 0)
	var collect func(*Node)
	collect = func(n *Node) {
		if n == nil {
			return
		}
		if len(n.Children) == 0 && n.Pane != nil && n.Pane.app != nil {
			paneSnap := capturePaneSnapshot(n.Pane)
			paneIndex[n.Pane] = len(capture.Panes)
			capture.Panes = append(capture.Panes, paneSnap)
		}
		for _, child := range n.Children {
			collect(child)
		}
	}
	collect(d.activeWorkspace.tree.Root)
	capture.Root = buildTreeCapture(d.activeWorkspace.tree.Root, paneIndex)
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
	if provider, ok := p.app.(SnapshotProvider); ok {
		appType, config := provider.SnapshotMetadata()
		snap.AppType = appType
		snap.AppConfig = cloneAppConfig(config)
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

	for _, fp := range d.floatingPanels {
		if fp == nil || fp.app == nil {
			continue
		}
		buf := fp.app.Render()
		if len(buf) == 0 || len(buf[0]) == 0 {
			continue
		}
		
		// Floating panels have explicit dimensions in the struct
		// But we should use the rendered buffer size if it matches?
		// The app was resized to fp.width/height in ShowFloatingPanel.
		
		rect := Rectangle{
			X:      fp.x,
			Y:      fp.y,
			Width:  fp.width,
			Height: fp.height,
		}
		
		// Ensure we capture what was rendered, but respecting the requested area
		cloned := cloneBuffer(buf, rect.Height, rect.Width)

		snap := PaneSnapshot{
			ID:     fp.id,
			Title:  fp.app.GetTitle(),
			Buffer: cloned,
			Rect:   rect,
			// Set a high Z-order for floating panels implicitly by being last?
			// PaneSnapshot doesn't store ZOrder. The protocol handles it by order or we need to add it?
			// The current client renderer likely draws in order of the array. 
			// So appending floating panels LAST ensures they are on top.
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
