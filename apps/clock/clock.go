package clock // Package name changed from tui

import (
	"fmt"
	"sync"
	"texelation/texel" // Import the core DE package
	"time"

	"github.com/gdamore/tcell/v2"
)

// clockApp is now unexported to hide implementation details.
type clockApp struct {
	width, height int
	currentTime   string
	mu            sync.RWMutex
	stop          chan struct{}
	refreshChan   chan<- bool
	buf           [][]texel.Cell
}

// NewClockApp creates a new ClockApp and returns it as a texel.App interface.
func NewClockApp() texel.App {
	return &clockApp{
		stop: make(chan struct{}),
	}
}

// HandleKey does nothing for the clock app.
func (a *clockApp) HandleKey(ev *tcell.EventKey) {}

func (a *clockApp) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
}

// Run starts a ticker to update the time every second.
func (a *clockApp) Run() error {
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
				// Non-blocking send
				select {
				case a.refreshChan <- true:
				default:
				}
			}
		case <-a.stop:
			return nil
		}
	}
}

// Stop signals the Run loop to terminate.
func (a *clockApp) Stop() {
	close(a.stop)
}

// Resize stores the new dimensions of the pane.
func (a *clockApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
}

// Render now returns a buffer of texel.Cell
func (a *clockApp) Render() [][]texel.Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.width <= 0 || a.height <= 0 {
		return [][]texel.Cell{}
	}

	if len(a.buf) != a.width || (a.height > 0 && cap(a.buf[0]) != a.width) {
		a.buf = make([][]texel.Cell, a.height)
		for y := 0; y < a.height; y++ {
			a.buf[y] = make([]texel.Cell, a.width)
		}
	}

	for i := range a.buf {
		for j := range a.buf[i] {
			a.buf[i][j] = texel.Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}

	style := tcell.StyleDefault.Foreground(tcell.PaletteColor(6))

	str := fmt.Sprintf("Time: %s", a.currentTime)
	y := a.height / 2
	x := (a.width - len(str)) / 2

	if y < a.height && x >= 0 {
		for i, ch := range str {
			if x+i < a.width {
				a.buf[y][x+i] = texel.Cell{Ch: ch, Style: style}
			}
		}
	}

	return a.buf
}

func (a *clockApp) GetTitle() string {
	return "Clock"
}
