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

const (
	fadeForegroundMix      = 0.45
	backgroundBlendMix     = 0.4
	defaultBackgroundBlend = 0.3
)

var baseBackgroundColor = tcell.ColorBlack

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
		source := input[y]
		for x := range row {
			cell := &row[x]
			fg, bg, attrs := source[x].Style.Decompose()
			style := tcell.StyleDefault.Attributes(attrs)

			finalFg := fg
			finalBg := bg

			if shouldFadeForeground(source, x) {
				finalFg = blendColors(fg, overlayColor, fadeForegroundMix)
				if !bg.Valid() {
					finalBg = blendColors(baseBackgroundColor, overlayColor, defaultBackgroundBlend)
				}
			} else {
				if bg.Valid() {
					finalBg = blendColors(bg, overlayColor, backgroundBlendMix)
				} else {
					finalBg = blendColors(baseBackgroundColor, overlayColor, defaultBackgroundBlend)
				}
			}

			if finalFg.Valid() {
				style = style.Foreground(finalFg)
			}
			if finalBg.Valid() {
				style = style.Background(finalBg)
			}

			cell.Style = style
		}
	}
	return out
}

func shouldFadeForeground(row []texel.Cell, idx int) bool {
	fg, bg, _ := row[idx].Style.Decompose()
	if !fg.Valid() || !bg.Valid() || fg != bg {
		return false
	}
	if idx > 0 {
		lf, lb, _ := row[idx-1].Style.Decompose()
		if lf == fg && lb == bg {
			return true
		}
	}
	if idx+1 < len(row) {
		rf, rb, _ := row[idx+1].Style.Decompose()
		if rf == fg && rb == bg {
			return true
		}
	}
	return false
}

func blendColors(base, overlay tcell.Color, mix float32) tcell.Color {
	if !overlay.Valid() || mix <= 0 {
		if base.Valid() {
			return base
		}
		return overlay
	}
	if mix >= 1 {
		return overlay
	}
	if !base.Valid() {
		base = baseBackgroundColor
	}
	br, bg, bb := base.RGB()
	or, og, ob := overlay.RGB()
	blend := func(bc, oc int32) int32 {
		return int32(float32(bc)*(1-mix) + float32(oc)*mix)
	}
	return tcell.NewRGBColor(blend(br, or), blend(bg, og), blend(bb, ob))
}
