//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"
	"time"

	"texelation/apps/welcome"
)

// TestTViewResizeNeverShowsEmptyFrames verifies that during resize,
// we never show empty frames. This simulates the constant resize
// calls the user is seeing in production.
func TestTViewResizeNeverShowsEmptyFrames(t *testing.T) {
	// Create the welcome app
	app := welcome.NewStaticTView()

	// Initial size
	app.Resize(80, 24)

	// Run it to initialize
	if err := app.Run(); err != nil {
		t.Fatalf("App.Run() returned error: %v", err)
	}

	// Wait for initial frame
	time.Sleep(100 * time.Millisecond)

	// Verify initial content
	buffer := app.Render()
	if len(buffer) == 0 || len(buffer) != 24 {
		t.Fatalf("Initial buffer wrong size: got %d rows, want 24", len(buffer))
	}

	// Count initial content
	initialCount := 0
	for y := 0; y < len(buffer); y++ {
		for x := 0; x < len(buffer[y]); x++ {
			if buffer[y][x].Ch != ' ' && buffer[y][x].Ch != rune(0) {
				initialCount++
			}
		}
	}
	t.Logf("Initial buffer: %d non-space characters", initialCount)

	if initialCount < 100 {
		t.Fatalf("Initial buffer appears empty: only %d characters", initialCount)
	}

	// Now simulate constant resize (what the user is experiencing)
	// Resize multiple times and check that we NEVER see empty frames
	sizes := []struct{ w, h int }{
		{100, 30},
		{120, 40},
		{80, 24},
		{90, 25},
		{110, 35},
	}

	for i, size := range sizes {
		t.Logf("Resize iteration %d: changing to %dx%d", i, size.w, size.h)

		// Resize
		app.Resize(size.w, size.h)

		// Immediately render (this is what happens in production)
		buffer := app.Render()

		// The buffer might still be the old size until tview redraws
		// But it should NEVER be empty - should show old content
		if len(buffer) == 0 {
			t.Fatalf("Iteration %d: Buffer is empty after resize!", i)
		}

		// Count content
		nonSpaceCount := 0
		for y := 0; y < len(buffer); y++ {
			for x := 0; x < len(buffer[y]); x++ {
				if buffer[y][x].Ch != ' ' && buffer[y][x].Ch != rune(0) {
					nonSpaceCount++
				}
			}
		}

		t.Logf("Iteration %d: Immediately after resize: %d non-space characters", i, nonSpaceCount)

		// We should ALWAYS have content, even immediately after resize
		// Should show old content until tview finishes drawing new size
		if nonSpaceCount < 100 {
			t.Errorf("Iteration %d: Buffer appears empty immediately after resize (only %d characters)", i, nonSpaceCount)
		}

		// Give tview time to redraw at new size
		time.Sleep(50 * time.Millisecond)

		// Render again - should now have new size
		buffer = app.Render()

		nonSpaceCount = 0
		for y := 0; y < len(buffer); y++ {
			for x := 0; x < len(buffer[y]); x++ {
				if buffer[y][x].Ch != ' ' && buffer[y][x].Ch != rune(0) {
					nonSpaceCount++
				}
			}
		}

		t.Logf("Iteration %d: After redraw: %dx%d, %d non-space characters", i, len(buffer[0]), len(buffer), nonSpaceCount)

		// Should still have content
		if nonSpaceCount < 100 {
			t.Errorf("Iteration %d: Buffer appears empty after redraw (only %d characters)", i, nonSpaceCount)
		}
	}

	// Stop the app
	app.Stop()

	t.Logf("Test passed: Never saw empty frames during %d resize operations", len(sizes))
}
