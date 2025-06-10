package parser

import (
	"log"
)

// VTerm and other structs remain the same...
type VTerm struct {
	width, height    int
	cursorX, cursorY int
	grid             [][]Cell
	currentFG        Color
	currentBG        Color
	currentAttr      Attribute
	tabStops         map[int]bool
	cursorVisible    bool
	TitleChanged     func(string)
}

// ProcessCSI is the main entry point for handling a parsed CSI sequence.
func (v *VTerm) ProcessCSI(command byte, params []int, private bool) {
	if private {
		v.processPrivateCSI(command, params)
		return
	}

	mode := 0
	if len(params) > 0 {
		mode = params[0]
	}

	switch command {
	case 'm': // Select Graphic Rendition (SGR)
		if len(params) == 0 {
			params = []int{0}
		}
		for _, param := range params {
			switch {
			case param == 0:
				v.ResetAttributes()
			case param == 1:
				v.SetAttribute(AttrBold)
			case param == 4:
				v.SetAttribute(AttrUnderline)
			case param == 7:
				v.SetAttribute(AttrReverse)
			// Standard foreground colors
			case param >= 30 && param <= 37:
				v.SetForegroundColor(Color(param - 30))
			// Standard background colors
			case param >= 40 && param <= 47:
				v.SetBackgroundColor(Color(param - 40))
			// High-intensity foreground colors
			case param >= 90 && param <= 97:
				v.SetAttribute(AttrBold) // Termbox simulates bright colors with bold
				v.SetForegroundColor(Color(param - 90))
			// High-intensity background colors
			case param >= 100 && param <= 107:
				v.SetBackgroundColor(Color(param - 100))
			}
		}
	case 'H', 'f':
		row, col := 1, 1
		if len(params) > 0 && params[0] != 0 {
			row = params[0]
		}
		if len(params) > 1 && params[1] != 0 {
			col = params[1]
		}
		v.SetCursorPos(row-1, col-1)
	case 'J': // Erase in Display
		switch mode {
		case 0:
			v.ClearToEndOfScreen()
		case 2:
			v.ClearScreen()
			v.SetCursorPos(0, 0)
		}
	case 'K': // Erase in Line
		v.ClearLine(mode)
	case 'g': // Tab Clear
		if mode == 3 {
			v.ClearAllTabStops()
		}
	case 'c': // Send Device Attributes
		log.Println("Parser: Ignoring device attribute request (0c)")
	}
}

// --- The rest of the file is unchanged. I've omitted it for brevity. ---
// NewVTerm(), Grid(), Cursor(), placeChar(), etc. all remain the same.

// Option is a functional option for configuring a VTerm.
type Option func(*VTerm)

// WithTitleChangeHandler returns an option that sets a callback for when the terminal title changes.
func WithTitleChangeHandler(handler func(string)) Option {
	return func(v *VTerm) {
		v.TitleChanged = handler
	}
}
func NewVTerm(width, height int, opts ...Option) *VTerm {
	v := &VTerm{
		width:         width,
		height:        height,
		grid:          make([][]Cell, height),
		currentFG:     ColorDefault,
		currentBG:     ColorDefault,
		tabStops:      make(map[int]bool),
		cursorVisible: true,
	}
	for _, opt := range opts {
		opt(v)
	}
	for i := range v.grid {
		v.grid[i] = make([]Cell, width)
	}
	v.ClearScreen()
	for i := 0; i < width; i++ {
		if i%8 == 0 {
			v.tabStops[i] = true
		}
	}
	return v
}
func (v *VTerm) SetTitle(title string) {
	if v.TitleChanged != nil {
		v.TitleChanged(title)
	}
}
func (v *VTerm) Grid() [][]Cell                { return v.grid }
func (v *VTerm) Cursor() (int, int)            { return v.cursorX, v.cursorY }
func (v *VTerm) CursorVisible() bool           { return v.cursorVisible }
func (v *VTerm) SetCursorVisible(visible bool) { v.cursorVisible = visible }
func (v *VTerm) placeChar(r rune) {
	if v.cursorX >= v.width {
		v.cursorX = 0
		v.LineFeed()
	}
	if v.cursorY >= v.height {
		v.cursorY = v.height - 1
	}
	v.grid[v.cursorY][v.cursorX] = Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}
	v.cursorX++
}
func (v *VTerm) scrollUp() {
	copy(v.grid[0:], v.grid[1:])
	newLine := make([]Cell, v.width)
	for i := range newLine {
		newLine[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
	}
	v.grid[v.height-1] = newLine
}
func (v *VTerm) SetForegroundColor(c Color) { v.currentFG = c }
func (v *VTerm) SetBackgroundColor(c Color) { v.currentBG = c }
func (v *VTerm) SetAttribute(a Attribute)   { v.currentAttr |= a }
func (v *VTerm) ResetAttributes() {
	v.currentFG = ColorDefault
	v.currentBG = ColorDefault
	v.currentAttr = 0
}
func (v *VTerm) SetCursorPos(row, col int) {
	if row < 0 {
		row = 0
	}
	if row >= v.height {
		row = v.height - 1
	}
	if col < 0 {
		col = 0
	}
	if col >= v.width {
		col = v.width - 1
	}
	v.cursorY, v.cursorX = row, col
}
func (v *VTerm) ClearScreen() {
	for y := 0; y < v.height; y++ {
		for x := 0; x < v.width; x++ {
			v.grid[y][x] = Cell{Rune: ' ', FG: ColorDefault, BG: ColorDefault}
		}
	}
}
func (v *VTerm) ClearLine(mode int) {
	start, end := 0, 0
	switch mode {
	case 0:
		start, end = v.cursorX, v.width-1
	case 1:
		start, end = 0, v.cursorX
	case 2:
		start, end = 0, v.width-1
	}
	for x := start; x <= end && x < v.width; x++ {
		v.grid[v.cursorY][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
	}
}
func (v *VTerm) LineFeed() {
	v.cursorY++
	if v.cursorY >= v.height {
		v.cursorY = v.height - 1
		v.scrollUp()
	}
}
func (v *VTerm) CarriageReturn() { v.cursorX = 0 }
func (v *VTerm) Backspace() {
	if v.cursorX > 0 {
		v.cursorX--
	}
}
func (v *VTerm) Tab() {
	for x := v.cursorX + 1; x < v.width; x++ {
		if v.tabStops[x] {
			v.cursorX = x
			return
		}
	}
	v.cursorX = v.width - 1
}
func (v *VTerm) ClearAllTabStops() { v.tabStops = make(map[int]bool) }
func (v *VTerm) processPrivateCSI(command byte, params []int) {
	if len(params) == 0 {
		return
	}
	mode := params[0]
	switch command {
	case 'h':
		switch mode {
		case 1:
			log.Println("Parser: Ignoring set cursor key application mode (1h)")
		case 25:
			v.SetCursorVisible(true)
		case 1049:
			log.Println("Parser: Ignoring set alternate screen buffer (1049h)")
		case 2004:
			log.Println("Parser: Ignoring set bracketed paste mode (2004h)")
		}
	case 'l':
		switch mode {
		case 1:
			log.Println("Parser: Ignoring reset cursor key application mode (1l)")
		case 25:
			v.SetCursorVisible(false)
		case 1049:
			log.Println("Parser: Ignoring reset alternate screen buffer (1049l)")
		case 2004:
			log.Println("Parser: Ignoring reset bracketed paste mode (2004l)")
		}
	}
}
func (v *VTerm) ClearToEndOfScreen() {
	v.ClearLine(0)
	for y := v.cursorY + 1; y < v.height; y++ {
		for x := 0; x < v.width; x++ {
			v.grid[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}
}
