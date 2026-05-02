// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/runner.go
// Summary: Paints a boot.App to an externally-owned tcell screen.

package boot

import (
	"log"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
)

// defaultRefreshInterval drives the splash repaint cadence. The splash
// has no event polling of its own — resizes get noticed lazily on the
// next tick and the elapsed-time line ticks once per second-ish — so a
// short interval is required for the UI to feel alive without pegging
// CPU. 100ms means at most ~10 frames per second of ANSI flushing
// during the bootstrap window, well under what tcell can sustain.
const defaultRefreshInterval = 100 * time.Millisecond

// Runner drives a boot.App against an externally-managed tcell.Screen.
// The screen MUST be initialized before Start is called and MUST NOT
// be Fini'd while the runner is active. Stop() returns once the paint
// goroutine has exited; the caller can then hand the screen off to
// the production renderer without flicker.
type Runner struct {
	screen tcell.Screen
	app    *App
	tick   time.Duration

	wakeMu  sync.Mutex
	wakeCh  chan struct{} // buffered(1); coalesced wake-up signal
	stopCh  chan struct{}
	doneCh  chan struct{} // closed when the paint goroutine exits
	pollCh  chan struct{} // closed when the event-drain goroutine exits
	started bool

	// paintMu serializes paint() against external readers (mainly
	// tests). tcell SimulationScreen's GetContents is not safe to call
	// concurrently with an in-flight Show, so we gate them here. The
	// production path (Stop → handoff to clientrt) doesn't need this —
	// after Stop returns there is no concurrent paint left to race —
	// but tests legitimately want to read mid-flight.
	paintMu sync.Mutex
}

// NewRunner constructs a runner. The screen must be initialized.
func NewRunner(screen tcell.Screen, app *App) *Runner {
	return &Runner{
		screen: screen,
		app:    app,
		tick:   defaultRefreshInterval,
		wakeCh: make(chan struct{}, 1),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		pollCh: make(chan struct{}),
	}
}

// Start spins up the paint and event-drain goroutines. It is
// idempotent; repeated calls after the first are no-ops, so callers
// can defer Stop without worrying about double-Start.
//
// The event-drain goroutine is load-bearing: tcell's tscreen flushes
// pending output via PostEvent / PollEvent cycles, so a screen that
// nobody polls quietly stops repainting after the first frame. We
// drop events during the splash window — the user has nothing
// meaningful to do while the server hydrates, and any keystrokes
// queued here would be lost on handoff anyway. clientrt.Run starts
// its own poll loop after Stop returns.
func (r *Runner) Start() {
	r.wakeMu.Lock()
	if r.started {
		r.wakeMu.Unlock()
		return
	}
	r.started = true
	r.wakeMu.Unlock()

	go r.loop()
	go r.drainEvents()
}

// Wake nudges the paint goroutine to repaint immediately. Call after
// SetStage / SetDetail so the user sees the new status without
// waiting for the next tick. Wakes are coalesced — many calls between
// two paints produce one paint.
func (r *Runner) Wake() {
	select {
	case r.wakeCh <- struct{}{}:
	default:
	}
}

// Stop signals the paint and poll goroutines to exit and blocks
// until they have. Safe to call multiple times AND from concurrent
// goroutines — the close-once is gated by wakeMu, so two callers
// hitting the racy "select default → close" pattern can't both
// fire close on an already-closed channel. After Stop returns the
// screen is no longer being touched by the runner; the caller may
// hand it off.
func (r *Runner) Stop() {
	r.wakeMu.Lock()
	if !r.started {
		r.wakeMu.Unlock()
		return
	}
	alreadyClosed := false
	select {
	case <-r.stopCh:
		alreadyClosed = true
	default:
		close(r.stopCh)
	}
	r.wakeMu.Unlock()

	// Wake the poll goroutine out of its blocking PollEvent call.
	// PostEvent is non-blocking; on success the next PollEvent
	// returns the interrupt and the goroutine notices stopCh closed.
	if !alreadyClosed && r.screen != nil {
		_ = r.screen.PostEvent(tcell.NewEventInterrupt(nil))
	}
	<-r.doneCh
	<-r.pollCh
}

func (r *Runner) loop() {
	defer close(r.doneCh)

	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()

	r.paint()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.paint()
		case <-r.wakeCh:
			r.paint()
		}
	}
}

// drainEvents reads tcell events for the duration of the splash and
// discards them. tcell needs PollEvent to be serviced for the screen
// driver to keep flushing buffered output — without a reader the
// driver stalls after the first frame. Resize events are absorbed
// implicitly: paint() reads screen.Size() each tick, so the next
// paint reflects the new dimensions even though we drop the event.
func (r *Runner) drainEvents() {
	defer close(r.pollCh)
	for {
		ev := r.screen.PollEvent()
		if ev == nil {
			// Screen was Fini'd. Should not happen during the splash
			// — main keeps the screen alive across Stop — but if it
			// ever does, log a breadcrumb. Without one the symptom
			// (frozen splash on real terminals, since tscreen needs
			// PollEvent to keep flushing) has no traceable cause.
			log.Printf("boot.Runner: drainEvents got nil PollEvent (screen Fini'd?); splash will stop repainting")
			return
		}
		select {
		case <-r.stopCh:
			return
		default:
		}
	}
}

// paint reads the current canvas size, asks the App for a frame, and
// flushes it to the screen. Skips the flush when the screen is not
// yet sized (size=(0,0) right after Init on some terminals).
//
// We use Sync() rather than Show() for the splash repaint. Show()
// emits a diff against tcell's last known cell buffer; the splash
// has been observed to "stick" on the first frame on real terminals
// (Ghostty in particular) — likely because the diff layer thinks
// nothing meaningful changed even though the foreground colour did.
// Sync() invalidates the cell cache and forces a full redraw, which
// is fine here because the splash repaints at 10fps over a small
// box and the cost is negligible compared to a TUI's full screen.
func (r *Runner) paint() {
	if r.screen == nil || r.app == nil {
		return
	}
	r.paintMu.Lock()
	defer r.paintMu.Unlock()
	w, h := r.screen.Size()
	if w <= 0 || h <= 0 {
		return
	}
	r.app.SetSize(w, h)
	frame := r.app.RenderFrame()
	if frame.Width == 0 || frame.Height == 0 {
		return
	}
	r.screen.Clear()
	for y := 0; y < frame.Height; y++ {
		row := frame.Runes[y]
		styles := frame.Styles[y]
		for x := 0; x < frame.Width; x++ {
			r.screen.SetContent(x, y, row[x], nil, styles[x])
		}
	}
	r.screen.Sync()
}

// withPaintLock runs fn with the paint mutex held, blocking any
// concurrent in-flight paint. Tests use this to safely read the
// simulation screen mid-run; production callers don't need it.
func (r *Runner) withPaintLock(fn func()) {
	r.paintMu.Lock()
	defer r.paintMu.Unlock()
	fn()
}
