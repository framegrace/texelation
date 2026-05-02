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
