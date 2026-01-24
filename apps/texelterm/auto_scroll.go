// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/auto_scroll.go
// Summary: Edge-based auto-scrolling during selection operations.

package texelterm

import (
	"sync"
	"time"
)

// AutoScrollConfig provides configuration values for auto-scroll behavior.
// This is injected rather than read from global config, enabling testability.
type AutoScrollConfig struct {
	// EdgeZone is the number of rows from the edge that trigger auto-scroll.
	// Default: 2
	EdgeZone int
	// MaxScrollSpeed is the maximum scroll speed in lines per second.
	// Default: 15
	MaxScrollSpeed int
}

// AutoScrollManager handles edge-based auto-scrolling during selection.
// When the mouse is near the top or bottom edge of the terminal during a selection,
// it automatically scrolls the view to allow selecting content outside the viewport.
type AutoScrollManager struct {
	mu          sync.Mutex
	active      bool
	stopChan    chan struct{}
	mouseX      int
	mouseY      int
	height      int
	config      AutoScrollConfig
	wg          sync.WaitGroup
	onScroll    func(lines int)                  // Callback when scroll occurs
	onRefresh   func()                           // Callback to request refresh
	onPosUpdate func(x, y int) (int64, int, int) // Callback to resolve selection position (logicalLine, charOffset, viewportRow)
}

// NewAutoScrollManager creates a new auto-scroll manager with the given configuration.
// If config values are zero/invalid, defaults are applied (EdgeZone=2, MaxScrollSpeed=15).
func NewAutoScrollManager(config AutoScrollConfig) *AutoScrollManager {
	// Apply defaults for zero/invalid values
	if config.EdgeZone <= 0 {
		config.EdgeZone = 2
	}
	if config.MaxScrollSpeed <= 0 {
		config.MaxScrollSpeed = 15
	}
	return &AutoScrollManager{
		config: config,
	}
}

// SetCallbacks configures the callbacks for scroll events.
func (a *AutoScrollManager) SetCallbacks(
	onScroll func(lines int),
	onRefresh func(),
	onPosUpdate func(x, y int) (int64, int, int),
) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onScroll = onScroll
	a.onRefresh = onRefresh
	a.onPosUpdate = onPosUpdate
}

// SetSize updates the terminal height for edge zone calculations.
func (a *AutoScrollManager) SetSize(height int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.height = height
}

// UpdatePosition updates the mouse position for scroll calculations.
func (a *AutoScrollManager) UpdatePosition(mouseX, mouseY int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mouseX = mouseX
	a.mouseY = mouseY
}

// GetPosition returns the current tracked mouse position.
func (a *AutoScrollManager) GetPosition() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mouseX, a.mouseY
}

// ShouldAutoScroll checks if mouse is in the edge zone requiring auto-scroll.
func (a *AutoScrollManager) ShouldAutoScroll(mouseY, height int) bool {
	edgeZone := a.config.EdgeZone
	nearTop := mouseY < edgeZone
	nearBottom := mouseY >= height-edgeZone
	return nearTop || nearBottom
}

// Start begins the auto-scroll loop if not already running.
func (a *AutoScrollManager) Start() {
	a.mu.Lock()
	if a.active {
		a.mu.Unlock()
		return
	}
	a.active = true
	a.stopChan = make(chan struct{})
	a.wg.Add(1)
	a.mu.Unlock()

	go a.scrollLoop()
}

// Stop terminates the auto-scroll loop.
func (a *AutoScrollManager) Stop() {
	a.mu.Lock()
	if !a.active {
		a.mu.Unlock()
		return
	}
	a.active = false
	close(a.stopChan)
	a.mu.Unlock()

	a.wg.Wait()
}

// IsActive returns whether auto-scroll is currently running.
func (a *AutoScrollManager) IsActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active
}

// scrollLoop runs the auto-scroll goroutine.
func (a *AutoScrollManager) scrollLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	a.mu.Lock()
	stopChan := a.stopChan
	a.mu.Unlock()

	var accumulator float64
	startTime := time.Now()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			scrollLines := a.calculateScroll(&accumulator, startTime)
			if scrollLines != 0 {
				a.performScroll(scrollLines)
			}
		}
	}
}

// calculateScroll determines how many lines to scroll based on mouse position.
func (a *AutoScrollManager) calculateScroll(accumulator *float64, startTime time.Time) int {
	a.mu.Lock()
	mouseY := a.mouseY
	height := a.height
	edgeZone := a.config.EdgeZone
	maxSpeed := a.config.MaxScrollSpeed
	a.mu.Unlock()

	if height == 0 {
		return 0
	}

	// Calculate scroll speed based on distance from edge and elapsed time
	var speedLinesPerSec float64

	// Ramp up speed over time (max 3 seconds for full multiplier)
	elapsed := time.Since(startTime).Seconds()
	timeMultiplier := 1.0 + (elapsed * 2.0) // 1x -> 7x over 3s
	if timeMultiplier > 8.0 {
		timeMultiplier = 8.0
	}

	if mouseY < edgeZone {
		// Near top - scroll up (negative)
		distance := float64(edgeZone - mouseY)
		speedLinesPerSec = -(distance * float64(maxSpeed) / float64(edgeZone))
	} else if mouseY >= height-edgeZone {
		// Near bottom - scroll down (positive)
		distance := float64(mouseY - (height - edgeZone) + 1)
		speedLinesPerSec = distance * float64(maxSpeed) / float64(edgeZone)
	} else {
		// Not in edge zone
		*accumulator = 0
		return 0
	}

	// Apply time multiplier
	speedLinesPerSec *= timeMultiplier

	// Convert lines/sec to lines/tick (50ms = 20 ticks/sec)
	*accumulator += speedLinesPerSec / 20.0

	var scrollLines int
	if *accumulator >= 1.0 || *accumulator <= -1.0 {
		scrollLines = int(*accumulator)
		*accumulator -= float64(scrollLines)
	}

	return scrollLines
}

// performScroll executes the scroll, updates selection position, and notifies listeners.
func (a *AutoScrollManager) performScroll(lines int) {
	a.mu.Lock()
	onScroll := a.onScroll
	onRefresh := a.onRefresh
	onPosUpdate := a.onPosUpdate
	mouseX := a.mouseX
	mouseY := a.mouseY
	a.mu.Unlock()

	// Scroll the viewport
	if onScroll != nil {
		onScroll(lines)
	}

	// Update selection position after scroll - this extends the selection
	// as content scrolls under the stationary mouse cursor
	if onPosUpdate != nil {
		onPosUpdate(mouseX, mouseY)
	}

	if onRefresh != nil {
		onRefresh()
	}
}
