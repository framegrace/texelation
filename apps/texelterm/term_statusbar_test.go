package texelterm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm"
)

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
