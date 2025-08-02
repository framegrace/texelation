package texel

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
)

// --- FadeEffect-Specific Option ---

// WithIsControl is an option to set the control mode behavior.
// It returns a function that can only be applied to a FadeEffect.
func WithIsControl(b bool) func(*FadeEffect) {
	return func(f *FadeEffect) {
		f.isControlModeEffect = b
	}
}

// --- FadeEffect ---

type FadeEffect struct {
	BaseEffect          // Embeds all common fields like state, intensity, duration, etc.
	FadeColor           tcell.Color
	isControlModeEffect bool
}

// NewFadeEffect creates a new fade effect.
func NewFadeEffect(scr *Screen, color tcell.Color, intensity float32, opts ...interface{}) *FadeEffect {
	// Start with default values for this specific effect type
	f := &FadeEffect{
		BaseEffect: newBaseEffect(scr, intensity), // Default intensity for fade
		FadeColor:  color,
	}

	// Loop through options and apply them based on their type
	for _, opt := range opts {
		switch o := opt.(type) {
		case EffectOption: // Generic options (WithDuration, WithIntensity)
			o(f)
		case func(*FadeEffect): // FadeEffect-specific options (WithIsControl)
			o(f)
		}
	}
	return f
}

// Clone creates a new, independent instance of the FadeEffect.
func (f *FadeEffect) Clone() Effect {
	return NewFadeEffect(f.screen, f.FadeColor, f.targetIntensity,
		WithDuration(f.duration),
		WithIsControl(f.isControlModeEffect),
	)
}

func (f *FadeEffect) IsContinuous() bool {
	return false
}

func (f *FadeEffect) OnEvent(event Event) {
	if f.isControlModeEffect {
		switch event.Type {
		case EventControlOn:
			f.activate()
		case EventControlOff:
			f.inactivate()
		}
	}
}

func (f *FadeEffect) Apply(buffer [][]Cell, owner *pane) [][]Cell {
	if !f.isControlModeEffect {
		if owner.IsActive {
			f.inactivate()
		} else {
			f.activate()
		}
	}

	if f.getState() == StateOff {
		return buffer
	}
	intensity := f.getIntensity()
	if intensity == 0 {
		return buffer
	}
	for y := range buffer {
		for x := range buffer[y] {
			cell := &buffer[y][x]
			fg, bg, attrs := cell.Style.Decompose()
			// Access resources through the desktop now
			if !fg.Valid() {
				fg = f.desktop.DefaultFgColor.TrueColor()
			}
			if !bg.Valid() {
				bg = f.desktop.DefaultBgColor.TrueColor()
			}
			blendedFg := blendColor(fg, f.FadeColor, intensity)
			blendedBg := blendColor(bg, f.FadeColor, intensity)
			bold := attrs&tcell.AttrBold != 0
			underline := attrs&tcell.AttrUnderline != 0
			reverse := attrs&tcell.AttrReverse != 0
			cell.Style = f.desktop.getStyle(blendedFg, blendedBg, bold, underline, reverse)
		}
	}
	return buffer
}

// blendColor performs a linear interpolation between two tcell.Colors.
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

func (f *FadeEffect) String() string {
	return fmt.Sprintf("FadeEffect: %s, %t", f.FadeColor, f.isControlModeEffect)
}
