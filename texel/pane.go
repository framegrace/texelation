// texel/pane_v2.go
package texel

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"time"
	"unicode/utf8"
)

// Pane represents a rectangular area on the screen that hosts an App.
type pane struct {
	absX0, absY0, absX1, absY1 int
	app                        App
	name                       string
	prevBuf                    [][]Cell
	screen                     *Screen

	// Effects system
	effects  *EffectPipeline
	animator *EffectAnimator

	// Pre-created effects for common use cases
	inactiveFade *FadeEffect
	resizingFade *FadeEffect

	// Public state fields
	IsActive   bool
	IsResizing bool
}

// newPane creates a new, empty Pane. The App is attached later.
func newPane(s *Screen) *pane {
	p := &pane{
		screen:   s,
		effects:  NewEffectPipeline(),
		animator: NewEffectAnimator(),
	}

	// Create pre-made effects for common states
	p.inactiveFade = NewFadeEffect(s.desktop, tcell.NewRGBColor(20, 20, 0))
	p.resizingFade = NewFadeEffect(s.desktop, tcell.NewRGBColor(255, 184, 108)) // Orange

	// Add them to the pipeline (they start with 0 intensity)
	p.effects.AddEffect(p.inactiveFade)
	p.effects.AddEffect(p.resizingFade)

	return p
}

// AttachApp connects an application to the pane, gives it its initial size,
// and starts its main run loop.
func (p *pane) AttachApp(app App, refreshChan chan<- bool) {
	if p.app != nil {
		p.app.Stop() // Stop any existing app
	}
	p.app = app
	p.name = app.GetTitle()
	p.app.SetRefreshNotifier(refreshChan)
	if listener, ok := app.(Listener); ok {
		p.screen.Subscribe(listener)
	}
	// The app is resized considering the space for borders.
	p.app.Resize(p.drawableWidth(), p.drawableHeight())
	go p.app.Run()
}

// SetActive changes the active state of the pane and animates the appropriate effects
func (p *pane) SetActive(active bool) {
	if p.IsActive == active {
		return
	}

	p.IsActive = active

	// Animate the inactive fade effect
	if active {
		// Fade out the inactive effect
		p.animator.FadeOut(p.inactiveFade, 200*time.Millisecond, func() {
			p.screen.Refresh() // Request a redraw when animation completes
		})
	} else {
		// Fade in the inactive effect
		p.animator.FadeIn(p.inactiveFade, 200*time.Millisecond, func() {
			p.screen.Refresh()
		})
	}
}

// SetResizing changes the resizing state of the pane and animates the appropriate effects
func (p *pane) SetResizing(resizing bool) {
	if p.IsResizing == resizing {
		return
	}

	p.IsResizing = resizing

	// Animate the resizing fade effect
	if resizing {
		p.animator.FadeIn(p.resizingFade, 100*time.Millisecond, func() {
			p.screen.Refresh()
		})
	} else {
		p.animator.FadeOut(p.resizingFade, 100*time.Millisecond, func() {
			p.screen.Refresh()
		})
	}
}

// AddEffect adds a custom effect to the pane's pipeline
func (p *pane) AddEffect(effect Effect) {
	p.effects.AddEffect(effect)
}

// RemoveEffect removes an effect from the pane's pipeline
func (p *pane) RemoveEffect(effect Effect) {
	p.effects.RemoveEffect(effect)
}

// Render draws the pane's borders, title, and the hosted application's content.
func (p *pane) Render() [][]Cell {
	w := p.Width()
	h := p.Height()

	tm := theme.Get()
	defstyle := tcell.StyleDefault.Background(tm.GetColor("desktop", "default_bg", tcell.ColorReset).TrueColor()).Foreground(tm.GetColor("desktop", "default_fg", tcell.ColorReset).TrueColor())

	// Create the pane's buffer.
	buffer := make([][]Cell, h)
	for i := range buffer {
		buffer[i] = make([]Cell, w)
		for j := range buffer[i] {
			buffer[i][j] = Cell{Ch: ' ', Style: defstyle}
		}
	}

	// Don't draw decorations if the pane is too small.
	if w < 2 || h < 2 {
		return buffer
	}

	// Determine border style based on active state.
	borderStyle := defstyle.Foreground(
		tm.GetColor("pane", "inactive_border_fg", tcell.ColorPink).TrueColor())
	if p.IsActive {
		borderStyle = defstyle.Foreground(
			tm.GetColor("pane", "active_border_fg", tcell.ColorPink).TrueColor())
	}
	if p.IsResizing {
		borderStyle = defstyle.Foreground(
			tm.GetColor("pane", "resizing_border_fg", tcell.ColorPink).TrueColor())
	}

	// Draw borders
	for x := 0; x < w; x++ {
		buffer[0][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
		buffer[h-1][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
	}
	for y := 0; y < h; y++ {
		buffer[y][0] = Cell{Ch: tcell.RuneVLine, Style: borderStyle}
		buffer[y][w-1] = Cell{Ch: tcell.RuneVLine, Style: borderStyle}
	}
	buffer[0][0] = Cell{Ch: tcell.RuneULCorner, Style: borderStyle}
	buffer[0][w-1] = Cell{Ch: tcell.RuneURCorner, Style: borderStyle}
	buffer[h-1][0] = Cell{Ch: tcell.RuneLLCorner, Style: borderStyle}
	buffer[h-1][w-1] = Cell{Ch: '╯', Style: borderStyle}

	// Draw Title
	title := p.getTitle()
	if title != "" {
		// Truncate title if it's too long for the pane width.
		if utf8.RuneCountInString(title)+4 > w {
			title = string([]rune(title)[:w-4])
		}
		titleStr := " " + title + " "
		for i, ch := range titleStr {
			if 1+i < w-1 {
				buffer[0][1+i] = Cell{Ch: ch, Style: borderStyle}
			}
		}
	}

	// Render the app's content inside the borders.
	if p.app != nil {
		appBuffer := p.app.Render()
		for y, row := range appBuffer {
			for x, cell := range row {
				if 1+x < w-1 && 1+y < h-1 {
					buffer[1+y][1+x] = cell
				}
			}
		}
	}

	// Apply all effects in the pipeline to the entire pane buffer
	p.effects.Apply(&buffer)

	return buffer
}

// Rest of the methods remain the same...
func (p *pane) drawableWidth() int {
	w := p.Width() - 2
	if w < 0 {
		return 0
	}
	return w
}

func (p *pane) drawableHeight() int {
	h := p.Height() - 2
	if h < 0 {
		return 0
	}
	return h
}

func (p *pane) String() string {
	return p.name
}

func (p *pane) setTitle(t string) {
	p.name = t
}

func (p *pane) getTitle() string {
	if p.app != nil {
		return p.app.GetTitle()
	}
	return p.name
}

func (p *pane) Close() {
	// Stop all animations
	p.animator.StopAll()

	// Clean up app
	if listener, ok := p.app.(Listener); ok {
		p.screen.Unsubscribe(listener)
	}
	if p.app != nil {
		p.app.Stop()
	}
}

func (p *pane) Width() int {
	w := p.absX1 - p.absX0
	if w < 0 {
		return 0
	}
	return w
}

func (p *pane) Height() int {
	h := p.absY1 - p.absY0
	if h < 0 {
		return 0
	}
	return h
}

func (p *pane) setDimensions(x0, y0, x1, y1 int) {
	p.absX0, p.absY0, p.absX1, p.absY1 = x0, y0, x1, y1
	if p.app != nil {
		p.app.Resize(p.drawableWidth(), p.drawableHeight())
	}
}
