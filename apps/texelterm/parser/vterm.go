package parser

import (
	"fmt"
	"log"
)

const (
	defaultHistorySize = 2000
)

type VTerm struct {
	width, height                      int
	cursorX, cursorY                   int
	savedMainCursorX, savedMainCursorY int
	savedAltCursorX, savedAltCursorY   int
	historyBuffer                      [][]Cell
	maxHistorySize                     int
	historyHead, historyLen            int
	viewOffset                         int
	inAltScreen                        bool
	altBuffer                          [][]Cell
	currentFG, currentBG               Color
	currentAttr                        Attribute
	tabStops                           map[int]bool
	cursorVisible                      bool
	wrapNext, autoWrapMode, insertMode bool
	appCursorKeys                      bool
	TitleChanged                       func(string)
	WriteToPty                         func([]byte)
	marginTop, marginBottom            int
	defaultFG, defaultBG               Color
	DefaultFgChanged, DefaultBgChanged func(Color)
	QueryDefaultFg, QueryDefaultBg     func()
	ScreenRestored                     func()
	dirtyLines                         map[int]bool
	allDirty                           bool
	prevCursorY                        int
	InSynchronizedUpdate               bool
}

func NewVTerm(width, height int, opts ...Option) *VTerm {
	v := &VTerm{
		width:                width,
		height:               height,
		maxHistorySize:       defaultHistorySize,
		historyBuffer:        make([][]Cell, defaultHistorySize),
		viewOffset:           0,
		currentFG:            DefaultFG,
		currentBG:            DefaultBG,
		tabStops:             make(map[int]bool),
		wrapNext:             false,
		cursorVisible:        true,
		autoWrapMode:         true,
		insertMode:           false,
		appCursorKeys:        false,
		inAltScreen:          false,
		InSynchronizedUpdate: false,
		defaultFG:            DefaultFG,
		defaultBG:            DefaultBG,
		dirtyLines:           make(map[int]bool),
		allDirty:             true,
	}
	for _, opt := range opts {
		opt(v)
	}
	for i := 0; i < width; i++ {
		if i%8 == 0 {
			v.tabStops[i] = true
		}
	}
	v.ClearScreen()
	return v
}

// --- Buffer & Grid Logic ---
func (v *VTerm) Grid() [][]Cell {
	if v.inAltScreen {
		return v.altBuffer
	}
	grid := make([][]Cell, v.height)
	topHistoryLine := v.getTopHistoryLine()
	for i := 0; i < v.height; i++ {
		historyIdx := topHistoryLine + i
		grid[i] = make([]Cell, v.width)
		var logicalLine []Cell
		if historyIdx >= 0 && historyIdx < v.historyLen {
			logicalLine = v.getHistoryLine(historyIdx)
		}
		for x := 0; x < v.width; x++ {
			if logicalLine != nil && x < len(logicalLine) {
				grid[i][x] = logicalLine[x]
			} else {
				grid[i][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
		}
	}
	return grid
}

func (v *VTerm) placeChar(r rune) {
	if v.wrapNext {
		v.cursorX = 0
		v.LineFeed()
		v.wrapNext = false
	}
	logicalY := v.cursorY + v.getTopHistoryLine()
	if v.inAltScreen {
		if v.cursorY >= 0 && v.cursorY < v.height && v.cursorX >= 0 && v.cursorX < v.width {
			v.altBuffer[v.cursorY][v.cursorX] = Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}
			v.MarkDirty(v.cursorY)
		}
	} else {
		if v.viewOffset > 0 {
			v.viewOffset = 0
			v.MarkAllDirty()
			v.SetCursorPos(v.historyLen-1-v.getTopHistoryLine(), v.cursorX)
		}
		logicalY = v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)
		for len(line) <= v.cursorX {
			line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
		}
		if v.insertMode {
			line = append(line, Cell{})
			copy(line[v.cursorX+1:], line[v.cursorX:])
		}
		line[v.cursorX] = Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}
		v.setHistoryLine(logicalY, line)
		v.MarkDirty(v.cursorY)
	}
	if v.autoWrapMode && v.cursorX == v.width-1 {
		v.wrapNext = true
	} else if v.cursorX < v.width-1 {
		v.SetCursorPos(v.cursorY, v.cursorX+1)
	}
}

// --- Cursor and Scrolling ---
func (v *VTerm) SetCursorPos(y, x int) {
	v.wrapNext = false
	v.prevCursorY = v.cursorY
	if x < 0 {
		x = 0
	}
	if x >= v.width {
		x = v.width - 1
	}
	v.cursorX = x
	if y < 0 {
		y = 0
	}
	if y >= v.height {
		y = v.height - 1
	}
	v.cursorY = y
	v.MarkDirty(v.prevCursorY)
	v.MarkDirty(v.cursorY)
}

func (v *VTerm) LineFeed() {
	v.MarkDirty(v.cursorY)
	if v.inAltScreen {
		if v.cursorY == v.marginBottom {
			v.scrollRegion(1, v.marginTop, v.marginBottom, v.altBuffer)
		} else if v.cursorY < v.height-1 {
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		}
	} else {
		logicalY := v.cursorY + v.getTopHistoryLine()
		if logicalY+1 >= v.historyLen {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}
		if v.cursorY < v.height-1 {
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		} else {
			v.viewOffset = 0
			v.MarkAllDirty()
		}
	}
}

func (v *VTerm) scrollRegion(n int, top int, bottom int, buffer [][]Cell) {
	if n > 0 {
		for i := 0; i < n; i++ {
			copy(buffer[top:bottom], buffer[top+1:bottom+1])
			buffer[bottom] = make([]Cell, v.width)
			for x := 0; x < v.width; x++ {
				buffer[bottom][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		}
	} else {
		for i := 0; i < -n; i++ {
			copy(buffer[top+1:bottom+1], buffer[top:bottom])
			buffer[top] = make([]Cell, v.width)
			for x := 0; x < v.width; x++ {
				buffer[top][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		}
	}
	v.MarkAllDirty()
}

func (v *VTerm) Scroll(delta int) {
	if v.inAltScreen {
		return
	}
	v.viewOffset -= delta
	if v.viewOffset < 0 {
		v.viewOffset = 0
	}
	maxOffset := v.historyLen - v.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.viewOffset > maxOffset {
		v.viewOffset = maxOffset
	}
	v.MarkAllDirty()
}

// --- CSI Handlers ---
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
		case 7:
			v.autoWrapMode = true
		case 25:
			v.SetCursorVisible(true)
		case 1002, 1004, 1006, 2004:
			// Ignore mouse and focus reporting for now
		case 1049:
			if v.inAltScreen {
				return
			}
			v.inAltScreen = true
			v.savedMainCursorX, v.savedMainCursorY = v.cursorX, v.cursorY+v.getTopHistoryLine()
			v.altBuffer = make([][]Cell, v.height)
			for i := 0; i < v.height; i++ {
				v.altBuffer[i] = make([]Cell, v.width)
			}
			v.ClearScreen()
		case 2026: // <-- START Synchronized Update
			//log.Printf("Parser: Synchronized output start: ?%d%c", mode, command)
			v.InSynchronizedUpdate = true
		default:
			log.Printf("Parser: Unhandled private CSI set mode: ?%d%c", mode, command)
		}
	case 'l':
		switch mode {
		case 1:
			v.appCursorKeys = false
		case 7:
			v.autoWrapMode = false
		case 25:
			v.SetCursorVisible(false)
		case 1002, 1004, 1006, 2004, 2031, 2048:
			// Ignore mouse and focus reporting for now
		case 1049:
			if !v.inAltScreen {
				return
			}
			v.inAltScreen = false
			v.altBuffer = nil
			physicalY := v.savedMainCursorY - v.getTopHistoryLine()
			v.SetCursorPos(physicalY, v.savedMainCursorX)
			v.MarkAllDirty()
		case 2026: // <-- END Synchronized Update
			//log.Printf("Parser: Synchronized output end: ?%d%c", mode, command)
			v.InSynchronizedUpdate = false
		default:
			log.Printf("Parser: Unhandled private CSI reset mode: ?%d%c", mode, command)
		}
	}
}

func (v *VTerm) ClearScreen() {
	v.MarkAllDirty()
	if v.inAltScreen {
		for y := 0; y < v.height; y++ {
			for x := 0; x < v.width; x++ {
				v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		}
		v.SetCursorPos(0, 0)
	} else {
		v.historyBuffer = make([][]Cell, v.maxHistorySize)
		v.historyHead = 0
		v.historyLen = 1
		v.historyBuffer[0] = make([]Cell, 0, v.width)
		v.viewOffset = 0
		v.SetCursorPos(0, 0)
	}
}

func (v *VTerm) SaveCursor() {
	if v.inAltScreen {
		v.savedAltCursorX, v.savedAltCursorY = v.cursorX, v.cursorY
	} else {
		v.savedMainCursorX, v.savedMainCursorY = v.cursorX, v.cursorY+v.getTopHistoryLine()
	}
}

func (v *VTerm) RestoreCursor() {
	if v.inAltScreen {
		v.SetCursorPos(v.savedAltCursorY, v.savedAltCursorX)
	} else {
		physicalY := v.savedMainCursorY - v.getTopHistoryLine()
		v.SetCursorPos(physicalY, v.savedMainCursorX)
	}
}

// --- History and Viewport Management ---
func (v *VTerm) getHistoryLine(index int) []Cell {
	if index < 0 || index >= v.historyLen {
		return nil
	}
	physicalIndex := (v.historyHead + index) % v.maxHistorySize
	return v.historyBuffer[physicalIndex]
}

func (v *VTerm) setHistoryLine(index int, line []Cell) {
	if index < 0 || index >= v.historyLen {
		return
	}
	physicalIndex := (v.historyHead + index) % v.maxHistorySize
	v.historyBuffer[physicalIndex] = line
}

func (v *VTerm) insertHistoryLine(index int, line []Cell) {
	if index < 0 || index > v.historyLen {
		return
	}
	if v.historyLen < v.maxHistorySize {
		physicalInsertIndex := (v.historyHead + index) % v.maxHistorySize
		for i := v.historyLen; i > index; i-- {
			srcPhysical := (v.historyHead + i - 1) % v.maxHistorySize
			dstPhysical := (v.historyHead + i) % v.maxHistorySize
			v.historyBuffer[dstPhysical] = v.historyBuffer[srcPhysical]
		}
		v.historyBuffer[physicalInsertIndex] = line
		v.historyLen++
	} else {
		v.setHistoryLine(index, line)
	}
}

func (v *VTerm) deleteHistoryLine(index int) {
	if index < 0 || index >= v.historyLen {
		return
	}
	for i := index; i < v.historyLen-1; i++ {
		srcPhysical := (v.historyHead + i + 1) % v.maxHistorySize
		dstPhysical := (v.historyHead + i) % v.maxHistorySize
		v.historyBuffer[dstPhysical] = v.historyBuffer[srcPhysical]
	}
	v.historyLen--
}

func (v *VTerm) appendHistoryLine(line []Cell) {
	if v.historyLen < v.maxHistorySize {
		physicalIndex := (v.historyHead + v.historyLen) % v.maxHistorySize
		v.historyBuffer[physicalIndex] = line
		v.historyLen++
	} else {
		v.historyHead = (v.historyHead + 1) % v.maxHistorySize
		physicalIndex := (v.historyHead + v.historyLen - 1) % v.maxHistorySize
		v.historyBuffer[physicalIndex] = line
	}
}

func (v *VTerm) MarkDirty(line int) {
	if line >= 0 && line < v.height {
		v.dirtyLines[line] = true
	}
}

func (v *VTerm) MarkAllDirty() { v.allDirty = true }

func (v *VTerm) GetDirtyLines() (map[int]bool, bool) {
	return v.dirtyLines, v.allDirty
}

func (v *VTerm) ClearDirty() {
	v.allDirty = false
	v.dirtyLines = make(map[int]bool)
	v.MarkDirty(v.prevCursorY)
	v.MarkDirty(v.cursorY)
}

func (v *VTerm) getTopHistoryLine() int {
	if v.inAltScreen {
		return 0
	}
	top := v.historyLen - v.height - v.viewOffset
	if top < 0 {
		top = 0
	}
	return top
}

func (v *VTerm) CarriageReturn() {
	v.wrapNext = false
	v.SetCursorPos(v.cursorY, 0)
}

func (v *VTerm) Backspace() {
	v.wrapNext = false
	if v.cursorX > 0 {
		v.SetCursorPos(v.cursorY, v.cursorX-1)
	}
}

func (v *VTerm) Tab() {
	v.wrapNext = false
	for x := v.cursorX + 1; x < v.width; x++ {
		if v.tabStops[x] {
			v.SetCursorPos(v.cursorY, x)
			return
		}
	}
	v.SetCursorPos(v.cursorY, v.width-1)
}

func (v *VTerm) Reset() {
	v.MarkAllDirty()
	v.savedMainCursorX, v.savedMainCursorY = 0, 0
	v.savedAltCursorX, v.savedAltCursorY = 0, 0
	v.ClearScreen()
	v.ResetAttributes()
	v.SetMargins(0, 0)
	v.cursorVisible = true
	v.wrapNext = false
	v.autoWrapMode = true
	v.insertMode = false
	v.appCursorKeys = false
	v.tabStops = make(map[int]bool)
	for i := 0; i < v.width; i++ {
		if i%8 == 0 {
			v.tabStops[i] = true
		}
	}
}

func (v *VTerm) ReverseIndex() {
	if v.cursorY == v.marginTop {
		v.scrollRegion(-1, v.marginTop, v.marginBottom, v.altBuffer)
	} else {
		v.SetCursorPos(v.cursorY-1, v.cursorX)
	}
}

func (v *VTerm) ProcessCSI(command rune, params []int, intermediate rune) {
	//	if intermediate != 0 {
	//		log.Printf("Parser: Unhandled CSI sequence with intermediate %q and final %q, params: %v", intermediate, command, params)
	//		return
	//	}
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}

	if intermediate == '$' && command == 'p' {
		if len(params) > 0 {
			mode := param(0, 0)
			log.Printf("Parser: Handling DECRQM intermediate %d mode %d", intermediate, mode)
			var response string
			switch mode {
			case 2026: // Synchronized Output
				// Respond that we support this mode.
				log.Printf("Parser: Enabling Synchronized output (2026) %d", mode)
				response = "\x1b[?2026;1$y"
			case 2027, 2031, 2048:
				// Respond that we do not support these other modes.
				log.Printf("Parser: Disabling the rest: %d", mode)
				response = fmt.Sprintf("\x1b[?%d;0$y", mode)
			default:
				log.Printf("Parser: Unhandled DECRQM query for mode %d", mode)
			}
			if response != "" && v.WriteToPty != nil {
				v.WriteToPty([]byte(response))
			}
		}
		return
	}

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
	case 'c':
		response := "\x1b[?6c"
		if v.WriteToPty != nil {
			v.WriteToPty([]byte(response))
		}
	case 'q':
		// Ignore DECSCA (Select Character Protection Attribute)
	case 't':
		// Ignore window manipulation commands
	default:
		log.Printf("Parser: Unhandled CSI sequence: %q, params: %v", command, params)
	}
}

func (v *VTerm) handleCursorMovement(command rune, params []int) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}
	switch command {
	case 'A':
		v.MoveCursorUp(param(0, 1))
	case 'B':
		v.MoveCursorDown(param(0, 1))
	case 'C':
		v.MoveCursorForward(param(0, 1))
	case 'D':
		v.MoveCursorBackward(param(0, 1))
	case 'G':
		v.SetCursorPos(v.cursorY, param(0, 1)-1)
	case 'H', 'f':
		v.SetCursorPos(param(0, 1)-1, param(1, 1)-1)
	case 'd':
		v.SetCursorPos(param(0, 1)-1, v.cursorX)
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
	case 'J':
		v.ClearScreenMode(param(0, 0))
	case 'K':
		v.ClearLine(param(0, 0))
	case 'P':
		v.DeleteCharacters(param(0, 1))
	case 'X':
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
	case 'S':
		v.scrollRegion(param(0, 1), v.marginTop, v.marginBottom, v.altBuffer)
	case 'T':
		v.scrollRegion(-param(0, 1), v.marginTop, v.marginBottom, v.altBuffer)
	}
}

func (v *VTerm) ClearScreenMode(mode int) {
	v.MarkAllDirty()
	logicalY := v.cursorY + v.getTopHistoryLine()
	switch mode {
	case 0:
		v.ClearLine(0)
		if v.inAltScreen {
			for y := v.cursorY + 1; y < v.height; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else {
			line := v.getHistoryLine(logicalY)
			if v.cursorX < len(line) {
				v.setHistoryLine(logicalY, line[:v.cursorX])
			}
			for i := logicalY + 1; i < v.historyLen; i++ {
				v.setHistoryLine(i, make([]Cell, 0, v.width))
			}
		}
	case 1:
		v.ClearLine(1)
		if v.inAltScreen {
			for y := 0; y < v.cursorY; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else {
			for i := v.getTopHistoryLine(); i < logicalY; i++ {
				v.setHistoryLine(i, make([]Cell, 0, v.width))
			}
		}
	case 2, 3:
		v.ClearScreen()
	}
}

func (v *VTerm) ClearLine(mode int) {
	v.MarkDirty(v.cursorY)
	var line []Cell
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		line = v.getHistoryLine(v.cursorY + v.getTopHistoryLine())
	}
	start, end := 0, v.width
	switch mode {
	case 0:
		start = v.cursorX
	case 1:
		end = v.cursorX + 1
	case 2:
	}
	for len(line) < v.width {
		line = append(line, Cell{Rune: ' '})
	}
	for x := start; x < end; x++ {
		if x < len(line) {
			line[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}
	if !v.inAltScreen {
		v.setHistoryLine(v.cursorY+v.getTopHistoryLine(), line)
	}
}

func (v *VTerm) EraseCharacters(n int) {
	v.MarkDirty(v.cursorY)
	var line []Cell
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		line = v.getHistoryLine(v.cursorY + v.getTopHistoryLine())
	}
	for i := 0; i < n; i++ {
		if v.cursorX+i < len(line) {
			line[v.cursorX+i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}
	if !v.inAltScreen {
		v.setHistoryLine(v.cursorY+v.getTopHistoryLine(), line)
	}
}

func (v *VTerm) InsertCharacters(n int) {
	v.MarkDirty(v.cursorY)
	var line []Cell
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		line = v.getHistoryLine(v.cursorY + v.getTopHistoryLine())
	}
	blanks := make([]Cell, n)
	for i := range blanks {
		blanks[i] = Cell{Rune: ' '}
	}
	line = append(line[:v.cursorX], append(blanks, line[v.cursorX:]...)...)
	if v.inAltScreen {
		v.altBuffer[v.cursorY] = line
	} else {
		v.setHistoryLine(v.cursorY+v.getTopHistoryLine(), line)
	}
}

func (v *VTerm) DeleteCharacters(n int) {
	v.MarkDirty(v.cursorY)
	var line []Cell
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		line = v.getHistoryLine(v.cursorY + v.getTopHistoryLine())
	}
	if v.cursorX >= len(line) {
		return
	}
	if v.cursorX+n > len(line) {
		line = line[:v.cursorX]
	} else {
		line = append(line[:v.cursorX], line[v.cursorX+n:]...)
	}
	if v.inAltScreen {
		v.altBuffer[v.cursorY] = line
	} else {
		v.setHistoryLine(v.cursorY+v.getTopHistoryLine(), line)
	}
}

func (v *VTerm) InsertLines(n int) {
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}
	if v.inAltScreen {
		for i := 0; i < n; i++ {
			copy(v.altBuffer[v.cursorY+1:v.marginBottom+1], v.altBuffer[v.cursorY:v.marginBottom])
			v.altBuffer[v.cursorY] = make([]Cell, v.width)
			for x := 0; x < v.width; x++ {
				v.altBuffer[v.cursorY][x] = Cell{Rune: ' '}
			}
		}
	} else {
		logicalY := v.cursorY + v.getTopHistoryLine()
		for i := 0; i < n; i++ {
			v.insertHistoryLine(logicalY, make([]Cell, 0, v.width))
		}
	}
	v.MarkAllDirty()
}

func (v *VTerm) DeleteLines(n int) {
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}
	if v.inAltScreen {
		copy(v.altBuffer[v.cursorY:v.marginBottom], v.altBuffer[v.cursorY+n:v.marginBottom+1])
		for y := v.marginBottom - n + 1; y <= v.marginBottom; y++ {
			v.altBuffer[y] = make([]Cell, v.width)
			for x := 0; x < v.width; x++ {
				v.altBuffer[y][x] = Cell{Rune: ' '}
			}
		}
	} else {
		logicalY := v.cursorY + v.getTopHistoryLine()
		for i := 0; i < n; i++ {
			if logicalY < v.historyLen {
				v.deleteHistoryLine(logicalY)
			}
		}
	}
	v.MarkAllDirty()
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
		case p == 38:
			if i+2 < len(params) && params[i+1] == 5 {
				v.currentFG = Color{Mode: ColorMode256, Value: uint8(params[i+2])}
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 {
				v.currentFG = Color{Mode: ColorModeRGB, R: uint8(params[i+2]), G: uint8(params[i+3]), B: uint8(params[i+4])}
				i += 4
			}
		case p == 48:
			if i+2 < len(params) && params[i+1] == 5 {
				v.currentBG = Color{Mode: ColorMode256, Value: uint8(params[i+2])}
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 {
				v.currentBG = Color{Mode: ColorModeRGB, R: uint8(params[i+2]), G: uint8(params[i+3]), B: uint8(params[i+4])}
				i += 4
			}
		case p == 49:
			v.currentBG = DefaultBG
		}
		i++
	}
}

func (v *VTerm) SetMargins(top, bottom int) {
	if top == 0 {
		top = 1
	}
	if bottom == 0 || bottom > v.height {
		bottom = v.height
	}
	if top >= bottom {
		v.marginTop = 0
		v.marginBottom = v.height - 1
		return
	}
	v.marginTop = top - 1
	v.marginBottom = bottom - 1
	v.SetCursorPos(v.marginTop, 0)
}

func (v *VTerm) MoveCursorForward(n int) {
	v.SetCursorPos(v.cursorY, v.cursorX+n)
}

func (v *VTerm) MoveCursorBackward(n int) {
	v.SetCursorPos(v.cursorY, v.cursorX-n)
}

func (v *VTerm) MoveCursorUp(n int) {
	v.wrapNext = false
	newY := v.cursorY - n
	if newY < v.marginTop {
		newY = v.marginTop
	}
	v.SetCursorPos(newY, v.cursorX)
}

func (v *VTerm) MoveCursorDown(n int) {
	v.wrapNext = false
	newY := v.cursorY + n
	if newY > v.marginBottom {
		newY = v.marginBottom
	}
	v.SetCursorPos(newY, v.cursorX)
}

func (v *VTerm) SetAttribute(a Attribute) { v.currentAttr |= a }

func (v *VTerm) ClearAttribute(a Attribute) { v.currentAttr &^= a }

func (v *VTerm) ResetAttributes() {
	v.currentFG = v.defaultFG
	v.currentBG = v.defaultBG
	v.currentAttr = 0
}

func (v *VTerm) SetCursorVisible(visible bool) {
	v.cursorVisible = visible
	v.MarkDirty(v.cursorY)
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

type Option func(*VTerm)

func WithPtyWriter(writer func([]byte)) Option { return func(v *VTerm) { v.WriteToPty = writer } }

func WithTitleChangeHandler(handler func(string)) Option {
	return func(v *VTerm) { v.TitleChanged = handler }
}

func (v *VTerm) SetTitle(title string) {
	if v.TitleChanged != nil {
		v.TitleChanged(title)
	}
}

func WithDefaultFgChangeHandler(handler func(Color)) Option {
	return func(v *VTerm) { v.DefaultFgChanged = handler }
}

func WithDefaultBgChangeHandler(handler func(Color)) Option {
	return func(v *VTerm) { v.DefaultBgChanged = handler }
}

func WithQueryDefaultFgHandler(handler func()) Option {
	return func(v *VTerm) { v.QueryDefaultFg = handler }
}

func WithQueryDefaultBgHandler(handler func()) Option {
	return func(v *VTerm) { v.QueryDefaultBg = handler }
}

func WithScreenRestoredHandler(handler func()) Option {
	return func(v *VTerm) { v.ScreenRestored = handler }
}

func (v *VTerm) Resize(width, height int) {
	if width == v.width && height == v.height {
		return
	}
	savedLogicalY := v.savedMainCursorY
	if !v.inAltScreen {
		savedLogicalY = v.cursorY + v.getTopHistoryLine()
	}

	oldHeight := v.height
	v.width = width
	v.height = height

	if v.inAltScreen {
		newAltBuffer := make([][]Cell, v.height)
		for i := 0; i < v.height; i++ {
			newAltBuffer[i] = make([]Cell, v.width)
			if i < oldHeight {
				oldLine := v.altBuffer[i]
				copy(newAltBuffer[i], oldLine)
			}
		}
		v.altBuffer = newAltBuffer
		v.MarkAllDirty()
		v.SetCursorPos(v.cursorY, v.cursorX)
		return
	}
	v.MarkAllDirty()
	v.SetMargins(0, 0)
	physicalY := savedLogicalY - v.getTopHistoryLine()
	v.SetCursorPos(physicalY, v.savedMainCursorX)
}

func (v *VTerm) AppCursorKeys() bool { return v.appCursorKeys }

func (v *VTerm) Cursor() (int, int) { return v.cursorX, v.cursorY }

func (v *VTerm) CursorVisible() bool { return v.cursorVisible }

func (v *VTerm) DefaultFG() Color { return v.defaultFG }

func (v *VTerm) CurrentFG() Color { return v.currentFG }

func (v *VTerm) DefaultBG() Color { return v.defaultBG }

func (v *VTerm) CurrentBG() Color { return v.currentBG }
