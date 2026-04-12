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
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/apps/texelterm"
)

func TestTexelTermRunRendersOutput(t *testing.T) {
	// Use a temp HOME to avoid loading persisted history from real user data
	t.Setenv("HOME", t.TempDir())

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
	// Use a temp HOME to avoid loading/creating persisted state
	t.Setenv("HOME", t.TempDir())

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
	// Use a temp HOME to avoid loading persisted history from real user data
	t.Setenv("HOME", t.TempDir())

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

// mockClipboard implements texelcore.ClipboardService for testing.
type mockClipboard struct {
	mu   sync.Mutex
	mime string
	data []byte
}

func (m *mockClipboard) SetClipboard(mime string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mime = mime
	m.data = make([]byte, len(data))
	copy(m.data, data)
}

func (m *mockClipboard) GetClipboard() (string, []byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return "", nil, false
	}
	return m.mime, m.data, true
}

// TestClipboardOSC52NoDeadlock verifies that OSC 52 clipboard operations
// do not deadlock. Before the fix, the PTY reader held a.mu during Parse(),
// and the clipboard callback tried to re-acquire a.mu, causing a deadlock
// (Go mutexes are not reentrant). The fix uses a dedicated clipboardMu.
//
// If this test hangs (times out), the deadlock is present.
func TestClipboardOSC52NoDeadlock(t *testing.T) {
	// Use a temp HOME to avoid loading persisted history from real user data
	t.Setenv("HOME", t.TempDir())

	// Script outputs an OSC 52 clipboard-set sequence for "hello" (base64: aGVsbG8=).
	// The sequence is: ESC ] 52 ; c ; aGVsbG8= BEL
	// This fires OnClipboardSet synchronously during Parse() while a.mu is held.
	script := writeScript(t, "#!/bin/sh\nprintf '\\033]52;c;aGVsbG8=\\007'\n")

	app := texelterm.New("texelterm", script)
	app.Resize(40, 10)
	app.SetRefreshNotifier(make(chan bool, 4))

	// Set clipboard service before running (must type-assert to ClipboardAware)
	clip := &mockClipboard{}
	if ca, ok := app.(texelcore.ClipboardAware); ok {
		ca.SetClipboardService(clip)
	} else {
		t.Fatal("TexelTerm does not implement ClipboardAware")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for the script to execute and the OSC 52 sequence to be parsed.
	// If the deadlock is present, the PTY reader goroutine will hang forever
	// and this sleep will not help — the test will timeout at 10s.
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	var clipData []byte
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for clipboard data — likely deadlock in OSC 52 handler")
		case <-tick.C:
			clip.mu.Lock()
			if clip.data != nil {
				clipData = make([]byte, len(clip.data))
				copy(clipData, clip.data)
			}
			clip.mu.Unlock()
			if clipData != nil {
				goto done
			}
		}
	}
done:

	if string(clipData) != "hello" {
		t.Errorf("expected clipboard data %q, got %q", "hello", string(clipData))
	}

	app.Stop()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm did not exit after stop")
	}
}

// TestRenderCursorPositionAfterHorizontalResize verifies that the rendered
// cursor (reverse-styled cell) tracks the correct physical row when lines
// above the cursor are affected by a width decrease on a new terminal that
// hasn't filled the viewport yet.
//
// The sparse viewport model does not wrap long lines on resize — it truncates
// them to the current width. So the cursor stays on the same logical row
// (the prompt line) regardless of width changes. The key invariant tested
// here is that the cursor is always on the row containing the prompt.
func TestRenderCursorPositionAfterHorizontalResize(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Script outputs a long line (51 chars) then a newline, then a short prompt.
	// At width 80: line 0 = long text, line 1 = "$ " with cursor.
	// At width 30 (sparse model): line 0 = first 30 chars, line 1 = "$ " with cursor.
	script := writeScript(t, "#!/bin/sh\nprintf 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\\n$ '\n")

	app := texelterm.New("texelterm", script)
	app.Resize(80, 24)
	app.SetRefreshNotifier(make(chan bool, 4))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	// Wait for output to render
	time.Sleep(500 * time.Millisecond)

	// Verify cursor is visible at the prompt row before resize
	buf := app.Render()
	if len(buf) == 0 {
		t.Fatal("no render buffer")
	}

	cursorRowBefore := findCursorRow(buf)
	if cursorRowBefore < 0 {
		t.Fatal("cursor not found in initial render")
	}
	t.Logf("Before resize: cursor at row %d", cursorRowBefore)

	// Shrink width so the long line is truncated (sparse) or wraps (legacy)
	app.Resize(30, 24)
	buf = app.Render()

	cursorRowAfter := findCursorRow(buf)
	if cursorRowAfter < 0 {
		t.Fatal("cursor not found after resize")
	}
	t.Logf("After resize to width 30: cursor at row %d", cursorRowAfter)

	// Verify the cursor row contains prompt content — this is the key
	// invariant: the cursor must be on the prompt regardless of wrapping model.
	promptRow := rowToString(buf[cursorRowAfter])
	if !strings.Contains(promptRow, "$") {
		t.Errorf("cursor row %d doesn't contain prompt: %q", cursorRowAfter, promptRow)
		for y := 0; y < 5 && y < len(buf); y++ {
			t.Logf("  row %d: %q", y, rowToString(buf[y]))
		}
	}

	app.Stop()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm did not exit after stop")
	}
}

// findCursorRow returns the row index of the first cell with Reverse style
// (cursor indicator), or -1 if not found.
func findCursorRow(buf [][]texelcore.Cell) int {
	for y, row := range buf {
		for _, cell := range row {
			_, _, attrs := cell.Style.Decompose()
			if attrs&tcell.AttrReverse != 0 {
				return y
			}
		}
	}
	return -1
}
