package core

// Layout positions children within a container rect.
type Layout interface {
	Apply(container Rect, children []Widget)
}

// NeighborInfo describes which edges of a widget have neighboring widgets.
// This is used by border widgets to determine separator rendering.
type NeighborInfo struct {
	Top    bool // Has neighbor above
	Bottom bool // Has neighbor below
	Left   bool // Has neighbor to the left
	Right  bool // Has neighbor to the right
}

// NeighborLayout extends Layout with neighbor detection capabilities.
// This allows layouts to provide information about which widgets are
// adjacent to each other, enabling smart border rendering.
type NeighborLayout interface {
	Layout
	// GetNeighbors returns neighbor information for the given child widget.
	// Returns empty NeighborInfo if the child is not found in this layout.
	GetNeighbors(child Widget) NeighborInfo
}
