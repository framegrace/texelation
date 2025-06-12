package parser

import (
	"fmt"
	"log"
)

// WithPtyWriter returns an option that sets a callback for writing data back to the PTY.

// VTerm holds the state of a virtual terminal.
type VTerm struct {
	width, height              int
	cursorX, cursorY           int
	savedCursorX, savedCursorY int
	grid                       [][]Cell
	currentFG, currentBG       Color
	currentAttr                Attribute
	tabStops                   map[int]bool
	cursorVisible              bool
	wrapNext                   bool
	autoWrapMode               bool
	TitleChanged               func(string)
	WriteToPty                 func([]byte)
	marginTop, marginBottom    int
}

// NewVTerm creates and initializes a new virtual terminal.
func NewVTerm(width, height int, opts ...Option) *VTerm {
	v := &VTerm{
		width:         width,
		height:        height,
		grid:          make([][]Cell, height),
		currentFG:     DefaultFG,
		currentBG:     DefaultBG,
		tabStops:      make(map[int]bool),
		wrapNext:      false,
		cursorVisible: true,
		autoWrapMode:  true,
		marginTop:     0,          // Default margin is top row
		marginBottom:  height - 1, // Default margin is bottom row
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

func (v *VTerm) Resize(width, height int) {
	if width == v.width && height == v.height {
		return
	}

	// Create a new grid of the correct size, filled with default cells
	newGrid := make([][]Cell, height)
	for y := range newGrid {
		newGrid[y] = make([]Cell, width)
		for x := range newGrid[y] {
			// Initialize with the default background color, not the current one.
			newGrid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	// Copy the old content into the new grid
	rowsToCopy := min(v.height, height)
	colsToCopy := min(v.width, width)

	for y := 0; y < rowsToCopy; y++ {
		copy(newGrid[y][:colsToCopy], v.grid[y][:colsToCopy])
	}

	v.grid = newGrid
	v.width = width
	v.height = height

	// Clamp the bottom margin in case the screen has shrunk.
	if v.marginBottom >= v.height {
		v.marginBottom = v.height - 1
	}

	// Clamp cursor position to new bounds
	v.SetCursorPos(v.cursorY, v.cursorX)
}

// SetMargins defines the active scrolling region.
func (v *VTerm) SetMargins(top, bottom int) {
	// ANSI coordinates are 1-based.
	if top == 0 {
		top = 1
	}
	if bottom == 0 {
		bottom = v.height
	}

	// Clamp to screen size
	if top < 1 {
		top = 1
	}
	if bottom > v.height {
		bottom = v.height
	}
	if top >= bottom {
		return
	} // Invalid region

	v.marginTop = top - 1
	v.marginBottom = bottom - 1
	v.SetCursorPos(0, 0) // Per spec, move cursor to home on change
}

// EraseCharacters overwrites N characters from the cursor with space.
func (v *VTerm) EraseCharacters(n int) {
	for i := 0; i < n; i++ {
		if v.cursorX+i < v.width {
			v.grid[v.cursorY][v.cursorX+i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}
}

// DeleteCharacters deletes N characters, shifting the rest of the line left.
func (v *VTerm) DeleteCharacters(n int) {
	line := v.grid[v.cursorY]
	end := v.width
	start := v.cursorX

	copy(line[start:], line[start+n:])

	// Clear the newly empty space at the end of the line
	for i := end - n; i < end; i++ {
		line[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
	}
}

func (v *VTerm) scrollUp() {
	copy(v.grid[v.marginTop:], v.grid[v.marginTop+1:v.marginBottom+1])
	newLine := make([]Cell, v.width)
	for i := range newLine {
		newLine[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
	}
	v.grid[v.marginBottom] = newLine
}

func (v *VTerm) scrollDown(n int) {
	// Shift lines down within the scrolling region
	for i := 0; i < n; i++ {
		copy(v.grid[v.marginTop+1:v.marginBottom+1], v.grid[v.marginTop:v.marginBottom])
		// Clear the new top line of the region
		newLine := make([]Cell, v.width)
		for j := range newLine {
			newLine[j] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
		v.grid[v.marginTop] = newLine
	}
}

// --- NEW METHODS ---

// MoveCursorUp moves the cursor n positions up.
func (v *VTerm) MoveCursorUp(n int) {
	v.wrapNext = false
	v.cursorY -= n
	if v.cursorY < v.marginTop { // Respect top margin
		v.cursorY = v.marginTop
	}
}

// MoveCursorDown moves the cursor n positions down.
func (v *VTerm) MoveCursorDown(n int) {
	v.wrapNext = false
	v.cursorY += n
	if v.cursorY > v.marginBottom { // Respect bottom margin
		v.cursorY = v.marginBottom
	}
}

// SetCursorRow moves the cursor to a specific row without changing the column.
func (v *VTerm) SetCursorRow(row int) {
	if row < 0 {
		row = 0
	}
	if row >= v.height {
		row = v.height - 1
	}
	v.cursorY = row
}

func (v *VTerm) SetAttribute(a Attribute) {
	v.currentAttr |= a
}
func (v *VTerm) SaveCursor() {
	v.savedCursorX, v.savedCursorY = v.cursorX, v.cursorY
}
func (v *VTerm) RestoreCursor() {
	v.cursorX, v.cursorY = v.savedCursorX, v.savedCursorY
}
func (v *VTerm) ProcessCSI(command byte, params []int, private bool) {
	if private {
		v.processPrivateCSI(command, params)
		return
	}

	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}

	switch command {
	case 'A': // Cursor Up
		v.MoveCursorUp(param(0, 1))
	case 'B': // Cursor Down
		v.MoveCursorDown(param(0, 1))
	case 'H', 'f':
		v.SetCursorPos(param(0, 1)-1, param(1, 1)-1)
	case 'C': // Cursor Forward
		v.MoveCursorForward(param(0, 1))
	case 'D': // Cursor Backward
		v.MoveCursorBackward(param(0, 1))
	case 'G':
		v.SetCursorColumn(param(0, 1) - 1)
	case 'n': // Device Status Report (DSR)
		if param(0, 0) == 6 {
			// --- ADD THIS LINE ---
			log.Println("Parser: Received cursor position request (6n). Responding.")
			// --- END ---
			// The application is asking for the cursor position.
			// Format the response: ESC[<row>;<col>R (1-based)
			response := fmt.Sprintf("\x1b[%d;%dR", v.cursorY+1, v.cursorX+1)
			if v.WriteToPty != nil {
				v.WriteToPty([]byte(response))
			}
		}
	case 'd': // Vertical Line Position Absolute (VPA)
		v.SetCursorRow(param(0, 1) - 1)
	case 'r': // Set Top and Bottom Margins (DECSTBM)
		v.SetMargins(param(0, 1), param(1, v.height))
	case 'P': // Delete Character (DCH)
		v.DeleteCharacters(param(0, 1))
	case 'T': // Scroll Down (SD)
		v.scrollDown(param(0, 1))
	case 'X': // Erase Character (ECH)
		v.EraseCharacters(param(0, 1))
	case 'm':
		i := 0
		if len(params) == 0 {
			params = []int{0}
		}
		for i < len(params) {
			p := params[i]
			switch {
			case p == 0:
				v.ResetAttributes()
			case p == 1:
				v.SetAttribute(AttrBold)
			case p == 4:
				v.SetAttribute(AttrUnderline)
			case p == 7:
				v.SetAttribute(AttrReverse)
			case p >= 30 && p <= 37:
				v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 30)}
			case p >= 40 && p <= 47:
				v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 40)}
			case p >= 90 && p <= 97:
				v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 90 + 8)}
			case p >= 100 && p <= 107:
				v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 100 + 8)}

			// --- NEW: Handle extended colors ---
			case p == 38: // Set extended foreground color
				if i+2 < len(params) && params[i+1] == 5 { // 256-color palette
					v.currentFG = Color{Mode: ColorMode256, Value: uint8(params[i+2])}
					i += 2 // Consume the next 2 parameters
				} else if i+4 < len(params) && params[i+1] == 2 { // RGB true-color
					v.currentFG = Color{Mode: ColorModeRGB, R: uint8(params[i+2]), G: uint8(params[i+3]), B: uint8(params[i+4])}
					i += 4 // Consume the next 4 parameters
				}
			case p == 48: // Set extended background color
				if i+2 < len(params) && params[i+1] == 5 { // 256-color palette
					v.currentBG = Color{Mode: ColorMode256, Value: uint8(params[i+2])}
					i += 2
				} else if i+4 < len(params) && params[i+1] == 2 { // RGB true-color
					v.currentBG = Color{Mode: ColorModeRGB, R: uint8(params[i+2]), G: uint8(params[i+3]), B: uint8(params[i+4])}
					i += 4
				}
			}
			i++
		}
	case 's':
		v.SaveCursor()
	case 'u':
		v.RestoreCursor()
	case 'J':
		v.ClearScreenMode(param(0, 0))
	case 'K':
		v.ClearLine(param(0, 0))
	case 'g':
		if param(0, 0) == 3 {
			v.ClearAllTabStops()
		}
	case 'c':
		log.Println("Parser: Ignoring device attribute request (0c)")
	}
}

func (v *VTerm) ClearScreenMode(mode int) {
	switch mode {
	case 0:
		v.ClearToEndOfScreen()
	case 2:
		v.ClearScreen()
		v.SetCursorPos(0, 0)
	}
}

// ResetAttributes now also resets the margins.
func (v *VTerm) ResetAttributes() {
	v.currentFG = DefaultFG
	v.currentBG = DefaultBG
	v.currentAttr = 0
	v.marginTop = 0
	v.marginBottom = v.height - 1
}
func (v *VTerm) Grid() [][]Cell                { return v.grid }
func (v *VTerm) Cursor() (int, int)            { return v.cursorX, v.cursorY }
func (v *VTerm) CursorVisible() bool           { return v.cursorVisible }
func (v *VTerm) SetCursorVisible(visible bool) { v.cursorVisible = visible }

func (v *VTerm) placeChar(r rune) {
	if v.wrapNext {
		v.cursorX = 0
		v.LineFeed()
		v.wrapNext = false
	}

	if v.cursorY >= 0 && v.cursorY < v.height && v.cursorX >= 0 && v.cursorX < v.width {
		v.grid[v.cursorY][v.cursorX] = Cell{
			Rune: r,
			FG:   v.currentFG,
			BG:   v.currentBG,
			Attr: v.currentAttr,
		}
	}
	if v.autoWrapMode && v.cursorX == v.width-1 {
		v.wrapNext = true
	} else if v.cursorX < v.width-1 {
		v.cursorX++
	}
}
func (v *VTerm) SetCursorPos(row, col int) {
	v.wrapNext = false
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

func (v *VTerm) SetCursorColumn(col int) {
	if col < 0 {
		col = 0
	}
	if col >= v.width {
		col = v.width - 1
	}
	v.cursorX = col
}
func (v *VTerm) ClearScreen() {
	for y := 0; y < v.height; y++ {
		for x := 0; x < v.width; x++ {
			v.grid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
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
	if v.cursorY == v.marginBottom {
		v.scrollUp()
	} else if v.cursorY < v.height-1 {
		v.cursorY++
	}
}

func (v *VTerm) CarriageReturn() {
	v.wrapNext = false
	v.cursorX = 0
}
func (v *VTerm) Backspace() {
	v.wrapNext = false
	if v.cursorX > 0 {
		v.cursorX--
	}
}
func (v *VTerm) Tab() {
	v.wrapNext = false
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
		case 7:
			v.autoWrapMode = true // DECAWM enable
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
		case 7:
			v.autoWrapMode = false // DECAWM disable
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

// MoveCursorForward moves the cursor n positions to the right.
func (v *VTerm) MoveCursorForward(n int) {
	v.cursorX += n
	if v.cursorX >= v.width {
		v.cursorX = v.width - 1
	}
}

// MoveCursorBackward moves the cursor n positions to the left.
func (v *VTerm) MoveCursorBackward(n int) {
	v.cursorX -= n
	if v.cursorX < 0 {
		v.cursorX = 0
	}
}

type Option func(*VTerm)

func WithTitleChangeHandler(handler func(string)) Option {
	return func(v *VTerm) { v.TitleChanged = handler }
}
func (v *VTerm) SetTitle(title string) {
	if v.TitleChanged != nil {
		v.TitleChanged(title)
	}
}

func WithPtyWriter(writer func([]byte)) Option {
	return func(v *VTerm) {
		v.WriteToPty = writer
	}
}
