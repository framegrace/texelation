// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/snapshot_restore.go
// Summary: Implements snapshot restore capabilities for the core desktop engine.
// Usage: Used throughout the project to implement snapshot restore inside the desktop and panes.

package texel

import (
	"github.com/gdamore/tcell/v2"
)

func (d *DesktopEngine) ApplyTreeCapture(capture TreeCapture) error {
	if len(capture.Panes) == 0 {
		return nil
	}

	// Reset layout transition grace period to prevent animations during restore
	if d.layoutTransitions != nil {
		d.layoutTransitions.ResetGracePeriod()
	}

	// 1. Prepare all panes (they are flat list)
	// We need a map to look them up by index
	panes := make([]*pane, len(capture.Panes))
	
	// We need a dummy screen for creating panes initially, but we'll re-assign them to correct workspaces
	// Ensure at least one workspace exists
	if len(d.workspaces) == 0 {
		d.SwitchToWorkspace(1)
	}
	dummyScreen := d.activeWorkspace

	for i, snap := range capture.Panes {
		p := newPane(dummyScreen)
		p.setID(snap.ID)
		app := d.appFromSnapshot(snap)
		// Use PrepareAppForRestore instead of AttachApp to defer starting until after layout
		p.PrepareAppForRestore(app, dummyScreen.refreshChan)
		panes[i] = p
	}

	// 2. Restore Workspaces
	
	// If legacy capture (Root only), treat as single workspace
	if len(capture.WorkspaceRoots) == 0 && capture.Root != nil {
		capture.WorkspaceRoots = map[int]*TreeNodeCapture{1: capture.Root}
	}

	// Track which workspaces we touched
	restoredIDs := make(map[int]bool)

	for id, rootCapture := range capture.WorkspaceRoots {
		restoredIDs[id] = true
		
		// Ensure workspace exists
		if _, exists := d.workspaces[id]; !exists {
			// Manually create workspace to avoid triggering side-effects of SwitchToWorkspace
			ws, err := newWorkspace(id, d.ShellAppFactory, d.appLifecycle, d)
			if err != nil {
				continue
			}
			d.workspaces[id] = ws
		}
		
		screen := d.workspaces[id]
		
		// Stop existing apps in this workspace
		if screen.tree != nil {
			stopApps(screen.tree.Root, screen.appLifecycle)
		}

		// Rebuild tree
		root, active, err := buildNodesFromCapture(screen, rootCapture, panes)
		if err != nil {
			return err
		}

		if screen.tree == nil {
			screen.tree = NewTree()
		}
		
		// Re-assign panes to this workspace
		assignPanesToWorkspace(root, screen)
		
		if active == nil {
			active = findFirstLeaf(root)
		}
		screen.tree.Root = root
		screen.tree.ActiveLeaf = active
		if active != nil && active.Pane != nil {
			active.Pane.SetActive(true)
		}
		
		// Calculate layout for this workspace
		screen.recalculateLayout()
	}
	
	// 3. Start apps
	for _, p := range panes {
		p.StartPreparedApp()
	}
	
	// 4. Activate correct workspace
	if capture.ActiveWorkspaceID > 0 {
		if _, exists := d.workspaces[capture.ActiveWorkspaceID]; exists {
			d.activeWorkspace = d.workspaces[capture.ActiveWorkspaceID]
		}
	} else if len(restoredIDs) > 0 {
		// Fallback: pick first available
		for id := range restoredIDs {
			d.activeWorkspace = d.workspaces[id]
			break
		}
	}

	if d.activeWorkspace != nil {
		d.activeWorkspace.Refresh()
		d.activeWorkspace.notifyFocus()
	}
	d.broadcastStateUpdate()
	return nil
}

func assignPanesToWorkspace(node *Node, ws *Workspace) {
	if node == nil {
		return
	}
	if node.Pane != nil {
		node.Pane.screen = ws
		// Update refresh notifier
		if node.Pane.app != nil {
			node.Pane.app.SetRefreshNotifier(ws.refreshChan)
		}
		if node.Pane.pipeline != nil {
			node.Pane.pipeline.SetRefreshNotifier(ws.refreshChan)
		}
	}
	for _, child := range node.Children {
		assignPanesToWorkspace(child, ws)
	}
}

func stopApps(node *Node, lifecycle AppLifecycleManager) {
	if node == nil {
		return
	}
	if node.Pane != nil && node.Pane.app != nil {
		lifecycle.StopApp(node.Pane.app)
	}
	for _, child := range node.Children {
		stopApps(child, lifecycle)
	}
}

func buildNodesFromCapture(screen *Workspace, capture *TreeNodeCapture, panes []*pane) (*Node, *Node, error) {
	if capture == nil {
		return nil, nil, nil
	}
	node := &Node{}
	if len(capture.Children) == 0 {
		idx := capture.PaneIndex
		if idx < 0 || idx >= len(panes) {
			// Instead of failing, create a placeholder pane to preserve layout
			// This handles cases where a pane was excluded or index is corrupted
			p := newPane(screen)
			app := NewSnapshotApp("Error: Missing Pane", nil)
			p.PrepareAppForRestore(app, screen.refreshChan)
			node.Pane = p
			return node, node, nil
		}
		node.Pane = panes[idx]
		return node, node, nil
	}

	node.Split = capture.Split
	node.SplitRatios = make([]float64, len(capture.SplitRatios))
	copy(node.SplitRatios, capture.SplitRatios)
	node.Children = make([]*Node, len(capture.Children))
	if len(node.SplitRatios) != len(node.Children) {
		node.SplitRatios = make([]float64, len(node.Children))
		if len(node.Children) > 0 {
			equal := 1.0 / float64(len(node.Children))
			for i := range node.SplitRatios {
				node.SplitRatios[i] = equal
			}
		}
	}

	var firstLeaf *Node
	for i, childCapture := range capture.Children {
		childNode, leaf, err := buildNodesFromCapture(screen, childCapture, panes)
		if err != nil {
			return nil, nil, err
		}
		if childNode == nil {
			continue
		}
		childNode.Parent = node
		node.Children[i] = childNode
		if firstLeaf == nil && leaf != nil {
			firstLeaf = leaf
		}
		if firstLeaf == nil {
			firstLeaf = findFirstLeaf(childNode)
		}
	}
	return node, firstLeaf, nil
}

func findFirstLeaf(node *Node) *Node {
	if node == nil {
		return nil
	}
	if len(node.Children) == 0 {
		if node.Pane != nil {
			return node
		}
		return nil
	}
	for _, child := range node.Children {
		if leaf := findFirstLeaf(child); leaf != nil {
			return leaf
		}
	}
	return nil
}

func NewSnapshotApp(title string, buffer [][]Cell) App {
	rows := make([][]Cell, len(buffer))
	for i, row := range buffer {
		rows[i] = make([]Cell, len(row))
		copy(rows[i], row)
	}
	return &snapshotApp{title: title, buffer: rows}
}

type snapshotApp struct {
	title  string
	buffer [][]Cell
	notify chan<- bool
}

func (s *snapshotApp) Run() error            { return nil }
func (s *snapshotApp) Stop()                 {}
func (s *snapshotApp) Resize(cols, rows int) {}

func (s *snapshotApp) Render() [][]Cell {
	out := make([][]Cell, len(s.buffer))
	for i, row := range s.buffer {
		out[i] = make([]Cell, len(row))
		copy(out[i], row)
	}
	return out
}

func (s *snapshotApp) GetTitle() string { return s.title }

func (s *snapshotApp) HandleKey(*tcell.EventKey) {}

func (s *snapshotApp) SetRefreshNotifier(ch chan<- bool) { s.notify = ch }
