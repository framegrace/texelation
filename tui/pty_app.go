package tui

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"textmode-env/tui/parser"

	"github.com/creack/pty"
	"github.com/nsf/termbox-go"
)

// PTYApp uses our own custom parser.
type PTYApp struct {
	title   string
	command string
	cmd     *exec.Cmd
	pty     *os.File
	vterm   *parser.VTerm
	parser  *parser.Parser
	mu      sync.Mutex
	stop    chan struct{}
}

// mapParserColorToTermbox converts our rich Color type to a simple termbox attribute.
func mapParserColorToTermbox(c parser.Color) termbox.Attribute {
	switch c.Mode {
	case parser.ColorModeDefault:
		return termbox.ColorDefault
	case parser.ColorModeStandard:
		// Standard colors 0-7 are Black, Red, ... White
		// Standard colors 8-15 are the bright versions
		if c.Value < 8 {
			return termbox.Attribute(c.Value + 1)
		}
		// Bright colors (8-15) are rendered as bold in termbox
		return termbox.Attribute(c.Value-8+1) | termbox.AttrBold
	case parser.ColorMode256:
		// This is a simple approximation of 256 colors to the basic 16.
		// A more complex mapping could be implemented here for better results.
		val := c.Value
		switch {
		case val >= 232: // Grayscale
			return termbox.ColorWhite // Approximate grayscale to white/black
		case val >= 16: // 6x6x6 color cube
			// Approximate to the nearest of the 6 standard colors
			// This is a very rough approximation
			r := (val - 16) / 36
			g := ((val - 16) % 36) / 6
			b := (val - 16) % 6
			if r > 2 || g > 2 || b > 2 {
				return termbox.ColorWhite | termbox.AttrBold
			}
			return termbox.ColorDefault
		default: // Basic 16 colors
			if val < 8 {
				return termbox.Attribute(val + 1)
			}
			return termbox.Attribute(val-8+1) | termbox.AttrBold
		}
	default:
		return termbox.ColorDefault
	}
}

// Render translates our VTerm's state into the main application's buffer.
func (a *PTYApp) Render() [][]Cell {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.vterm == nil {
		return [][]Cell{}
	}

	vtermGrid := a.vterm.Grid()
	rows := len(vtermGrid)
	if rows == 0 {
		return [][]Cell{}
	}
	cols := len(vtermGrid[0])

	buffer := make([][]Cell, rows)
	cursorX, cursorY := a.vterm.Cursor()
	cursorVisible := a.vterm.CursorVisible()

	for y := 0; y < rows; y++ {
		buffer[y] = make([]Cell, cols)
		for x := 0; x < cols; x++ {
			parserCell := vtermGrid[y][x]
			buffer[y][x] = applyParserStyle(parserCell)

			if cursorVisible && x == cursorX && y == cursorY {
				fg, bg := buffer[y][x].Fg, buffer[y][x].Bg
				buffer[y][x].Fg = bg
				buffer[y][x].Bg = fg
			}
		}
	}
	return buffer
}

// applyParserStyle translates our new parser.Cell into the main tui.Cell for rendering.
func applyParserStyle(pCell parser.Cell) Cell {
	// Translate colors, including approximating 256-color codes
	fg := mapParserColorToTermbox(pCell.FG)
	bg := mapParserColorToTermbox(pCell.BG)

	// Translate attributes
	if pCell.Attr&parser.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	// Note: Bold is handled by the color mapping for bright colors now.
	// We can still add the explicit bold attribute for non-colored bold text.
	if pCell.Attr&parser.AttrBold != 0 {
		fg |= termbox.AttrBold
	}
	if pCell.Attr&parser.AttrUnderline != 0 {
		fg |= termbox.AttrUnderline
	}

	return Cell{
		Ch: pCell.Rune,
		Fg: fg,
		Bg: bg,
	}
}

// --- Rest of file is unchanged ---

func NewPTYApp(title, command string) *PTYApp {
	return &PTYApp{
		title:   title,
		command: command,
		stop:    make(chan struct{}),
	}
}

func (a *PTYApp) Run() error {
	cols, rows := 80, 24
	a.mu.Lock()
	if a.vterm != nil {
		grid := a.vterm.Grid()
		rows = len(grid)
		if rows > 0 {
			cols = len(grid[0])
		}
	}
	a.mu.Unlock()

	cmd := exec.Command(a.command)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLUMNS="+strconv.Itoa(cols),
		"LINES="+strconv.Itoa(rows),
	)
	a.cmd = cmd

	var err error
	a.pty, err = pty.Start(cmd)
	if err != nil {
		log.Printf("Failed to start pty for command '%s': %v", a.command, err)
		return err
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-a.stop:
				return
			default:
				n, err := a.pty.Read(buf)
				if n > 0 {
					a.mu.Lock()
					if a.parser != nil {
						a.parser.Parse(buf[:n])
					}
					a.mu.Unlock()
				}
				if err != nil {
					return
				}
			}
		}
	}()
	return nil
}

func (a *PTYApp) Stop() {
	close(a.stop)
	if a.pty != nil {
		a.pty.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Kill()
	}
}

// Resize creates our VTerm and Parser and informs the PTY of the size change.
func (a *PTYApp) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// --- NEW: Define the callback function ---
	titleChangeHandler := func(newTitle string) {
		// This function will be called by the VTerm when a title change occurs.
		// We need to lock the mutex since this can be called from the parser's goroutine.
		//a.mu.Lock()
		//defer a.mu.Unlock()
		a.title = newTitle
	}

	// NEW: Define the callback that writes back to the PTY
	ptyWriter := func(b []byte) {
		if a.pty != nil {
			a.pty.Write(b)
		}
	}

	// Create our virtual terminal, now with both handlers
	a.vterm = parser.NewVTerm(cols, rows,
		parser.WithTitleChangeHandler(titleChangeHandler),
		parser.WithPtyWriter(ptyWriter),
	)
	a.parser = parser.NewParser(a.vterm)

	// Inform the PTY of the size change
	if a.pty != nil {
		pty.Setsize(a.pty, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}
}

func (a *PTYApp) GetTitle() string {
	// This method now returns the dynamically updated title!
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}
