// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/tree.go
// Summary: Implements tree capabilities for the core desktop engine.
// Usage: Used throughout the project to implement tree inside the desktop and panes.

package texel

import (
	"log"
	"math"
	"time"
)

// Rect defines a rectangle using fractional coordinates (0.0 to 1.0).
type Rect struct {
	X, Y, W, H float64
}

type SplitType int

const (
	Horizontal SplitType = iota
	Vertical
)

// Node represents a node in the pane layout tree. It can be an internal
// node (with children) or a leaf node (with a pane).
type Node struct {
	Parent      *Node
	Split       SplitType
	Pane        *pane // A pane is only present in leaf nodes
	SplitRatios []float64
	Children    []*Node
}

// Tree manages the node hierarchy of panes.
type Tree struct {
	Root            *Node
	ActiveLeaf      *Node
	layoutAnimator  *LayoutAnimator
	animationEnabled bool
}

// NewTree creates a new layout tree.
func NewTree() *Tree {
	return &Tree{
		layoutAnimator:   NewLayoutAnimator(200 * time.Millisecond), // 200ms default for layout animations
		animationEnabled: false, // Disabled by default; enable via SetLayoutAnimationEnabled
	}
}

// SetLayoutAnimationEnabled enables or disables layout animations.
func (t *Tree) SetLayoutAnimationEnabled(enabled bool) {
	t.animationEnabled = enabled
	if t.layoutAnimator != nil {
		t.layoutAnimator.SetEnabled(enabled)
	}
}

// HasActiveLayoutAnimations returns true if any layout animations are currently running.
func (t *Tree) HasActiveLayoutAnimations() bool {
	if t.layoutAnimator == nil {
		return false
	}
	return t.layoutAnimator.HasActiveAnimations()
}

// SetRoot sets the root of the tree to a single node containing the given pane.
func (t *Tree) SetRoot(p *pane) {
	leaf := &Node{
		Pane: p,
	}
	t.Root = leaf
	t.ActiveLeaf = leaf
	//	if leaf.Pane != nil {
	//		leaf.Pane.IsActive = true
	//	}
	// Don't set IsActive here - let the caller handle it
	log.Printf("Tree.SetRoot: Set root pane '%s', IsActive will be set by caller", p.getTitle())
}

// ratiosAreEqual checks if all float values in a slice are effectively equal.
func ratiosAreEqual(ratios []float64) bool {
	if len(ratios) <= 1 {
		return true
	}
	first := ratios[0]
	for _, r := range ratios[1:] {
		// Use a small epsilon for float comparison to handle potential precision issues.
		if math.Abs(r-first) > 0.001 {
			return false
		}
	}
	return true
}

// SplitActive splits the active leaf node, attaching the provided new pane.
// It now intelligently decides whether to add to the current group or create a new sub-group.
func (t *Tree) SplitActive(splitDir SplitType, newPane *pane) *Node {
	if t.ActiveLeaf == nil {
		log.Printf("SplitActive: No active leaf to split")
		return nil
	}

	splitDirStr := "Vertical"
	if splitDir == Horizontal {
		splitDirStr = "Horizontal"
	}

	log.Printf("SplitActive: Splitting active leaf with pane '%s' in %s direction",
		t.ActiveLeaf.Pane.getTitle(), splitDirStr)

	if t.ActiveLeaf.Pane != nil {
		t.ActiveLeaf.Pane.IsActive = false
	}

	nodeToModify := t.ActiveLeaf
	parent := t.findParentOf(t.Root, nil, nodeToModify)
	var newActiveNode *Node

	log.Printf("SplitActive: nodeToModify has parent=%v", parent != nil)
	if parent != nil {
		parentSplitStr := "Vertical"
		if parent.Split == Horizontal {
			parentSplitStr = "Horizontal"
		}
		log.Printf("SplitActive: Parent split=%s, ratios=%v, ratiosEqual=%v",
			parentSplitStr, parent.SplitRatios, ratiosAreEqual(parent.SplitRatios))
	}

	// Check if we can add to existing group
	addToExistingGroup := parent != nil && parent.Split == splitDir && ratiosAreEqual(parent.SplitRatios)
	log.Printf("SplitActive: addToExistingGroup=%v", addToExistingGroup)

	if addToExistingGroup {
		// CASE 1: Add to existing, equally-sized group.
		log.Printf("SplitActive: Adding to existing group")
		newNode := &Node{Parent: parent, Pane: newPane}
		parent.Children = append(parent.Children, newNode)

		// Re-balance the ratios equally among all children.
		numChildren := len(parent.Children)
		equalRatio := 1.0 / float64(numChildren)
		parent.SplitRatios = make([]float64, numChildren)
		for i := range parent.SplitRatios {
			parent.SplitRatios[i] = equalRatio
		}
		newActiveNode = newNode
		log.Printf("SplitActive: Added to existing group, now %d children with ratio %.3f each",
			numChildren, equalRatio)

		// Animate new pane from minimum size to 1 (full weight)
		// Start from 0.15 instead of 0 to ensure pane has some drawable area
		if t.layoutAnimator != nil && t.animationEnabled {
			now := time.Now()
			log.Printf("SplitActive: Starting animation for new pane (adding to group)")
			t.layoutAnimator.AnimatePaneWeightWithDuration(newPane.id, 0.15, 0, now)  // Instant: set to 15%
			log.Printf("SplitActive: Set new pane weight to 0.15 (instant)")
			t.layoutAnimator.AnimatePaneWeight(newPane.id, 1.0, now)              // Animate to 1
			log.Printf("SplitActive: Started animation 0.15→1 for new pane")
		}

	} else {
		// CASE 2: Split the current pane into a new group of two.
		log.Printf("SplitActive: Creating new split group")
		originalPane := nodeToModify.Pane
		log.Printf("SplitActive: Original pane: '%s'", originalPane.getTitle())

		nodeToModify.Pane = nil // The leaf becomes an internal node.
		nodeToModify.Split = splitDir
		nodeToModify.SplitRatios = []float64{0.5, 0.5}

		child1 := &Node{Parent: nodeToModify, Pane: originalPane}
		child2 := &Node{Parent: nodeToModify, Pane: newPane}
		nodeToModify.Children = []*Node{child1, child2}
		newActiveNode = child2

		log.Printf("SplitActive: Created new %s split group:", splitDirStr)
		log.Printf("  - Child 1: pane '%s'", child1.Pane.getTitle())
		log.Printf("  - Child 2: pane '%s'", child2.Pane.getTitle())
		log.Printf("  - Ratios: %v", nodeToModify.SplitRatios)

		// Animate new pane from minimum size to 1 (full weight)
		// Start from 0.15 instead of 0 to ensure pane has some drawable area
		// Original pane keeps weight 1 (no animation needed)
		if t.layoutAnimator != nil && t.animationEnabled {
			now := time.Now()
			log.Printf("SplitActive: Starting animation for new pane (new split group)")
			t.layoutAnimator.AnimatePaneWeightWithDuration(newPane.id, 0.15, 0, now)  // Instant: set to 15%
			log.Printf("SplitActive: Set new pane weight to 0.15 (instant)")
			t.layoutAnimator.AnimatePaneWeight(newPane.id, 1.0, now)              // Animate to 1
			log.Printf("SplitActive: Started animation 0.15→1 for new pane")
		}
	}

	t.ActiveLeaf = newActiveNode
	if t.ActiveLeaf.Pane != nil {
		t.ActiveLeaf.Pane.IsActive = true
	}

	log.Printf("SplitActive: New active leaf is pane '%s'", t.ActiveLeaf.Pane.getTitle())

	// Debug: traverse the tree to see the final structure
	log.Printf("SplitActive: Final tree structure:")
	t.debugPrintTree(t.Root, 0)

	return newActiveNode
}

// CloseActiveLeaf closes the active pane and returns the next pane to be
// activated.
func (t *Tree) CloseActiveLeaf() *Node {
	leaf := t.ActiveLeaf
	if leaf == nil || leaf.Parent == nil {
		// Don't close the root pane
		return t.ActiveLeaf
	}
	if leaf.Pane != nil {
		leaf.Pane.IsActive = false
	}

	parent := leaf.Parent
	// Find the index of the leaf being closed
	childIndex := -1
	for i, child := range parent.Children {
		if child == leaf {
			childIndex = i
			break
		}
	}
	if childIndex == -1 {
		return t.ActiveLeaf
	} // Should not happen

	// Remove the child from the parent's slice
	parent.Children = append(parent.Children[:childIndex], parent.Children[childIndex+1:]...)

	// If the parent has only one child left, the split is no longer needed.
	// Promote the remaining child to replace its parent.
	var nextActiveNode *Node
	if len(parent.Children) == 1 {
		remainingChild := parent.Children[0]
		grandparent := parent.Parent
		remainingChild.Parent = grandparent

		if grandparent == nil {
			t.Root = remainingChild
		} else {
			// Find parent's index in grandparent's children and replace it
			for i, child := range grandparent.Children {
				if child == parent {
					grandparent.Children[i] = remainingChild
					break
				}
			}
		}
		nextActiveNode = t.findFirstLeaf(remainingChild)
	} else {
		// Otherwise, set focus to the previous sibling, or the new last one if we closed the first.
		newIndex := childIndex
		if newIndex >= len(parent.Children) {
			newIndex = len(parent.Children) - 1
		}
		nextActiveNode = t.findFirstLeaf(parent.Children[newIndex])
	}

	// Clean up animation state for the closed pane
	if t.layoutAnimator != nil && leaf.Pane != nil {
		t.layoutAnimator.Reset(leaf.Pane.id)
	}

	leaf.Pane.Close() // Ensure the closed app is stopped
	t.ActiveLeaf = nextActiveNode
	if t.ActiveLeaf.Pane != nil {
		t.ActiveLeaf.Pane.IsActive = true
	}
	return t.ActiveLeaf
}

// SwapActivePane swaps the active pane with its neighbor in the given direction.
func (t *Tree) SwapActivePane(d Direction) {
	neighbor := t.findNeighbor(d)
	if neighbor == nil {
		return
	}
	// Swap the panes within the leaves
	t.ActiveLeaf.Pane, neighbor.Pane = neighbor.Pane, t.ActiveLeaf.Pane
	// The neighbor is now the active pane, but the active *leaf* is still the same.
	// We need to move the active leaf pointer.
	t.MoveActive(d)
}

// MoveActive moves the active pane in the given direction.
func (t *Tree) MoveActive(d Direction) {
	target := t.findNeighbor(d)
	if target != nil {
		if t.ActiveLeaf.Pane != nil {
			t.ActiveLeaf.Pane.IsActive = false
		}
		t.ActiveLeaf = target
		if t.ActiveLeaf.Pane != nil {
			t.ActiveLeaf.Pane.IsActive = true
		}
	}
}

// Traverse traverses the tree and calls the given function for each node.
func (t *Tree) Traverse(f func(*Node)) {
	t.traverse(t.Root, f)
}

// GetActiveTitle returns the title of the active application.
func (t *Tree) GetActiveTitle() string {
	if t.ActiveLeaf != nil && t.ActiveLeaf.Pane != nil && t.ActiveLeaf.Pane.app != nil {
		return t.ActiveLeaf.Pane.app.GetTitle()
	}
	return ""
}

// findNeighbor finds the neighbor of the active leaf in the given direction.
func (t *Tree) findNeighbor(d Direction) *Node {
	curr := t.ActiveLeaf
	for curr.Parent != nil {
		parent := curr.Parent

		// Find our index in the parent's children list
		myIndex := -1
		for i, child := range parent.Children {
			if child == curr {
				myIndex = i
				break
			}
		}
		if myIndex == -1 {
			return nil
		} // Should not happen

		// Check for neighbors based on direction and parent's split type
		switch d {
		case DirRight:
			if parent.Split == Vertical && myIndex+1 < len(parent.Children) {
				return t.findFirstLeaf(parent.Children[myIndex+1])
			}
		case DirLeft:
			if parent.Split == Vertical && myIndex-1 >= 0 {
				return t.findFirstLeaf(parent.Children[myIndex-1])
			}
		case DirDown:
			if parent.Split == Horizontal && myIndex+1 < len(parent.Children) {
				return t.findFirstLeaf(parent.Children[myIndex+1])
			}
		case DirUp:
			if parent.Split == Horizontal && myIndex-1 >= 0 {
				return t.findFirstLeaf(parent.Children[myIndex-1])
			}
		}

		// If we couldn't find a direct neighbor, move up the tree
		curr = parent
	}
	return nil
}

// findFirstLeaf finds the first leaf node in the given subtree.
func (t *Tree) findFirstLeaf(node *Node) *Node {
	if node == nil {
		return nil
	}
	curr := node
	// While the current node is not a leaf, descend to the first child.
	for len(curr.Children) > 0 {
		curr = curr.Children[0]
	}
	return curr
}

// FindNodeWithPane returns the first node whose pane matches the provided pane pointer.
func (t *Tree) FindNodeWithPane(target *pane) *Node {
	if t == nil || target == nil {
		return nil
	}
	var result *Node
	t.Traverse(func(n *Node) {
		if result != nil || n == nil {
			return
		}
		if n.Pane == target {
			result = n
		}
	})
	return result
}

// NodeBounds returns the absolute bounds covered by the provided node.
// Coordinates are [x0,y0) to [x1,y1), matching pane dimensions.
func (t *Tree) NodeBounds(node *Node) (int, int, int, int, bool) {
	if node == nil {
		return 0, 0, 0, 0, false
	}
	if node.Pane != nil {
		return node.Pane.absX0, node.Pane.absY0, node.Pane.absX1, node.Pane.absY1, true
	}

	hasBounds := false
	var minX, minY, maxX, maxY int
	for _, child := range node.Children {
		cx0, cy0, cx1, cy1, ok := t.NodeBounds(child)
		if !ok {
			continue
		}
		if !hasBounds {
			minX, minY, maxX, maxY = cx0, cy0, cx1, cy1
			hasBounds = true
			continue
		}
		if cx0 < minX {
			minX = cx0
		}
		if cy0 < minY {
			minY = cy0
		}
		if cx1 > maxX {
			maxX = cx1
		}
		if cy1 > maxY {
			maxY = cy1
		}
	}
	if !hasBounds {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX, maxY, true
}

// findParentOf finds the parent of the given node.
func (t *Tree) findParentOf(current, parent, target *Node) *Node {
	if current == nil {
		return nil
	}
	if current == target {
		return parent
	}
	for _, child := range current.Children {
		if found := t.findParentOf(child, current, target); found != nil {
			return found
		}
	}
	return nil
}

// traverse is the recursive helper for Traverse.
func (t *Tree) traverse(n *Node, f func(*Node)) {
	if n == nil {
		return
	}
	f(n)
	// Loop over the children slice instead of Left/Right
	for _, child := range n.Children {
		t.traverse(child, f)
	}
}

// Resize recalculates the dimensions of all panes in the tree.
// Also add debugging to the main Resize method:
func (t *Tree) Resize(x, y, w, h int) {
	log.Printf("Tree.Resize: Setting root to (%d,%d) size %dx%d", x, y, w, h)

	// Update layout animations before computing dimensions
	if t.layoutAnimator != nil {
		t.layoutAnimator.Update(time.Now())
	}

	if t.Root != nil {
		t.resizeNode(t.Root, x, y, w, h)
	} else {
		log.Printf("Tree.Resize: Root is nil!")
	}
}

// getPaneIDForNode returns the pane ID if this node is a leaf, nil otherwise.
func (t *Tree) getPaneIDForNode(n *Node) *[16]byte {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		return &n.Pane.id
	}
	return nil
}

// resizeNode is the recursive helper for Resize.
func (t *Tree) resizeNode(n *Node, x, y, w, h int) {
	if n == nil {
		log.Printf("resizeNode: node is nil")
		return
	}

	log.Printf("resizeNode: node at (%d,%d) size %dx%d, hasPane=%v, numChildren=%d",
		x, y, w, h, n.Pane != nil, len(n.Children))

	if len(n.Children) == 0 && n.Pane != nil {
		log.Printf("resizeNode: Setting pane '%s' dimensions to (%d,%d)-(%d,%d)",
			n.Pane.getTitle(), x, y, x+w, y+h)
		n.Pane.setDimensions(x, y, x+w, y+h)
		// This is the crucial fix: invalidate the previous buffer to force a full redraw.
		n.Pane.prevBuf = nil
		return
	}

	numChildren := len(n.Children)
	if numChildren == 0 || len(n.SplitRatios) != numChildren {
		log.Printf("resizeNode: Invalid internal node - numChildren=%d, numRatios=%d",
			numChildren, len(n.SplitRatios))
		return // Not a valid internal node
	}

	log.Printf("resizeNode: Internal node with %d children, split=%v, ratios=%v",
		numChildren, n.Split, n.SplitRatios)

	// Compute effective ratios with animation factors
	effectiveRatios := make([]float64, numChildren)
	totalEffective := 0.0
	for i, child := range n.Children {
		weightFactor := 1.0
		// Get animation weight factor for this child's pane (if it has one)
		if t.layoutAnimator != nil {
			if leafPaneID := t.getPaneIDForNode(child); leafPaneID != nil {
				weightFactor = t.layoutAnimator.GetPaneWeightFactorCached(*leafPaneID)
				if weightFactor != 1.0 {
					log.Printf("resizeNode: Child %d has animation weight factor %.3f", i, weightFactor)
				}
			}
		}
		effectiveRatios[i] = n.SplitRatios[i] * weightFactor
		totalEffective += effectiveRatios[i]
	}

	// Normalize effective ratios to sum to 1.0
	if totalEffective > 0 {
		for i := range effectiveRatios {
			effectiveRatios[i] /= totalEffective
		}
	} else {
		// All weights are 0 (all panes animating out) - distribute equally
		equalRatio := 1.0 / float64(numChildren)
		for i := range effectiveRatios {
			effectiveRatios[i] = equalRatio
		}
	}

	if n.Split == Vertical {
		log.Printf("resizeNode: Processing vertical split (effective ratios: %v)", effectiveRatios)
		currentX := x
		for i, child := range n.Children {
			childW := int(float64(w) * effectiveRatios[i])
			if i == numChildren-1 {
				childW = w - (currentX - x)
			}
			log.Printf("resizeNode: Child %d gets (%d,%d) size %dx%d", i, currentX, y, childW, h)
			t.resizeNode(child, currentX, y, childW, h)
			currentX += childW
		}
	} else { // Horizontal
		log.Printf("resizeNode: Processing horizontal split (effective ratios: %v)", effectiveRatios)
		currentY := y
		for i, child := range n.Children {
			childH := int(float64(h) * effectiveRatios[i])
			if i == numChildren-1 {
				childH = h - (currentY - y)
			}
			log.Printf("resizeNode: Child %d gets (%d,%d) size %dx%d", i, x, currentY, w, childH)
			t.resizeNode(child, x, currentY, w, childH)
			currentY += childH
		}
	}
}

func (t *Tree) debugPrintTree(node *Node, depth int) {
	if node == nil {
		return
	}

	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	if node.Pane != nil {
		log.Printf("%sLeaf: '%s' (active=%v)", indent, node.Pane.getTitle(), node.Pane.IsActive)
	} else {
		splitStr := "Vertical"
		if node.Split == Horizontal {
			splitStr = "Horizontal"
		}
		log.Printf("%sInternal: %s split, %d children, ratios=%v",
			indent, splitStr, len(node.Children), node.SplitRatios)
		for i, child := range node.Children {
			log.Printf("%s  Child %d:", indent, i)
			t.debugPrintTree(child, depth+2)
		}
	}
}

// FindLeafAt returns the leaf node whose pane contains the provided coordinates.
func (t *Tree) FindLeafAt(x, y int) *Node {
	return findLeafAt(t.Root, x, y)
}

func findLeafAt(node *Node, x, y int) *Node {
	if node == nil {
		return nil
	}
	if node.Pane != nil {
		if node.Pane.contains(x, y) {
			return node
		}
		return nil
	}
	for _, child := range node.Children {
		if hit := findLeafAt(child, x, y); hit != nil {
			return hit
		}
	}
	return nil
}
