// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/event_loop_fairness_test.go
// Summary: Tests for the priority-drain + coalesce helpers that
// keep the client's main event loop fair under heavy server
// traffic.

package clientruntime

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestCoalesceRenderCh_DrainsAllQueuedTicks verifies the helper
// non-blocking-drains every queued tick on the channel. Without
// this, a burst of N back-to-back signals would each trigger a
// separate render call.
func TestCoalesceRenderCh_DrainsAllQueuedTicks(t *testing.T) {
	ch := make(chan struct{}, 8)
	for i := 0; i < 5; i++ {
		ch <- struct{}{}
	}
	if got := len(ch); got != 5 {
		t.Fatalf("setup: ch len = %d, want 5", got)
	}

	coalesceRenderCh(ch)

	if got := len(ch); got != 0 {
		t.Errorf("after coalesce: ch len = %d, want 0", got)
	}
}

// TestCoalesceRenderCh_NonBlockingOnEmpty verifies the helper
// returns promptly when the channel is empty. A blocking call
// here would freeze the main loop's renderCh case.
func TestCoalesceRenderCh_NonBlockingOnEmpty(t *testing.T) {
	ch := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		coalesceRenderCh(ch)
		close(done)
	}()
	select {
	case <-done:
		// returned promptly — good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("coalesceRenderCh blocked on empty channel")
	}
}

// drainScreenEvents helper expectations
//
// The helper has the signature:
//
//	func drainScreenEvents(events <-chan tcell.Event, handle func(tcell.Event) bool) (drained int, ok bool)
//
// Pulled events are dispatched via handle. ok=false means either the
// channel is closed OR handle returned false (signalling exit).

// TestDrainScreenEvents_ReturnsCountAndDispatches verifies the
// helper pulls every queued event and dispatches each via the
// supplied callback in order, returning the total count and
// ok=true for the empty-after-drain case.
//
// We tag events via NewEventInterrupt(int) and compare the int
// payload (via .Data()) rather than pointer-equal the events
// themselves — tcell.Event is an interface and pointer equality
// would silently break if tcell ever pooled or copied events.
func TestDrainScreenEvents_ReturnsCountAndDispatches(t *testing.T) {
	ch := make(chan tcell.Event, 4)
	want := []int{10, 20, 30}
	for _, id := range want {
		ch <- tcell.NewEventInterrupt(id)
	}

	var dispatched []int
	handle := func(ev tcell.Event) bool {
		dispatched = append(dispatched, ev.(*tcell.EventInterrupt).Data().(int))
		return true
	}

	drained, ok := drainScreenEvents(ch, handle)

	if !ok {
		t.Errorf("ok = false, want true on clean drain")
	}
	if drained != len(want) {
		t.Errorf("drained = %d, want %d", drained, len(want))
	}
	if len(dispatched) != len(want) {
		t.Fatalf("dispatched len = %d, want %d", len(dispatched), len(want))
	}
	for i, id := range want {
		if dispatched[i] != id {
			t.Errorf("dispatch[%d]: got %d, want %d", i, dispatched[i], id)
		}
	}
}

// TestDrainScreenEvents_EmptyChannelReturnsZero verifies the helper
// is a fast no-op when nothing is queued. A blocking implementation
// would freeze the main loop's per-iteration priority drain.
func TestDrainScreenEvents_EmptyChannelReturnsZero(t *testing.T) {
	ch := make(chan tcell.Event, 4)
	handle := func(ev tcell.Event) bool {
		t.Errorf("handle should not be called on empty channel; got %v", ev)
		return true
	}

	done := make(chan struct {
		drained int
		ok      bool
	}, 1)
	go func() {
		drained, ok := drainScreenEvents(ch, handle)
		done <- struct {
			drained int
			ok      bool
		}{drained, ok}
	}()

	select {
	case res := <-done:
		if res.drained != 0 {
			t.Errorf("drained = %d, want 0", res.drained)
		}
		if !res.ok {
			t.Errorf("ok = false, want true on empty drain")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drainScreenEvents blocked on empty channel")
	}
}

// TestDrainScreenEvents_ChannelClosedReturnsNotOK verifies the
// helper returns ok=false when the channel is closed, so the main
// loop's caller can return nil and exit cleanly.
func TestDrainScreenEvents_ChannelClosedReturnsNotOK(t *testing.T) {
	ch := make(chan tcell.Event, 1)
	close(ch)
	handle := func(ev tcell.Event) bool {
		t.Errorf("handle should not be called when channel closed; got %v", ev)
		return true
	}

	drained, ok := drainScreenEvents(ch, handle)

	if ok {
		t.Error("ok = true, want false for closed channel")
	}
	if drained != 0 {
		t.Errorf("drained = %d, want 0 (channel closed before any event)", drained)
	}
}

// TestDrainScreenEvents_DrainsThenChannelCloses covers the realistic
// shutdown shape: events are queued, then the producer closes the
// channel. The helper must drain the buffered events first and only
// then observe the close. Without this case, a future regression
// where the close-detection logic short-circuits before draining
// the buffer would silently lose user input.
func TestDrainScreenEvents_DrainsThenChannelCloses(t *testing.T) {
	ch := make(chan tcell.Event, 4)
	want := []int{1, 2}
	for _, id := range want {
		ch <- tcell.NewEventInterrupt(id)
	}
	close(ch)

	var dispatched []int
	handle := func(ev tcell.Event) bool {
		dispatched = append(dispatched, ev.(*tcell.EventInterrupt).Data().(int))
		return true
	}

	drained, ok := drainScreenEvents(ch, handle)

	if ok {
		t.Error("ok = true, want false (channel closed after drain)")
	}
	if drained != len(want) {
		t.Errorf("drained = %d, want %d", drained, len(want))
	}
	if len(dispatched) != len(want) {
		t.Fatalf("dispatched %d events, want %d", len(dispatched), len(want))
	}
	for i, id := range want {
		if dispatched[i] != id {
			t.Errorf("dispatch[%d]: got %d, want %d", i, dispatched[i], id)
		}
	}
}

// TestDrainScreenEvents_HandleReturnsFalseStopsDrain covers the
// production exit signal: handleScreenEvent returns false to mean
// "the run loop should exit" (e.g. ctrl-Q, session disconnect).
// The helper must propagate that immediately — drained must reflect
// only events whose handler ran successfully (excluding the one
// that returned false), and any remaining queued events stay in
// the channel. Without this case, an off-by-one regression in the
// helper (count++ before vs. after the handle check, or draining
// one extra event after the false return) would silently leak
// events past the exit signal.
func TestDrainScreenEvents_HandleReturnsFalseStopsDrain(t *testing.T) {
	ch := make(chan tcell.Event, 4)
	for _, id := range []int{1, 2, 3} {
		ch <- tcell.NewEventInterrupt(id)
	}

	var dispatched []int
	handle := func(ev tcell.Event) bool {
		id := ev.(*tcell.EventInterrupt).Data().(int)
		dispatched = append(dispatched, id)
		return id != 2 // signal exit on the second event
	}

	drained, ok := drainScreenEvents(ch, handle)

	if ok {
		t.Error("ok = true, want false when handle returned false")
	}
	if drained != 1 {
		t.Errorf("drained = %d, want 1 (only the first successful event counts)", drained)
	}
	if len(dispatched) != 2 {
		t.Errorf("dispatched %d events, want 2 (the handler ran for events 1 and 2)", len(dispatched))
	}
	// The third event must remain in the channel — exit-on-false
	// means the helper stops AT the false event, not after it.
	if got := len(ch); got != 1 {
		t.Errorf("ch len after exit = %d, want 1 (third event should not have been drained)", got)
	}
}
