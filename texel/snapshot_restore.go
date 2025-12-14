// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/snapshot_restore.go
// Summary: Implements snapshot restore capabilities for the core desktop engine.
// Usage: Used throughout the project to implement snapshot restore inside the desktop and panes.

package texel

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
)

func (d *DesktopEngine) ApplyTreeCapture(capture TreeCapture) error {
	if len(capture.Panes) == 0 || capture.Root == nil {
		return nil
	}

	// Reset layout transition grace period to prevent animations during restore
	if d.layoutTransitions != nil {
		d.layoutTransitions.ResetGracePeriod()
	}

	if d.activeWorkspace == nil {
		// ensure at least one workspace exists
		if len(d.workspaces) == 0 {
			d.SwitchToWorkspace(1)
		}
		if d.activeWorkspace == nil {
			return fmt.Errorf("desktop has no active workspace")
		}
	}

	screen := d.activeWorkspace
	// stop existing apps before replacing the tree
	if screen.tree != nil {
		stopApps(screen.tree.Root, screen.appLifecycle)
	}

	panes := make([]*pane, len(capture.Panes))
	for i, snap := range capture.Panes {
		p := newPane(screen)
		p.setID(snap.ID)
		app := d.appFromSnapshot(snap)
		// Use PrepareAppForRestore instead of AttachApp to defer starting until after layout
		p.PrepareAppForRestore(app, screen.refreshChan)
		panes[i] = p
	}

	root, active, err := buildNodesFromCapture(screen, capture.Root, panes)
	if err != nil {
		return err
	}

	if screen.tree == nil {
		screen.tree = NewTree()
	}
	if active == nil {
		active = findFirstLeaf(root)
	}
	screen.tree.Root = root
	screen.tree.ActiveLeaf = active
	if active != nil && active.Pane != nil {
		active.Pane.SetActive(true)
	}

	// Calculate layout BEFORE starting apps so they get proper dimensions
	screen.recalculateLayout()

	// Now start all prepared apps with their correct dimensions
	for _, p := range panes {
		p.StartPreparedApp()
	}

	screen.Refresh()
	screen.notifyFocus()
	d.broadcastStateUpdate()
	return nil
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
