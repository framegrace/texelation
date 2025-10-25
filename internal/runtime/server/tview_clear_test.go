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

// TestTViewNeverShowsEmptyFrames verifies that calling Render() never returns
// empty or partial frames, even when tview calls Clear() followed by Show()
// before drawing content.
func TestTViewNeverShowsEmptyFrames(t *testing.T) {
	// Create the welcome app
	app := welcome.NewStaticTView()

	// Resize it to reasonable dimensions
	app.Resize(80, 24)

	// Run it to initialize (starts tview in background)
	if err := app.Run(); err != nil {
		t.Fatalf("App.Run() returned error: %v", err)
	}

	// Give tview time to render initial frame
	time.Sleep(100 * time.Millisecond)

	// Now repeatedly call Render() and check that we NEVER get empty frames
	// This simulates what happens when the desktop calls Render() at various
	// points during tview's Clear() → Draw → Show() cycle
	for i := 0; i < 20; i++ {
		buffer := app.Render()

		// Verify buffer dimensions
		if len(buffer) == 0 {
			t.Fatalf("Iteration %d: Buffer is empty (0 rows)", i)
		}

		if len(buffer) != 24 {
			t.Errorf("Iteration %d: Expected 24 rows, got %d", i, len(buffer))
		}

		if len(buffer[0]) != 80 {
			t.Errorf("Iteration %d: Expected 80 columns, got %d", i, len(buffer[0]))
		}

		// Count non-space characters
		nonSpaceCount := 0
		for y := 0; y < len(buffer); y++ {
			for x := 0; x < len(buffer[y]); x++ {
				if buffer[y][x].Ch != ' ' && buffer[y][x].Ch != rune(0) {
					nonSpaceCount++
				}
			}
		}

		t.Logf("Iteration %d: Buffer has %d non-space characters", i, nonSpaceCount)

		// We should ALWAYS have content (borders, title, text)
		// If we get less than 100 characters, we probably caught an empty frame
		if nonSpaceCount < 100 {
			t.Errorf("Iteration %d: Buffer appears mostly empty (only %d non-space characters)", i, nonSpaceCount)

			// Print first few rows to see what we got
			for y := 0; y < 5 && y < len(buffer); y++ {
				rowText := ""
				for x := 0; x < len(buffer[y]); x++ {
					rowText += string(buffer[y][x].Ch)
				}
				t.Logf("Row %d: %s", y, rowText)
			}
		}

		// Look for expected content
		foundWelcome := false
		for y := 0; y < len(buffer); y++ {
			rowText := ""
			for x := 0; x < len(buffer[y]); x++ {
				rowText += string(buffer[y][x].Ch)
			}
			if contains(rowText, "Welcome") {
				foundWelcome = true
				break
			}
		}

		if !foundWelcome {
			t.Errorf("Iteration %d: Did not find 'Welcome' text in buffer", i)
		}

		// Small delay between renders
		time.Sleep(10 * time.Millisecond)
	}

	// Stop the app
	app.Stop()

	t.Logf("Test passed: Never saw empty or partial frames across 20 render calls")
}
