package texelterm

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	//	"strconv"
	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2" // Import tcell
	"sync"
	"syscall"
	"texelation/apps/texelterm/parser"
	"texelation/texel"
	"time"
)

// The TexelTerm struct remains the same, but its Render method will produce tcell-compatible output.
type TexelTerm struct {
	title        string
	command      string
	width        int
	height       int
	cmd          *exec.Cmd
	pty          *os.File
	vterm        *parser.VTerm
	parser       *parser.Parser
	mu           sync.Mutex
	stop         chan struct{}
	refreshChan  chan<- bool
	wg           sync.WaitGroup
	buf          [][]texel.Cell
	colorPalette [258]tcell.Color // Local palette for this terminal instance
}

func New(title, command string) texel.App {
	return &TexelTerm{
		title:        title,
		command:      command,
		width:        80, // Sensible defaults
		height:       24,
		stop:         make(chan struct{}),
		colorPalette: newDefaultPalette(),
	}
}

func (a *TexelTerm) Vterm() *parser.VTerm {
	return a.vterm
}

// mapParserColorToTCell translates our internal parser.Color to a true RGB tcell.Color using the local palette.
func (a *TexelTerm) mapParserColorToTCell(c parser.Color) tcell.Color {
	switch c.Mode {
	case parser.ColorModeDefault:
		// Use the default foreground color from our local palette
		return a.colorPalette[256]
	case parser.ColorModeStandard:
		return a.colorPalette[c.Value]
	case parser.ColorMode256:
		return a.colorPalette[c.Value]
	case parser.ColorModeRGB:
		return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
	default:
		return tcell.ColorDefault
	}
}

func (a *TexelTerm) applyParserStyle(pCell parser.Cell) texel.Cell {
	// Get the foreground color by mapping it through our local palette.
	fgColor := a.mapParserColorToTCell(pCell.FG)

	// Handle the background color explicitly.
	var bgColor tcell.Color
	if pCell.BG.Mode == parser.ColorModeDefault {
		// If the cell's BG is the default, use our palette's default BG.
		bgColor = a.colorPalette[257]
	} else {
		// Otherwise, map it normally.
		bgColor = a.mapParserColorToTCell(pCell.BG)
	}

	style := tcell.StyleDefault
	style = style.Foreground(fgColor).Background(bgColor)
	style = style.Bold(pCell.Attr&parser.AttrBold != 0)
	style = style.Underline(pCell.Attr&parser.AttrUnderline != 0)
	style = style.Reverse(pCell.Attr&parser.AttrReverse != 0)

	return texel.Cell{
		Ch:    pCell.Rune,
		Style: style,
	}
}

// SetRefreshNotifier implements the new interface method.
func (a *TexelTerm) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
}

func (a *TexelTerm) HandleMessage(msg texel.Message) {
	// This app doesn't handle messages.
}

func (a *TexelTerm) Render() [][]texel.Cell {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.vterm == nil {
		return [][]texel.Cell{}
	}

	vtermGrid := a.vterm.Grid()
	rows := len(vtermGrid)
	if rows == 0 {
		return [][]texel.Cell{}
	}
	cols := len(vtermGrid[0])

	// Ensure our buffer is the correct size.
	if len(a.buf) != rows || (rows > 0 && cap(a.buf[0]) != cols) {
		a.buf = make([][]texel.Cell, rows)
		for y := range a.buf {
			a.buf[y] = make([]texel.Cell, cols)
		}
		a.vterm.MarkAllDirty() // New buffer requires a full render.
	}

	cursorX, cursorY := a.vterm.Cursor()
	cursorVisible := a.vterm.CursorVisible()
	dirtyLines, allDirty := a.vterm.GetDirtyLines()

	// The render function for a single line.
	renderLine := func(y int) {
		for x := 0; x < cols; x++ {
			parserCell := vtermGrid[y][x]
			a.buf[y][x] = a.applyParserStyle(parserCell)
			// Apply cursor style AFTER converting the cell.
			if cursorVisible && x == cursorX && y == cursorY {
				a.buf[y][x].Style = a.buf[y][x].Style.Reverse(true)
			}
		}
	}

	if allDirty {
		// If everything is dirty, render all lines.
		for y := 0; y < rows; y++ {
			renderLine(y)
		}
	} else {
		// Otherwise, just render the lines that have changed.
		for y := range dirtyLines {
			if y >= 0 && y < rows {
				renderLine(y)
			}
		}
	}

	// Important: Clear the dirty state for the next frame.
	a.vterm.ClearDirty()

	return a.buf
}

// --- MODIFIED HandleKey METHOD ---
func (a *TexelTerm) HandleKey(ev *tcell.EventKey) {
	if a.pty == nil {
		return
	}

	a.mu.Lock()
	appMode := a.vterm.AppCursorKeys()
	a.mu.Unlock()

	key := ev.Key()

	// --- New Scrollback Handling Logic ---
	if ev.Modifiers()&tcell.ModAlt != 0 {
		handled := true
		switch key {
		case tcell.KeyPgDn:
			go func() {
				// You can adjust the sleep time to make the animation faster or slower.
				scrollInterval := 2 * time.Millisecond
				for i := 0; i < a.height; i++ {
					a.mu.Lock()
					a.vterm.Scroll(1)
					a.mu.Unlock()
					a.refreshChan <- true
					time.Sleep(scrollInterval)
				}
			}()
		case tcell.KeyPgUp:
			go func() {
				scrollInterval := 2 * time.Millisecond
				for i := 0; i < a.height; i++ {
					a.mu.Lock()
					a.vterm.Scroll(-1)
					a.mu.Unlock()
					a.refreshChan <- true
					time.Sleep(scrollInterval)
				}
			}()
		case tcell.KeyDown:
			a.mu.Lock()
			a.vterm.Scroll(1) // Scroll up by one line
			a.mu.Unlock()
		case tcell.KeyUp:
			a.mu.Lock()
			a.vterm.Scroll(-1) // Scroll down by one line
			a.mu.Unlock()
		default:
			handled = false
		}
		if handled {
			if a.refreshChan != nil {
				select {
				case a.refreshChan <- true:
				default:
				}
			}
			return
		}
	}

	// --- Existing Key Handling Logic ---
	var keyBytes []byte
	switch key {
	case tcell.KeyUp:
		if appMode {
			keyBytes = []byte("\x1bOA")
		} else {
			keyBytes = []byte("\x1b[A")
		}
	case tcell.KeyDown:
		if appMode {
			keyBytes = []byte("\x1bOB")
		} else {
			keyBytes = []byte("\x1b[B")
		}
	case tcell.KeyRight:
		if appMode {
			keyBytes = []byte("\x1bOC")
		} else {
			keyBytes = []byte("\x1b[C")
		}
	case tcell.KeyLeft:
		if appMode {
			keyBytes = []byte("\x1bOD")
		} else {
			keyBytes = []byte("\x1b[D")
		}
	case tcell.KeyHome:
		keyBytes = []byte("\x1b[H")
	case tcell.KeyEnd:
		keyBytes = []byte("\x1b[F")
	case tcell.KeyInsert:
		keyBytes = []byte("\x1b[2~")
	case tcell.KeyDelete:
		keyBytes = []byte("\x1b[3~")
	case tcell.KeyPgUp:
		keyBytes = []byte("\x1b[5~")
	case tcell.KeyPgDn:
		keyBytes = []byte("\x1b[6~")
	case tcell.KeyF1:
		keyBytes = []byte("\x1bOP")
	case tcell.KeyF2:
		keyBytes = []byte("\x1bOQ")
	case tcell.KeyF3:
		keyBytes = []byte("\x1bOR")
	case tcell.KeyF4:
		keyBytes = []byte("\x1bOS")
	case tcell.KeyEnter:
		keyBytes = []byte("\r")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		keyBytes = []byte{'\b'}
	case tcell.KeyTab:
		keyBytes = []byte("\t")
	case tcell.KeyEsc:
		keyBytes = []byte("\x1b")
	default:
		keyBytes = []byte(string(ev.Rune()))
	}

	if keyBytes != nil {
		a.pty.Write(keyBytes)
	}
}

func (a *TexelTerm) Run() error {
	a.mu.Lock()
	cols := a.width
	rows := a.height
	a.mu.Unlock()

	cmd := exec.Command(a.command)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		log.Printf("Failed to start pty with size: %v", err)
		return err
	}
	a.pty = ptmx
	a.cmd = cmd

	// Initialize our virtual terminal and parser
	a.mu.Lock()
	fgChangeHandler := func(c parser.Color) {
		// Update the palette's special slot for the default foreground
		a.colorPalette[256] = a.mapParserColorToTCell(c)
	}

	bgChangeHandler := func(c parser.Color) {
		// Update the palette's special slot for the default background
		a.colorPalette[257] = a.mapParserColorToTCell(c)
	}

	queryFgHandler := func() {
		if a.pty == nil {
			return
		}

		// 1. Get the color from our palette's default slot
		color := a.colorPalette[256]
		r, g, b := color.RGB()

		// 2. Format it as a 16-bit hex string (e.g., "rgb:rrrr/gggg/bbbb")
		// We multiply by 257 to scale an 8-bit value (0-255) to 16-bit (0-65535)
		log.Printf("Responding query with %d,%d,%d", r, g, b)
		log.Printf("Response: \\x1b]10;rgb:%04x/%04x/%04x\a", r*257, g*257, b*257)
		responseStr := fmt.Sprintf("\x1b]10;rgb:%04x/%04x/%04x\a", r*257, g*257, b*257)

		// 3. Write the response back to the PTY for vi to read
		a.pty.Write([]byte(responseStr))
	}

	// This handler will be called when the VTerm receives OSC 11;?
	queryBgHandler := func() {
		if a.pty == nil {
			return
		}

		color := a.colorPalette[257]
		r, g, b := color.RGB()
		log.Printf("Responding query with %d,%d,%d", r, g, b)
		log.Printf("Response: \\x1b]10;rgb:%04x/%04x/%04x\a", r*257, g*257, b*257)
		responseStr := fmt.Sprintf("\x1b]11;rgb:%04x/%04x/%04x\a", r*257, g*257, b*257)
		a.pty.Write([]byte(responseStr))
	}

	screenRestoredHandler := func() {
		log.Print("RESTORE HANDLER CALLED")
		go a.Resize(a.width, a.height)
	}

	titleChangeHandler := func(newTitle string) {
		a.title = newTitle
		if a.refreshChan != nil {
			// Non-blocking send to avoid deadlocks if the channel is full
			select {
			case a.refreshChan <- true:
			default:
			}
		}
	}
	ptyWriter := func(b []byte) {
		if a.pty != nil {
			a.pty.Write(b)
		}
	}
	a.vterm = parser.NewVTerm(cols, rows,
		parser.WithTitleChangeHandler(titleChangeHandler),
		parser.WithPtyWriter(ptyWriter),
		parser.WithDefaultFgChangeHandler(fgChangeHandler),
		parser.WithDefaultBgChangeHandler(bgChangeHandler),
		parser.WithQueryDefaultFgHandler(queryFgHandler),
		parser.WithQueryDefaultBgHandler(queryBgHandler),
		parser.WithScreenRestoredHandler(screenRestoredHandler),
	)
	a.parser = parser.NewParser(a.vterm)
	a.mu.Unlock()

	a.wg.Add(1)

	go func() {
		defer a.wg.Done()
		defer ptmx.Close()

		// Create a buffered reader for robust rune-wise reading
		reader := bufio.NewReader(ptmx)
		wasInSync := false

		for {
			// Check for the stop signal before blocking on ReadRune
			select {
			case <-a.stop:
				return
			default:
			}

			// Read a single, complete rune, handling UTF-8 boundaries automatically.
			r, _, err := reader.ReadRune()
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading from PTY: %v", err)
				}
				return
			}

			a.mu.Lock()
			if a.parser != nil {
				a.parser.Parse(r)
			}
			inSync := a.vterm.InSynchronizedUpdate
			a.mu.Unlock()

			// Request a refresh after processing a rune.
			if !inSync {
				if wasInSync {
					// We just finished a synchronized update, so draw the final frame.
					a.vterm.MarkAllDirty() // Ensure a full redraw
				}
				// In normal operation or after a sync block, send a refresh.
				if a.refreshChan != nil {
					select {
					case a.refreshChan <- true:
					default:
					}
				}
			}
		}
	}()

	return cmd.Wait()
}

func (a *TexelTerm) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	a.width = cols
	a.height = rows

	if a.vterm != nil {
		a.vterm.Resize(cols, rows)
	}

	if a.pty != nil {
		pty.Setsize(a.pty, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
		//a.pty.Write([]byte{'\x0C'})
	}
}
func (a *TexelTerm) Stop() {
	close(a.stop)

	if a.pty != nil {
		a.pty.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Signal(syscall.SIGTERM)
	}
}

func (a *TexelTerm) GetTitle() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

func newDefaultPalette() [258]tcell.Color {
	// Based on the standard xterm 256 color palette.
	var p [258]tcell.Color
	// First 16 ANSI colors
	p[0] = tcell.NewRGBColor(0, 0, 0)        // Black
	p[1] = tcell.NewRGBColor(128, 0, 0)      // Maroon
	p[2] = tcell.NewRGBColor(0, 128, 0)      // Green
	p[3] = tcell.NewRGBColor(128, 128, 0)    // Olive
	p[4] = tcell.NewRGBColor(0, 0, 128)      // Navy
	p[5] = tcell.NewRGBColor(128, 0, 128)    // Purple
	p[6] = tcell.NewRGBColor(0, 128, 128)    // Teal
	p[7] = tcell.NewRGBColor(192, 192, 192)  // Silver
	p[8] = tcell.NewRGBColor(128, 128, 128)  // Grey
	p[9] = tcell.NewRGBColor(255, 0, 0)      // Red
	p[10] = tcell.NewRGBColor(0, 255, 0)     // Lime
	p[11] = tcell.NewRGBColor(255, 255, 0)   // Yellow
	p[12] = tcell.NewRGBColor(0, 0, 255)     // Blue
	p[13] = tcell.NewRGBColor(255, 0, 255)   // Fuchsia
	p[14] = tcell.NewRGBColor(0, 255, 255)   // Aqua
	p[15] = tcell.NewRGBColor(255, 255, 255) // White

	// 6x6x6 color cube
	levels := []int32{0, 95, 135, 175, 215, 255}
	i := 16
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				p[i] = tcell.NewRGBColor(levels[r], levels[g], levels[b])
				i++
			}
		}
	}

	// Grayscale ramp
	for j := 0; j < 24; j++ {
		gray := int32(8 + j*10)
		p[i] = tcell.NewRGBColor(gray, gray, gray)
		i++
	}

	// Default Foreground (White) and Background (Black)
	p[256] = p[15]
	p[257] = p[0]

	return p
}

// DumpState implements the texel.DebuggableApp interface.
// It logs the critical color state and grid contents of the vterm.
func (a *TexelTerm) DumpState(frameNum int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.vterm == nil {
		log.Printf("    VTerm is nil.")
		return
	}

	// Log the crucial VTerm color state at this exact moment.
	log.Printf("    VTerm State: defaultBG=[%s], currentBG=[%s]", a.vterm.DefaultBG(), a.vterm.CurrentBG()) // We will add these getter methods next

	grid := a.vterm.Grid()
	if grid == nil {
		log.Printf("    VTerm Grid is nil.")
		return
	}
	// Log any cells that have a non-default background to see what vi has drawn
	for r, row := range grid {
		var line string
		for c, cell := range row {
			// Compare the cell's background to the VTerm's dynamic default
			if cell.BG != a.vterm.DefaultBG() {
				line += fmt.Sprintf(" | Col:%d Char:'%c' BG:[%s] ", c, cell.Rune, cell.BG)
			}
		}
		if line != "" {
			log.Printf("    Row %d:%s", r, line)
		}
	}
}
