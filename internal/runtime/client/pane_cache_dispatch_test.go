// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/pane_cache_dispatch_test.go
// Summary: Tests for PaneCache dispatch in the protocol handler.

package clientruntime

import (
	"sync/atomic"
	"testing"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/protocol"
)

// makeTestState builds a minimal clientState suitable for handler tests.
func makeTestState() *clientState {
	return &clientState{
		cache:       client.NewBufferCache(),
		paneCaches:  make(map[[16]byte]*client.PaneCache),
		themeValues: make(map[string]map[string]interface{}),
	}
}

func TestClientState_PaneCacheFor_CreatesOnDemand(t *testing.T) {
	state := makeTestState()
	var id [16]byte
	id[0] = 1

	pc := state.paneCacheFor(id)
	if pc == nil {
		t.Fatal("paneCacheFor returned nil")
	}

	// Second call must return the same instance.
	pc2 := state.paneCacheFor(id)
	if pc != pc2 {
		t.Error("paneCacheFor returned different instance on second call")
	}
}

func TestClientState_DropPaneCache(t *testing.T) {
	state := makeTestState()
	var id [16]byte
	id[0] = 7

	_ = state.paneCacheFor(id)
	state.dropPaneCache(id)

	state.paneCachesMu.RLock()
	_, exists := state.paneCaches[id]
	state.paneCachesMu.RUnlock()
	if exists {
		t.Error("dropPaneCache did not remove the entry")
	}
}

func TestClientState_BufferDeltaAppliesToPaneCache(t *testing.T) {
	state := makeTestState()

	var paneID [16]byte
	paneID[0] = 42

	// Synthesise a BufferDelta with RowBase=500 and one row of text.
	delta := protocol.BufferDelta{
		PaneID:  paneID,
		RowBase: 500,
		Styles: []protocol.StyleEntry{
			{AttrFlags: 0},
		},
		Rows: []protocol.RowDelta{
			{
				Row: 0,
				Spans: []protocol.CellSpan{
					{StartCol: 0, Text: "hello", StyleIndex: 0},
				},
			},
		},
	}

	payload, err := protocol.EncodeBufferDelta(delta)
	if err != nil {
		t.Fatalf("encode delta: %v", err)
	}

	hdr := protocol.Header{Type: protocol.MsgBufferDelta}
	var pendingAck atomic.Uint64
	ackCh := make(chan struct{}, 1)

	handleControlMessage(state, nil, hdr, payload, [16]byte{}, nil, nil, &pendingAck, ackCh)

	pc := state.paneCacheFor(paneID)
	row, ok := pc.RowAt(500)
	if !ok {
		t.Fatal("PaneCache.RowAt(500) returned false; delta was not applied")
	}
	if len(row) < 5 {
		t.Fatalf("row has %d cells, want >= 5", len(row))
	}
	want := "hello"
	for i, r := range []rune(want) {
		if row[i].Ch != r {
			t.Errorf("row[%d].Ch = %q, want %q", i, row[i].Ch, r)
		}
	}
}

func TestClientState_FetchRangeResponseAppliesToPaneCache(t *testing.T) {
	state := makeTestState()

	var paneID [16]byte
	paneID[0] = 99

	resp := protocol.FetchRangeResponse{
		RequestID: 1,
		PaneID:    paneID,
		Revision:  1,
		Flags:     protocol.FetchRangeNone,
		Styles: []protocol.StyleEntry{
			{AttrFlags: 0},
		},
		Rows: []protocol.LogicalRow{
			{
				GlobalIdx: 1000,
				Spans: []protocol.CellSpan{
					{StartCol: 0, Text: "world", StyleIndex: 0},
				},
			},
		},
	}

	payload, err := protocol.EncodeFetchRangeResponse(resp)
	if err != nil {
		t.Fatalf("encode fetch range response: %v", err)
	}

	hdr := protocol.Header{Type: protocol.MsgFetchRangeResponse}
	var pendingAck atomic.Uint64
	ackCh := make(chan struct{}, 1)

	handleControlMessage(state, nil, hdr, payload, [16]byte{}, nil, nil, &pendingAck, ackCh)

	pc := state.paneCacheFor(paneID)
	row, ok := pc.RowAt(1000)
	if !ok {
		t.Fatal("PaneCache.RowAt(1000) returned false; FetchRangeResponse was not applied")
	}
	if len(row) < 5 {
		t.Fatalf("row has %d cells, want >= 5", len(row))
	}
	want := "world"
	for i, r := range []rune(want) {
		if row[i].Ch != r {
			t.Errorf("row[%d].Ch = %q, want %q", i, row[i].Ch, r)
		}
	}
}

func TestClientState_SnapshotPrunesPaneCache(t *testing.T) {
	state := makeTestState()

	var id1, id2 [16]byte
	id1[0] = 1
	id2[0] = 2

	// Prime both caches.
	_ = state.paneCacheFor(id1)
	_ = state.paneCacheFor(id2)

	// Snapshot that contains only id1.
	snap := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{
			{PaneID: id1},
		},
	}
	payload, err := protocol.EncodeTreeSnapshot(snap)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}

	hdr := protocol.Header{Type: protocol.MsgTreeSnapshot}
	var pendingAck atomic.Uint64
	ackCh := make(chan struct{}, 1)

	handleControlMessage(state, nil, hdr, payload, [16]byte{}, nil, nil, &pendingAck, ackCh)

	state.paneCachesMu.RLock()
	_, has1 := state.paneCaches[id1]
	_, has2 := state.paneCaches[id2]
	state.paneCachesMu.RUnlock()

	if !has1 {
		t.Error("id1 should still be in paneCaches after snapshot")
	}
	if has2 {
		t.Error("id2 should have been pruned from paneCaches after snapshot")
	}
}
