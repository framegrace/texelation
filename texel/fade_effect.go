// texel/fade_effect_v2.go
package texel

import (
	"github.com/gdamore/tcell/v2"
	"sync"
)

// FadeEffect applies a color overlay with variable intensity
type FadeEffect struct {
	mu        sync.RWMutex
	intensity float32
	fadeColor tcell.Color
	desktop   *Desktop // For accessing default colors and style cache
}

// NewFadeEffect creates a new fade effect
func NewFadeEffect(desktop *Desktop, fadeColor tcell.Color) *FadeEffect {
	return &FadeEffect{
		intensity: 0.0,
		fadeColor: fadeColor,
		desktop:   desktop,
	}
}

// Apply applies the fade effect to the buffer
func (f *FadeEffect) Apply(buffer *[][]Cell) {
	f.mu.RLock()
	intensity := f.intensity
	f.mu.RUnlock()

	if intensity <= 0.0 {
		return
	}

	for y := range *buffer {
		for x := range (*buffer)[y] {
			cell := &(*buffer)[y][x]
			fg, bg, attrs := cell.Style.Decompose()

			// Use desktop defaults for invalid colors
			if !fg.Valid() {
				fg = f.desktop.DefaultFgColor.TrueColor()
			}
			if !bg.Valid() {
				bg = f.desktop.DefaultBgColor.TrueColor()
			}

			// Blend colors
			blendedFg := blendColor(fg, f.fadeColor, intensity)
			blendedBg := blendColor(bg, f.fadeColor, intensity)

			// Preserve text attributes
			bold := attrs&tcell.AttrBold != 0
			underline := attrs&tcell.AttrUnderline != 0
			reverse := attrs&tcell.AttrReverse != 0

			cell.Style = f.desktop.getStyle(blendedFg, blendedBg, bold, underline, reverse)
		}
	}
}

// Clone creates a new instance of the fade effect
func (f *FadeEffect) Clone() Effect {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return NewFadeEffect(f.desktop, f.fadeColor)
}

// GetIntensity returns the current fade intensity
func (f *FadeEffect) GetIntensity() float32 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.intensity
}

// SetIntensity sets the fade intensity
func (f *FadeEffect) SetIntensity(intensity float32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	f.intensity = intensity
}

// IsAnimating returns true if the effect has non-zero intensity
func (f *FadeEffect) IsAnimating() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.intensity > 0.0
}

// DitherEffect applies a dithering pattern with variable intensity
type DitherEffect struct {
	mu         sync.RWMutex
	intensity  float32
	ditherChar rune
	frameCount int
}

// NewDitherEffect creates a new dither effect
func NewDitherEffect(ditherChar rune) *DitherEffect {
	return &DitherEffect{
		intensity:  0.0,
		ditherChar: ditherChar,
	}
}

// Apply applies the dither effect to the buffer
func (d *DitherEffect) Apply(buffer *[][]Cell) {
	d.mu.Lock()
	intensity := d.intensity
	d.frameCount++
	currentFrame := d.frameCount
	d.mu.Unlock()

	if intensity <= 0.0 {
		return
	}

	// Create a flickering pattern based on frame count and intensity
	shouldDither := (currentFrame%2) == 0 && intensity > 0.5

	if shouldDither {
		for y := range *buffer {
			for x := range (*buffer)[y] {
				if (*buffer)[y][x].Ch != ' ' {
					(*buffer)[y][x].Ch = d.ditherChar
				}
			}
		}
	}
}

// Clone creates a new instance of the dither effect
func (d *DitherEffect) Clone() Effect {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return NewDitherEffect(d.ditherChar)
}

// GetIntensity returns the current dither intensity
func (d *DitherEffect) GetIntensity() float32 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.intensity
}

// SetIntensity sets the dither intensity
func (d *DitherEffect) SetIntensity(intensity float32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	d.intensity = intensity
}

// IsAnimating returns true if the effect has non-zero intensity
func (d *DitherEffect) IsAnimating() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.intensity > 0.0
}

// PixelateEffect applies a pixelation pattern with variable intensity
type PixelateEffect struct {
	mu        sync.RWMutex
	intensity float32
	char1     rune
	char2     rune
}

// NewPixelateEffect creates a new pixelate effect
func NewPixelateEffect(char1, char2 rune) *PixelateEffect {
	return &PixelateEffect{
		intensity: 0.0,
		char1:     char1,
		char2:     char2,
	}
}

// Apply applies the pixelate effect to the buffer
func (p *PixelateEffect) Apply(buffer *[][]Cell) {
	p.mu.RLock()
	intensity := p.intensity
	p.mu.RUnlock()

	if intensity <= 0.0 {
		return
	}

	// Use intensity as probability to pixelate each character
	for y := range *buffer {
		for x := range (*buffer)[y] {
			if (*buffer)[y][x].Ch != ' ' {
				// Use position-based randomness for consistent pattern
				hash := (y*31 + x) % 100
				threshold := int(intensity * 100)

				if hash < threshold {
					// Choose character based on position
					if (x+y)%2 == 0 {
						(*buffer)[y][x].Ch = p.char1
					} else {
						(*buffer)[y][x].Ch = p.char2
					}
				}
			}
		}
	}
}

// Clone creates a new instance of the pixelate effect
func (p *PixelateEffect) Clone() Effect {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return NewPixelateEffect(p.char1, p.char2)
}

// GetIntensity returns the current pixelate intensity
func (p *PixelateEffect) GetIntensity() float32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.intensity
}

// SetIntensity sets the pixelate intensity
func (p *PixelateEffect) SetIntensity(intensity float32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	p.intensity = intensity
}

// IsAnimating returns true if the effect has non-zero intensity
func (p *PixelateEffect) IsAnimating() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.intensity > 0.0
}

// blendColor performs linear interpolation between two colors
func blendColor(original, blend tcell.Color, intensity float32) tcell.Color {
	if !original.Valid() {
		return original
	}
	r1, g1, b1 := original.RGB()
	r2, g2, b2 := blend.RGB()
	r := int32(float32(r1)*(1-intensity) + float32(r2)*intensity)
	g := int32(float32(g1)*(1-intensity) + float32(g2)*intensity)
	b := int32(float32(b1)*(1-intensity) + float32(b2)*intensity)
	return tcell.NewRGBColor(r, g, b)
}
