package core

import "github.com/gdamore/tcell/v2"

// Rect describes a position and size in cells.
type Rect struct {
	X, Y int
	W, H int
}

func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// Style wraps a tcell.Style for convenience if we later want extensions.
type Style = tcell.Style
