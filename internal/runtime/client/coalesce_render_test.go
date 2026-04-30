// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/coalesce_render_test.go
// Summary: Tests for the render-coalescing window that defers a render
// after a TreeSnapshot whose pane geometry changed, so the BufferDelta
// that follows on the wire lands in the same render frame and the user
// does not see a one-frame "jump" while resizing a pane.
// Issue #199.

package clientruntime

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/protocol"
)

// makeCoalesceTestState builds a clientState with an empty BufferCache
// and pane-cache map, suitable for driving handleControlMessage with
// MsgTreeSnapshot payloads. We do not wire renderCh here — the hold flag
// is set on state regardless of channel state, which is the contract the
// render loop relies on.
func makeCoalesceTestState() *clientState {
	return &clientState{
		cache:      client.NewBufferCache(),
		paneCaches: make(map[[16]byte]*client.PaneCache),
	}
}

// snapshotWithPane returns a single-pane TreeSnapshot at the given rect.
func snapshotWithPane(id [16]byte, x, y, w, h int32) protocol.TreeSnapshot {
	return protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id,
			X:      x,
			Y:      y,
			Width:  w,
			Height: h,
		}},
	}
}

// applySnapshot drives the MsgTreeSnapshot path of handleControlMessage,
// matching how readLoop dispatches a real wire message.
func applySnapshot(t *testing.T, state *clientState, snap protocol.TreeSnapshot) {
	t.Helper()
	payload, err := protocol.EncodeTreeSnapshot(snap)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	hdr := protocol.Header{Type: protocol.MsgTreeSnapshot}
	var pendingAck atomic.Uint64
	ackCh := make(chan struct{}, 1)
	handleControlMessage(state, nil, hdr, payload, [16]byte{}, nil, nil, &pendingAck, ackCh)
}

// TestRenderCoalescing_HoldsAfterGeometryChange asserts that a
// TreeSnapshot whose pane rect differs from the cached rect arms the
// render-hold flag, and that the hold has roughly renderCoalesceWindow
// duration remaining immediately after.
func TestRenderCoalescing_HoldsAfterGeometryChange(t *testing.T) {
	state := makeCoalesceTestState()
	var id [16]byte
	id[0] = 1

	// Seed the cache with an initial geometry.
	applySnapshot(t, state, snapshotWithPane(id, 0, 0, 80, 24))

	// First snapshot is a brand-new pane (no prior cache entry), so it
	// already armed a hold. Clear it so we can isolate the second
	// snapshot's effect.
	state.clearHoldRender()

	before := time.Now()
	// Apply a snapshot that changes the pane's rect (simulating a resize
	// from the top edge: Y and Height shift).
	applySnapshot(t, state, snapshotWithPane(id, 0, 5, 80, 19))
	after := time.Now()

	remaining := state.holdRenderRemaining(after)
	if remaining <= 0 {
		t.Fatalf("hold should be active after geometry change, remaining=%v", remaining)
	}
	// Sanity: remaining must be at most the configured window minus the
	// elapsed time between the call and the measurement, but no more than
	// the configured window.
	maxExpected := renderCoalesceWindow + 5*time.Millisecond
	if remaining > maxExpected {
		t.Fatalf("hold remaining %v exceeds window+slack %v", remaining, maxExpected)
	}
	// And remaining should still be positive for at least a few ms after
	// `after`, since we just armed the hold.
	if remaining < time.Millisecond {
		t.Fatalf("hold remaining %v unexpectedly small (called at %v, measured at %v)",
			remaining, before, after)
	}

	// A second snapshot with no rect change must not shorten the hold.
	prevHold := state.holdRenderUntil
	applySnapshot(t, state, snapshotWithPane(id, 0, 5, 80, 19))
	state.holdRenderMu.Lock()
	curHold := state.holdRenderUntil
	state.holdRenderMu.Unlock()
	if !curHold.Equal(prevHold) {
		t.Fatalf("no-op snapshot must not change hold: prev=%v cur=%v", prevHold, curHold)
	}
}

// TestRenderCoalescing_DoesNotHoldOnNoChange asserts that a TreeSnapshot
// whose pane rects match the current cache does NOT arm the hold flag.
// This protects steady-state snapshots (e.g., focus or revision changes
// without geometry shifts) from being unnecessarily delayed.
func TestRenderCoalescing_DoesNotHoldOnNoChange(t *testing.T) {
	state := makeCoalesceTestState()
	var id [16]byte
	id[0] = 1

	// Seed the cache.
	applySnapshot(t, state, snapshotWithPane(id, 0, 0, 80, 24))
	state.clearHoldRender()

	// Apply an identical snapshot.
	applySnapshot(t, state, snapshotWithPane(id, 0, 0, 80, 24))

	if rem := state.holdRenderRemaining(time.Now()); rem != 0 {
		t.Fatalf("hold should be zero after identical snapshot, got %v", rem)
	}
	state.holdRenderMu.Lock()
	zero := state.holdRenderUntil.IsZero()
	state.holdRenderMu.Unlock()
	if !zero {
		t.Fatalf("holdRenderUntil should remain zero after identical snapshot")
	}
}

// TestRenderCoalescing_HoldClearsAfterRender asserts that
// clearHoldRender resets the hold marker so a subsequent
// holdRenderRemaining call returns zero (matching what the render loop
// sees after it consumes a frame).
func TestRenderCoalescing_HoldClearsAfterRender(t *testing.T) {
	state := makeCoalesceTestState()
	state.setHoldRenderUntil(time.Now().Add(50 * time.Millisecond))
	if rem := state.holdRenderRemaining(time.Now()); rem <= 0 {
		t.Fatalf("hold should be active, remaining=%v", rem)
	}
	state.clearHoldRender()
	if rem := state.holdRenderRemaining(time.Now()); rem != 0 {
		t.Fatalf("hold remaining should be zero after clear, got %v", rem)
	}
}

// TestRenderCoalescing_HoldExpires asserts that holdRenderRemaining
// returns 0 once the hold instant has passed, even if clearHoldRender
// has not yet been called. This is what lets a tickCh fire after the
// window even if no BufferDelta arrived.
func TestRenderCoalescing_HoldExpires(t *testing.T) {
	state := makeCoalesceTestState()
	past := time.Now().Add(-1 * time.Second)
	state.setHoldRenderUntil(past)
	if rem := state.holdRenderRemaining(time.Now()); rem != 0 {
		t.Fatalf("hold remaining for past instant should be zero, got %v", rem)
	}
}
