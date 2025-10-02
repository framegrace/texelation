package texel

import (
	"crypto/sha1"
	"fmt"
)

// PaneSnapshot captures the render buffer for a pane along with a stable ID.
type PaneSnapshot struct {
	ID     [16]byte
	Title  string
	Buffer [][]Cell
}

// SnapshotBuffers collects the current buffers for all panes in the active workspace.
func (d *Desktop) SnapshotBuffers() []PaneSnapshot {
	if d.activeWorkspace == nil || d.activeWorkspace.tree == nil {
		return nil
	}

	snapshots := make([]PaneSnapshot, 0)
	d.activeWorkspace.tree.Traverse(func(n *Node) {
		if n == nil || n.Pane == nil || n.Pane.app == nil {
			return
		}
		buf := n.Pane.Render()
		var id [16]byte
		sum := sha1.Sum([]byte(fmt.Sprintf("%p", n.Pane)))
		copy(id[:], sum[:16])
		snapshots = append(snapshots, PaneSnapshot{
			ID:     id,
			Title:  n.Pane.getTitle(),
			Buffer: buf,
		})
	})
	return snapshots
}
