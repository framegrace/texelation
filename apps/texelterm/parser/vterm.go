package parser

import (
	"fmt"
	"log"
)

// WithPtyWriter returns an option that sets a callback for writing data back to the PTY.

// VTerm holds the state of a virtual terminal.
type VTerm struct {
	width, height                     int
	cursorX, cursorY                  int
	savedCursorX, savedCursorY        int
	grid                              [][]Cell
	currentFG, currentBG              Color
	currentAttr                       Attribute
	tabStops                          map[int]bool
	cursorVisible                     bool
	wrapNext                          bool
	autoWrapMode                      bool
	insertMode                        bool
	appCursorKeys                     bool
	TitleChanged                      func(string)
	WriteToPty                        func([]byte)
	marginTop, marginBottom           int
	savedGrid                         [][]Cell
	savedWidth, savedHeight           int
	savedMarginTop, savedMarginBottom int
	defaultFG, defaultBG              Color
	DefaultFgChanged                  func(Color)
	DefaultBgChanged                  func(Color)
	QueryDefaultFg                    func()
	QueryDefaultBg                    func()
	ScreenRestored                    func()
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
		insertMode:    false,
		appCursorKeys: false,
		marginTop:     0,          // Default margin is top row
		marginBottom:  height - 1, // Default margin is bottom row
		defaultFG:     DefaultFG,
		defaultBG:     DefaultBG,
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

func WithScreenRestoredHandler(handler func()) Option {
	return func(v *VTerm) { v.ScreenRestored = handler }
}

// Add these two new Option functions at the end of the file
func WithQueryDefaultFgHandler(handler func()) Option {
	return func(v *VTerm) { v.QueryDefaultFg = handler }
}

func WithQueryDefaultBgHandler(handler func()) Option {
	return func(v *VTerm) { v.QueryDefaultBg = handler }
}

// Add these two new Option functions at the end of the file
func WithDefaultFgChangeHandler(handler func(Color)) Option {
	return func(v *VTerm) { v.DefaultFgChanged = handler }
}

func WithDefaultBgChangeHandler(handler func(Color)) Option {
	return func(v *VTerm) { v.DefaultBgChanged = handler }
}

func (v *VTerm) Resize(width, height int) {
	if width == v.width && height == v.height {
		return
	}

	// --- THE DEFINITIVE FIX ---

	// 1. Create a template for any new cells based on the VTerm's CURRENT style.
	// This preserves the background color set by the application (e.g., vi's colorscheme).
	newCellTemplate := Cell{
		Rune: ' ',
		FG:   v.defaultFG, // Use current foreground
		BG:   v.defaultBG, // Use current background
		Attr: 0,           // Use current attributes
	}

	// 2. Create the new grid.
	newGrid := make([][]Cell, height)
	for r := range newGrid {
		newGrid[r] = make([]Cell, width)
	}

	// 3. Copy old content and fill new areas with our correctly-styled template.
	rowsToCopy := min(v.height, height)
	colsToCopy := min(v.width, width)

	for r := 0; r < rowsToCopy; r++ {
		// Copy the slice of existing columns.
		copy(newGrid[r][:colsToCopy], v.grid[r][:colsToCopy])
		// Fill any newly added columns on existing rows.
		for c := v.width; c < width; c++ {
			newGrid[r][c] = newCellTemplate
		}
	}
	// Fill any newly added rows.
	for r := v.height; r < height; r++ {
		for c := 0; c < width; c++ {
			newGrid[r][c] = newCellTemplate
		}
	}

	v.grid = newGrid
	v.width = width
	v.height = height

	// You already have this fix, which is excellent. A resize MUST reset the
	// scrolling region to prevent visual artifacts in many applications.
	v.marginTop = 0
	v.marginBottom = v.height - 1

	// Clamp cursor position to new bounds.
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
func (v *VTerm) AppCursorKeys() bool {
	return v.appCursorKeys
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

// InsertLines inserts n blank lines at the cursor, pushing subsequent lines down.
func (v *VTerm) InsertLines(n int) {
	// This command is only active when the cursor is within the scrolling margins.
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Clamp n to prevent inserting more lines than available in the region.
	if v.cursorY+n > v.marginBottom+1 {
		n = v.marginBottom - v.cursorY + 1
	}

	// Shift lines down within the scrolling region, starting from the bottom.
	for y := v.marginBottom; y >= v.cursorY+n; y-- {
		copy(v.grid[y], v.grid[y-n])
	}

	// Clear the n new lines at the cursor position.
	for y := v.cursorY; y < v.cursorY+n && y <= v.marginBottom; y++ {
		newLine := make([]Cell, v.width)
		for x := range newLine {
			newLine[x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
		v.grid[y] = newLine
	}
}

// DeleteLines deletes n lines at the cursor, pulling subsequent lines up.
func (v *VTerm) DeleteLines(n int) {
	// This command is only active when the cursor is within the scrolling margins.
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Clamp n to prevent deleting more lines than available in the region.
	if v.cursorY+n > v.marginBottom+1 {
		n = v.marginBottom - v.cursorY + 1
	}

	// Shift lines up within the scrolling region, starting from the cursor.
	for y := v.cursorY; y <= v.marginBottom-n; y++ {
		copy(v.grid[y], v.grid[y+n])
	}

	// Clear the n new lines at the bottom of the scrolling region.
	for y := v.marginBottom - n + 1; y <= v.marginBottom; y++ {
		newLine := make([]Cell, v.width)
		for x := range newLine {
			// Use default background color for cleared lines.
			newLine[x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
		v.grid[y] = newLine
	}
}

func (v *VTerm) scrollUp(n int) {
	for i := 0; i < n; i++ {
		// Keep a reference to the top line of the region, which will scroll out of view.
		scrolledLine := v.grid[v.marginTop]

		// Shift the line pointers up by one within the scrolling region.
		// This is more efficient than copying cell data.
		copy(v.grid[v.marginTop:v.marginBottom], v.grid[v.marginTop+1:v.marginBottom+1])

		// Clear the content of the line that has now scrolled out.
		for i := range scrolledLine {
			// Use the default background color for the new line, not the current one,
			// to avoid "smearing" colors during scrolling.
			scrolledLine[i] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}

		// Place the now-cleared line at the bottom of the region.
		v.grid[v.marginBottom] = scrolledLine
	}
}

func (v *VTerm) scrollDown(n int) {
	for i := 0; i < n; i++ {
		// To scroll down, we must copy rows backwards from the bottom of
		// the region to avoid overwriting the source rows prematurely.
		for y := v.marginBottom; y > v.marginTop; y-- {
			copy(v.grid[y], v.grid[y-1])
		}

		// Clear the new top line of the region.
		newLine := make([]Cell, v.width)
		for j := range newLine {
			newLine[j] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
		v.grid[v.marginTop] = newLine
	}
}

// ReverseIndex handles the ESC M sequence. It moves the cursor up one line,
// scrolling the content of the scrolling region down if the cursor is at the top margin.
func (v *VTerm) ReverseIndex() {
	if v.cursorY == v.marginTop {
		// If at the top of the region, scroll the region down by one line.
		v.scrollDown(1)
	} else if v.cursorY > 0 {
		// Otherwise, just move the cursor up.
		v.cursorY--
	}
}

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
func (v *VTerm) ClearAttribute(a Attribute) {
	v.currentAttr &^= a
}
func (v *VTerm) SaveCursor() {
	v.savedCursorX, v.savedCursorY = v.cursorX, v.cursorY
}
func (v *VTerm) RestoreCursor() {
	v.wrapNext = false
	v.cursorX, v.cursorY = v.savedCursorX, v.savedCursorY
}

func (v *VTerm) ClearScreenMode(mode int) {
	switch mode {
	case 0:
		v.ClearToEndOfScreen()
	case 1: // Erase from beginning of screen to cursor
		v.ClearToBeginningOfScreen()
	case 2:
		v.ClearScreen()
		v.SetCursorPos(0, 0)
	}
}
func (v *VTerm) Reset() {
	// Reset cursor position and saved state.
	v.SetCursorPos(0, 0)
	v.savedCursorX, v.savedCursorY = 0, 0
	v.savedMarginTop, v.savedMarginBottom = 0, 0
	v.savedGrid = nil

	// Reset grid content.
	v.ClearScreen()

	// Reset graphical attributes and colors.
	v.ResetAttributes()

	// Reset margins to full screen size.
	v.marginTop = 0
	v.marginBottom = v.height - 1

	// Reset modes to their defaults.
	v.cursorVisible = true
	v.wrapNext = false
	v.autoWrapMode = true
	v.insertMode = false
	v.appCursorKeys = false
	v.ClearAllTabStops()
	for i := 0; i < v.width; i++ {
		if i%8 == 0 {
			v.tabStops[i] = true
		}
	}
}

// ResetAttributes now correctly only resets graphical attributes, NOT scrolling margins.
func (v *VTerm) ResetAttributes() {
	v.currentFG = v.defaultFG
	v.currentBG = v.defaultBG
	v.currentAttr = 0
}

func (v *VTerm) SoftReset() {
	v.ResetAttributes() // Reset graphical attributes
	v.marginTop = 0     // Reset margins
	v.marginBottom = v.height - 1
	v.SetCursorVisible(true) // Cursor becomes visible
	v.appCursorKeys = false  // Set normal cursor key mode
	v.SaveCursor()           // Reset saved cursor position
}

func (v *VTerm) Grid() [][]Cell                { return v.grid }
func (v *VTerm) Cursor() (int, int)            { return v.cursorX, v.cursorY }
func (v *VTerm) CursorVisible() bool           { return v.cursorVisible }
func (v *VTerm) SetCursorVisible(visible bool) { v.cursorVisible = visible }

func (v *VTerm) ProcessCSI(command rune, params []int, intermediate rune) {
	if intermediate == '!' && command == 'p' {
		v.SoftReset() // Handle DECSTR
		return
	}

	if intermediate != 0 {
		log.Printf("Parser: Unhandled CSI sequence with intermediate %q and final %q, params: %v", intermediate, command, params)
		return
	}

	// Private CSI (e.g. CSI ?1049h) has its own dispatcher
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}

	// This is the private parameter for sequences like `CSI ?25l`.
	// The `private` flag is set when the parser sees a `?`
	if param(0, -1) > 0 && (command == 'h' || command == 'l') {
		v.processPrivateCSI(command, params)
		return
	}

	switch command {
	case 'A', 'B', 'C', 'D', 'G', 'H', 'f', 'd':
		v.handleCursorMovement(command, params)
	case 'J', 'K', 'P', 'X':
		v.handleErase(command, params)
	case '@':
		v.InsertCharacters(param(0, 1))
	case 'L':
		v.InsertLines(param(0, 1))
	case 'M':
		v.DeleteLines(param(0, 1))
	case 'S', 'T':
		v.handleScroll(command, params)
	case 'm':
		v.handleSGR(params)
	case 'n':
		if param(0, 0) == 6 {
			response := fmt.Sprintf("\x1b[%d;%dR", v.cursorY+1, v.cursorX+1)
			if v.WriteToPty != nil {
				v.WriteToPty([]byte(response))
			}
		}
	case 'r':
		v.SetMargins(param(0, 1), param(1, v.height))
	case 's':
		v.SaveCursor()
	case 'u':
		v.RestoreCursor()
	case 'g':
		if param(0, 0) == 3 {
			v.ClearAllTabStops()
		}
	case 'c': // Send Device Attributes (DA)
		if param(0, 0) == 0 {
			// This is the response to vi's "Who are you?" question.
			// We will identify as a VT102, which is a common and safe identity
			// that signals we support basic modern features.
			// The response sequence is ESC [ ? 6 c
			response := "\x1b[?6c"
			if v.WriteToPty != nil {
				v.WriteToPty([]byte(response))
			}
		}
	// --- END NE
	default:
		log.Printf("Parser: Unhandled CSI sequence: %q, params: %v", command, params)
	}
}

func (v *VTerm) handleCursorMovement(command rune, params []int) {
	v.wrapNext = false
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
	case 'C': // Cursor Forward
		v.MoveCursorForward(param(0, 1))
	case 'D': // Cursor Backward
		v.MoveCursorBackward(param(0, 1))
	case 'G': // Cursor Character Absolute
		v.SetCursorColumn(param(0, 1) - 1)
	case 'H', 'f': // Cursor Position / Horizontal and Vertical Position
		v.SetCursorPos(param(0, 1)-1, param(1, 1)-1)
	case 'd': // Vertical Line Position Absolute (VPA)
		v.SetCursorRow(param(0, 1) - 1)
	}
}

func (v *VTerm) InsertCharacters(n int) {
	line := v.grid[v.cursorY]
	end := v.width
	start := v.cursorX

	// repeat n times
	for rep := 0; rep < n; rep++ {
		// shift cells one to the right
		if start < end-1 {
			for i := end - 2; i >= start; i-- {
				line[i+1] = line[i]
			}
			// clear the newly opened slot
			line[start] = Cell{
				Rune: ' ',
				FG:   v.currentFG,
				BG:   v.currentBG,
			}
		}
	}
}

func (v *VTerm) handleErase(command rune, params []int) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}
	switch command {
	case 'J': // Erase in Display
		v.ClearScreenMode(param(0, 0))
	case 'K': // Erase in Line
		v.ClearLine(param(0, 0))
	case 'P': // Delete Character
		v.DeleteCharacters(param(0, 1))
	case 'X': // Erase Character
		v.EraseCharacters(param(0, 1))
	}
}

func (v *VTerm) handleScroll(command rune, params []int) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}
	switch command {
	case 'S': // Scroll Up
		v.scrollUp(param(0, 1))
	case 'T': // Scroll Down
		//		for i := 0; i < param(0, 1); i++ {
		v.scrollDown(param(0, 1))
		//		}
	}
}

func (v *VTerm) handleMode(command rune, params []int) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}
	switch command {
	case 'h': // Set Mode
		if param(0, 0) == 4 {
			v.insertMode = true
		}
	case 'l': // Reset Mode
		if param(0, 0) == 4 {
			v.insertMode = false
		} else if param(0, 0) == 2 {
			log.Println("Parser: Ignoring unlock keyboard action mode (2l)")
		}
	}
}

func (v *VTerm) handleSGR(params []int) {
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
		case p == 22:
			v.ClearAttribute(AttrBold)
		case p == 24:
			v.ClearAttribute(AttrUnderline)
		case p == 27:
			v.ClearAttribute(AttrReverse)
		case p >= 30 && p <= 37:
			v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 30)}
		case p == 39:
			v.currentFG = DefaultFG
		case p >= 40 && p <= 47:
			v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 40)}
		case p >= 90 && p <= 97:
			v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 90 + 8)}
		case p >= 100 && p <= 107:
			v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 100 + 8)}
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
		case p == 49:
			v.currentBG = DefaultBG
		}
		i++
	}
}

func (v *VTerm) placeChar(r rune) {
	if v.wrapNext {
		v.cursorX = 0
		v.LineFeed()
		v.wrapNext = false
	}
	// In insert mode, we first make space by shifting the rest of the line.
	if v.insertMode {
		line := v.grid[v.cursorY]
		// This right-to-left loop correctly shifts all characters
		// from the cursor to the end of the line one position to the right.
		if v.cursorX < v.width-1 {
			for i := v.width - 2; i >= v.cursorX; i-- {
				line[i+1] = line[i]
			}
		}
	}

	// Now, ALWAYS place the character at the current cursor position (overwrite).
	// In insert mode, we are writing into the blank space we just created.
	if v.cursorY >= 0 && v.cursorY < v.height && v.cursorX >= 0 && v.cursorX < v.width {
		v.grid[v.cursorY][v.cursorX] = Cell{
			Rune: r,
			FG:   v.currentFG,
			BG:   v.currentBG,
			Attr: v.currentAttr,
		}
	}

	// Finally, handle cursor advancement and auto-wrapping.
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
	//	v.defaultFG = v.currentFG
	//	v.defaultBG = v.currentBG
	for y := 0; y < v.height; y++ {
		for x := 0; x < v.width; x++ {
			v.grid[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
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
		v.scrollUp(1)
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
func (v *VTerm) processPrivateCSI(command rune, params []int) {
	if len(params) == 0 {
		return
	}
	mode := params[0]
	switch command {
	case 'h':
		switch mode {
		case 1:
			v.appCursorKeys = true
		case 4:
			v.insertMode = true
		case 7:
			v.autoWrapMode = true // DECAWM enable
		case 12:
			log.Println("Parser: Ignoring set blinking cursor (12h)")
		case 25:
			v.SetCursorVisible(true)
		case 1049:
			// Save the current screen state.
			v.savedWidth = v.width
			v.savedHeight = v.height
			v.savedMarginTop = v.marginTop
			v.savedMarginBottom = v.marginBottom
			v.savedGrid = make([][]Cell, v.height)
			for i := range v.grid {
				v.savedGrid[i] = make([]Cell, v.width)
				copy(v.savedGrid[i], v.grid[i])
			}
			v.SaveCursor()
			// Then clear the screen for the alternate buffer.
			v.ClearScreen()
			v.SetCursorPos(0, 0)
			v.marginTop = 0
			v.marginBottom = v.height - 1
		case 2004:
			log.Println("Parser: Ignoring set bracketed paste mode (2004h)")
		}
	case 'l':
		switch mode {
		case 1:
			v.appCursorKeys = false // DECCKM: Cursor Keys Normal Mode
		case 4:
			v.insertMode = false
		case 7:
			v.autoWrapMode = false // DECAWM disable
		case 12:
			log.Println("Parser: Ignoring reset steady cursor (12l)")
		case 25:
			v.SetCursorVisible(false)
		case 1049:
			// Restore the saved screen state.
			if v.savedGrid != nil {
				v.grid = v.savedGrid
				v.width = v.savedWidth
				v.height = v.savedHeight
				v.marginTop = v.savedMarginTop
				v.marginBottom = v.savedMarginBottom
				v.savedGrid = nil
			}
			v.RestoreCursor()
			if v.ScreenRestored != nil {
				v.ScreenRestored()
			}
		case 2004:
			log.Println("Parser: Ignoring reset bracketed paste mode (2004l)")
		}
	}
}

func (v *VTerm) ClearToEndOfScreen() {
	v.ClearLine(0) // Erase from cursor to end of the current line.
	for y := v.cursorY + 1; y < v.height; y++ {
		for x := 0; x < v.width; x++ {
			v.grid[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
		}
	}
}

func (v *VTerm) ClearToBeginningOfScreen() {
	v.ClearLine(1) // Erase from beginning of the current line to the cursor.
	for y := 0; y < v.cursorY; y++ {
		for x := 0; x < v.width; x++ {
			v.grid[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
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

// A helper function that was missing but implied by the code.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (v *VTerm) DumpGrid(label string) {
	log.Printf("--- GRID DUMP: %s ---", label)
	for y := 0; y < v.height; y++ {
		line := ""
		for x := 0; x < v.width; x++ {
			r := v.grid[y][x].Rune
			if r == ' ' {
				line += "." // Use '.' for spaces to make them visible
			} else {
				line += string(r)
			}
		}
		log.Printf("LINE %2d: %s", y, line)
	}
	log.Printf("--- END DUMP ---")
}

func (v *VTerm) DumpState() {
	log.Printf("    VTerm State: defaultBG=[%s], currentBG=[%s]", v.defaultBG, v.currentBG)
	log.Printf("    Grid Dimensions: %d x %d", v.width, v.height)
	for r := 0; r < v.height; r++ {
		var line string
		for c := 0; c < v.width; c++ {
			cell := v.grid[r][c]
			// To avoid spam, only log cells that don't have the expected default BG
			if cell.BG.Mode != v.defaultBG.Mode || cell.BG.Value != v.defaultBG.Value {
				line += fmt.Sprintf(" | C:%d BG:[%s] ", c, cell.BG)
			}
		}
		if line != "" {
			log.Printf("    Row %d:%s", r, line)
		}
	}
}

func (v *VTerm) DefaultBG() Color {
	// v.mu.Lock() // Not needed if the caller holds the lock, which texelTerm.DumpState does
	// defer v.mu.Unlock()
	return v.defaultBG
}

func (v *VTerm) CurrentBG() Color {
	// v.mu.Lock()
	// defer v.mu.Unlock()
	return v.currentBG
}

func (c Color) String() string {
	switch c.Mode {
	case ColorModeDefault:
		return "Default"
	case ColorModeStandard:
		return fmt.Sprintf("Palette#%d", c.Value)
	case ColorMode256:
		return fmt.Sprintf("Palette#%d", c.Value)
	case ColorModeRGB:
		return fmt.Sprintf("rgb:%02x/%02x/%02x", c.R, c.G, c.B)
	}
	return "Invalid"
}
