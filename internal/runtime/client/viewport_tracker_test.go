// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/viewport_tracker_test.go
// Summary: Tests for the per-pane viewport tracker and FlushFrame emission.

package clientruntime

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/protocol"
)

// --------------------------------------------------------------------------
// testConn: minimal net.Conn that records writes to a buffer.
// --------------------------------------------------------------------------

type testConn struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *testConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(b)
}
func (c *testConn) Read(_ []byte) (int, error)         { return 0, io.EOF }
func (c *testConn) Close() error                       { return nil }
func (c *testConn) LocalAddr() net.Addr                { return nil }
func (c *testConn) RemoteAddr() net.Addr               { return nil }
func (c *testConn) SetDeadline(_ time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(_ time.Time) error { return nil }

// bytes returns a copy of all data written so far.
func (c *testConn) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := make([]byte, c.buf.Len())
	copy(b, c.buf.Bytes())
	return b
}

// readMessages decodes all protocol frames written to the testConn.
func (c *testConn) readMessages() []protocol.Header {
	data := c.bytes()
	// Parse manually: each frame starts with magic(4)+header(36 bytes = 40 total).
	// See protocol.ReadMessage for the wire format.
	var headers []protocol.Header
	for len(data) >= 40 {
		// Skip magic (4 bytes).
		if binary.LittleEndian.Uint32(data[:4]) != 0x54584c01 {
			break
		}
		data = data[4:]
		if len(data) < 36 {
			break
		}
		hdr := protocol.Header{}
		hdr.Version = data[0]
		hdr.Type = protocol.MessageType(data[1])
		hdr.Flags = data[2]
		hdr.Reserved = data[3]
		copy(hdr.SessionID[:], data[4:20])
		hdr.Sequence = binary.LittleEndian.Uint64(data[20:28])
		hdr.PayloadLen = binary.LittleEndian.Uint32(data[28:32])
		hdr.Checksum = binary.LittleEndian.Uint32(data[32:36])
		data = data[36:]
		payloadLen := int(hdr.PayloadLen)
		if len(data) < payloadLen {
			break
		}
		data = data[payloadLen:]
		headers = append(headers, hdr)
	}
	return headers
}

// countType returns how many messages of the given type were written.
func (c *testConn) countType(msgType protocol.MessageType) int {
	n := 0
	for _, h := range c.readMessages() {
		if h.Type == msgType {
			n++
		}
	}
	return n
}

// --------------------------------------------------------------------------
// Helpers to build test state and snapshots.
// --------------------------------------------------------------------------

func makeStateWithViewports() *clientState {
	return &clientState{
		cache:       client.NewBufferCache(),
		paneCaches:  make(map[[16]byte]*client.PaneCache),
		themeValues: make(map[string]map[string]interface{}),
		viewports:   newViewportTrackers(),
	}
}

func paneID(b byte) [16]byte {
	var id [16]byte
	id[0] = b
	return id
}

func makeTreeSnapshot(id [16]byte, width, height int32) protocol.TreeSnapshot {
	return protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{
			{
				PaneID: id,
				Width:  width,
				Height: height,
			},
		},
	}
}

// --------------------------------------------------------------------------
// Part 5, item 1: TestViewportTracker_InitializesFromSnapshot
// --------------------------------------------------------------------------

func TestViewportTracker_InitializesFromSnapshot(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(1), 80, 24)
	state.onTreeSnapshot(snap)

	vc, ok := state.paneViewportFor(paneID(1))
	if !ok {
		t.Fatal("paneViewportFor returned false after snapshot")
	}
	if vc.Rows != 24 {
		t.Errorf("Rows = %d, want 24", vc.Rows)
	}
	if vc.Cols != 80 {
		t.Errorf("Cols = %d, want 80", vc.Cols)
	}
	if !vc.AutoFollow {
		t.Error("AutoFollow should be true on init")
	}
	if vc.AltScreen {
		t.Error("AltScreen should be false on init")
	}
	if vc.ViewTopIdx != 0 {
		t.Errorf("ViewTopIdx = %d, want 0", vc.ViewTopIdx)
	}
	// ViewBottomIdx should be Rows-1 = 23.
	if vc.ViewBottomIdx != 23 {
		t.Errorf("ViewBottomIdx = %d, want 23", vc.ViewBottomIdx)
	}

	// The tracker should be dirty so FlushFrame will emit.
	vp := state.viewports.get(paneID(1))
	vp.mu.Lock()
	dirty := vp.dirty
	vp.mu.Unlock()
	if !dirty {
		t.Error("dirty should be true after snapshot initialisation")
	}
}

// --------------------------------------------------------------------------
// Part 5, item 2: TestViewportTracker_AdvancesOnAutoFollowDelta
// --------------------------------------------------------------------------

func TestViewportTracker_AdvancesOnAutoFollowDelta(t *testing.T) {
	state := makeStateWithViewports()
	// Initialise pane with height=24 so rows is set.
	snap := makeTreeSnapshot(paneID(2), 80, 24)
	state.onTreeSnapshot(snap)

	// Clear dirty so we can detect the advance.
	vp := state.viewports.get(paneID(2))
	vp.mu.Lock()
	vp.dirty = false
	vp.mu.Unlock()

	// Delta with gid = RowBase + Row = 1000 + 0 = 1000, higher than knownBottomGid=0.
	delta := protocol.BufferDelta{
		PaneID:  paneID(2),
		RowBase: 1000,
		Rows: []protocol.RowDelta{
			{Row: 0},
			{Row: 5},
		},
	}
	state.onBufferDelta(delta)

	vc, ok := state.paneViewportFor(paneID(2))
	if !ok {
		t.Fatal("paneViewportFor returned false")
	}
	// maxGid = 1000+5 = 1005; ViewBottomIdx should be 1005.
	if vc.ViewBottomIdx != 1005 {
		t.Errorf("ViewBottomIdx = %d, want 1005", vc.ViewBottomIdx)
	}
	// ViewTopIdx = 1005 - (24-1) = 982.
	wantTop := int64(1005 - 23)
	if vc.ViewTopIdx != wantTop {
		t.Errorf("ViewTopIdx = %d, want %d", vc.ViewTopIdx, wantTop)
	}

	vp.mu.Lock()
	dirty := vp.dirty
	vp.mu.Unlock()
	if !dirty {
		t.Error("dirty should be true after AutoFollow advance")
	}
}

// --------------------------------------------------------------------------
// Regression: I3 — AutoFollow must advance even for the first delta at gid=0.
// --------------------------------------------------------------------------

func TestViewportTracker_AutoFollowAdvancesFromGidZero(t *testing.T) {
	state := makeStateWithViewports()
	// Initialise a single pane from a tree snapshot (Rows=24).
	id := paneID(0xA0)
	state.onTreeSnapshot(makeTreeSnapshot(id, 80, 24))

	// Drain the initial dirty state via flushFrame so subsequent dirty
	// assertions are clean. Use a testConn so nothing writes to a real socket.
	conn := &testConn{}
	var writeMu sync.Mutex
	flushFrame(state, conn, &writeMu, [16]byte{})

	// First-ever BufferDelta at gid=0 on the main screen.
	delta := protocol.BufferDelta{
		PaneID:  id,
		RowBase: 0,
		Rows: []protocol.RowDelta{
			{Row: 0},
		},
	}
	state.onBufferDelta(delta)

	vc, ok := state.paneViewportFor(id)
	if !ok {
		t.Fatal("paneViewportFor returned false after delta")
	}
	// With maxGid=0 and Rows=24: bottom=0, top=max(0, 0-23)=0.
	if vc.ViewBottomIdx != 0 {
		t.Errorf("ViewBottomIdx = %d, want 0", vc.ViewBottomIdx)
	}
	if vc.ViewTopIdx != 0 {
		t.Errorf("ViewTopIdx = %d, want 0", vc.ViewTopIdx)
	}

	// Tracker must be marked dirty so flushFrame will emit a ViewportUpdate.
	vp := state.viewports.get(id)
	vp.mu.Lock()
	dirty := vp.dirty
	vp.mu.Unlock()
	if !dirty {
		t.Error("dirty should be true after AutoFollow advance from gid=0")
	}
}

// --------------------------------------------------------------------------
// Part 5, item 3: TestViewportTracker_DetectsAltScreenFromDelta
// --------------------------------------------------------------------------

func TestViewportTracker_DetectsAltScreenFromDelta(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(3), 80, 24)
	state.onTreeSnapshot(snap)

	// Clear dirty.
	vp := state.viewports.get(paneID(3))
	vp.mu.Lock()
	vp.dirty = false
	vp.mu.Unlock()

	// Alt-screen delta.
	delta := protocol.BufferDelta{
		PaneID: paneID(3),
		Flags:  protocol.BufferDeltaAltScreen,
	}
	state.onBufferDelta(delta)

	vc, ok := state.paneViewportFor(paneID(3))
	if !ok {
		t.Fatal("paneViewportFor returned false")
	}
	if !vc.AltScreen {
		t.Error("AltScreen should be true after alt-screen delta")
	}

	vp.mu.Lock()
	dirty := vp.dirty
	vp.mu.Unlock()
	if !dirty {
		t.Error("dirty should be true after alt-screen transition")
	}

	// Transition back to main screen.
	vp.mu.Lock()
	vp.dirty = false
	vp.mu.Unlock()

	mainDelta := protocol.BufferDelta{
		PaneID: paneID(3),
		Flags:  0, // main screen
	}
	state.onBufferDelta(mainDelta)

	vc, _ = state.paneViewportFor(paneID(3))
	if vc.AltScreen {
		t.Error("AltScreen should be false after main-screen delta")
	}
	vp.mu.Lock()
	dirty = vp.dirty
	vp.mu.Unlock()
	if !dirty {
		t.Error("dirty should be true after alt→main transition")
	}
}

// --------------------------------------------------------------------------
// Part 5, item 4: TestFlushFrame_SendsViewportUpdate
// --------------------------------------------------------------------------

func TestFlushFrame_SendsViewportUpdate(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(4), 80, 24)
	state.onTreeSnapshot(snap)

	conn := &testConn{}
	var writeMu sync.Mutex
	flushFrame(state, conn, &writeMu, [16]byte{})

	n := conn.countType(protocol.MsgViewportUpdate)
	if n != 1 {
		t.Errorf("expected 1 MsgViewportUpdate, got %d", n)
	}
}

// --------------------------------------------------------------------------
// Part 5, item 5: TestFlushFrame_CoalescesMultipleChangesInFrame
// --------------------------------------------------------------------------

func TestFlushFrame_CoalescesMultipleChangesInFrame(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(5), 80, 24)
	state.onTreeSnapshot(snap)

	// Simulate multiple changes before FlushFrame.
	vp := state.viewports.get(paneID(5))
	for i := 0; i < 3; i++ {
		vp.mu.Lock()
		vp.ViewBottomIdx = int64(100 + i*10)
		vp.ViewTopIdx = vp.ViewBottomIdx - 23
		vp.dirty = true
		vp.mu.Unlock()
	}

	conn := &testConn{}
	var writeMu sync.Mutex
	flushFrame(state, conn, &writeMu, [16]byte{})

	// Must only emit one update per pane per frame (dirty was cleared once).
	n := conn.countType(protocol.MsgViewportUpdate)
	if n != 1 {
		t.Errorf("expected exactly 1 MsgViewportUpdate per frame, got %d", n)
	}
}

// --------------------------------------------------------------------------
// Part 5, item 6: TestFlushFrame_IssuesFetchForMissingRows
// --------------------------------------------------------------------------

func TestFlushFrame_IssuesFetchForMissingRows(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(6), 80, 24)
	state.onTreeSnapshot(snap)

	// Manually set viewport to a range not in cache.
	vp := state.viewports.get(paneID(6))
	vp.mu.Lock()
	vp.ViewTopIdx = 1000
	vp.ViewBottomIdx = 1023
	vp.Rows = 24
	vp.Cols = 80
	vp.dirty = true
	vp.mu.Unlock()

	conn := &testConn{}
	var writeMu sync.Mutex
	flushFrame(state, conn, &writeMu, [16]byte{})

	n := conn.countType(protocol.MsgFetchRange)
	if n != 1 {
		t.Errorf("expected 1 MsgFetchRange, got %d", n)
	}
}

// --------------------------------------------------------------------------
// Part 5, item 7: TestFlushFrame_AtMostOneInflightFetchPerPane
// --------------------------------------------------------------------------

func TestFlushFrame_AtMostOneInflightFetchPerPane(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(7), 80, 24)
	state.onTreeSnapshot(snap)

	// Pre-set inflight to true.
	vp := state.viewports.get(paneID(7))
	vp.mu.Lock()
	vp.ViewTopIdx = 1000
	vp.ViewBottomIdx = 1023
	vp.Rows = 24
	vp.Cols = 80
	vp.dirty = true
	vp.inflightFetch = true
	vp.mu.Unlock()

	conn := &testConn{}
	var writeMu sync.Mutex
	flushFrame(state, conn, &writeMu, [16]byte{})

	// No new MsgFetchRange should be sent.
	n := conn.countType(protocol.MsgFetchRange)
	if n != 0 {
		t.Errorf("expected 0 MsgFetchRange with inflight=true, got %d", n)
	}

	// But pendingFetch should have been populated.
	vp.mu.Lock()
	pending := vp.pendingFetch
	vp.mu.Unlock()
	if pending == nil {
		t.Error("pendingFetch should be set when inflight=true and rows are missing")
	}
}

// --------------------------------------------------------------------------
// Part 5, item 8: TestFlushFrame_EmitsPendingFetchOnResponse
// --------------------------------------------------------------------------

func TestFlushFrame_EmitsPendingFetchOnResponse(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(8), 80, 24)
	state.onTreeSnapshot(snap)

	// Pre-set pending fetch (inflight is already false).
	vp := state.viewports.get(paneID(8))
	vp.mu.Lock()
	pf := [2]int64{2000, 2024}
	vp.pendingFetch = &pf
	vp.inflightFetch = true // simulate: response arrives now.
	vp.mu.Unlock()

	conn := &testConn{}
	var writeMu sync.Mutex

	// Simulate the onFetchRangeResponse path.
	lo, hi, send := state.onFetchRangeResponse(paneID(8))
	if !send {
		t.Fatal("onFetchRangeResponse should return send=true when pendingFetch is set")
	}
	if lo != 2000 || hi != 2024 {
		t.Errorf("pending fetch range = [%d,%d), want [2000,2024)", lo, hi)
	}

	// Confirm inflight is now true and pending is cleared.
	vp.mu.Lock()
	inflight := vp.inflightFetch
	pending := vp.pendingFetch
	vp.mu.Unlock()
	if !inflight {
		t.Error("inflightFetch should be true after emitting pending fetch")
	}
	if pending != nil {
		t.Error("pendingFetch should be nil after being consumed")
	}

	// Verify we can send the fetch range.
	sendFetchRange(state, conn, &writeMu, [16]byte{}, paneID(8), lo, hi)
	n := conn.countType(protocol.MsgFetchRange)
	if n != 1 {
		t.Errorf("expected 1 MsgFetchRange after consuming pending, got %d", n)
	}
}

// --------------------------------------------------------------------------
// Additional: TestViewportTracker_GeometryChangeMarksDirty
// --------------------------------------------------------------------------

func TestViewportTracker_GeometryChangeMarksDirty(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(9), 80, 24)
	state.onTreeSnapshot(snap)

	// Clear dirty.
	vp := state.viewports.get(paneID(9))
	vp.mu.Lock()
	vp.dirty = false
	vp.mu.Unlock()

	// Second snapshot with different dimensions.
	snap2 := makeTreeSnapshot(paneID(9), 100, 30)
	state.onTreeSnapshot(snap2)

	vp.mu.Lock()
	dirty := vp.dirty
	rows := vp.Rows
	cols := vp.Cols
	vp.mu.Unlock()

	if !dirty {
		t.Error("dirty should be true after geometry change")
	}
	if rows != 30 {
		t.Errorf("Rows = %d, want 30", rows)
	}
	if cols != 100 {
		t.Errorf("Cols = %d, want 100", cols)
	}
}

// --------------------------------------------------------------------------
// Additional: TestViewportTracker_PruneRemovesStalePane
// --------------------------------------------------------------------------

func TestViewportTracker_PruneRemovesStalePane(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(10), 80, 24)
	state.onTreeSnapshot(snap)

	// New snapshot without paneID(10).
	snap2 := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{
			{PaneID: paneID(11), Width: 80, Height: 24},
		},
	}
	state.onTreeSnapshot(snap2)

	state.viewports.mu.RLock()
	_, has10 := state.viewports.panes[paneID(10)]
	_, has11 := state.viewports.panes[paneID(11)]
	state.viewports.mu.RUnlock()

	if has10 {
		t.Error("paneID(10) should have been pruned")
	}
	if !has11 {
		t.Error("paneID(11) should be present")
	}
}

// --------------------------------------------------------------------------
// Additional: TestFlushFrame_NilConnIsNoop
// --------------------------------------------------------------------------

func TestFlushFrame_NilConnIsNoop(t *testing.T) {
	state := makeStateWithViewports()
	snap := makeTreeSnapshot(paneID(12), 80, 24)
	state.onTreeSnapshot(snap)

	var writeMu sync.Mutex
	// Must not panic.
	flushFrame(state, nil, &writeMu, [16]byte{})
}

// --------------------------------------------------------------------------
// Additional: TestFlushFrame_ZeroDimSkipped
// --------------------------------------------------------------------------

func TestFlushFrame_ZeroDimSkipped(t *testing.T) {
	state := makeStateWithViewports()

	// Manually insert a tracker with zero dims (pathological).
	vp := state.viewports.get(paneID(13))
	vp.mu.Lock()
	vp.dirty = true
	vp.Rows = 0
	vp.Cols = 0
	vp.mu.Unlock()

	conn := &testConn{}
	var writeMu sync.Mutex
	flushFrame(state, conn, &writeMu, [16]byte{})

	n := conn.countType(protocol.MsgViewportUpdate)
	if n != 0 {
		t.Errorf("expected 0 MsgViewportUpdate for zero-dim pane, got %d", n)
	}
}
