// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/layout_transitions.go
// Summary: Server-side layout transition animations for smooth split/close operations.
// Usage: Animates SplitRatios over time when panes are added or removed, similar to manual resize.

package texel

import (
	"log"
	"math"
	"sync"
	"time"

	"texelation/internal/effects"
)

// LayoutTransitionConfig holds configuration for layout transitions from theme.json.
type LayoutTransitionConfig struct {
	Enabled      bool    `json:"enabled"`
	DurationMs   int     `json:"duration_ms"`
	Easing       string  `json:"easing"`
	MinThreshold int     `json:"min_threshold"`
}

// transitionState tracks an ongoing split ratio animation for a node.
type transitionState struct {
	node         *Node
	startRatios  []float64
	targetRatios []float64
	startTime    time.Time
	duration     time.Duration
	easing       string
	onComplete   func() // Called when animation finishes
}

// LayoutTransitionManager coordinates smooth layout transitions on the server side.
// Unlike client-side effects (visual overlays), this animates the tree structure itself.
type LayoutTransitionManager struct {
	mu         sync.Mutex
	enabled    bool
	duration   time.Duration
	easing     string
	timeline   *effects.Timeline
	animating  map[*Node]*transitionState
	desktop    *DesktopEngine
	ticker     *time.Ticker
	stopCh     chan struct{}
	graceStart time.Time
}

// NewLayoutTransitionManager creates a new layout transition manager.
func NewLayoutTransitionManager(config LayoutTransitionConfig, desktop *DesktopEngine) *LayoutTransitionManager {
	if !config.Enabled {
		log.Println("LayoutTransitionManager: Disabled via config")
		return &LayoutTransitionManager{enabled: false}
	}

	duration := time.Duration(config.DurationMs) * time.Millisecond
	if duration <= 0 {
		duration = 300 * time.Millisecond
	}

	easing := config.Easing
	if easing == "" {
		easing = "smoothstep"
	}

	m := &LayoutTransitionManager{
		enabled:   true,
		duration:  duration,
		easing:    easing,
		timeline:  effects.NewTimeline(0.0),
		animating: make(map[*Node]*transitionState),
		desktop:   desktop,
		stopCh:    make(chan struct{}),
	}

	// Grace period to avoid animating first panel on startup
	m.graceStart = time.Now()

	log.Printf("LayoutTransitionManager: Enabled (duration=%v, easing=%s)", duration, easing)
	m.startAnimationLoop()
	return m
}

// startAnimationLoop runs a ticker that updates animations and broadcasts tree changes.
func (m *LayoutTransitionManager) startAnimationLoop() {
	m.ticker = time.NewTicker(16 * time.Millisecond) // ~60fps

	go func() {
		for {
			select {
			case <-m.ticker.C:
				m.updateAnimations()
			case <-m.stopCh:
				return
			}
		}
	}()
}

// Stop stops the animation loop.
func (m *LayoutTransitionManager) Stop() {
	if m.ticker != nil {
		m.ticker.Stop()
	}
	close(m.stopCh)
}

// AnimateSplit starts animating split ratios from current to target values.
// This is called instead of immediately setting ratios in SplitActive/CloseActiveLeaf.
func (m *LayoutTransitionManager) AnimateSplit(node *Node, targetRatios []float64) {
	m.animateSplitWithCallback(node, targetRatios, nil)
}

// AnimateRemoval animates a child pane shrinking to tiny size, then calls onComplete.
// The onComplete callback should do the actual removal of the pane.
func (m *LayoutTransitionManager) AnimateRemoval(node *Node, closingIndex int, onComplete func()) {
	if !m.enabled || node == nil || closingIndex < 0 || closingIndex >= len(node.SplitRatios) {
		// If disabled or invalid, just call callback immediately
		if onComplete != nil {
			onComplete()
		}
		return
	}

	// Grace period: don't animate first removal on startup
	if time.Since(m.graceStart) < 200*time.Millisecond {
		log.Println("LayoutTransitionManager: Skipping removal animation (grace period)")
		if onComplete != nil {
			onComplete()
		}
		return
	}

	// Calculate target ratios: closing pane gets 0.001 (essentially 0), others share remaining
	targetRatios := make([]float64, len(node.SplitRatios))
	remaining := 0.999
	numOthers := len(node.SplitRatios) - 1
	if numOthers > 0 {
		for i := range targetRatios {
			if i == closingIndex {
				targetRatios[i] = 0.001 // Much smaller so it reaches 0 pixels faster
			} else {
				targetRatios[i] = remaining / float64(numOthers)
			}
		}
	} else {
		targetRatios[closingIndex] = 1.0
	}

	log.Printf("LayoutTransitionManager: Starting removal animation for index %d (ratios: %v → %v)",
		closingIndex, node.SplitRatios, targetRatios)
	m.animateSplitWithCallback(node, targetRatios, onComplete)
}

// animateSplitWithCallback is the internal method that handles animation with optional callback.
func (m *LayoutTransitionManager) animateSplitWithCallback(node *Node, targetRatios []float64, onComplete func()) {
	if !m.enabled || node == nil {
		// If disabled, just set ratios immediately and call callback
		if node != nil {
			node.SplitRatios = targetRatios
		}
		if onComplete != nil {
			onComplete()
		}
		return
	}

	// Grace period: don't animate first split on startup
	if time.Since(m.graceStart) < 200*time.Millisecond {
		log.Println("LayoutTransitionManager: Skipping animation (grace period)")
		node.SplitRatios = targetRatios
		if onComplete != nil {
			onComplete()
		}
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Copy current ratios as start state
	startRatios := make([]float64, len(node.SplitRatios))
	copy(startRatios, node.SplitRatios)

	state := &transitionState{
		node:         node,
		startRatios:  startRatios,
		targetRatios: targetRatios,
		startTime:    time.Now(),
		duration:     m.duration,
		easing:       m.easing,
		onComplete:   onComplete,
	}

	m.animating[node] = state
	log.Printf("LayoutTransitionManager: Starting animation for node (ratios: %v → %v, duration=%v)",
		startRatios, targetRatios, m.duration)
}

// updateAnimations advances all active animations and broadcasts updates.
func (m *LayoutTransitionManager) updateAnimations() {
	m.mu.Lock()

	if len(m.animating) == 0 {
		m.mu.Unlock()
		return
	}

	now := time.Now()
	needsBroadcast := false
	completed := make([]*Node, 0)
	callbacks := make([]func(), 0)

	for node, state := range m.animating {
		elapsed := now.Sub(state.startTime)
		progress := float64(elapsed) / float64(state.duration)

		if progress >= 1.0 {
			// Animation complete
			node.SplitRatios = state.targetRatios
			completed = append(completed, node)
			if state.onComplete != nil {
				callbacks = append(callbacks, state.onComplete)
			}
			needsBroadcast = true
			log.Printf("LayoutTransitionManager: Animation complete for node (final ratios: %v)", state.targetRatios)
		} else {
			// Interpolate ratios
			t := m.applyEasing(progress, state.easing)
			node.SplitRatios = make([]float64, len(state.startRatios))
			for i := range node.SplitRatios {
				node.SplitRatios[i] = state.startRatios[i] + (state.targetRatios[i]-state.startRatios[i])*t
			}
			needsBroadcast = true

			// For removal animations, complete early if the pane has shrunk to near-zero
			// This prevents visible "hanging" at the end when pane is 0 pixels but animation continues
			if state.onComplete != nil {
				for i, ratio := range node.SplitRatios {
					if state.targetRatios[i] < 0.01 && ratio < 0.005 {
						// Pane has shrunk to essentially nothing, complete animation now
						node.SplitRatios = state.targetRatios
						completed = append(completed, node)
						callbacks = append(callbacks, state.onComplete)
						log.Printf("LayoutTransitionManager: Early completion for removal (ratio %v reached, target was %v)",
							ratio, state.targetRatios[i])
						break
					}
				}
			}
		}
	}

	// Remove completed animations
	for _, node := range completed {
		delete(m.animating, node)
	}

	// Recalculate layout and broadcast if anything changed
	if needsBroadcast && m.desktop != nil {
		m.desktop.recalculateLayout()
		m.desktop.broadcastTreeChanged()
	}

	m.mu.Unlock()

	// Call completion callbacks AFTER releasing lock to avoid potential deadlocks
	for _, callback := range callbacks {
		callback()
	}
}

// applyEasing applies an easing function to the linear progress value.
func (m *LayoutTransitionManager) applyEasing(t float64, easing string) float64 {
	switch easing {
	case "linear":
		return t
	case "smoothstep":
		return t * t * (3 - 2*t)
	case "ease-in-out":
		if t < 0.5 {
			return 2 * t * t
		}
		return 1 - 2*(1-t)*(1-t)
	case "spring":
		// Physics-based spring animation with overshoot and damped oscillation
		// Simulates a spring connecting the current position to the target
		damping := 0.5    // How quickly oscillations die out (0-1, lower = more bouncy)
		frequency := 2.5  // How many bounces (higher = more oscillations)

		// Damped harmonic oscillator: overshoots target and wobbles before settling
		return 1.0 - math.Exp(-damping*10.0*t)*math.Cos(frequency*2.0*math.Pi*t)
	default:
		return t * t * (3 - 2*t) // default to smoothstep
	}
}

// IsAnimating returns true if any animations are in progress.
func (m *LayoutTransitionManager) IsAnimating() bool {
	if !m.enabled {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.animating) > 0
}

// UpdateConfig updates the manager's configuration at runtime (e.g., on theme reload).
// Does not affect animations already in progress.
func (m *LayoutTransitionManager) UpdateConfig(config LayoutTransitionConfig) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.enabled = config.Enabled

	if config.DurationMs > 0 {
		m.duration = time.Duration(config.DurationMs) * time.Millisecond
	}

	if config.Easing != "" {
		m.easing = config.Easing
	}

	log.Printf("LayoutTransitionManager: Config updated (enabled=%v, duration=%v, easing=%s)",
		m.enabled, m.duration, m.easing)
}
