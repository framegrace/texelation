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

// TestDirectTerminalEcho creates a TexelTerm directly to inspect VTerm
func TestDirectTerminalEcho(t *testing.T) {
	// Use the app but log more detailed info
	app := texelterm.New("texelterm", "/bin/bash")
	app.Resize(80, 24)
	app.SetRefreshNotifier(make(chan bool, 100))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for shell to start
	time.Sleep(800 * time.Millisecond)

	// Test both builtin and external command
	t.Log("=== Testing bash builtin (printf) ===")

	// Simulate typing: printf "HELLO"
	commands := []rune("printf 'HELLO'\n")
	for _, ch := range commands {
		ev := tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone)
		app.HandleKey(ev)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for output
	time.Sleep(800 * time.Millisecond)

	// Render
	buf := app.Render()
	foundPrintf := false
	for y := 0; y < min(10, len(buf)); y++ {
		var line strings.Builder
		for x := 0; x < len(buf[y]); x++ {
			ch := buf[y][x].Ch
			if ch == 0 {
				line.WriteRune(' ')
			} else {
				line.WriteRune(ch)
			}
		}
		lineStr := line.String()
		if strings.TrimSpace(lineStr) != "" {
			t.Logf("After printf - Row %2d: %q", y, lineStr)
		}
		if strings.Contains(lineStr, "HELLO") && !strings.Contains(lineStr, "printf") {
			foundPrintf = true
		}
	}

	t.Log("=== Testing external command (ls /tmp) ===")

	// Simulate typing: ls /tmp | head -1
	commands2 := []rune("ls /tmp | head -1\n")
	for _, ch := range commands2 {
		ev := tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone)
		app.HandleKey(ev)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for output
	time.Sleep(800 * time.Millisecond)

	// Render
	buf = app.Render()
	foundLs := false
	for y := 0; y < min(10, len(buf)); y++ {
		var line strings.Builder
		for x := 0; x < len(buf[y]); x++ {
			ch := buf[y][x].Ch
			if ch == 0 {
				line.WriteRune(' ')
			} else {
				line.WriteRune(ch)
			}
		}
		lineStr := line.String()
		if strings.TrimSpace(lineStr) != "" {
			t.Logf("After ls - Row %2d: %q", y, lineStr)
		}
		// ls should produce some output (look for anything that's not the command or prompt)
		if !strings.Contains(lineStr, "ls /tmp") && !strings.Contains(lineStr, "❯") &&
		   !strings.Contains(lineStr, "testArea") && strings.TrimSpace(lineStr) != "" {
			foundLs = true
			t.Logf("Found ls output: %q", lineStr)
		}
	}

	app.Stop()
	<-errCh

	t.Logf("Results: printf output found=%v, ls output found=%v", foundPrintf, foundLs)

	if !foundPrintf {
		t.Errorf("Expected to find 'HELLO' output from printf (builtin)")
	}
	if !foundLs {
		t.Errorf("Expected to find output from ls (external command)")
	}
}
