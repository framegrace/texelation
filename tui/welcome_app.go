package tui

import (
	"sync"

	"github.com/nsf/termbox-go"
)

// WelcomeApp is a simple internal widget that displays a static welcome message.
type WelcomeApp struct {
	width, height int
	mu            sync.RWMutex
}

// NewWelcomeApp creates a new WelcomeApp.
func NewWelcomeApp() *WelcomeApp {
	return &WelcomeApp{}
}

// Run does nothing as this app is static.
func (a *WelcomeApp) Run() error {
	return nil // No background process needed
}

// Stop does nothing.
func (a *WelcomeApp) Stop() {}

// Resize stores the new dimensions of the pane.
func (a *WelcomeApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
}

// Render draws the welcome message.
func (a *WelcomeApp) Render() [][]Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.width <= 0 || a.height <= 0 {
		return [][]Cell{}
	}

	// Create a buffer of the correct size
	buffer := make([][]Cell, a.height)
	for i := range buffer {
		buffer[i] = make([]Cell, a.width)
		for j := range buffer[i] {
			buffer[i][j] = Cell{Ch: ' '}
		}
	}

	messages := []string{
		"Welcome!",
		"This is a textmode DE.",
		"Press 'q' or 'Esc' to quit.",
	}

	for i, msg := range messages {
		y := (a.height / 2) - len(messages)/2 + i
		x := (a.width - len(msg)) / 2
		if y >= 0 && y < a.height && x >= 0 {
			for j, ch := range msg {
				if x+j < a.width {
					buffer[y][x+j] = Cell{Ch: ch, Fg: termbox.ColorGreen}
				}
			}
		}
	}
	return buffer
}

func (a *WelcomeApp) GetTitle() string {
	return "Welcome"
}
