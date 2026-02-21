package texelterm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm"
)

// TestStatusBarRenderedInOutput verifies that the render buffer includes
// the status bar rows at the bottom (separator + toggle buttons).
func TestStatusBarRenderedInOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	script := writeScript(t, "#!/bin/sh\nprintf 'hello'\n")

	app := texelterm.New("texelterm", script)
	app.Resize(40, 12) // 10 terminal rows + 2 status bar
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

	// Buffer should be 12 rows total (10 terminal + 2 status bar)
	if len(buf) != 12 {
		t.Fatalf("expected 12 rows, got %d", len(buf))
	}

	// Row 10 should be the separator line (─ characters)
	sepRow := rowToString(buf[10])
	if !strings.Contains(sepRow, "─") {
		t.Errorf("expected separator '─' on row 10, got %q", sepRow)
	}

	// Row 11 (content row) should contain toggle button labels
	contentRow := rowToString(buf[11])
	if !strings.Contains(contentRow, "TFM") {
		t.Errorf("expected 'TFM' on status bar content row, got %q", contentRow)
	}
	if !strings.Contains(contentRow, "NRM") {
		t.Errorf("expected 'NRM' on status bar content row, got %q", contentRow)
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
	app.Resize(40, 12)
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
