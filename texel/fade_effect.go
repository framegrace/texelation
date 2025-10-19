// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/fade_effect.go
// Summary: Implements fade effect capabilities for the core desktop engine.
// Usage: Used throughout the project to implement fade effect inside the desktop and panes.
// Notes: Legacy desktop logic migrated from the monolithic application.

// Fixed fade_effect.go
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
	effect := &FadeEffect{
		intensity: 0.0, // Explicitly set to 0
		fadeColor: fadeColor,
		desktop:   desktop,
	}
	return effect
}

// Add this debug logging to fade effect to see what's happening
func (f *FadeEffect) Apply(buffer *[][]Cell) {
	f.mu.RLock()
	intensity := f.intensity
	fadeColor := f.fadeColor
	f.mu.RUnlock()

	if intensity <= 0.0 {
		return
	}

	for y := range *buffer {
		for x := range (*buffer)[y] {
			cell := &(*buffer)[y][x]
			originalChar := cell.Ch
			fg, bg, attrs := cell.Style.Decompose()

			// Only process non-space characters or cells with background
			if originalChar == ' ' && !bg.Valid() {
				continue
			}

			// Use desktop defaults for invalid colors
			if !fg.Valid() {
				fg = f.desktop.DefaultFgColor
			}
			if !bg.Valid() {
				bg = f.desktop.DefaultBgColor
			}

			// Blend colors
			blendedFg := blendColor(fg, fadeColor, intensity)
			blendedBg := blendColor(bg, fadeColor, intensity)

			// Preserve text attributes and original character
			bold := attrs&tcell.AttrBold != 0
			underline := attrs&tcell.AttrUnderline != 0
			reverse := attrs&tcell.AttrReverse != 0

			// Create new style with blended colors
			cell.Style = tcell.StyleDefault.
				Foreground(blendedFg).
				Background(blendedBg).
				Bold(bold).
				Underline(underline).
				Reverse(reverse)

			// Keep the original character - this is crucial!
			cell.Ch = originalChar
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
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	f.mu.Lock()
	f.intensity = intensity
	f.mu.Unlock()
}

// IsAnimating returns true if the effect has non-zero intensity
func (f *FadeEffect) IsAnimating() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.intensity > 0.0
}

// blendColor performs linear interpolation between two colors
func blendColor(original, blend tcell.Color, intensity float32) tcell.Color {
	if !original.Valid() || !blend.Valid() {
		return original
	}

	r1, g1, b1 := original.RGB()
	r2, g2, b2 := blend.RGB()

	// Linear interpolation
	r := int32(float32(r1)*(1-intensity) + float32(r2)*intensity)
	g := int32(float32(g1)*(1-intensity) + float32(g2)*intensity)
	b := int32(float32(b1)*(1-intensity) + float32(b2)*intensity)

	// Clamp values to valid range
	if r < 0 {
		r = 0
	} else if r > 255 {
		r = 255
	}
	if g < 0 {
		g = 0
	} else if g > 255 {
		g = 255
	}
	if b < 0 {
		b = 0
	} else if b > 255 {
		b = 255
	}

	return tcell.NewRGBColor(r, g, b)
}
