package cards

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

const (
	// FlashTriggerID is the control identifier used to activate the flash overlay.
	FlashTriggerID          = "effects.flash"
	flashDefaultDescription = "Activate a transient flash overlay"
)

// FlashCard renders a visual overlay for a brief period when triggered.
type FlashCard struct {
	mu       sync.Mutex
	duration time.Duration
	color    tcell.Color
	refresh  chan<- bool
	timer    *time.Timer
	active   bool
}

// NewFlashCard constructs a flash effect with the provided duration and color.
func NewFlashCard(duration time.Duration, color tcell.Color) *FlashCard {
	if duration <= 0 {
		duration = 100 * time.Millisecond
	}
	if !color.Valid() {
		color = tcell.ColorWhite
	}
	return &FlashCard{duration: duration, color: color}
}

func (c *FlashCard) Run() error                        { return nil }
func (c *FlashCard) Stop()                             { c.deactivate() }
func (c *FlashCard) Resize(int, int)                   {}
func (c *FlashCard) HandleKey(*tcell.EventKey)         {}
func (c *FlashCard) HandleMessage(texel.Message)       {}
func (c *FlashCard) SetRefreshNotifier(ch chan<- bool) { c.refresh = ch }

// Active reports whether the flash overlay is currently shown.
func (c *FlashCard) Active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// RegisterControls wires the flash trigger onto the pipeline bus.
func (c *FlashCard) RegisterControls(reg ControlRegistry) error {
	return reg.Register(FlashTriggerID, flashDefaultDescription, func(interface{}) error {
		c.activate()
		return nil
	})
}

func (c *FlashCard) activate() {
	c.mu.Lock()
	c.active = true
	if c.timer != nil {
		c.timer.Stop()
	}
	duration := c.duration
	c.timer = time.AfterFunc(duration, c.deactivate)
	c.mu.Unlock()
	c.requestRefresh()
}

func (c *FlashCard) deactivate() {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	wasActive := c.active
	c.active = false
	c.mu.Unlock()
	if wasActive {
		c.requestRefresh()
	}
}

func (c *FlashCard) requestRefresh() {
	if c.refresh == nil {
		return
	}
	select {
	case c.refresh <- true:
	default:
	}
}

func (c *FlashCard) Render(input [][]texel.Cell) [][]texel.Cell {
	if input == nil {
		return nil
	}
	c.mu.Lock()
	active := c.active
	overlayColor := c.color
	c.mu.Unlock()
	if !active {
		return input
	}

	out := cloneBuffer(input)
	for y := range out {
		row := out[y]
		for x := range row {
			cell := &row[x]
			fg, _, attrs := cell.Style.Decompose()
			style := tcell.StyleDefault.Foreground(fg).Attributes(attrs).Background(overlayColor)
			cell.Style = style
		}
	}
	return out
}
