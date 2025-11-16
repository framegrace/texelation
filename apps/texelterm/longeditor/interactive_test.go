// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package longeditor_test

import (
	"strings"
	"testing"
	"time"

	"texelation/apps/texelterm"

	"github.com/gdamore/tcell/v2"
)

// TestInteractiveEchoOutput simulates typing commands into an interactive shell
func TestInteractiveEchoOutput(t *testing.T) {
	// Create interactive shell (no script argument)
	app := texelterm.New("texelterm", "/bin/bash")
	app.Resize(80, 24)
	app.SetRefreshNotifier(make(chan bool, 10))

	// Start the app
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for shell to start and show prompt
	time.Sleep(500 * time.Millisecond)

	// Simulate typing: echo "TESTECHO"
	commands := []rune("echo \"TESTECHO\"\n")
	for _, ch := range commands {
		ev := tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone)
		app.HandleKey(ev)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for command to execute and output to appear
	time.Sleep(500 * time.Millisecond)

	// Note: Can't access VTerm directly from Pipeline-wrapped app
	// So we'll rely on the Render() output

	// Render and check for our expected output
	buf := app.Render()
	found := false
	var allContent strings.Builder

	for y := 0; y < len(buf); y++ {
		var line strings.Builder
		for x := 0; x < len(buf[y]); x++ {
			ch := buf[y][x].Ch
			line.WriteRune(ch)
			allContent.WriteRune(ch)
		}
		lineStr := line.String()
		// Check if this line contains TESTECHO but NOT "echo" (i.e., output not input)
		if strings.Contains(lineStr, "TESTECHO") && !strings.Contains(lineStr, "echo") {
			found = true
			t.Logf("Found TESTECHO output on line %d: %q", y, lineStr)
			break
		} else if strings.Contains(lineStr, "TESTECHO") {
			t.Logf("Found TESTECHO in command line %d (not output): %q", y, lineStr)
		}
	}

	// Stop the app
	app.Stop()
	<-errCh

	// Log the buffer content for debugging
	t.Logf("Buffer content preview (first 500 chars): %q", allContent.String()[:min(500, allContent.Len())])

	// Verify output is present
	if !found {
		t.Errorf("Expected 'TESTECHO' in buffer after typing echo command, but not found.\\nBuffer dump:\\n%s",
			dumpBuffer(buf))
	}
}

// TestInteractiveLsOutput simulates typing ls command
func TestInteractiveLsOutput(t *testing.T) {
	// Create interactive shell
	app := texelterm.New("texelterm", "/bin/bash")
	app.Resize(80, 24)
	app.SetRefreshNotifier(make(chan bool, 10))

	// Start the app
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for shell to start
	time.Sleep(500 * time.Millisecond)

	// Simulate typing: ls /tmp
	commands := []rune("ls /tmp\n")
	for _, ch := range commands {
		ev := tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone)
		app.HandleKey(ev)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for command to execute
	time.Sleep(500 * time.Millisecond)

	// Render and check for any non-empty output (ls should produce something)
	buf := app.Render()
	hasOutput := false
	var allContent strings.Builder

	for y := 0; y < len(buf); y++ {
		for x := 0; x < len(buf[y]); x++ {
			ch := buf[y][x].Ch
			allContent.WriteRune(ch)
			// Look for any actual content (not spaces or nulls)
			if ch != 0 && ch != ' ' && ch != '\n' {
				hasOutput = true
			}
		}
	}

	// Stop the app
	app.Stop()
	<-errCh

	t.Logf("Buffer has output: %v", hasOutput)
	t.Logf("Buffer content preview (first 500 chars): %q", allContent.String()[:min(500, allContent.Len())])

	if !hasOutput {
		t.Errorf("Expected ls output in buffer, but buffer appears empty")
	}
}
