// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/fetch_pending_timer.go
// Summary: Background timer that surfaces a "fetch pending" event when
// a MsgFetchRange takes longer than 50ms to resolve. The connection
// handler arms this once per request and cancels it on response —
// pairing +1/-1 deltas so listeners can aggregate to a stable
// in-flight count.

package server

import (
	"sync"
	"time"
)

// fetchPendingThreshold is how long a MsgFetchRange may take before we
// surface a "fetch pending" event on the desktop dispatcher. Tuned for
// the spec's "missed-deadline handling": below this, the round-trip is
// fast enough that any UI indicator would just flicker; above, the
// user perceives a real wait and benefits from a statusbar hint.
//
// Declared as a var (not const) so tests can shorten it without waiting
// 50ms per case.
var fetchPendingThreshold = 50 * time.Millisecond

// fetchPendingBroadcaster is the broadcast callback the timer fires.
// Implemented in production by *texel.DesktopEngine.BroadcastFetchPending,
// substituted with a recording fake in tests.
type fetchPendingBroadcaster func(paneID [16]byte, delta int)

// fetchPendingTimer holds the per-request state for the slow-fetch
// indicator. The mutex synchronises the timer's goroutine with the
// cancel path so the +1 and matching -1 broadcasts always emit in
// order (atomic-only would let a late goroutine emit +1 *after* cancel
// emitted -1, leaving listeners' counts briefly negative).
type fetchPendingTimer struct {
	mu        sync.Mutex
	broadcast fetchPendingBroadcaster
	paneID    [16]byte
	timer     *time.Timer
	fired     bool // goroutine has broadcast +1
	cancelled bool // cancel path has run; goroutine must skip
}

// startFetchPendingTimer arms a one-shot 50ms timer that broadcasts
// +1 for the given pane if the request hasn't been cancelled by then.
// Returns the timer (or nil if broadcast is nil) plus a cancel function
// the caller MUST defer.
func startFetchPendingTimer(broadcast fetchPendingBroadcaster, paneID [16]byte) (*fetchPendingTimer, func(*fetchPendingTimer)) {
	if broadcast == nil {
		return nil, fetchPendingCancel
	}
	ft := &fetchPendingTimer{
		broadcast: broadcast,
		paneID:    paneID,
	}
	ft.timer = time.AfterFunc(fetchPendingThreshold, func() {
		ft.mu.Lock()
		defer ft.mu.Unlock()
		if ft.cancelled {
			return
		}
		ft.fired = true
		broadcast(paneID, +1)
	})
	return ft, fetchPendingCancel
}

// fetchPendingCancel stops the timer and, if it had already fired,
// broadcasts the matching -1. Safe to call with a nil timer (the
// no-broadcaster case).
//
// The mutex pairing with the goroutine guarantees that:
//   - If the goroutine ran first: the +1 was broadcast, fired=true
//     when we lock; we broadcast -1.
//   - If we ran first: cancelled=true when the goroutine eventually
//     locks; goroutine returns without broadcasting +1, and we don't
//     broadcast -1 either (no pair to close).
//   - Concurrent races serialise on the mutex; broadcasts are always
//     ordered +1 then -1.
func fetchPendingCancel(ft *fetchPendingTimer) {
	if ft == nil {
		return
	}
	ft.timer.Stop()
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.cancelled = true
	if ft.fired {
		ft.broadcast(ft.paneID, -1)
	}
}
