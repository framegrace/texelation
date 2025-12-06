package core

// Layout positions children within a container rect.
type Layout interface {
	Apply(container Rect, children []Widget)
}
