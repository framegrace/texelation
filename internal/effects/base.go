// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/base.go
// Summary: Base effect helpers that simplify effect implementation.
// Usage: Embed PaneEffectBase or WorkspaceEffectBase in your effect struct.
// Notes: Handles all Timeline boilerplate, letting authors focus on logic.

package effects

import (
	"time"
)

// PaneEffectBase provides a reusable foundation for pane-scoped effects.
// Embed this in your effect struct to automatically handle Timeline management.
//
// Example usage:
//
//	type myPaneEffect struct {
//	    PaneEffectBase
//	    color tcell.Color
//	}
//
//	func (e *myPaneEffect) HandleTrigger(trigger EffectTrigger) {
//	    if trigger.Type == TriggerPaneActive {
//	        target := float32(0)
//	        if !trigger.Active {
//	            target = 1.0
//	        }
//	        e.Animate(trigger.PaneID, target, trigger.Timestamp)
//	    }
//	}
//
//	func (e *myPaneEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {
//	    intensity := e.Get(pane.ID, time.Now())
//	    // ... apply visual effect using intensity
//	}
type PaneEffectBase struct {
	timeline *Timeline
	duration time.Duration
}

// NewPaneEffectBase creates a new pane effect base with the given animation duration.
func NewPaneEffectBase(duration time.Duration) PaneEffectBase {
	return PaneEffectBase{
		timeline: NewTimeline(0.0),
		duration: duration,
	}
}

// Update advances all pane animations to the given time.
// Call this from your effect's Update method.
func (b *PaneEffectBase) Update(now time.Time) {
	b.timeline.Update(now)
}

// Animate starts or updates an animation for the given pane ID.
// Returns the current animated value at this moment.
func (b *PaneEffectBase) Animate(paneID PaneID, target float32, now time.Time) float32 {
	return b.timeline.AnimateTo(paneID, target, b.duration, now)
}

// AnimateWithOptions starts an animation with custom easing for the given pane ID.
func (b *PaneEffectBase) AnimateWithOptions(paneID PaneID, target float32, opts AnimateOptions, now time.Time) float32 {
	return b.timeline.AnimateToWithOptions(paneID, target, opts, now)
}

// Get returns the current animated value for a pane ID.
func (b *PaneEffectBase) Get(paneID PaneID, now time.Time) float32 {
	return b.timeline.Get(paneID, now)
}

// GetCached returns the last computed value for a pane ID without recomputing.
// Use this in ApplyPane after Update() has been called in the same frame.
func (b *PaneEffectBase) GetCached(paneID PaneID) float32 {
	return b.timeline.GetCached(paneID)
}

// IsAnimating returns true if the pane is currently animating.
func (b *PaneEffectBase) IsAnimating(paneID PaneID, now time.Time) bool {
	return b.timeline.IsAnimating(paneID, now)
}

// Active returns true if any pane has an active animation or non-zero value.
func (b *PaneEffectBase) Active() bool {
	return b.timeline.HasActiveAnimations()
}

// Reset removes the animation state for a pane ID.
func (b *PaneEffectBase) Reset(paneID PaneID) {
	b.timeline.Reset(paneID)
}

// WorkspaceEffectBase provides a reusable foundation for workspace-scoped effects.
// Similar to PaneEffectBase but uses a single animation key for the entire workspace.
//
// Example usage:
//
//	type myWorkspaceEffect struct {
//	    WorkspaceEffectBase
//	    color tcell.Color
//	}
//
//	func (e *myWorkspaceEffect) HandleTrigger(trigger EffectTrigger) {
//	    if trigger.Type == TriggerWorkspaceControl {
//	        target := float32(0)
//	        if trigger.Active {
//	            target = 1.0
//	        }
//	        e.Animate("effect", target, trigger.Timestamp)
//	    }
//	}
//
//	func (e *myWorkspaceEffect) ApplyWorkspace(buffer [][]client.Cell) {
//	    intensity := e.Get("effect", time.Now())
//	    // ... apply visual effect using intensity
//	}
type WorkspaceEffectBase struct {
	timeline *Timeline
	duration time.Duration
}

// NewWorkspaceEffectBase creates a new workspace effect base with the given animation duration.
func NewWorkspaceEffectBase(duration time.Duration) WorkspaceEffectBase {
	return WorkspaceEffectBase{
		timeline: NewTimeline(0.0),
		duration: duration,
	}
}

// Update advances all workspace animations to the given time.
// Call this from your effect's Update method.
func (b *WorkspaceEffectBase) Update(now time.Time) {
	b.timeline.Update(now)
}

// Animate starts or updates an animation for the given key.
// Returns the current animated value at this moment.
func (b *WorkspaceEffectBase) Animate(key interface{}, target float32, now time.Time) float32 {
	return b.timeline.AnimateTo(key, target, b.duration, now)
}

// AnimateWithOptions starts an animation with custom easing for the given key.
func (b *WorkspaceEffectBase) AnimateWithOptions(key interface{}, target float32, opts AnimateOptions, now time.Time) float32 {
	return b.timeline.AnimateToWithOptions(key, target, opts, now)
}

// Get returns the current animated value for a key.
func (b *WorkspaceEffectBase) Get(key interface{}, now time.Time) float32 {
	return b.timeline.Get(key, now)
}

// GetCached returns the last computed value for a key without recomputing.
// Use this in ApplyWorkspace after Update() has been called in the same frame.
func (b *WorkspaceEffectBase) GetCached(key interface{}) float32 {
	return b.timeline.GetCached(key)
}

// IsAnimating returns true if the key is currently animating.
func (b *WorkspaceEffectBase) IsAnimating(key interface{}, now time.Time) bool {
	return b.timeline.IsAnimating(key, now)
}

// Active returns true if any key has an active animation or non-zero value.
func (b *WorkspaceEffectBase) Active() bool {
	return b.timeline.HasActiveAnimations()
}

// Reset removes the animation state for a key.
func (b *WorkspaceEffectBase) Reset(key interface{}) {
	b.timeline.Reset(key)
}
