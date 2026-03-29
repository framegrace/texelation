// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/workspace_layout.go
// Summary: Layout, split, and border resize operations for workspaces.

package texel

import (
	"log"

	"github.com/framegrace/texelation/internal/debuglog"
	"github.com/gdamore/tcell/v2"
)

func (w *Workspace) setArea(x, y, width, height int) {
	w.x, w.y, w.width, w.height = x, y, width, height
	w.recalculateLayout()
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

	debuglog.Printf("removeNode: removing pane '%s' (root=%v)", pane.getTitle(), isRoot)

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

	// Try to animate the removal if enabled and we have siblings
	if w.desktop != nil && w.desktop.layoutTransitions != nil && len(parent.Children) > 1 {
		debuglog.Printf("removeNode: Starting animated removal of pane '%s' at index %d", pane.getTitle(), closingIndex)
		w.desktop.layoutTransitions.AnimateRemoval(parent, closingIndex, func() {
			debuglog.Printf("removeNode: Animation complete, performing actual removal of '%s'", pane.getTitle())
			w.doRemoveNode(target, parent, closingIndex, wasActive)
		})
		return // The callback will finish the job
	}

	// No animation, do immediate removal
	debuglog.Printf("removeNode: Performing immediate removal of pane '%s'", pane.getTitle())
	w.doRemoveNode(target, parent, closingIndex, wasActive)
}

// doRemoveNode performs the actual removal of a pane from the tree.
// This is called either immediately or from the animation callback.
func (w *Workspace) doRemoveNode(target *Node, parent *Node, closingIndex int, wasActive bool) {
	pane := target.Pane
	if pane == nil {
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
		w.desktop.broadcastActivePaneChanged()
	}
	w.ensureWelcomePane()
}

func (w *Workspace) CloseActivePane() {
	if w == nil || w.tree == nil || w.tree.ActiveLeaf == nil {
		return
	}

	// Check if the app wants to intercept the close request (e.g., to show confirmation)
	pane := w.tree.ActiveLeaf.Pane
	if pane != nil && pane.app != nil {
		if requester, ok := pane.app.(CloseRequester); ok {
			if !requester.RequestClose() {
				// App intercepted the close (showing confirmation, etc.)
				return
			}
		}
	}

	w.removeNode(w.tree.ActiveLeaf, true)
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

	debuglog.Printf("PerformSplit: Splitting in direction %v", splitDir)

	// Get current pane for logging
	var currentTitle string
	if w.tree.ActiveLeaf.Pane != nil {
		currentTitle = w.tree.ActiveLeaf.Pane.getTitle()
		debuglog.Printf("PerformSplit: Current active pane: '%s'", currentTitle)
	}

	// Create new pane FIRST
	newPane := newPane(w)
	debuglog.Printf("PerformSplit: Created new pane")

	// Check if we'll be adding to existing group or creating new split
	// This replicates the logic from SplitActive to determine animation type
	nodeToModify := w.tree.ActiveLeaf
	parent := w.tree.findParentOf(w.tree.Root, nil, nodeToModify)
	addToExistingGroup := parent != nil && parent.Split == splitDir && ratiosAreEqual(parent.SplitRatios)

	debuglog.Printf("PerformSplit: addToExistingGroup=%v", addToExistingGroup)
	if parent != nil {
		debuglog.Printf("PerformSplit: Parent has %d children with ratios %v (equal=%v)",
			len(parent.Children), parent.SplitRatios, ratiosAreEqual(parent.SplitRatios))
	}

	// Perform the split in the tree (this sets final ratios)
	newNode := w.tree.SplitActive(splitDir, newPane)
	if newNode == nil {
		log.Printf("PerformSplit: Failed to split tree")
		return
	}
	debuglog.Printf("PerformSplit: Tree split completed")

	// IMPORTANT: Create and attach the app BEFORE starting animation.
	// The animation broadcasts tree snapshots at 60fps, and CaptureTree() skips
	// panes with app == nil, which would corrupt the persisted tree structure.
	var newApp App
	if w.desktop != nil && w.desktop.InitAppName != "" {
		if appInstance := w.desktop.Registry().CreateApp(w.desktop.InitAppName, nil); appInstance != nil {
			if app, ok := appInstance.(App); ok {
				newApp = app
			}
		}
	}
	if newApp == nil {
		newApp = w.ShellAppFactory()
	}
	newPane.AttachApp(newApp, w.refreshChan)
	debuglog.Printf("PerformSplit: Attached app '%s' to new pane (before animation)", newApp.GetTitle())

	// Capture the target ratios set by SplitActive, then animate from initial to target
	var nodeWithRatios *Node
	if addToExistingGroup && parent != nil {
		nodeWithRatios = parent
	} else if nodeToModify != nil {
		nodeWithRatios = nodeToModify
	}

	// Start transition animation if we have a node with ratios
	if nodeWithRatios != nil && len(nodeWithRatios.SplitRatios) > 0 {
		targetRatios := make([]float64, len(nodeWithRatios.SplitRatios))
		copy(targetRatios, nodeWithRatios.SplitRatios)

		// Set initial ratios (new pane starts tiny)
		numChildren := len(nodeWithRatios.Children)
		if numChildren > 1 {
			// Give new pane a tiny initial ratio
			initialRatios := make([]float64, numChildren)
			if addToExistingGroup {
				// Existing children share the space, new one gets tiny slice
				remaining := 0.99
				for i := 0; i < numChildren-1; i++ {
					initialRatios[i] = remaining / float64(numChildren-1)
				}
				initialRatios[numChildren-1] = 0.01
			} else {
				// Two children: existing gets 0.99, new gets 0.01
				initialRatios[0] = 0.99
				initialRatios[1] = 0.01
			}
			nodeWithRatios.SplitRatios = initialRatios
			debuglog.Printf("PerformSplit: Set initial ratios %v, will animate to %v", initialRatios, targetRatios)

			// Start animation
			if w.desktop != nil && w.desktop.layoutTransitions != nil {
				w.desktop.layoutTransitions.AnimateSplit(nodeWithRatios, targetRatios)
			}
		}
	}

	// Set pane states
	w.tree.Traverse(func(node *Node) {
		if node.Pane != nil && node != newNode && node != w.tree.ActiveLeaf {
			debuglog.Printf("PerformSplit: Deactivating old pane '%s'", node.Pane.getTitle())
			node.Pane.SetActive(false)
		}
	})

	// The new pane should be active
	debuglog.Printf("PerformSplit: Activating new pane '%s'", newPane.getTitle())
	newPane.SetActive(true)
	w.notifyFocus()

	// Recalculate layout after split (if animations are disabled or no animation started, do it now)
	// If animations are active, the animator will handle recalculate + broadcast on each frame
	animating := w.desktop != nil && w.desktop.layoutTransitions != nil && w.desktop.layoutTransitions.IsAnimating()
	if !animating {
		w.recalculateLayout()
		debuglog.Printf("PerformSplit: Split completed successfully (no animation)")
		if w.desktop != nil {
			w.desktop.broadcastTreeChanged()
		}
	} else {
		debuglog.Printf("PerformSplit: Split completed successfully (animating)")
	}
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

	// Compute step as 1 character relative to total dimension.
	var totalSize int
	if border.node.Split == Vertical {
		totalSize = w.width
	} else {
		totalSize = w.height
	}
	transferAmount := 1.0 / float64(max(totalSize, 1))
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
