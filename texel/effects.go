package texel

import (
	"fmt"
	"time"
)

// --- Event System ---
type EventType int

const (
	EventControlOn EventType = iota
	EventControlOff
	EventActivePaneChanged
)

type Event struct{ Type EventType }

func (e Event) String() string {
	return fmt.Sprintf("Event: %s", e.Type)
}

// --- Interfaces ---
type Effect interface {
	Apply(buffer [][]Cell) [][]Cell
	Clone() Effect
	IsContinuous() bool
}

type EventListener interface {
	OnEvent(owner *Pane, event Event)
}

type EffectState int

const (
	StateOff EffectState = iota
	StateFadingIn
	StateOn
	StateFadingOut
)

// --- Generic Functional Options ---

// animationConfigurer is a private interface used to make options generic.
type animationConfigurer interface {
	setDuration(d time.Duration)
	setTargetIntensity(i float32)
}

// EffectOption is a function that configures any effect that has animation properties.
type EffectOption func(animationConfigurer)

// WithDuration is an option to set a custom animation duration.
func WithDuration(d time.Duration) EffectOption {
	return func(e animationConfigurer) {
		e.setDuration(d)
	}
}
