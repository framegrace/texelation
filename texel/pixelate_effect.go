package texel

import (
	"math/rand"
)

// --- Pixelate Effect (Would live in dither_effect.go) ---

type PixelateEffect struct {
	BaseEffect // Embedded
	Char1      rune
	Char2      rune
}

func NewPixelateEffect(scr *Screen, c1, c2 rune, opts ...interface{}) *PixelateEffect {
	// Start with default values for this specific effect type
	d := &PixelateEffect{
		BaseEffect: newBaseEffect(scr, 1.0), // Default intensity for dither
		Char1:      c1,
		Char2:      c2,
	}

	// Loop through options and apply them based on their type
	for _, opt := range opts {
		switch o := opt.(type) {
		case EffectOption: // Generic options
			o(d)
		case func(*PixelateEffect): // PixelateEffect-specific options
			o(d)
		}
	}
	return d
}

func (d *PixelateEffect) Clone() Effect {
	return NewPixelateEffect(d.screen, d.Char1, d.Char2,
		WithDuration(d.duration),
	)
}

func (f *PixelateEffect) IsContinuous() bool {
	return false
}

func (d *PixelateEffect) OnEvent(owner *pane, event Event) {
	// This effect only cares about Control On/Off events.
	switch event.Type {
	case EventControlOn:
		d.activate()
	case EventControlOff:
		d.inactivate()
	}
}

func (d *PixelateEffect) Apply(buffer [][]Cell) [][]Cell {
	if d.getState() == StateOff {
		return buffer
	}
	intensity := d.getIntensity()
	if intensity == 0 {
		return buffer
	}

	// For the dither effect, we can use the intensity as a probability
	// to decide whether to replace the character. This creates a "fading-in" dither.
	for y := range buffer {
		for x := range buffer[y] {
			if buffer[y][x].Ch != ' ' {
				if rand.Float32() < intensity {
					// As the intensity animates from 0 to 1, we are more likely
					// to replace the character with the dither character.
					if rand.Float32() < 0.5 {
						buffer[y][x].Ch = d.Char1
					} else {
						buffer[y][x].Ch = d.Char2
					}
				}
			}
		}
	}
	return buffer
}
