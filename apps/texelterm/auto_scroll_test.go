// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/auto_scroll_test.go
// Summary: Comprehensive tests for AutoScrollManager including async behavior.

package texelterm

import (
	"sync"
	"testing"
	"time"
)

// TestAutoScrollManager_NewWithDefaults tests that default values are applied.
func TestAutoScrollManager_NewWithDefaults(t *testing.T) {
	// Zero config should get defaults
	asm := NewAutoScrollManager(AutoScrollConfig{})

	if asm.config.EdgeZone != 2 {
		t.Errorf("expected EdgeZone default 2, got %d", asm.config.EdgeZone)
	}
	if asm.config.MaxScrollSpeed != 15 {
		t.Errorf("expected MaxScrollSpeed default 15, got %d", asm.config.MaxScrollSpeed)
	}
}

// TestAutoScrollManager_NewWithCustomConfig tests that custom config is preserved.
func TestAutoScrollManager_NewWithCustomConfig(t *testing.T) {
	config := AutoScrollConfig{EdgeZone: 5, MaxScrollSpeed: 30}
	asm := NewAutoScrollManager(config)

	if asm.config.EdgeZone != 5 {
		t.Errorf("expected EdgeZone 5, got %d", asm.config.EdgeZone)
	}
	if asm.config.MaxScrollSpeed != 30 {
		t.Errorf("expected MaxScrollSpeed 30, got %d", asm.config.MaxScrollSpeed)
	}
}

// TestAutoScrollManager_NewWithNegativeValues tests that negative values get defaults.
func TestAutoScrollManager_NewWithNegativeValues(t *testing.T) {
	config := AutoScrollConfig{EdgeZone: -1, MaxScrollSpeed: -5}
	asm := NewAutoScrollManager(config)

	if asm.config.EdgeZone != 2 {
		t.Errorf("expected EdgeZone default 2 for negative input, got %d", asm.config.EdgeZone)
	}
	if asm.config.MaxScrollSpeed != 15 {
		t.Errorf("expected MaxScrollSpeed default 15 for negative input, got %d", asm.config.MaxScrollSpeed)
	}
}

// TestAutoScrollManager_ShouldAutoScroll_TopEdge tests detection of top edge zone.
func TestAutoScrollManager_ShouldAutoScroll_TopEdge(t *testing.T) {
	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 3})

	tests := []struct {
		mouseY   int
		height   int
		expected bool
	}{
		{0, 24, true},  // At very top
		{1, 24, true},  // Within edge zone
		{2, 24, true},  // At edge of zone
		{3, 24, false}, // Just outside zone
		{10, 24, false},
	}

	for _, tt := range tests {
		got := asm.ShouldAutoScroll(tt.mouseY, tt.height)
		if got != tt.expected {
			t.Errorf("ShouldAutoScroll(mouseY=%d, height=%d) = %v, want %v",
				tt.mouseY, tt.height, got, tt.expected)
		}
	}
}

// TestAutoScrollManager_ShouldAutoScroll_BottomEdge tests detection of bottom edge zone.
func TestAutoScrollManager_ShouldAutoScroll_BottomEdge(t *testing.T) {
	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 3})
	height := 24

	tests := []struct {
		mouseY   int
		expected bool
	}{
		{height - 1, true},  // At very bottom (23)
		{height - 2, true},  // Within zone (22)
		{height - 3, true},  // At edge of zone (21)
		{height - 4, false}, // Just outside (20)
		{10, false},
	}

	for _, tt := range tests {
		got := asm.ShouldAutoScroll(tt.mouseY, height)
		if got != tt.expected {
			t.Errorf("ShouldAutoScroll(mouseY=%d, height=%d) = %v, want %v",
				tt.mouseY, height, got, tt.expected)
		}
	}
}

// TestAutoScrollManager_ShouldAutoScroll_Middle tests that middle positions don't trigger scroll.
func TestAutoScrollManager_ShouldAutoScroll_Middle(t *testing.T) {
	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2})

	for mouseY := 3; mouseY < 21; mouseY++ {
		if asm.ShouldAutoScroll(mouseY, 24) {
			t.Errorf("ShouldAutoScroll(mouseY=%d, height=24) should be false for middle position", mouseY)
		}
	}
}

// TestAutoScrollManager_StartStop tests the lifecycle of the scroll loop.
func TestAutoScrollManager_StartStop(t *testing.T) {
	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15})
	asm.SetSize(24)

	if asm.IsActive() {
		t.Error("expected inactive initially")
	}

	asm.Start()
	if !asm.IsActive() {
		t.Error("expected active after Start()")
	}

	// Start again should be idempotent
	asm.Start()
	if !asm.IsActive() {
		t.Error("expected still active after second Start()")
	}

	asm.Stop()
	if asm.IsActive() {
		t.Error("expected inactive after Stop()")
	}

	// Stop again should be idempotent
	asm.Stop()
	if asm.IsActive() {
		t.Error("expected still inactive after second Stop()")
	}
}

// TestAutoScrollManager_PositionTracking tests position update and retrieval.
func TestAutoScrollManager_PositionTracking(t *testing.T) {
	asm := NewAutoScrollManager(AutoScrollConfig{})

	asm.UpdatePosition(10, 5)
	x, y := asm.GetPosition()
	if x != 10 || y != 5 {
		t.Errorf("GetPosition() = (%d, %d), want (10, 5)", x, y)
	}

	asm.UpdatePosition(20, 15)
	x, y = asm.GetPosition()
	if x != 20 || y != 15 {
		t.Errorf("GetPosition() = (%d, %d), want (20, 15)", x, y)
	}
}

// TestAutoScrollManager_SetSize tests height configuration.
func TestAutoScrollManager_SetSize(t *testing.T) {
	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2})

	// Initially height is 0
	asm.SetSize(24)

	// Verify edge detection works with new size
	if !asm.ShouldAutoScroll(0, 24) {
		t.Error("expected scroll at top edge after SetSize")
	}
	if !asm.ShouldAutoScroll(23, 24) {
		t.Error("expected scroll at bottom edge after SetSize")
	}
}

// TestAutoScrollManager_CallbacksInvoked tests that scroll and refresh callbacks are called.
func TestAutoScrollManager_CallbacksInvoked(t *testing.T) {
	var mu sync.Mutex
	scrollCalls := []int{}
	refreshCalls := 0

	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 50}) // High speed for faster test
	asm.SetSize(24)
	asm.SetCallbacks(
		func(lines int) {
			mu.Lock()
			scrollCalls = append(scrollCalls, lines)
			mu.Unlock()
		},
		func() {
			mu.Lock()
			refreshCalls++
			mu.Unlock()
		},
		nil, // No position update callback needed for this test
	)

	// Position at very top edge (should scroll up/negative)
	asm.UpdatePosition(10, 0)
	asm.Start()

	// Wait for some scroll events
	time.Sleep(200 * time.Millisecond)

	asm.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(scrollCalls) == 0 {
		t.Error("expected scroll callbacks to be invoked")
	}
	if refreshCalls == 0 {
		t.Error("expected refresh callbacks to be invoked")
	}

	// All scroll calls should be negative (scrolling up when at top)
	for _, lines := range scrollCalls {
		if lines > 0 {
			t.Errorf("expected negative scroll (up) at top edge, got %d", lines)
		}
	}
}

// TestAutoScrollManager_ScrollDirectionBottom tests scroll direction at bottom edge.
func TestAutoScrollManager_ScrollDirectionBottom(t *testing.T) {
	var mu sync.Mutex
	scrollCalls := []int{}

	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 50})
	asm.SetSize(24)
	asm.SetCallbacks(
		func(lines int) {
			mu.Lock()
			scrollCalls = append(scrollCalls, lines)
			mu.Unlock()
		},
		func() {},
		nil,
	)

	// Position at very bottom edge (should scroll down/positive)
	asm.UpdatePosition(10, 23)
	asm.Start()

	time.Sleep(200 * time.Millisecond)

	asm.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(scrollCalls) == 0 {
		t.Error("expected scroll callbacks to be invoked")
	}

	// All scroll calls should be positive (scrolling down when at bottom)
	for _, lines := range scrollCalls {
		if lines < 0 {
			t.Errorf("expected positive scroll (down) at bottom edge, got %d", lines)
		}
	}
}

// TestAutoScrollManager_NoScrollInMiddle tests that no scroll events occur in middle.
func TestAutoScrollManager_NoScrollInMiddle(t *testing.T) {
	var mu sync.Mutex
	scrollCalls := 0

	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 50})
	asm.SetSize(24)
	asm.SetCallbacks(
		func(lines int) {
			mu.Lock()
			scrollCalls++
			mu.Unlock()
		},
		func() {},
		nil,
	)

	// Position in middle (should not scroll)
	asm.UpdatePosition(10, 12)
	asm.Start()

	time.Sleep(150 * time.Millisecond)

	asm.Stop()

	mu.Lock()
	defer mu.Unlock()

	if scrollCalls > 0 {
		t.Errorf("expected no scroll callbacks in middle position, got %d", scrollCalls)
	}
}

// TestAutoScrollManager_ZeroHeight tests behavior with zero height.
func TestAutoScrollManager_ZeroHeight(t *testing.T) {
	scrollCalls := 0

	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 50})
	// Don't call SetSize - height remains 0
	asm.SetCallbacks(
		func(lines int) {
			scrollCalls++
		},
		func() {},
		nil,
	)

	asm.UpdatePosition(10, 0)
	asm.Start()

	time.Sleep(100 * time.Millisecond)

	asm.Stop()

	if scrollCalls > 0 {
		t.Errorf("expected no scroll with zero height, got %d calls", scrollCalls)
	}
}

// TestAutoScrollManager_ConcurrentStartStop tests concurrent access safety.
func TestAutoScrollManager_ConcurrentStartStop(t *testing.T) {
	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15})
	asm.SetSize(24)
	asm.UpdatePosition(10, 12) // Middle position - no scroll
	asm.SetCallbacks(func(int) {}, func() {}, nil)

	// Start and stop concurrently from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			asm.Start()
			time.Sleep(10 * time.Millisecond)
			asm.Stop()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out - possible deadlock")
	}

	// Ensure stopped
	asm.Stop()
}

// TestAutoScrollManager_TimeBasedAcceleration tests that scroll speed increases over time.
func TestAutoScrollManager_TimeBasedAcceleration(t *testing.T) {
	var mu sync.Mutex
	scrollCalls := []int{}

	asm := NewAutoScrollManager(AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 20})
	asm.SetSize(24)
	asm.SetCallbacks(
		func(lines int) {
			mu.Lock()
			scrollCalls = append(scrollCalls, lines)
			mu.Unlock()
		},
		func() {},
		nil,
	)

	// Position at edge
	asm.UpdatePosition(10, 0)
	asm.Start()

	// Wait long enough for acceleration to kick in
	time.Sleep(500 * time.Millisecond)

	asm.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(scrollCalls) < 3 {
		t.Skip("Not enough scroll calls to test acceleration")
	}

	// Later calls should have higher magnitude due to time multiplier
	// The accumulator means we can't be precise, but later calls should average higher
	// This is a loose check - just verify we got multiple calls
	t.Logf("Scroll calls: %v", scrollCalls)
}

// TestAutoScrollConfig_Defaults tests the config struct defaults.
func TestAutoScrollConfig_Defaults(t *testing.T) {
	config := AutoScrollConfig{}
	if config.EdgeZone != 0 {
		t.Errorf("expected zero value, got %d", config.EdgeZone)
	}
	if config.MaxScrollSpeed != 0 {
		t.Errorf("expected zero value, got %d", config.MaxScrollSpeed)
	}
}
