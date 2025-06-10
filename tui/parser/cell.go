package parser

import "github.com/nsf/termbox-go"

// Attribute defines a single text attribute.
type Attribute uint16

// Color defines a text color. For now, it maps directly to termbox colors.
type Color termbox.Attribute

const (
	// Basic 8 ANSI colors
	ColorDefault Color = Color(termbox.ColorDefault)
	ColorBlack   Color = Color(termbox.ColorBlack)
	ColorRed     Color = Color(termbox.ColorRed)
	ColorGreen   Color = Color(termbox.ColorGreen)
	ColorYellow  Color = Color(termbox.ColorYellow)
	ColorBlue    Color = Color(termbox.ColorBlue)
	ColorMagenta Color = Color(termbox.ColorMagenta)
	ColorCyan    Color = Color(termbox.ColorCyan)
	ColorWhite   Color = Color(termbox.ColorWhite)
)

const (
	AttrBold Attribute = 1 << iota
	AttrUnderline
	AttrReverse
)

// Cell represents a single character cell on the screen.
type Cell struct {
	Rune rune
	FG   Color
	BG   Color
	Attr Attribute
}
