// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/term_test.go
// Summary: Exercises texelterm behaviour to ensure the terminal app remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: Validates PTY lifecycle, rendering, and shutdown handling.

package texelterm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"texelation/apps/texelterm"
	"texelation/texel"
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

func rowToString(row []texel.Cell) string {
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

