package texelterm

import (
	"log"
	"os"
	"os/exec"
	//	"strconv"
	"sync"
	"syscall"
	"texelation/apps/texelterm/parser"
	"texelation/texel"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2" // Import tcell
)

// The texelTerm struct remains the same, but its Render method will produce tcell-compatible output.
type texelTerm struct {
	title       string
	command     string
	width       int
	height      int
	cmd         *exec.Cmd
	pty         *os.File
	vterm       *parser.VTerm
	parser      *parser.Parser
	mu          sync.Mutex
	stop        chan struct{}
	refreshChan chan<- bool
}

// SetRefreshNotifier implements the new interface method.
func (a *texelTerm) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
}

// Render translates our VTerm's state into the main application's buffer.
func (a *texelTerm) Render() [][]texel.Cell {
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

	buffer := make([][]texel.Cell, rows)
	cursorX, cursorY := a.vterm.Cursor()
	cursorVisible := a.vterm.CursorVisible()

	for y := 0; y < rows; y++ {
		buffer[y] = make([]texel.Cell, cols)
		for x := 0; x < cols; x++ {
			parserCell := vtermGrid[y][x]
			// The new apply function returns a tcell.Style
			buffer[y][x] = applyParserStyle(parserCell)

			if cursorVisible && x == cursorX && y == cursorY {
				// To show the cursor, we just get the style and reverse it.
				buffer[y][x].Style = buffer[y][x].Style.Reverse(true)
			}
		}
	}
	return buffer
}

func applyParserStyle(pCell parser.Cell) texel.Cell {
	style := tcell.StyleDefault
	fg := mapParserColorToTCell(pCell.FG)
	bg := mapParserColorToTCell(pCell.BG)
	style = style.Foreground(fg).Background(bg)
	style = style.Bold(pCell.Attr&parser.AttrBold != 0)
	style = style.Underline(pCell.Attr&parser.AttrUnderline != 0)
	style = style.Reverse(pCell.Attr&parser.AttrReverse != 0)
	return texel.Cell{
		Ch:    pCell.Rune,
		Style: style,
	}
}

// mapParserColorToTCell translates our custom Color type to a tcell.Color.
func mapParserColorToTCell(c parser.Color) tcell.Color {
	switch c.Mode {
	case parser.ColorModeDefault:
		return tcell.ColorDefault
	case parser.ColorModeStandard:
		return tcell.PaletteColor(int(c.Value))
	case parser.ColorMode256:
		return tcell.PaletteColor(int(c.Value))
	case parser.ColorModeRGB:
		return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
	default:
		return tcell.ColorDefault
	}
}

func (a *texelTerm) HandleKey(ev *tcell.EventKey) {
	if a.pty == nil {
		return
	}

	a.mu.Lock()
	appMode := a.vterm.AppCursorKeys()
	a.mu.Unlock()

	key := ev.Key()
	var keyBytes []byte

	// Use a switch to handle special keys first.
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
	// F5-F12 have more complex sequences, add as needed
	// ...

	case tcell.KeyEnter:
		keyBytes = []byte("\r")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// KeyBackspace is Ctrl-H, KeyBackspace2 is the real backspace
		keyBytes = []byte{'\b'}
	case tcell.KeyTab:
		keyBytes = []byte("\t")
	case tcell.KeyEsc:
		keyBytes = []byte("\x1b")

	// If it's not a special key, it's a rune or a Ctrl-key combo
	default:
		keyBytes = []byte(string(ev.Rune()))
	}

	if keyBytes != nil {
		a.pty.Write(keyBytes)
	}
}

func New(title, command string) texel.App {
	return &texelTerm{
		title:   title,
		command: command,
		width:   80, // Sensible defaults
		height:  24,
		stop:    make(chan struct{}),
	}
}

func (a *texelTerm) Run() error {
	a.mu.Lock()
	cols := a.width
	rows := a.height
	a.mu.Unlock()

	cmd := exec.Command(a.command)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		//		"COLUMNS="+strconv.Itoa(cols),
		//		"LINES="+strconv.Itoa(rows),
	)

	// Use pty.StartWithSize for a simpler, more reliable startup.
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
	titleChangeHandler := func(newTitle string) { a.title = newTitle }
	ptyWriter := func(b []byte) {
		if a.pty != nil {
			a.pty.Write(b)
		}
	}
	a.vterm = parser.NewVTerm(cols, rows,
		parser.WithTitleChangeHandler(titleChangeHandler),
		parser.WithPtyWriter(ptyWriter),
	)
	a.parser = parser.NewParser(a.vterm)
	a.mu.Unlock()

	// Start the reading goroutine
	go func() {
		defer ptmx.Close()
		buf := make([]byte, 4096)
		for {
			select {
			case <-a.stop:
				return
			default:
				n, err := ptmx.Read(buf)
				if n > 0 {
					a.mu.Lock()
					if a.parser != nil {
						a.parser.Parse(buf[:n])
					}
					a.mu.Unlock()
					// --- NEW: Signal that the screen needs a redraw ---
					if a.refreshChan != nil {
						// Non-blocking send. If a redraw is already pending, we don't need to send another.
						select {
						case a.refreshChan <- true:
						default:
						}
					}
				}
				if err != nil {
					return
				}
			}
		}
	}()

	return cmd.Wait()
}

func (a *texelTerm) Resize(cols, rows int) {
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
	}
}
func (a *texelTerm) Stop() {
	// --- MODIFIED: Added a detailed comment explaining shutdown logic ---
	// NOTE on graceful shutdown:
	// There is a potential race condition here. We signal the reading goroutine to stop
	// via the 'stop' channel and immediately kill the process. The goroutine might not
	// have finished processing the last of the output from the PTY.
	// A more robust solution would involve using a sync.WaitGroup to wait for the
	// reading goroutine to exit *before* killing the process, ensuring all I/O is handled.
	close(a.stop)
	if a.pty != nil {
		a.pty.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		// --- CORRECTED LOGIC ---
		// Send a SIGTERM signal for a graceful shutdown instead of SIGKILL.
		a.cmd.Process.Signal(syscall.SIGTERM)
		// --- END CORRECTION ---
	}
}

func (a *texelTerm) GetTitle() string {
	// This method now returns the dynamically updated title!
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}
