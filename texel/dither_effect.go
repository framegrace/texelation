package texel

import (
	"sync"
)

// DitherEffect alternates a pane's content with a dither character on each frame.
type DitherEffect struct {
	DitherChar rune
	mu         sync.Mutex
	isOn       bool
	frameCount int
}

// NewDitherEffect creates a new effect that flickers content with a character.
func NewDitherEffect(char rune) *DitherEffect {
	return &DitherEffect{DitherChar: char}
}

// Clone creates a new, independent instance of the DitherEffect.
func (d *DitherEffect) Clone() Effect {
	return NewDitherEffect(d.DitherChar)
}

func (d *DitherEffect) IsContinuous() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.isOn
}

// OnEvent toggles the effect on or off.
func (d *DitherEffect) OnEvent(owner *pane, event Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch event.Type {
	case EventControlOn:
		d.isOn = true
	case EventControlOff:
		d.isOn = false
	}
}

// Apply flickers the buffer between its original state and the dither character.
func (d *DitherEffect) Apply(buffer [][]Cell) [][]Cell {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.isOn {
		return buffer
	}

	// On even frames, show the original content.
	if d.frameCount%2 == 0 {
		d.frameCount++
		return buffer
	}

	// On odd frames, apply the dither character.
	for y := range buffer {
		for x := range buffer[y] {
			if buffer[y][x].Ch != ' ' {
				buffer[y][x].Ch = d.DitherChar
			}
		}
	}
	d.frameCount++
	return buffer
}
