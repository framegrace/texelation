package texel

import (
	"log"
)

// Rect defines a rectangle using fractional coordinates (0.0 to 1.0).
type Rect struct {
	X, Y, W, H float64
}

type SplitType int

const (
	Horizontal SplitType = iota
	Vertical
)

type Node struct {
	Parent *Node
	Left   *Node
	Right  *Node
	Split  SplitType
	Layout Rect
	Pane   *Pane // A pane is only present in leaf nodes
}

// Pane represents a rectangular area on the screen that hosts an App.
type Pane struct {
	absX0, absY0, absX1, absY1 int // These are now calculated from Layout
	app                        App
	effects                    []Effect
	prevBuf                    [][]Cell
	name                       string
}

// NewPane creates a new Pane with the given dimensions and hosts the provided App.
func NewPane(app App) *Pane {
	p := &Pane{
		app:  app,
		name: app.GetTitle(),
	}
	// The app will be resized properly by the first call to handleResize.
	return p
}

func (p *Pane) String() string {
	return p.name
}
func (p *Pane) setTitle(t string) {
	p.name = t
}
func (p *Pane) getTitle() string {
	return p.name
}
func (p *Pane) HandleEvent(event Event) {
	log.Printf("Panel %s received event %s", p, event)
	for _, effect := range p.effects {
		if listener, ok := effect.(EventListener); ok {
			log.Printf("Sending to listener %s", effect)
			listener.OnEvent(p, event)
		}
	}
}

func (p *Pane) Close() {
	if p.app != nil {
		p.app.Stop()
	}
}

// AddEffect adds a visual effect to the pane's processing pipeline.
func (p *Pane) AddEffect(e Effect) {
	// To avoid duplicates, you could add a check here if needed.
	p.effects = append(p.effects, e)
}

// ClearEffects removes all visual effects from the pane.
func (p *Pane) ClearEffects() {
	p.effects = make([]Effect, 0)
}

func (p *Pane) Width() int {
	w := p.absX1 - p.absX0
	if w < 0 {
		return 0
	}
	return w
}

func (p *Pane) Height() int {
	h := p.absY1 - p.absY0
	if h < 0 {
		return 0
	}
	return h
}

func (p *Pane) SetDimensions(x0, y0, x1, y1 int) {
	p.absX0, p.absY0, p.absX1, p.absY1 = x0, y0, x1, y1
	p.app.Resize(p.Width(), p.Height())
}
