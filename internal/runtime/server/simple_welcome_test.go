//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	"texelation/apps/welcome"
)

// TestSimpleColoredWelcomeRendersContent verifies that the simple colored welcome
// screen renders content properly without blocking or flickering.
func TestSimpleColoredWelcomeRendersContent(t *testing.T) {
	// Create the welcome app
	app := welcome.NewSimpleColored()

	// Resize it to reasonable dimensions
	app.Resize(80, 24)

	// Run it to initialize (should not block)
	if err := app.Run(); err != nil {
		t.Fatalf("App.Run() returned error: %v", err)
	}

	// Render the buffer
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

	// We expect at least some content
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

	// Verify Run() returned immediately (no blocking)
	// If we got here, it didn't block

	// Test multiple Render() calls return same buffer (caching)
	buffer2 := app.Render()
	if len(buffer2) != len(buffer) {
		t.Error("Second Render() returned different buffer size")
	}

	// Stop the app
	app.Stop()

	t.Logf("Simple colored welcome screen test passed: buffer has content, no blocking")
}
