// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/workspace.go
// Summary: Implements workspace (tab) capabilities for the core desktop engine.
// Usage: Manages a single workspace with tiling pane tree, navigation, and event routing.
// Notes: Renamed from Screen to better reflect its role as a workspace/tab manager.

package texel

import (
	"github.com/gdamore/tcell/v2"
	"log"
	"sync"
)

type Direction int

const (
	DirUp Direction = iota
	DirDown
	DirLeft
	DirRight
)

// DebuggableApp is an interface that apps can implement to provide
// detailed state information for debugging purposes.
type DebuggableApp interface {
	DumpState(frameNum int)
}

type AppFactory func() App

const (
	keyControlMode = tcell.KeyCtrlA
)

const (
	ResizeStep float64 = 0.05 // Resize by 5%
	MinRatio   float64 = 0.1  // Panes can't be smaller than 10%
)

type styleKey struct {
	fg, bg          tcell.Color
	bold, underline bool
	reverse         bool
}

type selectedBorder struct {
	node  *Node // The parent node whose children are being resized (the split node)
	index int   // The index of the left/top pane of the border. The border is between child[index] and child[index+1].
}

// Workspace represents a single workspace/tab with its own tiling pane tree.
// Each workspace manages independent pane layout, navigation, and event routing.
type Workspace struct {
	id                  int
	x, y, width, height int
	desktop             *Desktop
	tree                *Tree
	refreshChan         chan bool
	drawChan            chan bool
	dispatcher          *EventDispatcher
	ShellAppFactory     AppFactory
	appLifecycle        AppLifecycleManager

	resizeSelection    *selectedBorder
	debugFramesToDump  int
	refreshMonitorOnce sync.Once
}

// newWorkspace creates a new workspace with its own tiling pane tree.
func newWorkspace(id int, shellFactory AppFactory, lifecycle AppLifecycleManager, desktop *Desktop) (*Workspace, error) {
	w := &Workspace{
		id:              id,
		desktop:         desktop,
		tree:            NewTree(),
		refreshChan:     make(chan bool, 1),
		drawChan:        make(chan bool, 1),
		dispatcher:      NewEventDispatcher(),
		ShellAppFactory: shellFactory,
		appLifecycle:    lifecycle,
	}

	return w, nil
}

func (w *Workspace) SetControlMode(active bool) {
	log.Printf("SetControlMode called: active=%v", active)
}

func (w *Workspace) getDefaultBackground() tcell.Color {
	return w.desktop.DefaultBgColor
}

func (w *Workspace) setArea(x, y, width, height int) {
	w.x, w.y, w.width, w.height = x, y, width, height
	w.recalculateLayout()
}

func (w *Workspace) Refresh() {
	select {
	case w.refreshChan <- true:
	default:
	}
}

func (w *Workspace) startRefreshMonitor() {
	if w == nil || w.refreshChan == nil || w.desktop == nil {
		return
	}
	w.refreshMonitorOnce.Do(func() {
		go func() {
			for {
				select {
				case <-w.desktop.quit:
					return
				case <-w.refreshChan:
					if handler := w.desktop.refreshHandlerFunc(); handler != nil {
						handler()
					}
				}
			}
		}()
	})
}

func (w *Workspace) Broadcast(event Event) {
	w.dispatcher.Broadcast(event)
}

func (w *Workspace) Subscribe(listener Listener) {
	w.dispatcher.Subscribe(listener)
}

func (w *Workspace) Unsubscribe(listener Listener) {
	w.dispatcher.Unsubscribe(listener)
}

func (w *Workspace) notifyFocus() {
	if w.desktop == nil || w.tree == nil {
		return
	}
	w.desktop.notifyFocusNode(w.tree.ActiveLeaf)
}

func (w *Workspace) AddApp(app App) {
	log.Printf("AddApp: Adding app '%s'", app.GetTitle())

	p := newPane(w)
	w.tree.SetRoot(p)
	p.AttachApp(app, w.refreshChan)

	// Set initial active state AFTER attaching the app
	log.Printf("AddApp: Setting pane '%s' as active", p.getTitle())
	p.SetActive(true)
	w.notifyFocus()
	w.desktop.broadcastStateUpdate()
}

func (w *Workspace) moveActivePane(d Direction) {
	log.Printf("moveActivePane: Moving in direction %v", d)

	// Get current and target panes
	var currentPane, targetPane *pane
	var currentTitle, targetTitle string

	if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
		currentPane = w.tree.ActiveLeaf.Pane
		currentTitle = currentPane.getTitle()
	}

	// We need to find the neighbor manually since findNeighbor is a method on Tree
	// Let's just proceed with the move and get the result
	oldActiveLeaf := w.tree.ActiveLeaf

	// Move in tree first
	w.tree.MoveActive(d)

	// Check if we actually moved
	if w.tree.ActiveLeaf == oldActiveLeaf {
		log.Printf("moveActivePane: No movement occurred")
		return
	}

	// Get the target pane after the move
	if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
		targetPane = w.tree.ActiveLeaf.Pane
		targetTitle = targetPane.getTitle()
	}

	w.recalculateLayout()

	// Set states and handle animations properly
	if currentPane != nil {
		currentPane.IsActive = false
		currentPane.notifyStateChange()
	}

	if targetPane != nil {
		targetPane.IsActive = true
		targetPane.notifyStateChange()
	}

	log.Printf("moveActivePane: Moved from '%s' to '%s'", currentTitle, targetTitle)

	w.Broadcast(Event{Type: EventPaneActiveChanged, Payload: w.tree.ActiveLeaf})
	w.desktop.broadcastStateUpdate()
	w.notifyFocus()
}

func (w *Workspace) handleEvent(ev *tcell.EventKey) {
	// Handle pane navigation
	if ev.Modifiers()&tcell.ModShift != 0 {
		isPaneNavKey := true
		switch ev.Key() {
		case tcell.KeyUp:
			w.moveActivePane(DirUp)
		case tcell.KeyDown:
			w.moveActivePane(DirDown)
		case tcell.KeyLeft:
			w.moveActivePane(DirLeft)
		case tcell.KeyRight:
			w.moveActivePane(DirRight)
		default:
			isPaneNavKey = false
		}
		if isPaneNavKey {
			w.Refresh()
			return
		}
	}

	// Pass all other keys to the active application
	if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
		w.tree.ActiveLeaf.Pane.app.HandleKey(ev)
	}
}

func (w *Workspace) handlePaste(data []byte) {
	if w == nil || len(data) == 0 {
		return
	}
	if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
		w.tree.ActiveLeaf.Pane.handlePaste(data)
	}
}

func (w *Workspace) CloseActivePane() {
	if w.tree.ActiveLeaf == nil {
		return
	}

	closedPaneNode := w.tree.ActiveLeaf
	parent := closedPaneNode.Parent

	// If this is the root pane, don't close it
	if parent == nil {
		return
	}

	// Find the index of the pane being closed
	closingIndex := -1
	for i, child := range parent.Children {
		if child == closedPaneNode {
			closingIndex = i
			break
		}
	}

	if closingIndex == -1 {
		log.Printf("CloseActivePane: Could not find pane index")
		return
	}

	log.Printf("CloseActivePane: Closing pane '%s' at index %d",
		closedPaneNode.Pane.getTitle(), closingIndex)

	// Perform the actual tree cleanup
	if closedPaneNode.Pane != nil {
		closedPaneNode.Pane.IsActive = false
	}

	// Remove the child from the parent's slice
	parent.Children = append(parent.Children[:closingIndex], parent.Children[closingIndex+1:]...)
	parent.SplitRatios = append(parent.SplitRatios[:closingIndex], parent.SplitRatios[closingIndex+1:]...)

	// If the parent has only one child left, the split is no longer needed.
	// Promote the remaining child to replace its parent.
	var nextActiveNode *Node
	if len(parent.Children) == 1 {
		remainingChild := parent.Children[0]
		grandparent := parent.Parent
		remainingChild.Parent = grandparent

		if grandparent == nil {
			w.tree.Root = remainingChild
		} else {
			// Find parent's index in grandparent's children and replace it
			for i, child := range grandparent.Children {
				if child == parent {
					grandparent.Children[i] = remainingChild
					break
				}
			}
		}
		nextActiveNode = w.tree.findFirstLeaf(remainingChild)
	} else {
		// Normalize ratios after removal
		totalRatio := 0.0
		for _, ratio := range parent.SplitRatios {
			totalRatio += ratio
		}
		if totalRatio > 0 {
			for i := range parent.SplitRatios {
				parent.SplitRatios[i] = parent.SplitRatios[i] / totalRatio
			}
		}

		// Set focus to the previous sibling, or the new last one if we closed the first
		newIndex := closingIndex
		if newIndex >= len(parent.Children) {
			newIndex = len(parent.Children) - 1
		}
		nextActiveNode = w.tree.findFirstLeaf(parent.Children[newIndex])
	}

	closedPaneNode.Pane.Close() // Ensure the closed app is stopped
	w.tree.ActiveLeaf = nextActiveNode
	if w.tree.ActiveLeaf.Pane != nil {
		w.tree.ActiveLeaf.Pane.SetActive(true)
	}

	w.recalculateLayout()
	w.Broadcast(Event{Type: EventPaneClosed, Payload: closedPaneNode})
	w.notifyFocus()
	if w.desktop != nil {
		w.desktop.broadcastTreeChanged()
	}
}

func (w *Workspace) PerformSplit(splitDir SplitType) {
	if w.tree.ActiveLeaf == nil || w.ShellAppFactory == nil {
		log.Printf("PerformSplit: Cannot split - no active leaf or shell factory")
		return
	}

	log.Printf("PerformSplit: Splitting in direction %v", splitDir)

	// Get current pane for logging
	var currentTitle string
	if w.tree.ActiveLeaf.Pane != nil {
		currentTitle = w.tree.ActiveLeaf.Pane.getTitle()
		log.Printf("PerformSplit: Current active pane: '%s'", currentTitle)
	}

	// Create new pane FIRST
	newPane := newPane(w)
	log.Printf("PerformSplit: Created new pane")

	// Check if we'll be adding to existing group or creating new split
	// This replicates the logic from SplitActive to determine animation type
	nodeToModify := w.tree.ActiveLeaf
	parent := w.tree.findParentOf(w.tree.Root, nil, nodeToModify)
	addToExistingGroup := parent != nil && parent.Split == splitDir && ratiosAreEqual(parent.SplitRatios)

	log.Printf("PerformSplit: addToExistingGroup=%v", addToExistingGroup)
	if parent != nil {
		log.Printf("PerformSplit: Parent has %d children with ratios %v (equal=%v)",
			len(parent.Children), parent.SplitRatios, ratiosAreEqual(parent.SplitRatios))
	}

	// Perform the split in the tree
	newNode := w.tree.SplitActive(splitDir, newPane)
	if newNode == nil {
		log.Printf("PerformSplit: Failed to split tree")
		return
	}
	log.Printf("PerformSplit: Tree split completed")

	// Create and attach new app
	newApp := w.ShellAppFactory()
	newPane.AttachApp(newApp, w.refreshChan)
	log.Printf("PerformSplit: Attached app '%s' to new pane", newApp.GetTitle())

	// Set pane states
	w.tree.Traverse(func(node *Node) {
		if node.Pane != nil && node != newNode && node != w.tree.ActiveLeaf {
			log.Printf("PerformSplit: Deactivating old pane '%s'", node.Pane.getTitle())
			node.Pane.SetActive(false)
		}
	})

	// The new pane should be active
	log.Printf("PerformSplit: Activating new pane '%s'", newPane.getTitle())
	newPane.SetActive(true)
	w.notifyFocus()

	// Recalculate layout after split
	w.recalculateLayout()

	log.Printf("PerformSplit: Split completed successfully")
	if w.desktop != nil {
		w.desktop.broadcastTreeChanged()
	}
}

func (w *Workspace) SwapActivePane(d Direction) {
	if d != -1 {
		w.tree.SwapActivePane(d)
		w.recalculateLayout()
		w.Refresh()
		if w.desktop != nil {
			w.desktop.broadcastTreeChanged()
			w.desktop.broadcastStateUpdate()
		}
	}
}

// Update the draw method to also log when pane animations are detected
func (w *Workspace) Close() {

	// Close all panes
	w.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			node.Pane.Close()
		}
	})
}

func (w *Workspace) recalculateLayout() {
	w.tree.Resize(w.x, w.y, w.width, w.height)
}

func (w *Workspace) findBorderToResize(d Direction) *selectedBorder {
	var border *selectedBorder
	curr := w.tree.ActiveLeaf
	for curr.Parent != nil {
		parent := curr.Parent
		if (d == DirLeft || d == DirRight) && parent.Split == Vertical {
			for i, child := range parent.Children {
				if child == curr {
					if d == DirRight && i < len(parent.Children)-1 {
						border = &selectedBorder{node: parent, index: i}
					} else if d == DirLeft && i > 0 {
						border = &selectedBorder{node: parent, index: i - 1}
					}
					break
				}
			}
		} else if (d == DirUp || d == DirDown) && parent.Split == Horizontal {
			for i, child := range parent.Children {
				if child == curr {
					if d == DirDown && i < len(parent.Children)-1 {
						border = &selectedBorder{node: parent, index: i}
					} else if d == DirUp && i > 0 {
						border = &selectedBorder{node: parent, index: i - 1}
					}
					break
				}
			}
		}
		if border != nil {
			break
		}
		curr = parent
	}
	if border != nil {
		// Use the new SetResizing method
		if p1 := border.node.Children[border.index].Pane; p1 != nil {
			p1.SetResizing(true)
		}
		if p2 := border.node.Children[border.index+1].Pane; p2 != nil {
			p2.SetResizing(true)
		}
	}
	w.Refresh()
	return border
}

func (w *Workspace) handleInteractiveResize(ev *tcell.EventKey, currentSelection *selectedBorder) *selectedBorder {
	d := keyToDirection(ev)
	if currentSelection == nil {
		return w.findBorderToResize(d)
	}

	w.adjustBorder(currentSelection, d)
	return currentSelection
}

func (w *Workspace) adjustBorder(border *selectedBorder, d Direction) {
	if !(((d == DirLeft || d == DirRight) && border.node.Split == Vertical) ||
		((d == DirUp || d == DirDown) && border.node.Split == Horizontal)) {
		return
	}

	leftPaneIndex := border.index
	rightPaneIndex := border.index + 1
	var growerIndex, shrinkerIndex int

	if d == DirRight || d == DirDown {
		growerIndex = leftPaneIndex
		shrinkerIndex = rightPaneIndex
	} else {
		growerIndex = rightPaneIndex
		shrinkerIndex = leftPaneIndex
	}

	if border.node.SplitRatios[shrinkerIndex] <= MinRatio {
		return
	}

	transferAmount := ResizeStep
	if border.node.SplitRatios[shrinkerIndex]-transferAmount < MinRatio {
		transferAmount = border.node.SplitRatios[shrinkerIndex] - MinRatio
	}
	if transferAmount <= 0 {
		return
	}

	border.node.SplitRatios[growerIndex] += transferAmount
	border.node.SplitRatios[shrinkerIndex] -= transferAmount

	w.recalculateLayout()
	if w.desktop != nil {
		w.desktop.broadcastTreeChanged()
	}
	w.Refresh()
}

func (w *Workspace) clearResizeSelection(selection *selectedBorder) {
	if selection == nil {
		return
	}
	// Use the new SetResizing method
	if p1 := selection.node.Children[selection.index].Pane; p1 != nil {
		p1.SetResizing(false)
	}
	if p2 := selection.node.Children[selection.index+1].Pane; p2 != nil {
		p2.SetResizing(false)
	}
	w.Refresh()
}

func keyToDirection(ev *tcell.EventKey) Direction {
	switch ev.Key() {
	case tcell.KeyUp:
		return DirUp
	case tcell.KeyDown:
		return DirDown
	case tcell.KeyLeft:
		return DirLeft
	case tcell.KeyRight:
		return DirRight
	}
	return -1
}

func blit(tcs ScreenDriver, x, y int, buf [][]Cell) {
	for r, row := range buf {
		for c, cell := range row {
			tcs.SetContent(x+c, y+r, cell.Ch, nil, cell.Style)
		}
	}
}

func blitDiff(tcs ScreenDriver, x0, y0 int, oldBuf, buf [][]Cell) {
	for y, row := range buf {
		for x, cell := range row {
			if y >= len(oldBuf) || x >= len(oldBuf[y]) || cell != oldBuf[y][x] {
				tcs.SetContent(x0+x, y0+y, cell.Ch, nil, cell.Style)
			}
		}
	}
}
