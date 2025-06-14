package texel

// Rect defines a rectangle using fractional coordinates (0.0 to 1.0).
type Rect struct {
	X, Y, W, H float64
}

// Pane represents a rectangular area on the screen that hosts an App.
type Pane struct {
	absX0, absY0, absX1, absY1 int // These are now calculated from Layout
	Layout                     Rect
	app                        App
	effects                    []Effect
}

// NewPane creates a new Pane with the given dimensions and hosts the provided App.
func NewPane(layout Rect, app App) *Pane {
	p := &Pane{
		Layout: layout,
		app:    app,
	}
	// The app will be resized properly by the first call to handleResize.
	return p
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
