// texel/demo_effects.go - Examples of custom effects using the new system
package texel

import (
	"github.com/gdamore/tcell/v2"
	"math"
	"sync"
	"time"
)

// RainbowEffect cycles through colors creating a rainbow effect
type RainbowEffect struct {
	desktop   *Desktop
	mu        sync.RWMutex
	intensity float32
	hueOffset float32
}

func NewRainbowEffect(desktop *Desktop) *RainbowEffect {
	return &RainbowEffect{
		intensity: 0.0,
		hueOffset: 0.0,
		desktop:   desktop,
	}
}

func (r *RainbowEffect) Apply(buffer *[][]Cell) {
	r.mu.Lock()
	intensity := r.intensity
	r.hueOffset += 0.09 // Animate the rainbow
	if r.hueOffset > 2*math.Pi {
		r.hueOffset = 0
	}
	currentOffset := r.hueOffset
	r.mu.Unlock()

	if intensity <= 0.0 {
		return
	}

	for y := range *buffer {
		for x := range (*buffer)[y] {
			if (*buffer)[y][x].Ch != ' ' {
				// Calculate hue based on position and time
				hue := currentOffset + float32(x+y)*0.1
				color := hsvToRGB(hue, 1.0, 1.0)
				cell := &(*buffer)[y][x]
				originalChar := cell.Ch
				fg, bg, attrs := cell.Style.Decompose()
				// Only process non-space characters or cells with background
				if originalChar == ' ' && !bg.Valid() {
					continue
				}

				// Use desktop defaults for invalid colors
				if !fg.Valid() {
					fg = r.desktop.DefaultFgColor
				}
				if !bg.Valid() {
					bg = r.desktop.DefaultBgColor
				}

				// Blend colors

				// Blend with original color
				//				if fg.Valid() {
				blendedFg := blendColor(fg, color, intensity)
				cell.Style = tcell.StyleDefault.
					Foreground(blendedFg).
					Background(bg).
					Bold(attrs&tcell.AttrBold != 0).
					Underline(attrs&tcell.AttrUnderline != 0).
					Reverse(attrs&tcell.AttrReverse != 0)
					//				}
				cell.Ch = originalChar
			}
		}
	}
}

func (r *RainbowEffect) Clone() Effect {
	return NewRainbowEffect(r.desktop)
}

func (r *RainbowEffect) GetIntensity() float32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.intensity
}

func (r *RainbowEffect) SetIntensity(intensity float32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	r.intensity = intensity
}

func (r *RainbowEffect) IsAnimating() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.intensity > 0.0
}

// WaveEffect creates a wave distortion effect
type WaveEffect struct {
	mu        sync.RWMutex
	intensity float32
	time      float32
}

func NewWaveEffect() *WaveEffect {
	return &WaveEffect{
		intensity: 0.0,
		time:      0.0,
	}
}

func (w *WaveEffect) Apply(buffer *[][]Cell) {
	w.mu.Lock()
	intensity := w.intensity
	w.time += 0.1
	currentTime := w.time
	w.mu.Unlock()

	if intensity <= 0.0 {
		return
	}

	height := len(*buffer)
	if height == 0 {
		return
	}

	// Create a temporary buffer for the wave effect
	originalBuffer := make([][]Cell, height)
	for i := range originalBuffer {
		originalBuffer[i] = make([]Cell, len((*buffer)[i]))
		copy(originalBuffer[i], (*buffer)[i])
	}

	// Apply wave distortion
	for y := range *buffer {
		if len((*buffer)[y]) == 0 {
			continue
		}
		width := len((*buffer)[y])

		// Calculate wave offset
		waveOffset := int(float64(intensity) * 3.0 * math.Sin(float64(currentTime)+float64(y)*0.3))

		for x := range (*buffer)[y] {
			sourceX := x - waveOffset
			if sourceX >= 0 && sourceX < width {
				(*buffer)[y][x] = originalBuffer[y][sourceX]
			}
		}
	}
}

func (w *WaveEffect) Clone() Effect {
	return NewWaveEffect()
}

func (w *WaveEffect) GetIntensity() float32 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.intensity
}

func (w *WaveEffect) SetIntensity(intensity float32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	w.intensity = intensity
}

func (w *WaveEffect) IsAnimating() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.intensity > 0.0
}

// GlitchEffect creates a digital glitch effect
type GlitchEffect struct {
	mu         sync.RWMutex
	intensity  float32
	frameCount int
}

func NewGlitchEffect() *GlitchEffect {
	return &GlitchEffect{
		intensity: 0.0,
	}
}

func (g *GlitchEffect) Apply(buffer *[][]Cell) {
	g.mu.Lock()
	intensity := g.intensity
	g.frameCount++
	frame := g.frameCount
	g.mu.Unlock()

	if intensity <= 0.0 {
		return
	}

	glitchChars := []rune{'█', '▓', '▒', '░', '▄', '▀', '▌', '▐'}

	for y := range *buffer {
		for x := range (*buffer)[y] {
			if (*buffer)[y][x].Ch != ' ' {
				// Use frame and position for pseudo-random glitching
				hash := (frame + x*31 + y*17) % 100
				threshold := int((1.0 - intensity) * 100)

				if hash > threshold {
					// Apply glitch character
					charIndex := (x + y + frame) % len(glitchChars)
					(*buffer)[y][x].Ch = glitchChars[charIndex]

					// Sometimes invert colors for extra glitch effect
					if hash%10 == 0 {
						fg, bg, attrs := (*buffer)[y][x].Style.Decompose()
						(*buffer)[y][x].Style = tcell.StyleDefault.
							Foreground(bg).
							Background(fg).
							Bold(attrs&tcell.AttrBold != 0).
							Underline(attrs&tcell.AttrUnderline != 0).
							Reverse(attrs&tcell.AttrReverse != 0)
					}
				}
			}
		}
	}
}

func (g *GlitchEffect) Clone() Effect {
	return NewGlitchEffect()
}

func (g *GlitchEffect) GetIntensity() float32 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.intensity
}

func (g *GlitchEffect) SetIntensity(intensity float32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	g.intensity = intensity
}

func (g *GlitchEffect) IsAnimating() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.intensity > 0.0
}

// Helper function to convert HSV to RGB for the rainbow effect
func hsvToRGB(h, s, v float32) tcell.Color {
	h = float32(math.Mod(float64(h), 2*math.Pi)) / (2 * math.Pi) * 360

	c := v * s
	x := c * (1 - float32(math.Abs(math.Mod(float64(h/60), 2)-1)))
	m := v - c

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

// Example usage functions that could be added to Desktop:

// EnableRainbowMode adds a rainbow effect to the active pane
func (d *Desktop) EnableRainbowMode() {
	if d.activeWorkspace != nil &&
		d.activeWorkspace.tree.ActiveLeaf != nil &&
		d.activeWorkspace.tree.ActiveLeaf.Pane != nil {

		rainbow := NewRainbowEffect(d)
		d.activeWorkspace.tree.ActiveLeaf.Pane.AddEffect(rainbow)

		// Animate it in
		if d.activeWorkspace.tree.ActiveLeaf.Pane.animator != nil {
			d.activeWorkspace.tree.ActiveLeaf.Pane.animator.FadeIn(rainbow, 500*time.Millisecond, nil)
		}
	}
}

// EnableGlitchMode adds a glitch effect to the entire screen
func (d *Desktop) EnableGlitchMode() {
	if d.activeWorkspace != nil {
		glitch := NewGlitchEffect()
		d.activeWorkspace.AddEffect(glitch)

		// Animate it in
		d.activeWorkspace.animator.FadeIn(glitch, 300*time.Millisecond, nil)
	}
}
