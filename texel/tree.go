package texel

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
	Root       *Node
	ActiveLeaf *Node
}

// NewTree creates a new layout tree.
func NewTree() *Tree {
	return &Tree{}
}

// SetRoot sets the root of the tree to a single node containing the given pane.
func (t *Tree) SetRoot(p *pane) {
	leaf := &Node{
		Pane: p,
	}
	t.Root = leaf
	t.ActiveLeaf = leaf
}

// SplitActive splits the active leaf node, creating a new empty pane.
// It returns the new node containing the empty pane.
func (t *Tree) SplitActive(splitDir SplitType, newPane *pane) *Node {
	if t.ActiveLeaf == nil {
		return nil
	}

	nodeToModify := t.ActiveLeaf
	parent := t.findParentOf(t.Root, nil, nodeToModify)

	// Create a new empty pane that will be attached later.
	var newActiveNode *Node

	// CASE 1: Adding another pane to an existing group.
	if parent != nil && parent.Split == splitDir {
		newNode := &Node{Parent: parent, Pane: newPane}
		parent.Children = append(parent.Children, newNode)

		numChildren := len(parent.Children)
		equalRatio := 1.0 / float64(numChildren)
		parent.SplitRatios = make([]float64, numChildren)
		for i := range parent.SplitRatios {
			parent.SplitRatios[i] = equalRatio
		}
		newActiveNode = newNode

	} else {
		// CASE 2: Splitting a single pane for the first time.
		originalPane := nodeToModify.Pane
		nodeToModify.Pane = nil
		nodeToModify.Split = splitDir
		nodeToModify.SplitRatios = []float64{0.5, 0.5}

		child1 := &Node{Parent: nodeToModify, Pane: originalPane}
		child2 := &Node{Parent: nodeToModify, Pane: newPane}
		nodeToModify.Children = []*Node{child1, child2}
		newActiveNode = child2
	}

	t.ActiveLeaf = newActiveNode
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

	leaf.Pane.Close() // Ensure the closed app is stopped
	t.ActiveLeaf = nextActiveNode
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
	// Set the new active pane
	t.ActiveLeaf = neighbor
}

// MoveActive moves the active pane in the given direction.
func (t *Tree) MoveActive(d Direction) {
	target := t.findNeighbor(d)
	if target != nil {
		t.ActiveLeaf = target
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
func (t *Tree) Resize(x, y, w, h int) {
	if t.Root != nil {
		t.resizeNode(t.Root, x, y, w, h)
	}
}

// resizeNode is the recursive helper for Resize.
func (t *Tree) resizeNode(n *Node, x, y, w, h int) {
	if n == nil {
		return
	}

	if len(n.Children) == 0 && n.Pane != nil {
		n.Pane.setDimensions(x, y, x+w, y+h)
		n.Pane.prevBuf = nil
		//if n.Pane.app != nil {
		//	n.Pane.app.Resize(w, h)
		//	}
		return
	}

	numChildren := len(n.Children)
	if numChildren == 0 || len(n.SplitRatios) != numChildren {
		return // Not a valid internal node
	}

	if n.Split == Vertical {
		currentX := x
		for i, child := range n.Children {
			childW := int(float64(w) * n.SplitRatios[i])
			if i == numChildren-1 {
				childW = w - (currentX - x)
			}
			t.resizeNode(child, currentX, y, childW, h)
			currentX += childW
		}
	} else { // Horizontal
		currentY := y
		for i, child := range n.Children {
			childH := int(float64(h) * n.SplitRatios[i])
			if i == numChildren-1 {
				childH = h - (currentY - y)
			}
			t.resizeNode(child, x, currentY, w, childH)
			currentY += childH
		}
	}
}
