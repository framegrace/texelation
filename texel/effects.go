package texel

import (
	"time"
)

// --- Interfaces ---
type Effect interface {
	Apply(buffer [][]Cell, owner *pane, isActive bool) [][]Cell
	Clone() Effect
	IsContinuous() bool
}

type EffectState int

const (
	StateOff EffectState = iota
	StateFadingIn
	StateOn
	StateFadingOut
)

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
