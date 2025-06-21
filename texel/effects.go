package texel

import "github.com/gdamore/tcell/v2"

// Effect defines an interface for any visual effect that can be applied to a pane's buffer.
// It takes a buffer of cells and returns a modified buffer.
type Effect interface {
	Apply(buffer [][]Cell) [][]Cell
}

// FadeEffect darkens or "fades" a pane by blending its colors with a specified fade color.
type FadeEffect struct {
	// The color to blend with. Typically black for a fade-out effect.
	FadeColor tcell.Color
	// The intensity of the blend, from 0.0 (no effect) to 1.0 (fully faded).
	Intensity float32
	// Fallbacks for default (ColorDefault) fg/bg, captured from the screen's style.
	screen *Screen
}

// NewFadeEffect creates a new fade effect with a given color and intensity.
func NewFadeEffect(scr *Screen, color tcell.Color, intensity float32) *FadeEffect {
	if intensity < 0 {
		intensity = 0
	}
	if intensity > 1 {
		intensity = 1
	}
	return &FadeEffect{
		screen:    scr,
		FadeColor: color,
		Intensity: intensity,
	}
}

// Apply walks the buffer, blends fg/bg, then uses your screen.getStyle cache.
func (f *FadeEffect) Apply(buffer [][]Cell) [][]Cell {
	for y := range buffer {
		for x := range buffer[y] {
			cell := &buffer[y][x]
			fg, bg, attrs := cell.Style.Decompose()

			// treat “default” as white/black for blending
			if !fg.Valid() {
				fg = f.screen.DefaultFgColor
			}
			if !bg.Valid() {
				bg = f.screen.DefaultBgColor
			}

			// blend each channel
			blendedFg := f.blendColor(fg, f.FadeColor, f.Intensity)
			blendedBg := f.blendColor(bg, f.FadeColor, f.Intensity)

			// extract boolean flags from the AttrMask
			bold := attrs&tcell.AttrBold != 0
			underline := attrs&tcell.AttrUnderline != 0
			reverse := attrs&tcell.AttrReverse != 0

			cell.Style = f.screen.getStyle(blendedFg, blendedBg, bold, underline, reverse)
		}
	}
	return buffer
}

// blendColor performs a linear interpolation between two tcell.Colors.
func (f *FadeEffect) blendColor(original, blend tcell.Color, intensity float32) tcell.Color {
	// If the original color is the default, don't try to blend it.
	if !original.Valid() {
		return original
	}

	// Get RGB values for both colors.
	r1, g1, b1 := original.RGB()
	r2, g2, b2 := blend.RGB()

	// Perform the linear interpolation: result = original * (1 - intensity) + blend * intensity
	r := int32(float32(r1)*(1-intensity) + float32(r2)*intensity)
	g := int32(float32(g1)*(1-intensity) + float32(g2)*intensity)
	b := int32(float32(b1)*(1-intensity) + float32(b2)*intensity)

	return tcell.NewRGBColor(r, g, b)
}
