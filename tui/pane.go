package tui

// Pane represents a rectangular area on the screen that hosts an App.
type Pane struct {
	X0, Y0, X1, Y1 int // The absolute coordinates on the main screen
	app            App
	effects        []Effect // Slice to hold visual effects
}

// NewPane creates a new Pane with the given dimensions and hosts the provided App.
func NewPane(x0, y0, x1, y1 int, app App) *Pane {
	p := &Pane{
		X0: x0, Y0: y0, X1: x1, Y1: y1,
		app: app,
	}
	// Inform the app of its initial size, accounting for borders.
	app.Resize(p.Width(), p.Height())
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

// Width returns the inner width of the pane (excluding borders).
func (p *Pane) Width() int {
	w := p.X1 - p.X0 - 2
	if w < 0 {
		return 0
	}
	return w
}

// Height returns the inner height of the pane (excluding borders).
func (p *Pane) Height() int {
	h := p.Y1 - p.Y0 - 2
	if h < 0 {
		return 0
	}
	return h
}

// SetDimensions updates the pane's position and size and notifies the underlying app.
func (p *Pane) SetDimensions(x0, y0, x1, y1 int) {
	p.X0, p.Y0, p.X1, p.Y1 = x0, y0, x1, y1
	p.app.Resize(p.Width(), p.Height())
}
