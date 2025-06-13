package tui

import (
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
)

// ClockApp is a simple internal widget that displays the current time.
type ClockApp struct {
	width, height int
	currentTime   string
	mu            sync.RWMutex
	stop          chan struct{}
	refreshChan   chan<- bool
}

// NewClockApp creates a new ClockApp.
func NewClockApp() *ClockApp {
	return &ClockApp{
		stop: make(chan struct{}),
	}
}

// HandleKey does nothing for the clock app.
func (a *ClockApp) HandleKey(ev *tcell.EventKey) {}

func (a *ClockApp) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
}

// Run starts a ticker to update the time every second.
func (a *ClockApp) Run() error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	updateTime := func() {
		a.mu.Lock()
		a.currentTime = time.Now().Format("15:04:05")
		a.mu.Unlock()
	}
	updateTime()

	for {
		select {
		case <-ticker.C:
			updateTime()
			if a.refreshChan != nil {
				a.refreshChan <- true
			}
		case <-a.stop:
			return nil
		}
	}
}

// Stop signals the Run loop to terminate.
func (a *ClockApp) Stop() {
	close(a.stop)
}

// Resize stores the new dimensions of the pane.
func (a *ClockApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
}

// Render draws the current time centered in its buffer.
func (a *ClockApp) Render() [][]Cell {
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

	// CORRECTED: Use tcell.PaletteColor(6) for Cyan.
	style := tcell.StyleDefault.Foreground(tcell.PaletteColor(6))

	str := fmt.Sprintf("Time: %s", a.currentTime)
	y := a.height / 2
	x := (a.width - len(str)) / 2

	if y < a.height && x >= 0 {
		for i, ch := range str {
			if x+i < a.width {
				buffer[y][x+i] = Cell{Ch: ch, Style: style}
			}
		}
	}

	return buffer
}

func (a *ClockApp) GetTitle() string {
	return "Clock"
}
