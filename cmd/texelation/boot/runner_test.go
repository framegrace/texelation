// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// newSimScreen returns an initialized tcell SimulationScreen sized for
// the test. Caller must Fini() in cleanup. Sized at 80x24 to match
// the splash's default geometry assumptions.
func newSimScreen(t *testing.T) tcell.SimulationScreen {
	t.Helper()
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	scr.SetSize(80, 24)
	t.Cleanup(scr.Fini)
	return scr
}

// readScreenAsText flattens the simulation screen's rendered cells
// into row strings. Used to confirm Wake() actually drove a paint.
// The runner argument is required so we can take its paint lock —
// concurrent draw + GetContents on tcell.SimulationScreen is racy.
// The cell slice returned by GetContents aliases the simscreen's
// internal buffer, so the entire read MUST happen inside the lock —
// releasing it and then iterating produces a use-after-mutate race.
func readScreenAsText(t *testing.T, scr tcell.SimulationScreen, r *Runner) []string {
	t.Helper()
	var rows []string
	r.withPaintLock(func() {
		cells, w, h := scr.GetContents()
		if len(cells) != w*h {
			t.Fatalf("sim screen contents shape: got %d cells for %dx%d", len(cells), w, h)
		}
		rows = make([]string, h)
		var b strings.Builder
		for y := 0; y < h; y++ {
			b.Reset()
			for x := 0; x < w; x++ {
				c := cells[y*w+x]
				if len(c.Runes) == 0 {
					b.WriteRune(' ')
					continue
				}
				b.WriteRune(c.Runes[0])
			}
			rows[y] = b.String()
		}
	})
	return rows
}

// containsRow reports whether any row contains needle. Used so tests
// don't bake the box's exact y-coordinate into assertions.
func containsRow(rows []string, needle string) bool {
	for _, r := range rows {
		if strings.Contains(r, needle) {
			return true
		}
	}
	return false
}

func TestRunner_PaintsInitialFrameOnStart(t *testing.T) {
	scr := newSimScreen(t)
	app := New("Texelation")
	app.SetStage(StageStarting)

	r := NewRunner(scr, app)
	r.Start()
	defer r.Stop()

	// The first paint runs synchronously from the loop goroutine; give
	// it a beat to land on the simulated screen.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows := readScreenAsText(t, scr, r)
		if containsRow(rows, "Texelation") && containsRow(rows, string(StageStarting)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows := readScreenAsText(t, scr, r)
	t.Fatalf("initial paint missing title or stage. rows:\n%s", strings.Join(rows, "\n"))
}

func TestRunner_WakeRepaintsAfterStageChange(t *testing.T) {
	scr := newSimScreen(t)
	app := New("Texelation")
	app.SetStage(StageStarting)

	r := NewRunner(scr, app)
	r.Start()
	defer r.Stop()

	// Wait for initial paint.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if containsRow(readScreenAsText(t, scr, r), string(StageStarting)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	app.SetStage(StageConnecting)
	r.Wake()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows := readScreenAsText(t, scr, r)
		if containsRow(rows, string(StageConnecting)) && !containsRow(rows, string(StageStarting)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows := readScreenAsText(t, scr, r)
	t.Fatalf("Wake did not produce a stage update. rows:\n%s", strings.Join(rows, "\n"))
}

func TestRunner_StopReturnsCleanly(t *testing.T) {
	scr := newSimScreen(t)
	app := New("Texelation")
	r := NewRunner(scr, app)
	r.Start()

	done := make(chan struct{})
	go func() {
		r.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s")
	}
}

func TestRunner_StopIdempotent(t *testing.T) {
	scr := newSimScreen(t)
	app := New("Texelation")
	r := NewRunner(scr, app)
	r.Start()
	r.Stop()
	// Second Stop must not panic or block.
	r.Stop()
}

func TestRunner_StartIdempotent(t *testing.T) {
	scr := newSimScreen(t)
	app := New("Texelation")
	r := NewRunner(scr, app)
	r.Start()
	r.Start() // second call must not spawn a second goroutine
	r.Stop()
}
