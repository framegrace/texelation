package cards

import (
	"math"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// RainbowCard applies a simple rainbow tint to the incoming buffer when enabled.
type RainbowCard struct {
	mu         sync.Mutex
	enabled    bool
	speedHz    float64
	mix        float32
	phase      float64
	lastUpdate time.Time
	refresh    chan<- bool

	ticker     *time.Ticker
	tickerStop chan struct{}
}

// NewRainbowCard constructs a rainbow effect card.
func NewRainbowCard(speedHz float64, mix float32) *RainbowCard {
	if speedHz <= 0 {
		speedHz = 0.5
	}
	if mix < 0 {
		mix = 0
	} else if mix > 1 {
		mix = 1
	}
	return &RainbowCard{speedHz: speedHz, mix: mix}
}

func (c *RainbowCard) Run() error { return nil }
func (c *RainbowCard) Stop() {
	c.mu.Lock()
	c.stopTickerLocked()
	c.mu.Unlock()
}
func (c *RainbowCard) Resize(int, int)             {}
func (c *RainbowCard) HandleKey(*tcell.EventKey)   {}
func (c *RainbowCard) HandleMessage(texel.Message) {}
func (c *RainbowCard) SetRefreshNotifier(ch chan<- bool) {
	c.mu.Lock()
	c.refresh = ch
	c.mu.Unlock()
}

// Render tints the buffer when enabled.

func (c *RainbowCard) Render(input [][]texel.Cell) [][]texel.Cell {
	if input == nil {
		return nil
	}
	c.mu.Lock()
	enabled := c.enabled
	speed := c.speedHz
	mix := c.mix
	c.mu.Unlock()

	if !enabled {
		return input
	}

	now := time.Now()
	c.mu.Lock()
	if c.lastUpdate.IsZero() {
		c.lastUpdate = now
	}
	delta := now.Sub(c.lastUpdate).Seconds()
	c.lastUpdate = now
	c.phase = math.Mod(c.phase+2*math.Pi*speed*delta, 2*math.Pi)
	phase := c.phase
	c.mu.Unlock()

	out := cloneBuffer(input)

	for y := range out {
		row := out[y]
		for x := range row {
			cell := &row[x]
			fg, bg, attrs := cell.Style.Decompose()
			if !fg.Valid() {
				continue
			}
			angle := phase + float64(x+y)*0.12
			tint := hsvToRGB(float32(angle), 1.0, 1.0)
			blended := blendColor(fg, tint, mix)
			style := tcell.StyleDefault.Foreground(blended).Attributes(attrs)
			if bg.Valid() {
				style = style.Background(bg)
			}
			cell.Style = style
		}
	}

	return out
}

// Toggle flips the enabled state.
func (c *RainbowCard) Toggle() {
	c.mu.Lock()
	c.enabled = !c.enabled
	if c.enabled {
		c.lastUpdate = time.Now()
		c.ensureTickerLocked()
	} else {
		c.stopTickerLocked()
	}
	c.mu.Unlock()
	c.requestRefresh()
}

// Enabled reports whether the card is active.
func (c *RainbowCard) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

func (c *RainbowCard) requestRefresh() {
	if c.refresh == nil {
		return
	}
	select {
	case c.refresh <- true:
	default:
	}
}

func (c *RainbowCard) ensureTickerLocked() {
	if c.ticker != nil {
		return
	}
	c.ticker = time.NewTicker(50 * time.Millisecond)
	c.tickerStop = make(chan struct{})
	go func(stop <-chan struct{}, tick *time.Ticker) {
		for {
			select {
			case <-tick.C:
				c.requestRefresh()
			case <-stop:
				return
			}
		}
	}(c.tickerStop, c.ticker)
}

func (c *RainbowCard) stopTickerLocked() {
	if c.ticker == nil {
		return
	}
	c.ticker.Stop()
	close(c.tickerStop)
	c.ticker = nil
	c.tickerStop = nil
}

func blendColor(base, overlay tcell.Color, intensity float32) tcell.Color {
	if !overlay.Valid() || intensity <= 0 {
		return base
	}
	if !base.Valid() {
		return overlay
	}
	br, bg, bb := base.RGB()
	or, og, ob := overlay.RGB()
	blend := func(bc, oc int32) int32 {
		return int32(float32(bc)*(1-intensity) + float32(oc)*intensity)
	}
	return tcell.NewRGBColor(blend(br, or), blend(bg, og), blend(bb, ob))
}

func hsvToRGB(angle float32, saturation float32, value float32) tcell.Color {
	h := float32(math.Mod(float64(angle), 2*math.Pi)) / (2 * math.Pi) * 360
	c := value * saturation
	x := c * (1 - float32(math.Abs(math.Mod(float64(h/60), 2)-1)))
	m := value - c
	var r, g, b float32
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	r, g, b = (r+m)*255, (g+m)*255, (b+m)*255
	return tcell.NewRGBColor(int32(r), int32(g), int32(b))
}
