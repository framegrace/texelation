package layout

import "texelation/texelui/core"

// VBox arranges widgets vertically with optional spacing.
// Each widget is positioned from top to bottom, using its current height.
type VBox struct {
	Spacing int // Vertical spacing between widgets in cells
}

// Apply positions children vertically within the container.
// Children are positioned from top to bottom, using their current heights.
// If a child exceeds the container width, it will be clipped by the painter.
func (v VBox) Apply(container core.Rect, children []core.Widget) {
	y := container.Y
	spacing := v.Spacing
	if spacing < 0 {
		spacing = 0
	}

	for i, child := range children {
		// Position child at current Y
		child.SetPosition(container.X, y)

		// Get child's current height
		_, h := child.Size()

		// Move Y down by child height plus spacing (except for last child)
		y += h
		if i < len(children)-1 {
			y += spacing
		}

		// Stop if we've exceeded the container
		if y >= container.Y+container.H {
			break
		}
	}
}

// NewVBox creates a vertical box layout with the specified spacing.
func NewVBox(spacing int) *VBox {
	return &VBox{Spacing: spacing}
}

// GetNeighbors implements core.NeighborLayout.
// For VBox, widgets have top/bottom neighbors based on their position in the list.
func (v VBox) GetNeighbors(child core.Widget) core.NeighborInfo {
	return v.GetNeighborsInChildren(child, nil)
}

// GetNeighborsInChildren returns neighbor info for a child within a given children slice.
// If children is nil, returns empty NeighborInfo (used when Apply hasn't been called yet).
func (v VBox) GetNeighborsInChildren(child core.Widget, children []core.Widget) core.NeighborInfo {
	if children == nil || len(children) == 0 {
		return core.NeighborInfo{}
	}

	for i, c := range children {
		if c == child {
			return core.NeighborInfo{
				Top:    i > 0,
				Bottom: i < len(children)-1,
				Left:   false,
				Right:  false,
			}
		}
	}
	return core.NeighborInfo{}
}
