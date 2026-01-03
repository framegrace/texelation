package layout

import "texelation/texelui/core"

// HBox arranges widgets horizontally with optional spacing.
// Each widget is positioned from left to right, using its current width.
type HBox struct {
	Spacing int // Horizontal spacing between widgets in cells
}

// Apply positions children horizontally within the container.
// Children are positioned from left to right, using their current widths.
// If a child exceeds the container height, it will be clipped by the painter.
func (h HBox) Apply(container core.Rect, children []core.Widget) {
	x := container.X
	spacing := h.Spacing
	if spacing < 0 {
		spacing = 0
	}

	for i, child := range children {
		// Position child at current X
		child.SetPosition(x, container.Y)

		// Get child's current width
		w, _ := child.Size()

		// Move X right by child width plus spacing (except for last child)
		x += w
		if i < len(children)-1 {
			x += spacing
		}

		// Stop if we've exceeded the container
		if x >= container.X+container.W {
			break
		}
	}
}

// NewHBox creates a horizontal box layout with the specified spacing.
func NewHBox(spacing int) *HBox {
	return &HBox{Spacing: spacing}
}

// GetNeighbors implements core.NeighborLayout.
// For HBox, widgets have left/right neighbors based on their position in the list.
func (h HBox) GetNeighbors(child core.Widget) core.NeighborInfo {
	return h.GetNeighborsInChildren(child, nil)
}

// GetNeighborsInChildren returns neighbor info for a child within a given children slice.
// If children is nil, returns empty NeighborInfo (used when Apply hasn't been called yet).
func (h HBox) GetNeighborsInChildren(child core.Widget, children []core.Widget) core.NeighborInfo {
	if children == nil || len(children) == 0 {
		return core.NeighborInfo{}
	}

	for i, c := range children {
		if c == child {
			return core.NeighborInfo{
				Top:    false,
				Bottom: false,
				Left:   i > 0,
				Right:  i < len(children)-1,
			}
		}
	}
	return core.NeighborInfo{}
}
