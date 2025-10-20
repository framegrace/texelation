// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/layout_effect.go
// Summary: Implements layout effect capabilities for the core desktop engine.
// Usage: Used throughout the project to implement layout effect inside the desktop and panes.
// Notes: Legacy desktop logic migrated from the monolithic application.

package texel

import (
	"log"
	"sync"
)

type LayoutEffect struct {
	mu           sync.RWMutex
	intensity    float32 // 0.0 to 1.0
	targetRatios []float64
	startRatios  []float64
	node         *Node   // The node whose ratios we're animating
	screen       *Screen // For triggering layout recalculation
}

func NewLayoutEffect(node *Node, screen *Screen, startRatios, targetRatios []float64) *LayoutEffect {
	effect := &LayoutEffect{
		intensity:    0.0,
		targetRatios: make([]float64, len(targetRatios)),
		startRatios:  make([]float64, len(startRatios)),
		node:         node,
		screen:       screen,
	}
	// Copy slices to avoid reference issues
	copy(effect.targetRatios, targetRatios)
	copy(effect.startRatios, startRatios)

	log.Printf("NewLayoutEffect: Created for node with %d children, start=%v, target=%v",
		len(node.Children), startRatios, targetRatios)
	return effect
}

func (le *LayoutEffect) Apply(buffer *[][]Cell) {
	// Layout effects don't modify the visual buffer directly
	// They modify the tree structure and trigger relayout
	le.mu.RLock()
	intensity := le.intensity
	node := le.node
	screen := le.screen
	startRatios := le.startRatios
	targetRatios := le.targetRatios
	le.mu.RUnlock()

	if node == nil || len(startRatios) != len(targetRatios) {
		return
	}

	// Interpolate between start and target ratios
	currentRatios := make([]float64, len(startRatios))
	for i := range currentRatios {
		currentRatios[i] = startRatios[i] + (targetRatios[i]-startRatios[i])*float64(intensity)
	}

	// Ensure ratios sum to exactly 1.0 to prevent rounding errors
	// that could cause the bottom-right pane to shrink
	ratioSum := 0.0
	for _, ratio := range currentRatios {
		ratioSum += ratio
	}

	if ratioSum > 0.0 && len(currentRatios) > 0 {
		// Normalize ratios to sum to 1.0
		for i := range currentRatios {
			currentRatios[i] = currentRatios[i] / ratioSum
		}
	}

	// Update the node's split ratios
	node.SplitRatios = currentRatios

	// Trigger layout recalculation
	if screen != nil {
		screen.recalculateLayout()
	}
}

func (le *LayoutEffect) Clone() Effect {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return NewLayoutEffect(le.node, le.screen, le.startRatios, le.targetRatios)
}

func (le *LayoutEffect) GetIntensity() float32 {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.intensity
}

func (le *LayoutEffect) SetIntensity(intensity float32) {
	le.mu.Lock()
	defer le.mu.Unlock()
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	le.intensity = intensity
}

func (le *LayoutEffect) IsAnimating() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.intensity > 0.0 && le.intensity < 1.0
}
