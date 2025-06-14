package texel

import "github.com/gdamore/tcell/v2"

// Cell represents a single character cell on the terminal screen.
// It now uses tcell.Style to handle all formatting.
type Cell struct {
	Ch    rune
	Style tcell.Style
}
