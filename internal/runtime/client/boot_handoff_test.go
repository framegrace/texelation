// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/boot_handoff_test.go
// Summary: Tests for the three-flag boot-splash handoff predicate
// and the BufferDelta / TreeSnapshot / BootProgress dispatch paths
// in handleControlMessage that latch the gates.

package clientruntime

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/framegrace/texelation/protocol"
)

// TestBootHandoffReady_AllThreeGatesRequired verifies the predicate
// returns true only when treeSnapshotApplied, firstContentDelta, and
// fullRenderHappened are all set. Each single-flag drop must
// suppress the handoff. This is the load-bearing splash deactivation
// rule and the comment alongside the renderCh case spells out why
// each gate matters on rehydrated cold starts.
func TestBootHandoffReady_AllThreeGatesRequired(t *testing.T) {
	cases := []struct {
		name            string
		tree, cnt, full bool
		want            bool
	}{
		{"none", false, false, false, false},
		{"only tree", true, false, false, false},
		{"only content", false, true, false, false},
		{"only full", false, false, true, false},
		{"tree+content", true, true, false, false},
		{"tree+full", true, false, true, false},
		{"content+full", false, true, true, false},
		{"all three", true, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := makeTestState()
			state.treeSnapshotApplied.Store(tc.tree)
			state.firstContentDelta.Store(tc.cnt)
			state.fullRenderHappened.Store(tc.full)
			if got := state.bootHandoffReady(); got != tc.want {
				t.Errorf("bootHandoffReady = %v, want %v (tree=%v cnt=%v full=%v)",
					got, tc.want, tc.tree, tc.cnt, tc.full)
			}
		})
	}
}

// TestFirstContentDelta_DecorOnlyDoesNotLatch verifies that a
// BufferDelta carrying only DecorRows (pane borders, no actual
// content) does NOT flip firstContentDelta. The publisher emits
// decor-only deltas immediately after the resume snapshot — the
// whole point of this gate is to ignore them so the splash stays
// alive until real content arrives.
func TestFirstContentDelta_DecorOnlyDoesNotLatch(t *testing.T) {
	state := makeTestState()
	var paneID [16]byte
	paneID[0] = 1

	// Encode a delta with no Rows but one DecorRow (a border row).
	delta := protocol.BufferDelta{
		PaneID:  paneID,
		RowBase: 0,
		Styles:  []protocol.StyleEntry{{AttrFlags: 0}},
		DecorRows: []protocol.DecorRowDelta{
			{
				RowIdx: 0,
				Spans:  []protocol.CellSpan{{StartCol: 0, Text: "─", StyleIndex: 0}},
			},
		},
	}
	payload, err := protocol.EncodeBufferDelta(delta)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var pendingAck atomic.Uint64
	ackCh := make(chan struct{}, 1)
	hdr := protocol.Header{Type: protocol.MsgBufferDelta}
	handleControlMessage(state, hdr, payload, [16]byte{}, nil, nil, &pendingAck, ackCh)

	if state.firstContentDelta.Load() {
		t.Fatal("firstContentDelta latched on a decor-only delta; only Rows should count")
	}
}

// TestFirstContentDelta_RowsDoesLatch is the positive counterpart:
// a delta with at least one Row must flip firstContentDelta.
func TestFirstContentDelta_RowsDoesLatch(t *testing.T) {
	state := makeTestState()
	var paneID [16]byte
	paneID[0] = 2

	delta := protocol.BufferDelta{
		PaneID:  paneID,
		RowBase: 0,
		Styles:  []protocol.StyleEntry{{AttrFlags: 0}},
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "$", StyleIndex: 0}}},
		},
	}
	payload, err := protocol.EncodeBufferDelta(delta)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var pendingAck atomic.Uint64
	ackCh := make(chan struct{}, 1)
	hdr := protocol.Header{Type: protocol.MsgBufferDelta}
	handleControlMessage(state, hdr, payload, [16]byte{}, nil, nil, &pendingAck, ackCh)

	if !state.firstContentDelta.Load() {
		t.Fatal("firstContentDelta did not latch on a delta with Rows")
	}
}

// TestTreeSnapshot_LatchesGate verifies MsgTreeSnapshot processing
// flips treeSnapshotApplied. Without this latch the splash hands off
// before a real workspace snapshot has arrived (rehydrated cold
// starts defer TreeSnapshot to handleClientReady).
func TestTreeSnapshot_LatchesGate(t *testing.T) {
	state := makeTestState()
	if state.treeSnapshotApplied.Load() {
		t.Fatal("treeSnapshotApplied should start false")
	}

	snap := protocol.TreeSnapshot{} // empty snapshot is valid for the latch
	payload, err := protocol.EncodeTreeSnapshot(snap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var pendingAck atomic.Uint64
	ackCh := make(chan struct{}, 1)
	var lastSeq atomic.Uint64
	hdr := protocol.Header{Type: protocol.MsgTreeSnapshot}
	handleControlMessage(state, hdr, payload, [16]byte{}, &lastSeq, nil, &pendingAck, ackCh)

	if !state.treeSnapshotApplied.Load() {
		t.Fatal("treeSnapshotApplied did not latch on MsgTreeSnapshot")
	}
	if !state.fullRenderNeeded {
		t.Error("fullRenderNeeded should also be set by the snapshot path")
	}
}

// TestBootProgress_DispatchesToCallback verifies MsgBootProgress
// payloads in handleControlMessage reach the registered
// bootProgressFn. Without this dispatch the messages emitted by
// handleClientReady's slow phase are silently dropped after
// RequestResume returns, so the splash sits on whatever stage was
// set before the resume.
func TestBootProgress_DispatchesToCallback(t *testing.T) {
	state := makeTestState()

	var mu sync.Mutex
	var got []string
	state.setBootProgressFn(func(msg string) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	})

	for _, msg := range []string{"Validating session…", "Restoring pane 1 of 3…"} {
		payload, err := protocol.EncodeBootProgress(protocol.BootProgress{Message: msg})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		var pendingAck atomic.Uint64
		ackCh := make(chan struct{}, 1)
		hdr := protocol.Header{Type: protocol.MsgBootProgress}
		ret := handleControlMessage(state, hdr, payload, [16]byte{}, nil, nil, &pendingAck, ackCh)
		if ret {
			t.Errorf("handleControlMessage returned true for MsgBootProgress; should be false (cosmetic, no renderCh signal)")
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != "Validating session…" || got[1] != "Restoring pane 1 of 3…" {
		t.Errorf("dispatched messages mismatch: got %#v", got)
	}
}

// TestBootProgress_NilCallbackTolerated verifies the dispatch path
// is a no-op when no bootProgressFn is registered (the splashless
// path through clientrt.Run shouldn't crash on progress messages).
func TestBootProgress_NilCallbackTolerated(t *testing.T) {
	state := makeTestState()
	// no setBootProgressFn call — fn stays nil

	payload, err := protocol.EncodeBootProgress(protocol.BootProgress{Message: "hi"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var pendingAck atomic.Uint64
	ackCh := make(chan struct{}, 1)
	hdr := protocol.Header{Type: protocol.MsgBootProgress}

	// Must not panic.
	handleControlMessage(state, hdr, payload, [16]byte{}, nil, nil, &pendingAck, ackCh)
}
