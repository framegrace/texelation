package texel

import (
	"context"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"sync"
	"time"
)

// EffectState defines the possible states of an effect.
type EffectState int

const (
	StateOff EffectState = iota
	StateFadingIn
	StateOn
	StateFadingOut
)

type FadeEffect struct {
	FadeColor           tcell.Color
	targetIntensity     float32
	currentIntensity    float32
	state               EffectState
	isControlModeEffect bool

	screen *Screen
	mu     sync.Mutex

	name       string
	animCancel context.CancelFunc
}

// NewFadeEffect creates a new fade effect.
func NewFadeEffect(scr *Screen, color tcell.Color, intensity float32, isControlModeEffect bool) *FadeEffect {
	return &FadeEffect{
		screen:              scr,
		FadeColor:           color,
		targetIntensity:     intensity,
		state:               StateOff,
		isControlModeEffect: isControlModeEffect,
	}
}

func (f *FadeEffect) Clone() Effect {
	return NewFadeEffect(f.screen, f.FadeColor, f.targetIntensity, f.isControlModeEffect)
}

func (f *FadeEffect) String() string {
	return fmt.Sprintf("FadeEffect: %s,%i,%t", f.FadeColor, f.targetIntensity, f.isControlModeEffect)
}

func (f *FadeEffect) OnEvent(owner *Pane, event Event) {
	if f.isControlModeEffect {
		// This effect only cares about Control On/Off events.
		switch event.Type {
		case EventControlOn:
			f.activate()
		case EventControlOff:
			f.inactivate()
		}
	} else {
		// This is the normal inactive fade effect. It reacts to any event
		// that might change which pane is active.
		switch event.Type {
		case EventControlOff, EventActivePaneChanged:
			if f.screen.activeLeaf != nil && f.screen.activeLeaf.Pane == owner {
				f.inactivate()
			} else {
				f.activate()
			}
		case EventControlOn:
			// When control mode turns on, the normal fade should turn off.
			f.inactivate()
		}
	}
}

// Thread-safe getters and setters for state management
func (f *FadeEffect) setIntensity(i float32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentIntensity = i
}

func (f *FadeEffect) getIntensity() float32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.currentIntensity
}

func (f *FadeEffect) setState(s EffectState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
}

func (f *FadeEffect) getState() EffectState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *FadeEffect) activate() {
	currentState := f.getState()
	if currentState == StateOn || currentState == StateFadingIn {
		return // Already on or fading in
	}
	f.animate(f.targetIntensity, StateFadingIn, StateOn)
}

func (f *FadeEffect) inactivate() {
	currentState := f.getState()
	if currentState == StateOff || currentState == StateFadingOut {
		return // Already off or fading out
	}
	f.animate(0.0, StateFadingOut, StateOff)
}

// animate handles the smooth transition of the effect's intensity and state.
func (f *FadeEffect) animate(to float32, duringState, endState EffectState) {
	f.mu.Lock()
	// Cancel any existing animation for this effect
	if f.animCancel != nil {
		f.animCancel()
	}
	from := f.currentIntensity
	f.state = duringState // Set the state to FadingIn or FadingOut immediately
	ctx, cancel := context.WithCancel(context.Background())
	f.animCancel = cancel
	f.mu.Unlock()

	go func() {
		duration := 200 * time.Millisecond
		startTime := time.Now()
		ticker := time.NewTicker(16 * time.Millisecond) // Approx 60fps
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done(): // Animation was cancelled
				return
			case <-ticker.C:
				elapsed := time.Since(startTime)
				if elapsed >= duration {
					f.setIntensity(to)
					f.setState(endState) // Set the final state (On or Off)
					f.screen.Refresh()
					return // Animation finished
				}

				progress := float32(elapsed) / float32(duration)
				newIntensity := from + (to-from)*progress
				f.setIntensity(newIntensity)
				f.screen.Refresh()
			}
		}
	}()
}

// Apply walks the buffer, blends fg/bg, then uses your screen.getStyle cache.
func (f *FadeEffect) Apply(buffer [][]Cell) [][]Cell {
	state := f.getState()
	if state == StateOff {
		return buffer // No-op if effect is off
	}

	intensity := f.getIntensity()
	if intensity == 0 {
		return buffer // Also a no-op if intensity is zero
	}

	for y := range buffer {
		for x := range buffer[y] {
			cell := &buffer[y][x]
			fg, bg, attrs := cell.Style.Decompose()

			if !fg.Valid() {
				fg = f.screen.DefaultFgColor
			}
			if !bg.Valid() {
				bg = f.screen.DefaultBgColor
			}

			blendedFg := f.blendColor(fg, f.FadeColor, intensity)
			blendedBg := f.blendColor(bg, f.FadeColor, intensity)

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
