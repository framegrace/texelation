// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/timeline.go
// Summary: Thread-safe animation timeline with configurable easing functions.
// Usage: Simplifies effect implementation by handling all animation state automatically.
// Notes: Supports per-key timelines with linear, smoothstep, and custom easing.

package effects

import (
	"sync"
	"time"
)

// EasingFunc defines an easing function that maps progress [0,1] to eased value [0,1]
type EasingFunc func(progress float32) float32

// Common easing functions
var (
	// EaseLinear - No easing, constant speed
	EaseLinear EasingFunc = func(t float32) float32 { return t }

	// EaseSmoothstep - Smooth S-curve (default, recommended for most animations)
	// Accelerates at start, decelerates at end
	EaseSmoothstep EasingFunc = func(t float32) float32 {
		return t * t * (3.0 - 2.0*t)
	}

	// EaseSmootherstep - Even smoother S-curve with zero derivatives at 0 and 1
	EaseSmootherstep EasingFunc = func(t float32) float32 {
		return t * t * t * (t*(t*6.0-15.0) + 10.0)
	}

	// EaseInQuad - Quadratic ease-in (slow start, accelerating)
	EaseInQuad EasingFunc = func(t float32) float32 {
		return t * t
	}

	// EaseOutQuad - Quadratic ease-out (fast start, decelerating)
	EaseOutQuad EasingFunc = func(t float32) float32 {
		return t * (2.0 - t)
	}

	// EaseInOutQuad - Quadratic ease-in-out
	EaseInOutQuad EasingFunc = func(t float32) float32 {
		if t < 0.5 {
			return 2.0 * t * t
		}
		return -1.0 + (4.0-2.0*t)*t
	}

	// EaseInCubic - Cubic ease-in (slower start)
	EaseInCubic EasingFunc = func(t float32) float32 {
		return t * t * t
	}

	// EaseOutCubic - Cubic ease-out
	EaseOutCubic EasingFunc = func(t float32) float32 {
		t1 := t - 1.0
		return t1*t1*t1 + 1.0
	}

	// EaseInOutCubic - Cubic ease-in-out
	EaseInOutCubic EasingFunc = func(t float32) float32 {
		if t < 0.5 {
			return 4.0 * t * t * t
		}
		t1 := 2.0*t - 2.0
		return 1.0 + t1*t1*t1*0.5
	}
)

// AnimateOptions configures an animation transition
type AnimateOptions struct {
	Duration time.Duration // Animation duration (default: 0 = instant)
	Easing   EasingFunc    // Easing function (default: EaseSmoothstep)
}

// DefaultAnimateOptions returns options with smoothstep easing
func DefaultAnimateOptions(duration time.Duration) AnimateOptions {
	return AnimateOptions{
		Duration: duration,
		Easing:   EaseSmoothstep,
	}
}

// keyState tracks animation state for a single key
type keyState struct {
	current   float32
	start     float32
	target    float32
	startTime time.Time
	duration  time.Duration
	easing    EasingFunc
}

// Timeline provides thread-safe, per-key animation timelines with automatic state management
type Timeline struct {
	states         map[interface{}]*keyState
	mu             sync.RWMutex
	defaultEasing  EasingFunc
	defaultInitial float32
}

// NewTimeline creates a new timeline manager
// defaultInitial: initial value for uninitialized keys (typically 0.0)
func NewTimeline(defaultInitial float32) *Timeline {
	return &Timeline{
		states:         make(map[interface{}]*keyState),
		defaultEasing:  EaseSmoothstep,
		defaultInitial: defaultInitial,
	}
}

// AnimateTo starts or updates an animation for the given key
// Returns the current animated value at this moment
//
// Minimal usage:
//
//	value := timeline.AnimateTo(key, target, duration)
//
// With custom easing:
//
//	value := timeline.AnimateToWithOptions(key, target, AnimateOptions{
//	    Duration: 300*time.Millisecond,
//	    Easing: EaseInOutCubic,
//	})
func (tl *Timeline) AnimateTo(key interface{}, target float32, duration time.Duration) float32 {
	return tl.AnimateToWithOptions(key, target, DefaultAnimateOptions(duration))
}

// AnimateToWithOptions starts an animation with custom easing function
func (tl *Timeline) AnimateToWithOptions(key interface{}, target float32, opts AnimateOptions) float32 {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	now := time.Now()
	state := tl.states[key]

	if state == nil {
		// Initialize new key
		state = &keyState{
			current:  tl.defaultInitial,
			start:    tl.defaultInitial,
			target:   target,
			duration: opts.Duration,
			easing:   opts.Easing,
		}
		if opts.Easing == nil {
			state.easing = tl.defaultEasing
		}
		tl.states[key] = state

		// If duration is zero, jump to target immediately
		if opts.Duration <= 0 {
			state.current = target
			return target
		}

		state.startTime = now
		return state.current
	}

	// Update existing animation
	// First, compute current value to use as new start
	current := tl.computeValue(state, now)

	// Start new animation from current position
	state.current = current
	state.start = current
	state.target = target
	state.startTime = now
	state.duration = opts.Duration
	if opts.Easing != nil {
		state.easing = opts.Easing
	}

	// If duration is zero or already at target, finish immediately
	if opts.Duration <= 0 || current == target {
		state.current = target
		return target
	}

	return current
}

// Get returns the current animated value for a key
// If the key hasn't been initialized, returns the default initial value
func (tl *Timeline) Get(key interface{}) float32 {
	tl.mu.RLock()
	state := tl.states[key]
	tl.mu.RUnlock()

	if state == nil {
		return tl.defaultInitial
	}

	tl.mu.Lock()
	value := tl.computeValue(state, time.Now())
	state.current = value
	tl.mu.Unlock()

	return value
}

// IsAnimating returns true if the key is currently animating
func (tl *Timeline) IsAnimating(key interface{}) bool {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	state := tl.states[key]
	if state == nil || state.duration <= 0 {
		return false
	}

	elapsed := time.Since(state.startTime)
	return elapsed < state.duration && state.current != state.target
}

// HasActiveAnimations returns true if any key is currently animating
func (tl *Timeline) HasActiveAnimations() bool {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	for _, state := range tl.states {
		if state.duration > 0 && time.Since(state.startTime) < state.duration {
			if state.current != state.target {
				return true
			}
		}
	}
	return false
}

// Update advances all animations to the given time
// This is called by the effect manager on each frame
func (tl *Timeline) Update(now time.Time) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	for _, state := range tl.states {
		state.current = tl.computeValue(state, now)
	}
}

// Reset removes the timeline state for a key
func (tl *Timeline) Reset(key interface{}) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	delete(tl.states, key)
}

// Clear removes all timeline states
func (tl *Timeline) Clear() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.states = make(map[interface{}]*keyState)
}

// computeValue calculates the current value for a state at the given time
// Must be called with lock held
func (tl *Timeline) computeValue(state *keyState, now time.Time) float32 {
	if state.duration <= 0 {
		return state.target
	}

	if now.Before(state.startTime) {
		return state.start
	}

	elapsed := now.Sub(state.startTime)
	if elapsed >= state.duration {
		return state.target
	}

	// Calculate progress [0, 1]
	progress := float32(elapsed) / float32(state.duration)
	if progress < 0 {
		progress = 0
	} else if progress > 1 {
		progress = 1
	}

	// Apply easing function
	easing := state.easing
	if easing == nil {
		easing = tl.defaultEasing
	}
	easedProgress := easing(progress)

	// Interpolate
	return state.start + (state.target-state.start)*easedProgress
}
