// texel/pane_v2.go
package texel

import (
	"github.com/gdamore/tcell/v2"
	"log"
	"texelation/texel/theme"
	"time"
	"unicode/utf8"
)

// Z-order constants for common layering scenarios
const (
	ZOrderDefault   = 0    // Normal panes
	ZOrderFloating  = 100  // Floating windows
	ZOrderDialog    = 500  // Modal dialogs
	ZOrderAnimation = 1000 // During animations (zoom, etc.)
	ZOrderTooltip   = 2000 // Tooltips and temporary overlays
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
	ZOrder     int // Higher values render on top, default is 0
}

// newPane creates a new, empty Pane. The App is attached later.
func newPane(s *Screen) *pane {
	p := &pane{
		screen:     s,
		effects:    NewEffectPipeline(),
		animator:   NewEffectAnimator(),
		IsActive:   false, // Explicitly set to false initially
		IsResizing: false, // Explicitly set to false initially
	}

	// Create pre-made effects for common states
	// Use darker colors for inactive fade - this will darken the pane
	p.inactiveFade = NewFadeEffect(s.desktop, tcell.NewRGBColor(20, 20, 0)) // Dark yellow for darkening
	// Use orange tint for resizing
	p.resizingFade = NewFadeEffect(s.desktop, tcell.NewRGBColor(255, 184, 108)) // Orange from your theme

	log.Printf("newPane: Created pane with inactive fade intensity=%.3f, resizing fade intensity=%.3f",
		p.inactiveFade.GetIntensity(), p.resizingFade.GetIntensity())

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
	log.Printf("SetActive called on pane '%s': active=%v, current IsActive=%v", p.getTitle(), active, p.IsActive)

	if p.IsActive == active {
		log.Printf("SetActive: No change needed for pane '%s'", p.getTitle())
		return
	}

	p.IsActive = active

	// Stop any existing animations on the inactive fade effect to prevent conflicts
	p.animator.Stop(p.inactiveFade)

	// Animate the inactive fade effect
	if active {
		log.Printf("SetActive: Activating pane '%s' - fading out inactive effect", p.getTitle())
		// Fade out the inactive effect (return to normal brightness)
		p.animator.FadeOut(p.inactiveFade, 200*time.Millisecond, func() {
			log.Printf("SetActive: Pane '%s' activation animation completed", p.getTitle())
			p.screen.Refresh() // Request a redraw when animation completes
		})
	} else {
		log.Printf("SetActive: Deactivating pane '%s' - fading in inactive effect to 0.3", p.getTitle())
		// Fade in the inactive effect (darken the pane) - FIXED: Use 0.3 instead of 1.0
		p.animator.AnimateTo(p.inactiveFade, 0.3, 200*time.Millisecond, func() {
			log.Printf("SetActive: Pane '%s' deactivation animation completed", p.getTitle())
			p.screen.Refresh()
		})
	}
}

// SetResizing changes the resizing state of the pane and animates the appropriate effects
func (p *pane) SetResizing(resizing bool) {
	log.Printf("SetResizing called on pane '%s': resizing=%v, current IsResizing=%v", p.getTitle(), resizing, p.IsResizing)

	if p.IsResizing == resizing {
		return
	}

	p.IsResizing = resizing

	// Stop any existing animations on the resizing fade effect
	p.animator.Stop(p.resizingFade)

	// Animate the resizing fade effect
	if resizing {
		log.Printf("SetResizing: Pane '%s' entering resize mode", p.getTitle())
		// Use moderate intensity for resize effect
		p.animator.AnimateTo(p.resizingFade, 0.2, 100*time.Millisecond, func() {
			log.Printf("SetResizing: Pane '%s' resize fade-in completed", p.getTitle())
			p.screen.Refresh()
		})
	} else {
		log.Printf("SetResizing: Pane '%s' exiting resize mode", p.getTitle())
		p.animator.FadeOut(p.resizingFade, 100*time.Millisecond, func() {
			log.Printf("SetResizing: Pane '%s' resize fade-out completed", p.getTitle())
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

// SetZOrder sets the z-order (layering) of the pane
// Higher values render on top. Default is 0.
func (p *pane) SetZOrder(zOrder int) {
	p.ZOrder = zOrder
	log.Printf("SetZOrder: Pane '%s' z-order set to %d", p.getTitle(), zOrder)
	p.screen.Refresh() // Trigger redraw
}

// GetZOrder returns the current z-order of the pane
func (p *pane) GetZOrder() int {
	return p.ZOrder
}

// BringToFront sets the pane to render on top of other panes
func (p *pane) BringToFront() {
	p.SetZOrder(ZOrderFloating)
}

// SendToBack resets the pane to normal z-order
func (p *pane) SendToBack() {
	p.SetZOrder(ZOrderDefault)
}

// SetAsDialog configures the pane as a modal dialog
func (p *pane) SetAsDialog() {
	p.SetZOrder(ZOrderDialog)
}

// Render draws the pane's borders, title, and the hosted application's content.
func (p *pane) Render() [][]Cell {
	w := p.Width()
	h := p.Height()

	log.Printf("Render: Pane '%s' rendering %dx%d (abs: %d,%d-%d,%d)",
		p.getTitle(), w, h, p.absX0, p.absY0, p.absX1, p.absY1)

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
		log.Printf("Render: Pane '%s' too small to draw decorations (%dx%d)", p.getTitle(), w, h)
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

	// Draw Title - with proper bounds checking
	title := p.getTitle()
	if title != "" && w > 4 { // Only draw title if we have enough space
		titleRuneCount := utf8.RuneCountInString(title)
		maxTitleLength := w - 4 // Space for " " + title + " " + borders

		// Truncate title if it's too long for the pane width.
		if titleRuneCount > maxTitleLength && maxTitleLength > 0 {
			titleRunes := []rune(title)
			if maxTitleLength <= len(titleRunes) {
				title = string(titleRunes[:maxTitleLength])
			}
		}

		titleStr := " " + title + " "
		for i, ch := range titleStr {
			if 1+i < w-1 { // Ensure we don't go beyond borders
				buffer[0][1+i] = Cell{Ch: ch, Style: borderStyle}
			}
		}
	}

	// Render the app's content inside the borders.
	if p.app != nil {
		appBuffer := p.app.Render()
		if len(appBuffer) > 0 && len(appBuffer[0]) > 0 {
			log.Printf("Render: Pane '%s' app buffer size: %dx%d",
				p.getTitle(), len(appBuffer[0]), len(appBuffer))

			for y, row := range appBuffer {
				for x, cell := range row {
					if 1+x < w-1 && 1+y < h-1 {
						buffer[1+y][1+x] = cell
					}
				}
			}
		}
	} else {
		log.Printf("Render: Pane '%s' has no app!", p.getTitle())
	}

	// Apply all effects in the pipeline to the entire pane buffer
	p.effects.Apply(&buffer)

	log.Printf("Render: Pane '%s' final buffer size: %dx%d", p.getTitle(), len(buffer), len(buffer[0]))
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
	log.Printf("setDimensions: Pane '%s' set to (%d,%d)-(%d,%d), size %dx%d",
		p.getTitle(), x0, y0, x1, y1, x1-x0, y1-y0)

	p.absX0, p.absY0, p.absX1, p.absY1 = x0, y0, x1, y1

	if p.app != nil {
		drawableW := p.drawableWidth()
		drawableH := p.drawableHeight()
		log.Printf("setDimensions: Pane '%s' drawable area: %dx%d",
			p.getTitle(), drawableW, drawableH)
		p.app.Resize(drawableW, drawableH)
	}
}
