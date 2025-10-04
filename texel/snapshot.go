package texel

// PaneSnapshot captures the render buffer for a pane along with a stable ID.
type PaneSnapshot struct {
	ID     [16]byte
	Title  string
	Buffer [][]Cell
	Rect   Rectangle
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
func (d *Desktop) SnapshotBuffers() []PaneSnapshot {
	capture := d.CaptureTree()
	return capture.Panes
}

// CaptureTree gathers panes and the layout tree for persistence or transport.
func (d *Desktop) CaptureTree() TreeCapture {
	var capture TreeCapture
	if d.activeWorkspace == nil || d.activeWorkspace.tree == nil || d.activeWorkspace.tree.Root == nil {
		return capture
	}
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
	return capture
}

func capturePaneSnapshot(p *pane) PaneSnapshot {
	buf := p.Render()
	id := p.ID()
	return PaneSnapshot{
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
