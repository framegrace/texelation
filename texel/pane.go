package texel

import (
// "log"
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
	Parent   *Node
	Split    SplitType
	Pane     *pane // A pane is only present in leaf nodes
	Children []*Node
}

// Pane represents a rectangular area on the screen that hosts an App.
type pane struct {
	absX0, absY0, absX1, absY1 int // These are now calculated from Layout
	app                        App
	effects                    []Effect
	prevBuf                    [][]Cell
	name                       string
}

// NewPane creates a new Pane with the given dimensions and hosts the provided App.
func newPane(app App) *pane {
	p := &pane{
		app:  app,
		name: app.GetTitle(),
	}
	// The app will be resized properly by the first call to handleResize.
	return p
}

func (p *pane) String() string {
	return p.name
}
func (p *pane) setTitle(t string) {
	p.name = t
}
func (p *pane) getTitle() string {
	return p.name
}
func (p *pane) HandleEvent(event Event) {
	//log.Printf("Panel %s received event %s", p, event)
	for _, effect := range p.effects {
		if listener, ok := effect.(EventListener); ok {
			//log.Printf("Sending to listener %s", effect)
			listener.OnEvent(p, event)
		}
	}
}

func (p *pane) Close() {
	if p.app != nil {
		p.app.Stop()
	}
}

// AddEffect adds a visual effect to the pane's processing pipeline.
func (p *pane) AddEffect(e Effect) {
	// To avoid duplicates, you could add a check here if needed.
	p.effects = append(p.effects, e)
}

// ClearEffects removes all visual effects from the pane.
func (p *pane) ClearEffects() {
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
	p.app.Resize(p.Width(), p.Height())
}
