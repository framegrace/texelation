package texelterm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/texel"
)

// rowToString is a minimal helper for turning a rendered row into a string.
func rowToString(row []texel.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		ch := cell.Ch
		if ch == 0 {
			ch = ' '
		}
		b.WriteRune(ch)
	}
	return strings.TrimRight(b.String(), " ")
}

// writeShellLauncher writes a small script that clears the screen, disables
// the prompt, and launches an interactive /bin/sh. This gives us a predictable
// top-of-screen editing environment.
func writeShellLauncher(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "launcher.sh")
	content := "#!/bin/sh\n" +
		"printf '\\033[H\\033[2J'\n" +
		"exec /bin/bash -i\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write launcher: %v", err)
	}
	return path
}

// TestVisualWrapAtTopWithShell verifies that with a real shell running inside
// TexelTerm, typing past the viewport width on the first row causes visual
// wrapping to open rows below the cursor instead of re-wrapping the same row.
func TestVisualWrapAtTopWithShell(t *testing.T) {
	launcher := writeShellLauncher(t)

	app := New("texelterm", launcher)
	term, ok := app.(*TexelTerm)
	if !ok {
		t.Fatalf("unexpected app type %T", app)
	}

	// Match the user's reproduction: 35x10 terminal.
	term.Resize(35, 10)
	refresh := make(chan bool, 64)
	term.SetRefreshNotifier(refresh)

	errCh := make(chan error, 1)
	go func() {
		errCh <- term.Run()
	}()
	defer term.Stop()

	// Wait briefly for the shell to start and clear the screen.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-refresh:
		case <-time.After(20 * time.Millisecond):
		}
		buf := term.Render()
		if len(buf) == 4 {
			// We consider the shell "ready" once we have a consistent buffer.
			break
		}
	}

	// Type the user's pattern: "123456890" repeated four times.
	pattern := "123456890"
	for i := 0; i < 4; i++ {
		for _, r := range pattern {
			term.HandleKey(tcell.NewEventKey(tcell.KeyRune, r, 0))
		}
	}

	// Allow the parser to process the echoes.
	time.Sleep(100 * time.Millisecond)
	// Drain pending refresh signals.
	for {
		select {
		case <-refresh:
		default:
			goto drained
		}
	}
drained:

	buf := term.Render()
	if len(buf) != 10 {
		t.Fatalf("expected 10 rows, got %d", len(buf))
	}

	lines := make([]string, len(buf))
	for i := range buf {
		lines[i] = rowToString(buf[i])
	}

	// Find the row that contains the prompt arrow and the first digit of our
	// input (e.g. "❯ 1234...").
	startRow := -1
	startCol := -1
	for y, line := range lines {
		idx := strings.Index(line, "❯")
		if idx == -1 {
			continue
		}
		// Find the first digit after the arrow.
		for i := idx; i < len(line); i++ {
			ch := rune(line[i])
			if ch >= '0' && ch <= '9' {
				startRow = y
				startCol = i
				break
			}
		}
		if startRow != -1 {
			break
		}
	}
	if startRow == -1 {
		t.Fatalf("could not find prompt arrow and starting digit in buffer:\n%q", lines)
	}

	var digits strings.Builder
	for y := startRow; y < len(lines); y++ {
		line := lines[y]
		from := 0
		if y == startRow {
			if startCol >= len(line) {
				continue
			}
			from = startCol
		}
		for _, ch := range line[from:] {
			if ch >= '0' && ch <= '9' {
				digits.WriteRune(ch)
			}
		}
	}
	got := digits.String()
	want := strings.Repeat(pattern, 4)

	if got != want {
		t.Fatalf("wrapped digits mismatch.\nwant: %q\ngot:  %q\nlines: %#v", want, got, lines)
	}
}
