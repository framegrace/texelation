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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/texelterm"
	"texelation/texel"
    "texelation/texel/cards"
)

func skipIfNoPTY(t *testing.T) {
    if os.Getenv("TDE_SKIP_PTY_TESTS") == "1" {
        t.Skip("Skipping PTY-dependent tests (TDE_SKIP_PTY_TESTS=1)")
    }
    f, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
    if err != nil {
        t.Skipf("Skipping PTY-dependent tests (cannot open /dev/ptmx): %v", err)
    }
    _ = f.Close()
}

type recordingBus struct {
	cards.ControlBus
	triggered chan string
}

func (b *recordingBus) Trigger(id string, payload interface{}) error {
	if b.triggered != nil {
		select {
		case b.triggered <- id:
		default:
		}
	}
	return b.ControlBus.Trigger(id, payload)
}

func TestTexelTermRunRendersOutput(t *testing.T) {
    skipIfNoPTY(t)
    script := writeScript(t, "#!/bin/sh\nprintf 'hello texelterm'\n")

	app := texelterm.New("texelterm", script)
	app.Resize(40, 10)
	app.SetRefreshNotifier(make(chan bool, 4))

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm did not exit")
	}

	buffer := app.Render()
	if len(buffer) == 0 {
		t.Fatalf("expected render buffer, got none")
	}
	line := rowToString(buffer[0])
	if !strings.Contains(line, "hello texelterm") {
		t.Fatalf("expected output in buffer, got %q", line)
	}

	app.Stop()
}

func TestTexelTermStopTerminatesProcess(t *testing.T) {
    skipIfNoPTY(t)
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

func TestTexelTermAltScroll(t *testing.T) {
    skipIfNoPTY(t)
    script := writeScript(t, "#!/bin/sh\nfor i in $(seq 1 40); do printf 'line %02d\\n' \"$i\"; done\nsleep 5\n")

	app := texelterm.New("texelterm", script)
	app.Resize(20, 10)
	refresh := make(chan bool, 32)
	app.SetRefreshNotifier(refresh)

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()
	defer app.Stop()

	waitForOutput(t, app, refresh, "line 40", 2*time.Second)

	initial := app.Render()
	initialTop := rowToString(initial[0])
	baseline, ok := parseLineNumber(initialTop)
	if !ok {
		t.Fatalf("could not parse initial line: %q", initialTop)
	}

	drainRefresh(refresh)
	app.HandleKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModAlt))
	waitForRefresh(t, refresh)
	scrolled := app.Render()
	scrolledTop := rowToString(scrolled[0])
	scrollLine, ok := parseLineNumber(scrolledTop)
	if !ok || scrollLine >= baseline {
		t.Fatalf("expected scroll to reveal earlier lines, got %q", scrolledTop)
	}

	drainRefresh(refresh)
	app.HandleKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModAlt))
	waitForRefresh(t, refresh)
	reset := app.Render()
	resetTop := rowToString(reset[0])
	resetLine, ok := parseLineNumber(resetTop)
	if !ok || resetLine != baseline {
		t.Fatalf("expected scroll down to restore baseline, got %q", resetTop)
	}

	app.Stop()
	select {
	case err := <-errCh:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("unexpected shutdown error: %v", err)
			} else if exitErr.ExitCode() == 0 {
				// treat zero exit as success
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit after stop")
	}
}

func TestTexelTermVisualBellTriggersFlash(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\nprintf 'ready\\n'\nsleep 0.1\nprintf '\a'\nsleep 0.3\n")

	app := texelterm.New("texelterm", script)
	app.Resize(20, 6)
	refresh := make(chan bool, 16)
	app.SetRefreshNotifier(refresh)

	pipeline, ok := app.(*cards.Pipeline)
	if !ok {
		t.Fatal("app is not a cards pipeline")
	}

	caps := pipeline.ControlBus().Capabilities()
	foundFlash := false
	for _, cap := range caps {
		if cap.ID == cards.FlashTriggerID {
			foundFlash = true
			break
		}
	}
	if !foundFlash {
		t.Fatalf("flash capability not advertised: %+v", caps)
	}

	var flashCard *cards.EffectCard
	for _, card := range pipeline.Cards() {
		if f, ok := card.(*cards.EffectCard); ok && f.Effect().ID() == "flash" {
			flashCard = f
			break
		}
	}
	if flashCard == nil {
		t.Fatal("flash effect card not present")
	}

	accessor, ok := pipeline.Cards()[0].(cards.AppAccessor)
	if !ok {
		t.Fatal("pipeline head is not an app accessor")
	}
	term, ok := accessor.UnderlyingApp().(*texelterm.TexelTerm)
	if !ok {
		t.Fatal("unexpected app type returned by pipeline")
	}
	recorder := &recordingBus{ControlBus: pipeline.ControlBus(), triggered: make(chan string, 4)}
	term.AttachControlBus(recorder)

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run()
	}()
	defer app.Stop()

	// Ensure the session is active before waiting for the bell.
	waitForOutput(t, app, refresh, "ready", time.Second)

	select {
	case id := <-recorder.triggered:
		if id != cards.FlashTriggerID {
			t.Fatalf("unexpected trigger id %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for flash trigger")
	}

	waitForCondition(t, time.Second, func() bool {
		return flashCard.Effect().Active()
	})

	dummy := [][]texel.Cell{{{Ch: ' '}}}
	waitForCondition(t, time.Second, func() bool {
		flashCard.Render(dummy)
		return !flashCard.Effect().Active()
	})

	select {
	case err := <-errCh:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("unexpected shutdown error: %v", err)
			} else if exitErr.ExitCode() == 0 {
				// treat zero exit as success
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit after bell script")
	}
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

func parseLineNumber(line string) (int, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "line ") {
		return 0, false
	}
	num := strings.TrimPrefix(line, "line ")
	value, err := strconv.Atoi(strings.Fields(num)[0])
	if err != nil {
		return 0, false
	}
	return value, true
}

func drainRefresh(ch <-chan bool) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func waitForRefresh(t *testing.T, ch <-chan bool) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for refresh")
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not satisfied before timeout")
}

func waitForOutput(t *testing.T, app texel.App, refresh <-chan bool, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-refresh:
		case <-time.After(20 * time.Millisecond):
		}
		buf := app.Render()
		if len(buf) == 0 {
			continue
		}
		for _, row := range buf {
			if strings.Contains(rowToString(row), needle) {
				return
			}
		}
	}
	t.Fatalf("output did not contain %q before timeout", needle)
}
