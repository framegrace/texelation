// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/layout_animator.go
// Summary: Layout animation system for smooth pane transitions.
// Usage: Internal to Tree for animating split/remove/replace operations.
// Notes: Uses effects.Timeline for weight interpolation.

package texel

import (
	"sync"
	"time"

	"texelation/internal/effects"
)

// LayoutAnimator manages animated transitions for pane layout operations.
// It uses weight factors to smoothly animate splits, removals, and replacements.
//
// Design: This is Tree-internal (not exposed through Effect interface) to keep
// layout animation separate from visual effects. Effects can still react to
// layout changes via TriggerPaneSplit/Removing/Replaced events.
type LayoutAnimator struct {
	timeline *effects.Timeline
	duration time.Duration
	mu       sync.RWMutex
	enabled  bool // Can be disabled for instant layout changes
}

// NewLayoutAnimator creates a new layout animator with the given default duration.
func NewLayoutAnimator(duration time.Duration) *LayoutAnimator {
	return &LayoutAnimator{
		timeline: effects.NewTimeline(1.0), // Default to 1.0 (full weight)
		duration: duration,
		enabled:  false, // Disabled by default
	}
}

// SetEnabled enables or disables layout animations.
// When disabled, all operations are instant.
func (la *LayoutAnimator) SetEnabled(enabled bool) {
	la.mu.Lock()
	defer la.mu.Unlock()
	la.enabled = enabled
}

// IsEnabled returns whether animations are currently enabled.
func (la *LayoutAnimator) IsEnabled() bool {
	la.mu.RLock()
	defer la.mu.RUnlock()
	return la.enabled
}

// AnimatePaneWeight starts a weight animation for a pane.
// targetWeight should be in range [0..1]:
//   - 1.0 = full size (normal state)
//   - 0.0 = zero size (being removed)
//   - Values in between during transitions
func (la *LayoutAnimator) AnimatePaneWeight(paneID [16]byte, targetWeight float64, now time.Time) {
	la.mu.RLock()
	enabled := la.enabled
	duration := la.duration
	la.mu.RUnlock()

	if !enabled {
		// Instant transition when disabled
		duration = 0
	}

	// Convert float64 to float32 for Timeline
	target := float32(targetWeight)
	if target < 0 {
		target = 0
	} else if target > 1 {
		target = 1
	}

	la.timeline.AnimateTo(paneID, target, duration, now)
}

// AnimatePaneWeightWithDuration animates with a custom duration (overrides default).
func (la *LayoutAnimator) AnimatePaneWeightWithDuration(paneID [16]byte, targetWeight float64, duration time.Duration, now time.Time) {
	la.mu.RLock()
	enabled := la.enabled
	la.mu.RUnlock()

	if !enabled {
		duration = 0
	}

	target := float32(targetWeight)
	if target < 0 {
		target = 0
	} else if target > 1 {
		target = 1
	}

	la.timeline.AnimateTo(paneID, target, duration, now)
}

// GetPaneWeightFactor returns the current animated weight factor for a pane.
// Returns a value in [0..1]:
//   - 1.0 = full size
//   - 0.0 = zero size
//   - Values in between during animation
func (la *LayoutAnimator) GetPaneWeightFactor(paneID [16]byte, now time.Time) float64 {
	return float64(la.timeline.Get(paneID, now))
}

// GetPaneWeightFactorCached returns the last computed weight without recomputing.
// Use this after Update() has been called in the same frame.
func (la *LayoutAnimator) GetPaneWeightFactorCached(paneID [16]byte) float64 {
	return float64(la.timeline.GetCached(paneID))
}

// IsAnimating returns true if the pane is currently animating.
func (la *LayoutAnimator) IsAnimating(paneID [16]byte, now time.Time) bool {
	return la.timeline.IsAnimating(paneID, now)
}

// HasActiveAnimations returns true if any pane is currently animating.
func (la *LayoutAnimator) HasActiveAnimations() bool {
	return la.timeline.HasActiveAnimations()
}

// Update advances all animations to the given time.
// Should be called once per frame before layout calculations.
func (la *LayoutAnimator) Update(now time.Time) {
	la.timeline.Update(now)
}

// Reset removes the animation state for a pane.
// The pane will revert to the default weight (1.0) on next access.
func (la *LayoutAnimator) Reset(paneID [16]byte) {
	la.timeline.Reset(paneID)
}

// Clear removes all animation states.
func (la *LayoutAnimator) Clear() {
	la.timeline.Clear()
}
