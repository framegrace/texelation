// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"sync"
	"testing"
	"time"
)

// recordedFetchPending captures the (paneID, delta) sequence broadcast
// by a fetch-pending timer. Used by tests to assert events fire in the
// right order with the right counts.
type recordedFetchPending struct {
	mu     sync.Mutex
	events []recordedFetchPendingEvent
}

type recordedFetchPendingEvent struct {
	PaneID [16]byte
	Delta  int
}

func (r *recordedFetchPending) record(paneID [16]byte, delta int) {
	r.mu.Lock()
	r.events = append(r.events, recordedFetchPendingEvent{PaneID: paneID, Delta: delta})
	r.mu.Unlock()
}

func (r *recordedFetchPending) snapshot() []recordedFetchPendingEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedFetchPendingEvent, len(r.events))
	copy(out, r.events)
	return out
}

// TestFetchPendingCancel_NilBroadcasterSafe — startFetchPendingTimer
// with a nil broadcaster yields a nil timer; cancel must be a no-op.
func TestFetchPendingCancel_NilBroadcasterSafe(t *testing.T) {
	timer, cancel := startFetchPendingTimer(nil, [16]byte{0xab})
	if timer != nil {
		t.Errorf("nil broadcaster should yield nil timer, got %v", timer)
	}
	// Should not panic.
	cancel(timer)
}

// TestFetchPendingTimer_PairedDeltas — when the timer fires before
// cancel, both +1 and -1 events emit, in order, with the same paneID.
func TestFetchPendingTimer_PairedDeltas(t *testing.T) {
	prev := fetchPendingThreshold
	fetchPendingThreshold = 5 * time.Millisecond
	defer func() { fetchPendingThreshold = prev }()

	rec := &recordedFetchPending{}
	paneID := [16]byte{0xde, 0xad, 0xbe, 0xef}

	timer, cancel := startFetchPendingTimer(rec.record, paneID)
	if timer == nil {
		t.Fatalf("expected timer with valid broadcaster, got nil")
	}

	// Wait long enough for the timer to fire.
	time.Sleep(20 * time.Millisecond)

	// Cancel: should emit the matching -1.
	cancel(timer)

	events := rec.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (+1 / -1), got %d: %+v", len(events), events)
	}
	if events[0].PaneID != paneID || events[0].Delta != +1 {
		t.Errorf("event[0] = %+v, want PaneID=%x Delta=+1", events[0], paneID)
	}
	if events[1].PaneID != paneID || events[1].Delta != -1 {
		t.Errorf("event[1] = %+v, want PaneID=%x Delta=-1", events[1], paneID)
	}
}

// TestFetchPendingTimer_FastCancelBeforeFire — when cancel runs before
// the timer fires (the common "fast fetch" case), no events emit.
func TestFetchPendingTimer_FastCancelBeforeFire(t *testing.T) {
	prev := fetchPendingThreshold
	fetchPendingThreshold = 50 * time.Millisecond
	defer func() { fetchPendingThreshold = prev }()

	rec := &recordedFetchPending{}
	paneID := [16]byte{0xca, 0xfe}

	timer, cancel := startFetchPendingTimer(rec.record, paneID)
	if timer == nil {
		t.Fatalf("expected timer with valid broadcaster, got nil")
	}
	cancel(timer)

	// Wait past the threshold to make sure no late firing happens.
	time.Sleep(80 * time.Millisecond)

	events := rec.snapshot()
	if len(events) != 0 {
		t.Errorf("expected 0 events (cancel before fire), got %d: %+v", len(events), events)
	}
}

// TestFetchPendingTimer_ConcurrentRaces — repeatedly arm/cancel the
// timer with timing close to the threshold and assert the broadcast
// count is balanced (+ count == - count) and that no -1 ever precedes
// its matching +1.
func TestFetchPendingTimer_ConcurrentRaces(t *testing.T) {
	prev := fetchPendingThreshold
	fetchPendingThreshold = 1 * time.Millisecond
	defer func() { fetchPendingThreshold = prev }()

	rec := &recordedFetchPending{}
	paneID := [16]byte{0x1, 0x2, 0x3}

	const iters = 200
	for i := 0; i < iters; i++ {
		timer, cancel := startFetchPendingTimer(rec.record, paneID)
		// Sleep ~ threshold to maximise the race window.
		time.Sleep(time.Duration(i%3) * time.Millisecond)
		cancel(timer)
	}

	// Drain any in-flight goroutines (their broadcasts run synchronously
	// while they hold the timer mutex; once cancel returns, no more
	// emissions for that iteration are possible).
	time.Sleep(20 * time.Millisecond)

	events := rec.snapshot()
	plus, minus := 0, 0
	running := 0
	for _, e := range events {
		switch e.Delta {
		case +1:
			plus++
			running++
		case -1:
			minus++
			running--
			if running < 0 {
				t.Errorf("running count went negative at event %+v (a -1 preceded its +1)", e)
			}
		default:
			t.Errorf("unexpected delta in event %+v", e)
		}
	}
	if plus != minus {
		t.Errorf("unbalanced events: %d +1 vs %d -1", plus, minus)
	}
	if running != 0 {
		t.Errorf("non-zero running count at end: %d", running)
	}
}
