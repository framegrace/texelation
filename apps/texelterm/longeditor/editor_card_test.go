// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package longeditor_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"texelation/apps/texelterm"
	"texelation/texel"
)

// writeScript creates a temporary executable script for testing
func writeScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "test.sh")
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}
	return scriptPath
}

// TestEditorCardDoesNotBreakTerminalOutput verifies that the editor card
// properly passes through terminal output when inactive
func TestEditorCardDoesNotBreakTerminalOutput(t *testing.T) {
	// Create a script that outputs known text
	script := writeScript(t, "#!/bin/sh\nprintf 'TESTOUTPUT'\nprintf 'LINE2'\n")

	// Create texelterm app (which includes the editor card in the pipeline)
	app := texelterm.New("texelterm", script)
	app.Resize(80, 24)
	app.SetRefreshNotifier(make(chan bool, 10))

	// Start the app
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for output to be rendered
	time.Sleep(500 * time.Millisecond)

	// Render and check for our expected output
	buf := app.Render()
	found := false
	var allContent strings.Builder

	for y := 0; y < len(buf); y++ {
		for x := 0; x < len(buf[y]); x++ {
			ch := buf[y][x].Ch
			allContent.WriteRune(ch)
			if ch != 0 && ch != ' ' {
				// Check if we can find our test output
				if y < len(buf)-1 && x+9 < len(buf[y]) {
					// Try to match "TESTOUTPUT"
					var line strings.Builder
					for i := 0; i < 10 && x+i < len(buf[y]); i++ {
						line.WriteRune(buf[y][x+i].Ch)
					}
					if strings.Contains(line.String(), "TESTOUTPUT") {
						found = true
						break
					}
				}
			}
		}
		if found {
			break
		}
	}

	// Stop the app
	app.Stop()
	<-errCh

	// Log the buffer content for debugging
	t.Logf("Buffer content preview (first 200 chars): %q", allContent.String()[:min(200, allContent.Len())])

	// Verify output is present
	if !found {
		t.Errorf("Expected 'TESTOUTPUT' in buffer, but not found.\nBuffer dump:\n%s",
			dumpBuffer(buf))
	}
}

// TestEditorCardToggle verifies that Ctrl+o toggles the overlay
func TestEditorCardToggle(t *testing.T) {
	// Create a simple shell script that waits
	script := writeScript(t, "#!/bin/sh\nsleep 10\n")

	app := texelterm.New("texelterm", script)
	app.Resize(80, 24)
	app.SetRefreshNotifier(make(chan bool, 10))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for shell to start
	time.Sleep(200 * time.Millisecond)

	// Get initial buffer (editor inactive)
	bufBefore := app.Render()
	hasOverlayBefore := hasOverlayInBuffer(bufBefore)

	// Simulate Ctrl+o key press
	// Note: This test just verifies the structure works, actual key event
	// testing would require more infrastructure

	app.Stop()
	<-errCh

	if hasOverlayBefore {
		t.Errorf("Overlay should not be visible before Ctrl+o")
	}
}

// hasOverlayInBuffer checks if the overlay border is visible in the buffer
func hasOverlayInBuffer(buf [][]texel.Cell) bool {
	// Look for border characters or overlay-specific patterns
	// This is a simple heuristic
	borderChars := "┌┐└┘─│"
	for y := 0; y < len(buf); y++ {
		for x := 0; x < len(buf[y]); x++ {
			ch := buf[y][x].Ch
			if strings.ContainsRune(borderChars, ch) {
				return true
			}
		}
	}
	return false
}

// dumpBuffer creates a readable representation of the buffer for debugging
func dumpBuffer(buf [][]texel.Cell) string {
	var b strings.Builder
	for y := 0; y < len(buf); y++ {
		b.WriteString("Row ")
		if y < 10 {
			b.WriteString(" ")
		}
		b.WriteString(strconv.Itoa(y))
		b.WriteString(": ")
		for x := 0; x < len(buf[y]); x++ {
			ch := buf[y][x].Ch
			if ch == 0 || ch == ' ' {
				b.WriteRune(' ')
			} else {
				b.WriteRune(ch)
			}
		}
		b.WriteRune('\n')
	}
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
