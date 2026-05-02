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
