package texel

import (
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// Pane represents a rectangular area on the screen that hosts an App.
type pane struct {
	absX0, absY0, absX1, absY1 int
	app                        App
	effects                    []Effect
	prevBuf                    [][]Cell
	name                       string
	frozenBuffer               [][]Cell
	screen                     *Screen
}

// newPane creates a new, empty Pane. The App is attached later.
func newPane(s *Screen) *pane {
	return &pane{
		screen: s,
	}
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

// Render draws the pane's borders, title, and the hosted application's content.
func (p *pane) Render(isActive bool) [][]Cell {
	w := p.Width()
	h := p.Height()

	// Create the pane's buffer.
	buffer := make([][]Cell, h)
	for i := range buffer {
		buffer[i] = make([]Cell, w)
		for j := range buffer[i] {
			buffer[i][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}

	// Don't draw decorations if the pane is too small.
	if w < 2 || h < 2 {
		return buffer
	}

	// Determine border style based on active state.
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkSlateGray)
	if isActive {
		borderStyle = tcell.StyleDefault.Foreground(tcell.ColorOrange)
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
	buffer[h-1][w-1] = Cell{Ch: tcell.RuneLRCorner, Style: borderStyle}

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

	// Apply visual effects to the entire pane buffer (borders included).
	for _, effect := range p.effects {
		buffer = effect.Apply(buffer, p, isActive)
	}

	return buffer
}

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

func (p *pane) HandleEvent(event Event) {
	for _, effect := range p.effects {
		if listener, ok := effect.(Listener); ok {
			listener.OnEvent(event)
		}
	}
}

func (p *pane) Close() {
	if listener, ok := p.app.(Listener); ok {
		p.screen.Unsubscribe(listener)
	}
	for _, effect := range p.effects {
		if listener, ok := effect.(Listener); ok {
			p.screen.Unsubscribe(listener)
		}
	}
	if p.app != nil {
		p.app.Stop()
	}
}

func (p *pane) AddEffect(e Effect) {
	p.effects = append(p.effects, e)
	if listener, ok := e.(Listener); ok {
		p.screen.Subscribe(listener)
	}
}

func (p *pane) ClearEffects() {
	for _, effect := range p.effects {
		if listener, ok := effect.(Listener); ok {
			p.screen.Unsubscribe(listener)
		}
	}
	p.effects = make([]Effect, 0)
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
