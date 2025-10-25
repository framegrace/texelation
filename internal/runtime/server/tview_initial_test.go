//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	"texelation/apps/welcome"
)

// TestTViewInitialRenderNotEmpty verifies that the very first Render() call
// after Run() returns a valid buffer (not empty). This tests whether the
// initial frontBuffer is properly initialized.
func TestTViewInitialRenderNotEmpty(t *testing.T) {
	// Create the welcome app
	app := welcome.NewStaticTView()

	// Resize it to reasonable dimensions
	app.Resize(80, 24)

	// Run it to initialize (starts tview in background)
	if err := app.Run(); err != nil {
		t.Fatalf("App.Run() returned error: %v", err)
	}

	// Immediately call Render() without any delay
	// This is what happens when the desktop renders before tview has drawn anything
	buffer := app.Render()

	// Verify buffer dimensions
	if len(buffer) == 0 {
		t.Fatal("Initial buffer is empty (0 rows)")
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

	t.Logf("Initial buffer: %dx%d, non-space characters: %d", len(buffer[0]), len(buffer), nonSpaceCount)

	// Print first few rows to see what we got
	for y := 0; y < 10 && y < len(buffer); y++ {
		rowText := ""
		for x := 0; x < len(buffer[y]) && x < 80; x++ {
			rowText += string(buffer[y][x].Ch)
		}
		if len(rowText) > 0 && rowText != string(make([]rune, len(rowText))) {
			t.Logf("Row %d: %s", y, rowText)
		}
	}

	// The initial buffer might be empty (all spaces) because tview hasn't drawn yet
	// This is actually the root cause of the user's issue!
	if nonSpaceCount == 0 {
		t.Logf("WARNING: Initial buffer is all spaces - this is the bug!")
		t.Logf("The desktop will render this empty buffer until tview draws its first frame")
	}

	// Stop the app
	app.Stop()
}
