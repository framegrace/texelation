// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/term_test.go
// Summary: Exercises texelterm behaviour to ensure the terminal app remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: Validates PTY lifecycle, rendering, and shutdown handling.

package texelterm_test

import (
	texelcore "github.com/framegrace/texelui/core"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm"
)

func TestTexelTermRunRendersOutput(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\nprintf 'hello texelterm'\n")

	app := texelterm.New("texelterm", script)
	app.Resize(40, 10)
	app.SetRefreshNotifier(make(chan bool, 4))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for the process to finish and show confirmation
	time.Sleep(500 * time.Millisecond)

	// Check buffer before stopping
	buffer := app.Render()
	if len(buffer) == 0 {
		t.Fatalf("expected render buffer, got none")
	}
	line := rowToString(buffer[0])
	if !strings.Contains(line, "hello texelterm") {
		t.Fatalf("expected output in buffer, got %q", line)
	}

	// Stop the app to dismiss confirmation and allow Run() to return
	app.Stop()

	// Now wait for Run() to return
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm did not exit after stop")
	}
}

func TestTexelTermStopTerminatesProcess(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\ntrap 'exit 0' TERM\nwhile true; do sleep 1; done\n")

	app := texelterm.New("texelterm", script)
	app.Resize(40, 10)
	app.SetRefreshNotifier(make(chan bool, 4))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	time.Sleep(200 * time.Millisecond)
	app.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("unexpected shutdown error type: %v", err)
			} else if exitErr.ExitCode() == 0 {
				// Treat zero exit as success even when wrapped in ExitError.
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm run did not return after stop")
	}

	// Second stop should be a no-op.
	app.Stop()
}

func writeScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func rowToString(row []texelcore.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		if cell.Ch == 0 {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(cell.Ch)
	}
	return strings.TrimRight(b.String(), " ")
}

// TestTexelTermLineWrapOutput tests that long lines wrap correctly.
func TestTexelTermLineWrapOutput(t *testing.T) {
	// Output 20 characters on a 10-column wide terminal
	// Should wrap to 2 lines: "ABCDEFGHIJ" "KLMNOPQRST"
	// (reduced to 20 chars so we don't have a third line that conflicts with confirmation dialog)
	script := writeScript(t, "#!/bin/sh\nprintf 'ABCDEFGHIJKLMNOPQRST'\n")

	app := texelterm.New("texelterm", script)
	app.Resize(10, 10) // 10 columns wide
	app.SetRefreshNotifier(make(chan bool, 4))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for the output to be processed (shorter wait since script is simple)
	time.Sleep(200 * time.Millisecond)

	// Check buffer BEFORE the confirmation dialog appears
	buffer := app.Render()
	if len(buffer) < 2 {
		t.Fatalf("expected at least 2 rows in buffer, got %d", len(buffer))
	}

	t.Logf("Buffer contents:")
	for i := 0; i < 5 && i < len(buffer); i++ {
		t.Logf("  Row %d: %q", i, rowToString(buffer[i]))
	}

	// Verify wrapping - the first two rows should have the wrapped content
	row0 := rowToString(buffer[0])
	row1 := rowToString(buffer[1])

	if row0 != "ABCDEFGHIJ" {
		t.Errorf("Row 0: expected 'ABCDEFGHIJ', got %q", row0)
	}
	if row1 != "KLMNOPQRST" {
		t.Errorf("Row 1: expected 'KLMNOPQRST', got %q", row1)
	}

	app.Stop()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm did not exit after stop")
	}
}
