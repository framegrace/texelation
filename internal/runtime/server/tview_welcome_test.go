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

// TestTViewWelcomeRendersContent verifies that the tview welcome screen
// actually renders content with background thread approach.
func TestTViewWelcomeRendersContent(t *testing.T) {
	// Create the welcome app
	app := welcome.NewStaticTView()

	// Resize it to reasonable dimensions
	app.Resize(80, 24)

	// Run it to initialize (now non-blocking, starts tview in background)
	if err := app.Run(); err != nil {
		t.Fatalf("App.Run() returned error: %v", err)
	}

	// Give tview background thread a moment to render
	time.Sleep(100 * time.Millisecond)

	// Render the buffer (reads from VirtualScreen)
	buffer := app.Render()

	// Verify buffer dimensions
	if len(buffer) == 0 {
		t.Fatal("Buffer is empty (0 rows)")
	}

	if len(buffer) != 24 {
		t.Errorf("Expected 24 rows, got %d", len(buffer))
	}

	if len(buffer[0]) != 80 {
		t.Errorf("Expected 80 columns, got %d", len(buffer[0]))
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

	t.Logf("Buffer: %dx%d, non-space characters: %d", len(buffer[0]), len(buffer), nonSpaceCount)

	// We expect at least some content (borders, title, text)
	// A completely empty buffer would have 0 non-space chars
	if nonSpaceCount == 0 {
		t.Fatal("Buffer appears to be completely empty (no non-space characters)")
	}

	// Look for specific content
	foundWelcome := false
	foundTexelation := false

	for y := 0; y < len(buffer); y++ {
		rowText := ""
		for x := 0; x < len(buffer[y]); x++ {
			rowText += string(buffer[y][x].Ch)
		}

		if contains(rowText, "Welcome") {
			foundWelcome = true
			t.Logf("Found 'Welcome' in row %d: %s", y, rowText)
		}
		if contains(rowText, "Texelation") {
			foundTexelation = true
			t.Logf("Found 'Texelation' in row %d: %s", y, rowText)
		}
	}

	if !foundWelcome {
		t.Error("Did not find 'Welcome' text in buffer")
	}

	if !foundTexelation {
		t.Error("Did not find 'Texelation' text in buffer")
	}

	// Stop the app
	app.Stop()

	t.Logf("TView welcome screen test passed: buffer has content and expected text")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
