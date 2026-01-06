package cards

import (
	texelcore "github.com/framegrace/texelui/core"
	"time"

	"github.com/gdamore/tcell/v2"
)

// AlternatingCard wraps another card and renders it only on specific frames
// defined by Phase and Period. It automatically requests refreshes to drive the animation.
type AlternatingCard struct {
	wrapped Card
	period  int
	phase   int
	stopCh  chan struct{}
	counter uint64
}

// var globalFrameCounter uint64

func NewAlternatingCard(wrapped Card, period, phase int) *AlternatingCard {
	return &AlternatingCard{
		wrapped: wrapped,
		period:  period,
		phase:   phase,
		stopCh:  make(chan struct{}),
	}
}

func (c *AlternatingCard) Run() error {
	// We need to drive the refresh loop.
	// But wrapped.Run() might block.
	// We should start a ticker in a goroutine if this card is running.
	// But Card.Run is usually blocking.
	// Let's defer to wrapped.Run() but add a sidecar ticker.

	// Note: accessing 'c' inside goroutine is safe if config is static.

	// We don't have access to the refresh channel easily here unless we capture it in SetRefreshNotifier.
	return c.wrapped.Run()
}

func (c *AlternatingCard) Stop() {
	close(c.stopCh)
	c.wrapped.Stop()
}

func (c *AlternatingCard) Resize(cols, rows int) {
	c.wrapped.Resize(cols, rows)
}

func (c *AlternatingCard) Render(input [][]texelcore.Cell) [][]texelcore.Cell {
	c.counter++
	shouldRender := (c.counter % uint64(c.period)) == uint64(c.phase)

	if shouldRender {
		return c.wrapped.Render(input)
	}
	return input
}

func (c *AlternatingCard) HandleKey(ev *tcell.EventKey) {
	c.wrapped.HandleKey(ev)
}

func (c *AlternatingCard) GetTitle() string {
	if titled, ok := c.wrapped.(interface{ GetTitle() string }); ok {
		return titled.GetTitle()
	}
	return ""
}

type refreshCapturer struct {
	ch chan<- bool
}

func (c *AlternatingCard) SetRefreshNotifier(refreshChan chan<- bool) {
	c.wrapped.SetRefreshNotifier(refreshChan)

	// Start a ticker to force refresh at ~120fps
	go func() {
		ticker := time.NewTicker(8 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
				select {
				case refreshChan <- true:
				default:
				}
			}
		}
	}()
}
