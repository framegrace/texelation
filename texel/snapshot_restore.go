// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/snapshot_restore.go
// Summary: Implements snapshot restore capabilities for the core desktop engine.
// Usage: Used throughout the project to implement snapshot restore inside the desktop and panes.

package texel

import (
	"log"

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
		// Don't activate panes here — only the active workspace's pane
		// should be active. We activate after determining which workspace
		// is active (step 5 below).

		// Calculate layout for this workspace
		screen.recalculateLayout()
	}
	
	// 3. Apply workspace metadata (name and color) from the snapshot.
	// Skip empty values to preserve defaults from newWorkspace (old snapshots
	// lack metadata entirely).
	for _, meta := range capture.WorkspaceMetadata {
		if ws, exists := d.workspaces[meta.ID]; exists {
			if meta.Name != "" {
				ws.Name = meta.Name
			}
			if meta.Color != 0 {
				r := int32((meta.Color >> 16) & 0xFF)
				g := int32((meta.Color >> 8) & 0xFF)
				b := int32(meta.Color & 0xFF)
				ws.Color = tcell.NewRGBColor(r, g, b)
			}
		}
	}

	// 4. Defer app starts until we have actual viewport dimensions
	// Apps start with wrong size if we start them now (workspace has default 80x24)
	//
	// IMPORTANT: skip status-pane orphans. captureStatusPaneSnapshots
	// includes status panes (e.g. the system status bar added by the
	// host via desktop.AddStatusPane) in the snapshot for client-side
	// buffer-replay purposes. At restore time, the host has ALREADY
	// re-added its status panes before SetEventSink → applyBootCapture
	// runs, so the snapshot's recorded status pane is redundant. It
	// got a regular *pane allocated in step 1 above but is not
	// referenced by any TreeNodeCapture (status panes live in
	// d.statusPanes, separate from the workspace tree). Adding the
	// orphan to pendingAppStarts means StartPreparedApp later runs
	// against a pane whose Rect was never set by the tree's
	// recalculateLayout — drawableWidth/Height return 0, so the app
	// gets sized 0×0, exits cleanly, and the snapshot-restore path
	// silently leaks an orphan refresh notifier and confuses the
	// renderer (the texelterm pane ends up filling the slot the real
	// statusbar should occupy, with no top/bottom borders).
	//
	// We can't filter by ID — newStatusPaneID is random at boot, so
	// the runtime status panes' IDs don't match the snapshot's
	// captured IDs across restarts. Match by Title instead: the
	// captured statusbar snapshot has Title=sp.app.GetTitle(), and
	// the runtime statusbar's app keeps the same title across boots.
	// AppType is also checked (more robust if titles ever collide
	// with workspace panes).
	statusTitles := make(map[string]bool, len(d.statusPanes))
	for _, sp := range d.statusPanes {
		if sp.app != nil {
			statusTitles[sp.app.GetTitle()] = true
		}
	}
	isStatusOrphan := func(p *pane) bool {
		if p.app == nil {
			return false
		}
		title := p.app.GetTitle()
		if statusTitles[title] {
			return true
		}
		// Belt-and-braces: the captured StatusBar app reports
		// AppType="statusbar" via SnapshotMetadata; check that too.
		if provider, ok := p.app.(SnapshotProvider); ok {
			if appType, _ := provider.SnapshotMetadata(); appType == "statusbar" {
				return true
			}
		}
		return false
	}
	startable := panes[:0]
	for _, p := range panes {
		if isStatusOrphan(p) {
			// Stop the orphan's app and detach so it doesn't dangle.
			// The real status pane (from AddStatusPane) is unaffected.
			//
			// Lock-discipline / lifecycle note (Plan D2 Task 17.A):
			// The orphan app was constructed via appFromSnapshot →
			// factory and PrepareAppForRestore'd, but Run() was never
			// invoked. Calling Stop() on a never-Run() app is per-app
			// undefined: it may close channels its Run() was supposed
			// to drain, panic on nil internal state, or block waiting
			// for goroutines that never started. Wrap in recover() so
			// a misbehaving Stop cannot abort boot snapshot restore.
			if p.app != nil {
				stopOrphanAppSafely(d.appLifecycle, p.app)
				p.app = nil
			}
			continue
		}
		startable = append(startable, p)
	}
	d.pendingAppStartsMu.Lock()
	d.pendingAppStarts = append(d.pendingAppStarts, startable...)
	d.pendingAppStartsMu.Unlock()
	
	// 5. Activate correct workspace
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

	// Activate the focused pane only in the active workspace.
	// All other workspaces keep their panes deactivated so Shift-arrow
	// navigation starts from a clean state when switching workspaces.
	if d.activeWorkspace != nil {
		if leaf := d.activeWorkspace.tree.ActiveLeaf; leaf != nil && leaf.Pane != nil {
			leaf.Pane.SetActive(true)
		}
		d.activeWorkspace.Refresh()
		d.activeWorkspace.notifyFocus()
	}
	d.broadcastWorkspacesChanged()
	d.broadcastWorkspaceSwitched()
	d.broadcastModeChanged()
	d.broadcastActivePaneChanged()
	return nil
}

func assignPanesToWorkspace(node *Node, ws *Workspace) {
	if node == nil {
		return
	}
	if node.Pane != nil {
		node.Pane.screen = ws
		// Re-create per-pane refresh forwarder targeting the correct workspace.
		// The forwarder increments renderGen before forwarding to the workspace
		// channel, which is required for per-pane dirty tracking (Level 2 opt).
		paneRefresh := node.Pane.setupRefreshForwarder(ws.refreshChan)
		node.Pane.markDirty()
		if node.Pane.pipeline != nil {
			node.Pane.pipeline.SetRefreshNotifier(paneRefresh)
		} else if node.Pane.app != nil {
			node.Pane.app.SetRefreshNotifier(paneRefresh)
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

// stopOrphanAppSafely calls lifecycle.StopApp(app) with a recover so a
// panicking Stop on a never-Run() factory-built app cannot abort boot
// snapshot restore. See ApplyTreeCapture's orphan filter (Plan D2 17.A).
func stopOrphanAppSafely(lifecycle AppLifecycleManager, app App) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("snapshot_restore: orphan StopApp panicked, recovering: %v", r)
		}
	}()
	lifecycle.StopApp(app)
}
