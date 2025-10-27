//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/tview_interactive_demo_test.go
// Summary: Integration coverage for the interactive tview demo app.
// Usage: go test -tags=integration ./internal/runtime/server -run TestTViewInteractiveDemo
// Notes: Exercises rendering and high-frequency input to catch regressions like frozen panes.

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/welcome"
	"texelation/texel"
)

const demoRenderTimeout = 2 * time.Second

// TestTViewInteractiveDemoRendersTable ensures the dynamic table draws visible content.
func TestTViewInteractiveDemoRendersTable(t *testing.T) {
	app := welcome.NewInteractiveDemo()
	app.Resize(78, 22)

	if err := app.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	t.Cleanup(app.Stop)

	time.Sleep(150 * time.Millisecond)

	assertBufferEventuallyContains(t, app, []string{
		"Dynamic Table",
		"Counter",
		"Selected",
	})
}

// TestTViewInteractiveDemoHandlesBurstInput validates the app keeps responding after rapid key bursts.
func TestTViewInteractiveDemoHandlesBurstInput(t *testing.T) {
	app := welcome.NewInteractiveDemo()
	app.Resize(78, 22)

	if err := app.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	t.Cleanup(app.Stop)

	time.Sleep(150 * time.Millisecond)

	// Give the background loop a moment to draw the first frame.
	for i := 0; i < 200; i++ {
		key := '1' + rune(i%8)
		app.HandleKey(tcell.NewEventKey(tcell.KeyRune, key, 0))
		if i%25 == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	assertBufferEventuallyContains(t, app, []string{
		"Selected",
		"Event Log",
	})
}

func assertBufferEventuallyContains(t *testing.T, app texel.App, substrings []string) {
	t.Helper()
	deadline := time.Now().Add(demoRenderTimeout)
	for time.Now().Before(deadline) {
		buffer := app.Render()
		if bufferContainsAll(buffer, substrings) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("buffer never contained all substrings: %v", substrings)
}

func bufferContainsAll(buffer [][]texel.Cell, substrings []string) bool {
	text := bufferToString(buffer)
	for _, target := range substrings {
		if !strings.Contains(text, target) {
			return false
		}
	}
	return true
}

func bufferToString(buffer [][]texel.Cell) string {
	var b strings.Builder
	for _, row := range buffer {
		for _, cell := range row {
			r := cell.Ch
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
