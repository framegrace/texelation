package tui

import (
	"sync"

	"github.com/gdamore/tcell/v2"
)

// WelcomeApp is a simple internal widget that displays a static welcome message.
type WelcomeApp struct {
	width, height int
	mu            sync.RWMutex
}

// NewWelcomeApp now returns the App interface for consistency.
func NewWelcomeApp() App {
	return &WelcomeApp{}
}

func (a *WelcomeApp) Run() error {
	// No background process needed for this static app.
	return nil
}

func (a *WelcomeApp) Stop() {}

func (a *WelcomeApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
}

func (a *WelcomeApp) Render() [][]Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.width <= 0 || a.height <= 0 {
		return [][]Cell{}
	}

	buffer := make([][]Cell, a.height)
	for i := range buffer {
		buffer[i] = make([]Cell, a.width)
		for j := range buffer[i] {
			buffer[i][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}

	style := tcell.StyleDefault.Foreground(tcell.ColorGreen)

	messages := []string{
		"Welcome!",
		"This is a textmode DE.",
		"Press 'Ctrl-A' to enter Control Mode.",
		"Then 'Tab' to switch panes, 'q' to quit.",
	}

	for i, msg := range messages {
		y := (a.height / 2) - len(messages)/2 + i
		x := (a.width - len(msg)) / 2
		if y >= 0 && y < a.height && x >= 0 {
			for j, ch := range msg {
				if x+j < a.width {
					buffer[y][x+j] = Cell{Ch: ch, Style: style}
				}
			}
		}
	}
	return buffer
}

func (a *WelcomeApp) GetTitle() string {
	return "Welcome"
}

func (a *WelcomeApp) HandleKey(ev *tcell.EventKey) {
	// This app doesn't handle key presses.
}

// SetRefreshNotifier satisfies the interface, but this static app doesn't need to do anything with it.
func (a *WelcomeApp) SetRefreshNotifier(refreshChan chan<- bool) {
	// No-op
}
