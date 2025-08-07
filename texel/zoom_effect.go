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
	return &ZoomEffect{
		screen:     screen,
		node:       node,
		startRect:  start,
		endRect:    end,
		startTime:  time.Now(),
		duration:   duration,
		onComplete: onComplete,
	}
}

// Apply calculates and applies the new dimensions for the pane being zoomed.
func (e *ZoomEffect) Apply(buffer *[][]Cell) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	elapsed := time.Since(e.startTime)
	progress := float64(elapsed) / float64(e.duration)

	if progress >= 1.0 {
		progress = 1.0
		if e.onComplete != nil && !e.isCompleting {
			e.isCompleting = true
			e.onComplete()
		}
	}

	// Smooth easing
	p := progress * progress * (3.0 - 2.0*progress)

	currX := int(float64(e.startRect.x) + float64(e.endRect.x-e.startRect.x)*p)
	currY := int(float64(e.startRect.y) + float64(e.endRect.y-e.startRect.y)*p)
	currW := int(float64(e.startRect.w) + float64(e.endRect.w-e.startRect.w)*p)
	currH := int(float64(e.startRect.h) + float64(e.endRect.h-e.startRect.h)*p)

	if e.node != nil && e.node.Pane != nil {
		e.node.Pane.setDimensions(currX, currY, currX+currW, currY+currH)
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
