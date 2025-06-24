package welcome // Package name changed from tui

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"texelation/texel" // Import the core DE package
)

// welcomeApp is a simple internal widget that displays a static welcome message.
type welcomeApp struct {
	width, height int
	mu            sync.RWMutex
}

// NewWelcomeApp now returns the App interface for consistency.
func NewWelcomeApp() texel.App {
	return &welcomeApp{}
}

func (a *welcomeApp) HandleMessage(msg texel.Message) {
	// This app doesn't handle messages.
}

func (a *welcomeApp) Run() error {
	// No background process needed for this static app.
	return nil
}

func (a *welcomeApp) Stop() {}

func (a *welcomeApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
}

func (a *welcomeApp) Render() [][]texel.Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.width <= 0 || a.height <= 0 {
		return [][]texel.Cell{}
	}

	buffer := make([][]texel.Cell, a.height)
	for i := range buffer {
		buffer[i] = make([]texel.Cell, a.width)
		for j := range buffer[i] {
			buffer[i][j] = texel.Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}

	style := tcell.StyleDefault.Foreground(tcell.ColorGreen.TrueColor())

	messages := []string{
		"Welcome to Texelation!",
		"",
		"Press 'Ctrl-A' to enter Control Mode, then:",
		"  | or h  - Split horizontally",
		"  - or v  - Split vertically",
		"  x       - Close active pane",
		"  w, arrow- Swap active pane with neighbor",
		"",
		"Press 'Shift-Arrow' to navigate panes anytime.",
		"Press 'Ctrl-Q' to quit.",
	}

	for i, msg := range messages {
		y := (a.height / 2) - len(messages)/2 + i
		x := (a.width - len(msg)) / 2
		if y >= 0 && y < a.height && x >= 0 {
			for j, ch := range msg {
				if x+j < a.width {
					buffer[y][x+j] = texel.Cell{Ch: ch, Style: style}
				}
			}
		}
	}
	return buffer
}

func (a *welcomeApp) GetTitle() string {
	return "Welcome"
}

func (a *welcomeApp) HandleKey(ev *tcell.EventKey) {
	// This app doesn't handle key presses.
}

// SetRefreshNotifier satisfies the interface, but this static app doesn't need to do anything with it.
func (a *welcomeApp) SetRefreshNotifier(refreshChan chan<- bool) {
	// No-op
}
