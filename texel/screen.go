// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/screen.go
// Summary: Implements screen capabilities for the core desktop engine.
// Usage: Used throughout the project to implement screen inside the desktop and panes.
// Notes: Legacy desktop logic migrated from the monolithic application.

// texel/screen_v2.go
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

// Screen now represents a single workspace with its own effects pipeline
type Screen struct {
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

// newScreen creates a new workspace screen.
func newScreen(id int, shellFactory AppFactory, lifecycle AppLifecycleManager, desktop *Desktop) (*Screen, error) {
	s := &Screen{
		id:              id,
		desktop:         desktop,
		tree:            NewTree(),
		refreshChan:     make(chan bool, 1),
		drawChan:        make(chan bool, 1),
		dispatcher:      NewEventDispatcher(),
		ShellAppFactory: shellFactory,
		appLifecycle:    lifecycle,
	}

	return s, nil
}

func (s *Screen) SetControlMode(active bool) {
	log.Printf("SetControlMode called: active=%v", active)
}

func (s *Screen) getDefaultBackground() tcell.Color {
	return s.desktop.DefaultBgColor
}

func (s *Screen) setArea(x, y, w, h int) {
	s.x, s.y, s.width, s.height = x, y, w, h
	s.recalculateLayout()
}

func (s *Screen) Refresh() {
	select {
	case s.refreshChan <- true:
	default:
	}
}

func (s *Screen) startRefreshMonitor() {
	if s == nil || s.refreshChan == nil || s.desktop == nil {
		return
	}
	s.refreshMonitorOnce.Do(func() {
		go func() {
			for {
				select {
				case <-s.desktop.quit:
					return
				case <-s.refreshChan:
					if handler := s.desktop.refreshHandlerFunc(); handler != nil {
						handler()
					}
				}
			}
		}()
	})
}

func (s *Screen) Broadcast(event Event) {
	s.dispatcher.Broadcast(event)
}

func (s *Screen) Subscribe(listener Listener) {
	s.dispatcher.Subscribe(listener)
}

func (s *Screen) Unsubscribe(listener Listener) {
	s.dispatcher.Unsubscribe(listener)
}

func (s *Screen) notifyFocus() {
	if s.desktop == nil || s.tree == nil {
		return
	}
	s.desktop.notifyFocusNode(s.tree.ActiveLeaf)
}

func (s *Screen) AddApp(app App) {
	log.Printf("AddApp: Adding app '%s'", app.GetTitle())

	p := newPane(s)
	s.tree.SetRoot(p)
	p.AttachApp(app, s.refreshChan)

	// Set initial active state AFTER attaching the app
	log.Printf("AddApp: Setting pane '%s' as active", p.getTitle())
	p.SetActive(true)
	s.notifyFocus()
	s.desktop.broadcastStateUpdate()
}

func (s *Screen) moveActivePane(d Direction) {
	log.Printf("moveActivePane: Moving in direction %v", d)

	// Get current and target panes
	var currentPane, targetPane *pane
	var currentTitle, targetTitle string

	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		currentPane = s.tree.ActiveLeaf.Pane
		currentTitle = currentPane.getTitle()
	}

	// We need to find the neighbor manually since findNeighbor is a method on Tree
	// Let's just proceed with the move and get the result
	oldActiveLeaf := s.tree.ActiveLeaf

	// Move in tree first
	s.tree.MoveActive(d)

	// Check if we actually moved
	if s.tree.ActiveLeaf == oldActiveLeaf {
		log.Printf("moveActivePane: No movement occurred")
		return
	}

	// Get the target pane after the move
	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		targetPane = s.tree.ActiveLeaf.Pane
		targetTitle = targetPane.getTitle()
	}

	s.recalculateLayout()

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

	s.Broadcast(Event{Type: EventPaneActiveChanged, Payload: s.tree.ActiveLeaf})
	s.desktop.broadcastStateUpdate()
	s.notifyFocus()
}

func (s *Screen) handleEvent(ev *tcell.EventKey) {
	// Handle pane navigation
	if ev.Modifiers()&tcell.ModShift != 0 {
		isPaneNavKey := true
		switch ev.Key() {
		case tcell.KeyUp:
			s.moveActivePane(DirUp)
		case tcell.KeyDown:
			s.moveActivePane(DirDown)
		case tcell.KeyLeft:
			s.moveActivePane(DirLeft)
		case tcell.KeyRight:
			s.moveActivePane(DirRight)
		default:
			isPaneNavKey = false
		}
		if isPaneNavKey {
			s.Refresh()
			return
		}
	}

	// Pass all other keys to the active application
	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		s.tree.ActiveLeaf.Pane.app.HandleKey(ev)
	}
}

func (s *Screen) handlePaste(data []byte) {
	if s == nil || len(data) == 0 {
		return
	}
	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		s.tree.ActiveLeaf.Pane.handlePaste(data)
	}
}

func (s *Screen) CloseActivePane() {
	if s.tree.ActiveLeaf == nil {
		return
	}

	closedPaneNode := s.tree.ActiveLeaf
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
			s.tree.Root = remainingChild
		} else {
			// Find parent's index in grandparent's children and replace it
			for i, child := range grandparent.Children {
				if child == parent {
					grandparent.Children[i] = remainingChild
					break
				}
			}
		}
		nextActiveNode = s.tree.findFirstLeaf(remainingChild)
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
		nextActiveNode = s.tree.findFirstLeaf(parent.Children[newIndex])
	}

	closedPaneNode.Pane.Close() // Ensure the closed app is stopped
	s.tree.ActiveLeaf = nextActiveNode
	if s.tree.ActiveLeaf.Pane != nil {
		s.tree.ActiveLeaf.Pane.SetActive(true)
	}

	s.recalculateLayout()
	s.Broadcast(Event{Type: EventPaneClosed, Payload: closedPaneNode})
	s.notifyFocus()
	if s.desktop != nil {
		s.desktop.broadcastTreeChanged()
	}
}

func (s *Screen) PerformSplit(splitDir SplitType) {
	if s.tree.ActiveLeaf == nil || s.ShellAppFactory == nil {
		log.Printf("PerformSplit: Cannot split - no active leaf or shell factory")
		return
	}

	log.Printf("PerformSplit: Splitting in direction %v", splitDir)

	// Get current pane for logging
	var currentTitle string
	if s.tree.ActiveLeaf.Pane != nil {
		currentTitle = s.tree.ActiveLeaf.Pane.getTitle()
		log.Printf("PerformSplit: Current active pane: '%s'", currentTitle)
	}

	// Create new pane FIRST
	newPane := newPane(s)
	log.Printf("PerformSplit: Created new pane")

	// Check if we'll be adding to existing group or creating new split
	// This replicates the logic from SplitActive to determine animation type
	nodeToModify := s.tree.ActiveLeaf
	parent := s.tree.findParentOf(s.tree.Root, nil, nodeToModify)
	addToExistingGroup := parent != nil && parent.Split == splitDir && ratiosAreEqual(parent.SplitRatios)

	log.Printf("PerformSplit: addToExistingGroup=%v", addToExistingGroup)
	if parent != nil {
		log.Printf("PerformSplit: Parent has %d children with ratios %v (equal=%v)",
			len(parent.Children), parent.SplitRatios, ratiosAreEqual(parent.SplitRatios))
	}

	// Perform the split in the tree
	newNode := s.tree.SplitActive(splitDir, newPane)
	if newNode == nil {
		log.Printf("PerformSplit: Failed to split tree")
		return
	}
	log.Printf("PerformSplit: Tree split completed")

	// Create and attach new app
	newApp := s.ShellAppFactory()
	newPane.AttachApp(newApp, s.refreshChan)
	log.Printf("PerformSplit: Attached app '%s' to new pane", newApp.GetTitle())

	// Set pane states
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil && node != newNode && node != s.tree.ActiveLeaf {
			log.Printf("PerformSplit: Deactivating old pane '%s'", node.Pane.getTitle())
			node.Pane.SetActive(false)
		}
	})

	// The new pane should be active
	log.Printf("PerformSplit: Activating new pane '%s'", newPane.getTitle())
	newPane.SetActive(true)
	s.notifyFocus()

	// Recalculate layout after split
	s.recalculateLayout()

	log.Printf("PerformSplit: Split completed successfully")
	if s.desktop != nil {
		s.desktop.broadcastTreeChanged()
	}
}

func (s *Screen) SwapActivePane(d Direction) {
	if d != -1 {
		s.tree.SwapActivePane(d)
		s.recalculateLayout()
		s.Refresh()
		if s.desktop != nil {
			s.desktop.broadcastTreeChanged()
			s.desktop.broadcastStateUpdate()
		}
	}
}

// Update the draw method to also log when pane animations are detected
func (s *Screen) Close() {

	// Close all panes
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			node.Pane.Close()
		}
	})
}

func (s *Screen) recalculateLayout() {
	s.tree.Resize(s.x, s.y, s.width, s.height)
}

func (s *Screen) findBorderToResize(d Direction) *selectedBorder {
	var border *selectedBorder
	curr := s.tree.ActiveLeaf
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
	s.Refresh()
	return border
}

func (s *Screen) handleInteractiveResize(ev *tcell.EventKey, currentSelection *selectedBorder) *selectedBorder {
	d := keyToDirection(ev)
	if currentSelection == nil {
		return s.findBorderToResize(d)
	}

	s.adjustBorder(currentSelection, d)
	return currentSelection
}

func (s *Screen) adjustBorder(border *selectedBorder, d Direction) {
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

	s.recalculateLayout()
	if s.desktop != nil {
		s.desktop.broadcastTreeChanged()
	}
	s.Refresh()
}

func (s *Screen) clearResizeSelection(selection *selectedBorder) {
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
	s.Refresh()
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
