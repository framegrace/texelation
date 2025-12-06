// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/layout_transition.go
// Summary: Client-side layout transition animations for smooth pane splits/removes.
// Usage: Interpolates between tree snapshots to animate layout changes.

package clientruntime

import (
	"math"
	"sync"
	"time"

	"texelation/protocol"
)

// PaneLayout stores the visual layout of a pane for animation purposes.
type PaneLayout struct {
	ID     [16]byte
	X      int
	Y      int
	Width  int
	Height int
}

// LayoutTransitionConfig defines animation behavior.
type LayoutTransitionConfig struct {
	Enabled      bool
	Duration     time.Duration
	EasingFunc   string // "linear", "smoothstep", "ease-in", "ease-out", "ease-in-out"
	MinThreshold int    // Minimum size change (in cells) to trigger animation
}

// DefaultLayoutTransitionConfig returns sensible defaults.
func DefaultLayoutTransitionConfig() LayoutTransitionConfig {
	return LayoutTransitionConfig{
		Enabled:      true,
		Duration:     200 * time.Millisecond,
		EasingFunc:   "smoothstep",
		MinThreshold: 3, // Only animate if change is >= 3 cells
	}
}

// LayoutTransitionAnimator manages smooth transitions between tree snapshots.
type LayoutTransitionAnimator struct {
	mu           sync.RWMutex
	config       LayoutTransitionConfig
	oldLayouts   map[[16]byte]PaneLayout
	newLayouts   map[[16]byte]PaneLayout
	animating    bool
	startTime    time.Time
	renderSignal chan<- struct{}
}

// NewLayoutTransitionAnimator creates a new animator with default config.
func NewLayoutTransitionAnimator() *LayoutTransitionAnimator {
	return &LayoutTransitionAnimator{
		config:     DefaultLayoutTransitionConfig(),
		oldLayouts: make(map[[16]byte]PaneLayout),
		newLayouts: make(map[[16]byte]PaneLayout),
	}
}

// SetConfig updates the animation configuration.
func (a *LayoutTransitionAnimator) SetConfig(cfg LayoutTransitionConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = cfg
}

// AttachRenderSignal connects the animator to the client's render channel.
func (a *LayoutTransitionAnimator) AttachRenderSignal(ch chan<- struct{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.renderSignal = ch
}

// OnTreeSnapshot is called when a new tree snapshot arrives from the server.
// It detects layout changes and starts animations if needed.
func (a *LayoutTransitionAnimator) OnTreeSnapshot(snap protocol.TreeSnapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.config.Enabled {
		return
	}

	// Build new layout map
	newLayouts := make(map[[16]byte]PaneLayout)
	for _, pane := range snap.Panes {
		newLayouts[pane.PaneID] = PaneLayout{
			ID:     pane.PaneID,
			X:      int(pane.X),
			Y:      int(pane.Y),
			Width:  int(pane.Width),
			Height: int(pane.Height),
		}
	}

	// Check if any panes have changed significantly
	hasSignificantChange := false
	for id, newLayout := range newLayouts {
		oldLayout, existed := a.oldLayouts[id]
		if !existed {
			// New pane appeared
			hasSignificantChange = true
			break
		}
		// Check if change exceeds threshold
		if abs(newLayout.X-oldLayout.X) >= a.config.MinThreshold ||
			abs(newLayout.Y-oldLayout.Y) >= a.config.MinThreshold ||
			abs(newLayout.Width-oldLayout.Width) >= a.config.MinThreshold ||
			abs(newLayout.Height-oldLayout.Height) >= a.config.MinThreshold {
			hasSignificantChange = true
			break
		}
	}

	// Also check for removed panes
	if !hasSignificantChange {
		for id := range a.oldLayouts {
			if _, exists := newLayouts[id]; !exists {
				hasSignificantChange = true
				break
			}
		}
	}

	// Start animation if there's a significant change
	if hasSignificantChange && len(a.oldLayouts) > 0 {
		a.newLayouts = newLayouts
		a.animating = true
		a.startTime = time.Now()
		// Kick off animation loop
		go a.animationLoop()
	} else {
		// No significant change, just update directly
		a.oldLayouts = newLayouts
	}
}

// animationLoop runs the animation at 60fps until complete.
func (a *LayoutTransitionAnimator) animationLoop() {
	ticker := time.NewTicker(16 * time.Millisecond) // ~60fps
	defer ticker.Stop()

	for {
		<-ticker.C

		a.mu.RLock()
		if !a.animating {
			a.mu.RUnlock()
			return
		}

		elapsed := time.Since(a.startTime)
		if elapsed >= a.config.Duration {
			a.mu.RUnlock()
			a.completeAnimation()
			return
		}
		a.mu.RUnlock()

		// Trigger redraw
		a.signalRender()
	}
}

// completeAnimation finishes the animation and commits the new layout.
func (a *LayoutTransitionAnimator) completeAnimation() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.animating = false
	a.oldLayouts = a.newLayouts
	a.newLayouts = make(map[[16]byte]PaneLayout)

	// Final render at target position
	a.signalRender()
}

// GetInterpolatedLayout returns the current animated layout for a pane.
// Returns the layout and true if animating, or the static layout and false if not.
func (a *LayoutTransitionAnimator) GetInterpolatedLayout(paneID [16]byte) (PaneLayout, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.animating {
		if layout, ok := a.oldLayouts[paneID]; ok {
			return layout, false
		}
		return PaneLayout{}, false
	}

	oldLayout, hadOld := a.oldLayouts[paneID]
	newLayout, hasNew := a.newLayouts[paneID]

	if !hasNew {
		// Pane was removed - animate it shrinking
		if hadOld {
			t := a.getProgress()
			// Fade out the pane
			return PaneLayout{
				ID:     paneID,
				X:      oldLayout.X,
				Y:      oldLayout.Y,
				Width:  int(float64(oldLayout.Width) * (1.0 - t)),
				Height: int(float64(oldLayout.Height) * (1.0 - t)),
			}, true
		}
		return PaneLayout{}, false
	}

	if !hadOld {
		// Pane is new - animate it growing
		t := a.getProgress()
		return PaneLayout{
			ID:     paneID,
			X:      newLayout.X,
			Y:      newLayout.Y,
			Width:  int(float64(newLayout.Width) * t),
			Height: int(float64(newLayout.Height) * t),
		}, true
	}

	// Interpolate between old and new
	t := a.getProgress()
	return PaneLayout{
		ID:     paneID,
		X:      lerp(oldLayout.X, newLayout.X, t),
		Y:      lerp(oldLayout.Y, newLayout.Y, t),
		Width:  lerp(oldLayout.Width, newLayout.Width, t),
		Height: lerp(oldLayout.Height, newLayout.Height, t),
	}, true
}

// getProgress returns animation progress [0..1] with easing applied.
// Must be called with lock held (RLock is sufficient).
func (a *LayoutTransitionAnimator) getProgress() float64 {
	elapsed := time.Since(a.startTime)
	if elapsed >= a.config.Duration {
		return 1.0
	}

	t := float64(elapsed) / float64(a.config.Duration)

	// Apply easing function
	switch a.config.EasingFunc {
	case "linear":
		return t
	case "ease-in":
		return t * t
	case "ease-out":
		return t * (2.0 - t)
	case "ease-in-out":
		if t < 0.5 {
			return 2.0 * t * t
		}
		return -1.0 + (4.0-2.0*t)*t
	case "smoothstep":
		fallthrough
	default:
		return smoothstep(t)
	}
}

// IsAnimating returns true if an animation is currently in progress.
func (a *LayoutTransitionAnimator) IsAnimating() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.animating
}

// signalRender triggers a render update if connected.
// Must be called WITHOUT lock held to avoid deadlock.
func (a *LayoutTransitionAnimator) signalRender() {
	a.mu.RLock()
	ch := a.renderSignal
	a.mu.RUnlock()

	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Utility functions

func smoothstep(t float64) float64 {
	return t * t * (3.0 - 2.0*t)
}

func lerp(a, b int, t float64) int {
	return int(math.Round(float64(a) + float64(b-a)*t))
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
