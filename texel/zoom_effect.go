// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/zoom_effect.go
// Summary: Implements zoom effect capabilities for the core desktop engine.
// Usage: Used throughout the project to implement zoom effect inside the desktop and panes.
// Notes: Legacy desktop logic migrated from the monolithic application.

package texel

import (
	"log"
	"sync"
	"time"
)

// ZoomEffect animates the dimensions of a single pane.
type ZoomEffect struct {
	mu           sync.RWMutex
	intensity    float32
	startTime    time.Time
	duration     time.Duration
	startRect    PaneRect
	endRect      PaneRect
	node         *Node
	screen       *Screen
	onComplete   func()
	isCompleting bool
}

// NewZoomEffect creates a new zoom effect.
func NewZoomEffect(screen *Screen, node *Node, start, end PaneRect, duration time.Duration, onComplete func()) *ZoomEffect {
	effect := &ZoomEffect{
		screen:     screen,
		node:       node,
		startRect:  start,
		endRect:    end,
		startTime:  time.Now(),
		duration:   duration,
		onComplete: onComplete,
	}
	
	// Set the pane to render on top during the zoom animation
	if node != nil && node.Pane != nil {
		node.Pane.SetZOrder(ZOrderAnimation) // High z-order to ensure it's on top
		log.Printf("ZoomEffect: Set pane '%s' z-order to %d for zoom animation", node.Pane.getTitle(), ZOrderAnimation)
	}
	
	return effect
}

// Apply calculates and applies the new dimensions for the pane being zoomed.
func (e *ZoomEffect) Apply(buffer *[][]Cell) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	elapsed := time.Since(e.startTime)
	progress := float64(elapsed) / float64(e.duration)

	if progress >= 1.0 {
		progress = 1.0
		if !e.isCompleting {
			e.isCompleting = true
			if e.onComplete != nil {
				e.onComplete()
			}
			// DON'T reset z-order here - it will be reset when effect is removed
		}
	}

	// Smooth easing
	p := progress * progress * (3.0 - 2.0*progress)

	// Use more precise calculations and explicit rounding to avoid sub-pixel errors
	currX := int(float64(e.startRect.x) + float64(e.endRect.x-e.startRect.x)*p + 0.5)
	currY := int(float64(e.startRect.y) + float64(e.endRect.y-e.startRect.y)*p + 0.5)
	currW := int(float64(e.startRect.w) + float64(e.endRect.w-e.startRect.w)*p + 0.5)
	currH := int(float64(e.startRect.h) + float64(e.endRect.h-e.startRect.h)*p + 0.5)

	// Ensure minimum dimensions
	if currW < 1 {
		currW = 1
	}
	if currH < 1 {
		currH = 1
	}

	if e.node != nil && e.node.Pane != nil {
		e.node.Pane.setDimensions(currX, currY, currX+currW, currY+currH)
		log.Printf("ZoomEffect.Apply: Pane '%s' animated to (%d,%d) size %dx%d", 
			e.node.Pane.getTitle(), currX, currY, currW, currH)
	}
}

// Clone creates a new instance of the zoom effect.
func (e *ZoomEffect) Clone() Effect {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return NewZoomEffect(e.screen, e.node, e.startRect, e.endRect, e.duration, e.onComplete)
}

// GetIntensity returns the current animation intensity.
func (e *ZoomEffect) GetIntensity() float32 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.intensity
}

// SetIntensity sets the animation intensity.
func (e *ZoomEffect) SetIntensity(intensity float32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	e.intensity = intensity
	log.Printf("ZoomEffect intensity set to: %.2f", e.intensity)
}

// IsAnimating returns true if the effect is currently animating.
func (e *ZoomEffect) IsAnimating() bool {
	return time.Since(e.startTime) < e.duration
}

// Cleanup resets the z-order when the effect is removed
func (e *ZoomEffect) Cleanup() {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.node != nil && e.node.Pane != nil {
		e.node.Pane.SetZOrder(ZOrderDefault) // Reset to default z-order
		log.Printf("ZoomEffect.Cleanup: Reset pane '%s' z-order to %d", e.node.Pane.getTitle(), ZOrderDefault)
	}
}

