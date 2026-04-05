package texelterm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm"
)

// TestToggleOverlayRenderedInOutput verifies that the render buffer includes
// the toggle button overlay on row 0 (top-right).
func TestToggleOverlayRenderedInOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	script := writeScript(t, "#!/bin/sh\nprintf 'hello'\n")

	app := texelterm.New("texelterm", script)
	app.Resize(40, 11) // 10 terminal rows + 1 status bar
	app.SetRefreshNotifier(make(chan bool, 4))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	time.Sleep(300 * time.Millisecond)

	buf := app.Render()
	if buf == nil {
		t.Fatal("expected non-nil render buffer")
	}

	// Buffer should be 11 rows total (10 terminal + 1 status bar)
	if len(buf) != 11 {
		t.Fatalf("expected 11 rows, got %d", len(buf))
	}

	// Row 0 should contain the collapsed toggle overlay (hamburger icon)
	contentRow := rowToString(buf[0])
	if !strings.Contains(contentRow, "\u2261") { // ≡ hamburger
		t.Errorf("expected hamburger icon on overlay row 0, got %q", contentRow)
	}

	app.Stop()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm did not exit after stop")
	}
}

// TestStatusBarTerminalContentNotTruncated verifies that terminal content
// appears in the terminal area (above the status bar).
func TestStatusBarTerminalContentNotTruncated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	script := writeScript(t, "#!/bin/sh\nprintf 'VISIBLE'\n")

	app := texelterm.New("texelterm", script)
	app.Resize(40, 11) // 10 terminal rows + 1 status bar
	app.SetRefreshNotifier(make(chan bool, 4))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	time.Sleep(300 * time.Millisecond)

	buf := app.Render()
	if buf == nil {
		t.Fatal("expected non-nil render buffer")
	}

	// Terminal content should be in the first 10 rows
	row0 := rowToString(buf[0])
	if !strings.Contains(row0, "VISIBLE") {
		t.Errorf("expected 'VISIBLE' in terminal area row 0, got %q", row0)
	}

	app.Stop()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm did not exit after stop")
	}
}
