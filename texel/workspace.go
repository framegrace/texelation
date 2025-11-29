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

// Minimum usable pane dimensions (including borders)
const (
	MinPaneWidth  = 20 // ~18 chars drawable area
	MinPaneHeight = 8  // ~6 lines drawable area
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
	desktop             *DesktopEngine
	tree                *Tree
	refreshChan         chan bool
	drawChan            chan bool
	dispatcher          *EventDispatcher
	ShellAppFactory     AppFactory
	appLifecycle        AppLifecycleManager

	resizeSelection    *selectedBorder
	mouseResizeBorder  *selectedBorder
	debugFramesToDump  int
	refreshMonitorOnce sync.Once
}

// newWorkspace creates a new workspace with its own tiling pane tree.
func newWorkspace(id int, shellFactory AppFactory, lifecycle AppLifecycleManager, desktop *DesktopEngine) (*Workspace, error) {
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

	// Subscribe workspace to Desktop events so it can relay them to apps
	if desktop != nil {
		desktop.Subscribe(w)
	}

	return w, nil
}

func (w *Workspace) isDesktopClosing() bool {
	if w == nil || w.desktop == nil || w.desktop.quit == nil {
		return false
	}
	select {
	case <-w.desktop.quit:
		return true
	default:
		return false
	}
}

func forEachLeafPane(node *Node, fn func(*pane)) {
	if node == nil || fn == nil {
		return
	}
	if node.Pane != nil {
		fn(node.Pane)
		return
	}
	for _, child := range node.Children {
		forEachLeafPane(child, fn)
	}
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

// OnEvent implements Listener to receive Desktop events and relay them to workspace apps.
func (w *Workspace) OnEvent(event Event) {
	// Relay Desktop-level events to all apps in this workspace
	switch event.Type {
	case EventThemeChanged:
		log.Printf("Workspace %d: Received EventThemeChanged, relaying to apps", w.id)
		w.dispatcher.Broadcast(event)
	default:
		// Other Desktop events can be handled here if needed
	}
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

func (w *Workspace) handleAppExit(p *pane, exitedApp App, runErr error) {
	if w == nil || p == nil || w.tree == nil {
		return
	}
	if w.isDesktopClosing() {
		return
	}

	// Ignore stale callbacks if the pane has already attached a new app.
	if exitedApp != nil && p.app != nil && p.app != exitedApp {
		log.Printf("handleAppExit: ignoring exit for stale app '%s'", exitedApp.GetTitle())
		return
	}

	title := "unknown"
	if exitedApp != nil {
		title = exitedApp.GetTitle()
	} else if p.app != nil {
		title = p.app.GetTitle()
	}

	if runErr != nil {
		log.Printf("handleAppExit: app '%s' exited with error: %v", title, runErr)
	} else {
		log.Printf("handleAppExit: app '%s' exited cleanly", title)
	}

	node := w.tree.FindNodeWithPane(p)
	if node == nil {
		log.Printf("handleAppExit: pane for app '%s' already removed", title)
		return
	}

	w.removeNode(node, true)
}

func (w *Workspace) nodeAt(x, y int) *Node {
	if w == nil || w.tree == nil {
		return nil
	}
	return w.tree.FindLeafAt(x, y)
}

func (w *Workspace) setBorderResizing(border *selectedBorder, resizing bool) {
	if border == nil || border.node == nil {
		return
	}
	if border.index < 0 || border.index+1 >= len(border.node.Children) {
		return
	}
	left := border.node.Children[border.index]
	right := border.node.Children[border.index+1]
	forEachLeafPane(left, func(p *pane) {
		p.SetResizing(resizing)
	})
	forEachLeafPane(right, func(p *pane) {
		p.SetResizing(resizing)
	})
}

func (w *Workspace) activateLeaf(node *Node) bool {
	if w == nil || node == nil || node.Pane == nil || w.tree == nil {
		return false
	}
	if w.tree.ActiveLeaf == node {
		if !node.Pane.IsActive {
			node.Pane.SetActive(true)
		}
		return false
	}

	if current := w.tree.ActiveLeaf; current != nil && current.Pane != nil {
		current.Pane.SetActive(false)
	}

	w.tree.ActiveLeaf = node
	node.Pane.SetActive(true)

	w.Broadcast(Event{Type: EventPaneActiveChanged, Payload: node})
	w.notifyFocus()
	if w.desktop != nil {
		w.desktop.broadcastStateUpdate()
	}
	return true
}

func (w *Workspace) removeNode(target *Node, allowRoot bool) {
	if w == nil || w.tree == nil || target == nil {
		return
	}

	if w.mouseResizeBorder != nil {
		w.finishMouseResize()
	}

	pane := target.Pane
	if pane == nil {
		return
	}

	isRoot := target.Parent == nil
	if isRoot && !allowRoot {
		log.Printf("removeNode: refusing to remove root pane '%s'", pane.getTitle())
		return
	}

	log.Printf("removeNode: removing pane '%s' (root=%v)", pane.getTitle(), isRoot)

	wasActive := w.tree.ActiveLeaf == target

	if isRoot {
		pane.IsActive = false
		pane.Close()
		w.tree.Root = nil
		w.tree.ActiveLeaf = nil
		w.recalculateLayout()
		w.Broadcast(Event{Type: EventPaneClosed, Payload: target})
		w.notifyFocus()
		if w.desktop != nil {
			w.desktop.broadcastTreeChanged()
			w.desktop.broadcastStateUpdate()
		}
		w.ensureWelcomePane()
		return
	}

	parent := target.Parent
	closingIndex := -1
	for i, child := range parent.Children {
		if child == target {
			closingIndex = i
			break
		}
	}
	if closingIndex == -1 {
		log.Printf("removeNode: could not locate pane '%s' within parent", pane.getTitle())
		return
	}

	if len(parent.Children) > 0 {
		parent.Children = append(parent.Children[:closingIndex], parent.Children[closingIndex+1:]...)
	}
	if closingIndex < len(parent.SplitRatios) {
		parent.SplitRatios = append(parent.SplitRatios[:closingIndex], parent.SplitRatios[closingIndex+1:]...)
	}

	var nextActive *Node
	switch len(parent.Children) {
	case 0:
		grandparent := parent.Parent
		if grandparent == nil {
			w.tree.Root = nil
		} else {
			parentIndex := -1
			for i, child := range grandparent.Children {
				if child == parent {
					parentIndex = i
					break
				}
			}
			if parentIndex != -1 {
				grandparent.Children = append(grandparent.Children[:parentIndex], grandparent.Children[parentIndex+1:]...)
				if parentIndex < len(grandparent.SplitRatios) {
					grandparent.SplitRatios = append(grandparent.SplitRatios[:parentIndex], grandparent.SplitRatios[parentIndex+1:]...)
				}
				if len(grandparent.Children) > 0 {
					idx := parentIndex
					if idx >= len(grandparent.Children) {
						idx = len(grandparent.Children) - 1
					}
					nextActive = w.tree.findFirstLeaf(grandparent.Children[idx])
				}
			}
		}
	case 1:
		remainingChild := parent.Children[0]
		grandparent := parent.Parent
		remainingChild.Parent = grandparent
		if grandparent == nil {
			w.tree.Root = remainingChild
		} else {
			for i, child := range grandparent.Children {
				if child == parent {
					grandparent.Children[i] = remainingChild
					break
				}
			}
		}
		nextActive = w.tree.findFirstLeaf(remainingChild)
	default:
		totalRatio := 0.0
		for _, ratio := range parent.SplitRatios {
			totalRatio += ratio
		}
		if totalRatio > 0 {
			for i := range parent.SplitRatios {
				parent.SplitRatios[i] = parent.SplitRatios[i] / totalRatio
			}
		}
		newIndex := closingIndex
		if newIndex >= len(parent.Children) {
			newIndex = len(parent.Children) - 1
		}
		if newIndex >= 0 && newIndex < len(parent.Children) {
			nextActive = w.tree.findFirstLeaf(parent.Children[newIndex])
		}
	}

	pane.IsActive = false
	pane.Close()

	if wasActive {
		w.tree.ActiveLeaf = nextActive
		if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
			w.tree.ActiveLeaf.Pane.SetActive(true)
		}
	} else if w.tree.ActiveLeaf == nil {
		w.tree.ActiveLeaf = nextActive
		if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
			w.tree.ActiveLeaf.Pane.SetActive(true)
		}
	}

	w.recalculateLayout()
	w.Broadcast(Event{Type: EventPaneClosed, Payload: target})
	w.notifyFocus()
	if w.desktop != nil {
		w.desktop.broadcastTreeChanged()
		w.desktop.broadcastStateUpdate()
	}
	w.ensureWelcomePane()
}

func (w *Workspace) CloseActivePane() {
	if w == nil || w.tree == nil || w.tree.ActiveLeaf == nil {
		return
	}
	w.removeNode(w.tree.ActiveLeaf, true)
}

// ActivePane returns the currently active pane in this workspace.
// Returns nil if there is no active pane.
func (w *Workspace) ActivePane() *pane {
	if w == nil || w.tree == nil || w.tree.ActiveLeaf == nil {
		return nil
	}
	return w.tree.ActiveLeaf.Pane
}

func (w *Workspace) ensureWelcomePane() {
	if w == nil || w.tree == nil {
		return
	}
	if w.tree.Root != nil {
		return
	}
	if w.isDesktopClosing() {
		return
	}
	if w.desktop == nil || w.desktop.WelcomeAppFactory == nil {
		return
	}

	welcomeApp := w.desktop.WelcomeAppFactory()
	w.AddApp(welcomeApp)
	w.recalculateLayout()
	if w.desktop != nil {
		w.desktop.broadcastTreeChanged()
		w.desktop.broadcastStateUpdate()
	}
}

func (w *Workspace) borderForNeighbor(node *Node, dir Direction) *selectedBorder {
	if w == nil || w.tree == nil || node == nil {
		return nil
	}
	current := node
	for current.Parent != nil {
		parent := current.Parent
		index := -1
		for i, child := range parent.Children {
			if child == current {
				index = i
				break
			}
		}
		if index == -1 {
			return nil
		}

		switch dir {
		case DirLeft:
			if parent.Split == Vertical && index > 0 {
				return &selectedBorder{node: parent, index: index - 1}
			}
		case DirRight:
			if parent.Split == Vertical && index < len(parent.Children)-1 {
				return &selectedBorder{node: parent, index: index}
			}
		case DirUp:
			if parent.Split == Horizontal && index > 0 {
				return &selectedBorder{node: parent, index: index - 1}
			}
		case DirDown:
			if parent.Split == Horizontal && index < len(parent.Children)-1 {
				return &selectedBorder{node: parent, index: index}
			}
		}

		current = parent
	}
	return nil
}

func (w *Workspace) borderAt(x, y int) *selectedBorder {
	if w == nil || w.tree == nil {
		return nil
	}
	node := w.tree.FindLeafAt(x, y)
	if node == nil || node.Pane == nil {
		return nil
	}
	p := node.Pane
	if x == p.absX0 {
		if border := w.borderForNeighbor(node, DirLeft); border != nil {
			return border
		}
	}
	if x == p.absX1-1 {
		if border := w.borderForNeighbor(node, DirRight); border != nil {
			return border
		}
	}
	if y == p.absY0 {
		if border := w.borderForNeighbor(node, DirUp); border != nil {
			return border
		}
	}
	if y == p.absY1-1 {
		if border := w.borderForNeighbor(node, DirDown); border != nil {
			return border
		}
	}
	return nil
}

func (w *Workspace) startMouseResize(border *selectedBorder) {
	if border == nil {
		return
	}
	clone := &selectedBorder{
		node:  border.node,
		index: border.index,
	}
	w.mouseResizeBorder = clone
	w.setBorderResizing(clone, true)
}

func (w *Workspace) finishMouseResize() {
	if w.mouseResizeBorder == nil {
		return
	}
	w.setBorderResizing(w.mouseResizeBorder, false)
	w.mouseResizeBorder = nil
}

func (w *Workspace) updateMouseResize(x, y int) {
	if w == nil || w.tree == nil || w.mouseResizeBorder == nil {
		return
	}
	border := w.mouseResizeBorder
	if border.node == nil {
		return
	}

	switch border.node.Split {
	case Vertical:
		w.adjustBorderToX(border, x)
	case Horizontal:
		w.adjustBorderToY(border, y)
	}
}

func (w *Workspace) handleMouseResize(x, y int, buttons, prevButtons tcell.ButtonMask) bool {
	if w == nil || w.tree == nil {
		return false
	}

	resizing := w.mouseResizeBorder != nil
	buttonDown := buttons&tcell.Button1 != 0
	prevDown := prevButtons&tcell.Button1 != 0

	if resizing {
		if buttonDown {
			w.updateMouseResize(x, y)
		} else if prevDown {
			w.finishMouseResize()
		}
		return true
	}

	start := buttonDown && !prevDown
	if !start {
		return false
	}

	border := w.borderAt(x, y)
	if border == nil {
		return false
	}

	w.startMouseResize(border)
	return true
}

func (w *Workspace) adjustBorderToX(border *selectedBorder, x int) {
	if border == nil || border.node == nil || border.index < 0 || border.index+1 >= len(border.node.Children) {
		return
	}

	parent := border.node
	left := parent.Children[border.index]
	right := parent.Children[border.index+1]

	lx0, _, _, _, okLeft := w.tree.NodeBounds(left)
	_, _, rx1, _, okRight := w.tree.NodeBounds(right)
	px0, _, px1, _, okParent := w.tree.NodeBounds(parent)

	if !okLeft || !okRight || !okParent {
		return
	}

	minX := lx0 + MinPaneWidth
	maxX := rx1 - MinPaneWidth
	if maxX <= minX {
		return
	}

	if x < minX {
		x = minX
	}
	if x > maxX {
		x = maxX
	}

	parentWidth := px1 - px0
	if parentWidth <= 0 {
		return
	}

	leftWidth := x - lx0
	rightWidth := rx1 - x
	if leftWidth < MinPaneWidth {
		leftWidth = MinPaneWidth
	}
	if rightWidth < MinPaneWidth {
		rightWidth = MinPaneWidth
	}

	leftRatio := float64(leftWidth) / float64(parentWidth)
	rightRatio := float64(rightWidth) / float64(parentWidth)

	w.applyBorderRatios(parent, border.index, leftRatio, rightRatio)
	w.recalculateLayout()
	if w.desktop != nil {
		w.desktop.broadcastTreeChanged()
	}
	w.Refresh()
}

func (w *Workspace) adjustBorderToY(border *selectedBorder, y int) {
	if border == nil || border.node == nil || border.index < 0 || border.index+1 >= len(border.node.Children) {
		return
	}

	parent := border.node
	top := parent.Children[border.index]
	bottom := parent.Children[border.index+1]

	_, ty0, _, _, okTop := w.tree.NodeBounds(top)
	_, _, _, by1, okBottom := w.tree.NodeBounds(bottom)
	_, py0, _, py1, okParent := w.tree.NodeBounds(parent)

	if !okTop || !okBottom || !okParent {
		return
	}

	minY := ty0 + MinPaneHeight
	maxY := by1 - MinPaneHeight
	if maxY <= minY {
		return
	}

	if y < minY {
		y = minY
	}
	if y > maxY {
		y = maxY
	}

	parentHeight := py1 - py0
	if parentHeight <= 0 {
		return
	}

	topHeight := y - ty0
	bottomHeight := by1 - y
	if topHeight < MinPaneHeight {
		topHeight = MinPaneHeight
	}
	if bottomHeight < MinPaneHeight {
		bottomHeight = MinPaneHeight
	}

	topRatio := float64(topHeight) / float64(parentHeight)
	bottomRatio := float64(bottomHeight) / float64(parentHeight)

	w.applyBorderRatios(parent, border.index, topRatio, bottomRatio)
	w.recalculateLayout()
	if w.desktop != nil {
		w.desktop.broadcastTreeChanged()
	}
	w.Refresh()
}

func (w *Workspace) applyBorderRatios(parent *Node, index int, first, second float64) {
	if parent == nil || index < 0 || index+1 >= len(parent.SplitRatios) {
		return
	}

	sumOthers := 0.0
	for i, ratio := range parent.SplitRatios {
		if i == index || i == index+1 {
			continue
		}
		sumOthers += ratio
	}

	parent.SplitRatios[index] = first
	parent.SplitRatios[index+1] = second

	remaining := 1.0 - (first + second)
	if sumOthers > 0 {
		if remaining < 0 {
			remaining = 0
		}
		scale := remaining / sumOthers
		for i := range parent.SplitRatios {
			if i == index || i == index+1 {
				continue
			}
			parent.SplitRatios[i] *= scale
		}
	}

	total := 0.0
	for _, ratio := range parent.SplitRatios {
		total += ratio
	}
	if total > 0 {
		for i := range parent.SplitRatios {
			parent.SplitRatios[i] /= total
		}
	}
}

func (w *Workspace) PerformSplit(splitDir SplitType) {
	if w.tree.ActiveLeaf == nil || w.ShellAppFactory == nil {
		log.Printf("PerformSplit: Cannot split - no active leaf or shell factory")
		return
	}

	// Check if split would make panes too small
	if w.tree.ActiveLeaf.Pane != nil {
		currentW := w.tree.ActiveLeaf.Pane.Width()
		currentH := w.tree.ActiveLeaf.Pane.Height()

		if splitDir == Vertical && currentW/2 < MinPaneWidth {
			log.Printf("PerformSplit: Cannot split vertically - resulting panes too narrow (%d/2 < %d)", currentW, MinPaneWidth)
			return
		}
		if splitDir == Horizontal && currentH/2 < MinPaneHeight {
			log.Printf("PerformSplit: Cannot split horizontally - resulting panes too short (%d/2 < %d)", currentH, MinPaneHeight)
			return
		}
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

	w.finishMouseResize()

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
		w.setBorderResizing(border, true)
		w.Refresh()
	}
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
	w.setBorderResizing(selection, false)
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
