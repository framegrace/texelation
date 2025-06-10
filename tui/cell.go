package tui

import "github.com/nsf/termbox-go"

// Cell represents a single character cell on the terminal screen,
// with a character (ch), foreground color (fg), and background color (bg).
type Cell struct {
	Ch rune
	Fg termbox.Attribute
	Bg termbox.Attribute
}
