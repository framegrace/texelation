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

// TestSimpleShellEcho uses /bin/sh which has simpler prompts
func TestSimpleShellEcho(t *testing.T) {
	app := texelterm.New("texelterm", "/bin/sh")
	app.Resize(80, 24)
	app.SetRefreshNotifier(make(chan bool, 100))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for shell to start
	time.Sleep(500 * time.Millisecond)

	// Simulate typing: echo TESTOUTPUT
	commands := []rune("echo TESTOUTPUT\n")
	for _, ch := range commands {
		ev := tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone)
		app.HandleKey(ev)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for output
	time.Sleep(500 * time.Millisecond)

	// Render
	buf := app.Render()

	// Dump all content
	t.Log("=== Full buffer dump with /bin/sh ===")
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
			t.Logf("Row %2d: %q", y, lineStr)
		}
	}

	// Look for TESTOUTPUT (not in the command line)
	found := false
	for y := 0; y < len(buf); y++ {
		var line strings.Builder
		for x := 0; x < len(buf[y]); x++ {
			line.WriteRune(buf[y][x].Ch)
		}
		lineStr := line.String()
		if strings.Contains(lineStr, "TESTOUTPUT") && !strings.Contains(lineStr, "echo") {
			found = true
			t.Logf("Found TESTOUTPUT output on row %d: %q", y, lineStr)
			break
		}
	}

	app.Stop()
	<-errCh

	if !found {
		t.Errorf("Expected to find 'TESTOUTPUT' output (not in command line)")
	}
}
