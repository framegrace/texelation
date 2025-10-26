// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/testutil/test_client.go
// Summary: Provides a test client for integration testing client/server interactions.
// Usage: Used in integration tests to simulate client behavior and verify protocol flows.
// Notes: Wraps connection handling and provides convenient assertion methods.

package testutil

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/protocol"
)

// TestClient wraps a client connection and provides convenient methods for
// testing client/server interactions.
type TestClient struct {
	t            *testing.T
	conn         net.Conn
	cache        *client.BufferCache
	sessionID    [16]byte
	lastSequence uint64

	// Channels for routing messages
	snapshots    chan protocol.TreeSnapshot
	deltas       chan protocol.BufferDelta
	paneState    chan protocol.PaneState
	stateUpdates chan protocol.StateUpdate
	errors       chan error

	// Write protection
	writeMu sync.Mutex

	// Lifecycle
	stopCh chan struct{}
	doneCh chan struct{}

	// Tracking
	snapshotCount int
	deltaCount    int
}

// NewTestClient creates a new test client and connects to the server at socketPath.
// It performs the full handshake and starts the read loop.
func NewTestClient(t *testing.T, socketPath string) *TestClient {
	t.Helper()

	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	return NewTestClientWithConn(t, conn)
}

// NewTestClientWithConn wraps an existing connection (e.g. net.Pipe) with the test client.
func NewTestClientWithConn(t *testing.T, conn net.Conn) *TestClient {
	t.Helper()

	tc := &TestClient{
		t:            t,
		conn:         conn,
		cache:        client.NewBufferCache(),
		snapshots:    make(chan protocol.TreeSnapshot, 10),
		deltas:       make(chan protocol.BufferDelta, 100),
		paneState:    make(chan protocol.PaneState, 10),
		stateUpdates: make(chan protocol.StateUpdate, 10),
		errors:       make(chan error, 10),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}

	// Perform handshake
	if err := tc.handshake(); err != nil {
		conn.Close()
		t.Fatalf("handshake failed: %v", err)
	}

	// Start read loop
	go tc.readLoop()

	return tc
}

// handshake performs the protocol handshake (hello, welcome, connect).
func (tc *TestClient) handshake() error {
	// Send hello
	helloPayload, err := protocol.EncodeHello(protocol.Hello{ClientName: "test-client"})
	if err != nil {
		return fmt.Errorf("encode hello: %w", err)
	}
	if err := protocol.WriteMessage(tc.conn, protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgHello,
		Flags:   protocol.FlagChecksum,
	}, helloPayload); err != nil {
		return fmt.Errorf("write hello: %w", err)
	}

	// Read welcome
	hdr, _, err := protocol.ReadMessage(tc.conn)
	if err != nil {
		return fmt.Errorf("read welcome: %w", err)
	}
	if hdr.Type != protocol.MsgWelcome {
		return fmt.Errorf("expected welcome, got %v", hdr.Type)
	}

	// Send connect request
	connectPayload, err := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	if err != nil {
		return fmt.Errorf("encode connect: %w", err)
	}
	if err := protocol.WriteMessage(tc.conn, protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgConnectRequest,
		Flags:   protocol.FlagChecksum,
	}, connectPayload); err != nil {
		return fmt.Errorf("write connect: %w", err)
	}

	// Read connect accept
	hdr, payload, err := protocol.ReadMessage(tc.conn)
	if err != nil {
		return fmt.Errorf("read connect accept: %w", err)
	}
	if hdr.Type != protocol.MsgConnectAccept {
		return fmt.Errorf("expected connect accept, got %v", hdr.Type)
	}

	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		return fmt.Errorf("decode connect accept: %w", err)
	}

	tc.sessionID = accept.SessionID
	return nil
}

// readLoop reads messages from the connection and routes them to appropriate channels.
func (tc *TestClient) readLoop() {
	defer close(tc.doneCh)

	for {
		select {
		case <-tc.stopCh:
			return
		default:
		}

		hdr, payload, err := protocol.ReadMessage(tc.conn)
		if err != nil {
			select {
			case tc.errors <- err:
			case <-tc.stopCh:
			}
			return
		}

		if err := tc.handleMessage(hdr, payload); err != nil {
			select {
			case tc.errors <- err:
			case <-tc.stopCh:
			}
			return
		}
	}
}

// handleMessage processes a single message and routes it appropriately.
func (tc *TestClient) handleMessage(hdr protocol.Header, payload []byte) error {
	switch hdr.Type {
	case protocol.MsgTreeSnapshot:
		snapshot, err := protocol.DecodeTreeSnapshot(payload)
		if err != nil {
			return fmt.Errorf("decode snapshot: %w", err)
		}
		tc.cache.ApplySnapshot(snapshot)
		tc.snapshotCount++
		select {
		case tc.snapshots <- snapshot:
		case <-tc.stopCh:
		}

	case protocol.MsgBufferDelta:
		delta, err := protocol.DecodeBufferDelta(payload)
		if err != nil {
			return fmt.Errorf("decode delta: %w", err)
		}
		tc.cache.ApplyDelta(delta)
		tc.deltaCount++
		tc.lastSequence = hdr.Sequence

		// Send ACK
		if err := tc.sendAck(hdr.Sequence); err != nil {
			return fmt.Errorf("send ack: %w", err)
		}

		select {
		case tc.deltas <- delta:
		case <-tc.stopCh:
		}

	case protocol.MsgPaneState:
		state, err := protocol.DecodePaneState(payload)
		if err != nil {
			return fmt.Errorf("decode pane state: %w", err)
		}
		active := state.Flags&protocol.PaneStateActive != 0
		resizing := state.Flags&protocol.PaneStateResizing != 0
		tc.cache.SetPaneFlags(state.PaneID, active, resizing, state.ZOrder)

		select {
		case tc.paneState <- state:
		case <-tc.stopCh:
		}

	case protocol.MsgPing:
		ping, err := protocol.DecodePing(payload)
		if err != nil {
			return fmt.Errorf("decode ping: %w", err)
		}
		pongPayload, err := protocol.EncodePong(protocol.Pong{Timestamp: ping.Timestamp})
		if err != nil {
			return fmt.Errorf("encode pong: %w", err)
		}
		if err := tc.writeControl(protocol.MsgPong, pongPayload); err != nil {
			return fmt.Errorf("write pong: %w", err)
		}

	case protocol.MsgStateUpdate:
		stateUpdate, err := protocol.DecodeStateUpdate(payload)
		if err != nil {
			return fmt.Errorf("decode state update: %w", err)
		}
		select {
		case tc.stateUpdates <- stateUpdate:
		case <-tc.stopCh:
		}

	case protocol.MsgPaneFocus, protocol.MsgThemeUpdate, protocol.MsgClipboardSet:
		// Ignore these for now - can add channels later if needed

	default:
		// Ignore other message types
	}

	return nil
}

// sendAck sends a buffer acknowledgment for the given sequence number.
func (tc *TestClient) sendAck(sequence uint64) error {
	payload, err := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: sequence})
	if err != nil {
		return err
	}
	return tc.writeControl(protocol.MsgBufferAck, payload)
}

// writeControl writes a control message with proper locking.
func (tc *TestClient) writeControl(msgType protocol.MessageType, payload []byte) error {
	header := protocol.Header{
		Version:   protocol.Version,
		Type:      msgType,
		Flags:     protocol.FlagChecksum,
		SessionID: tc.sessionID,
	}
	tc.writeMu.Lock()
	defer tc.writeMu.Unlock()
	return protocol.WriteMessage(tc.conn, header, payload)
}

// SendKey sends a key event to the server.
func (tc *TestClient) SendKey(key tcell.Key, ch rune, mod tcell.ModMask) error {
	tc.t.Helper()

	payload, err := protocol.EncodeKeyEvent(protocol.KeyEvent{
		KeyCode:   uint32(key),
		RuneValue: ch,
		Modifiers: uint16(mod),
	})
	if err != nil {
		return fmt.Errorf("encode key event: %w", err)
	}

	return tc.writeControl(protocol.MsgKeyEvent, payload)
}

// SendResize sends a resize event to the server.
func (tc *TestClient) SendResize(cols, rows int) error {
	tc.t.Helper()

	payload, err := protocol.EncodeResize(protocol.Resize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return fmt.Errorf("encode resize: %w", err)
	}

	return tc.writeControl(protocol.MsgResize, payload)
}

// WaitForInitialSnapshot waits for the first snapshot after connection.
// Returns the snapshot or fails the test on timeout.
func (tc *TestClient) WaitForInitialSnapshot() protocol.TreeSnapshot {
	tc.t.Helper()
	return tc.WaitForTreeSnapshot(5 * time.Second)
}

// WaitForTreeSnapshot waits for a TreeSnapshot message with the given timeout.
// Returns the snapshot or fails the test on timeout.
func (tc *TestClient) WaitForTreeSnapshot(timeout time.Duration) protocol.TreeSnapshot {
	tc.t.Helper()

	select {
	case snapshot := <-tc.snapshots:
		return snapshot
	case err := <-tc.errors:
		tc.t.Fatalf("error while waiting for snapshot: %v", err)
	case <-time.After(timeout):
		tc.t.Fatalf("timeout waiting for tree snapshot after %v", timeout)
	}
	return protocol.TreeSnapshot{}
}

// TryGetTreeSnapshot attempts to get a TreeSnapshot with the given timeout.
// Returns the snapshot and true if received, or empty snapshot and false on timeout.
// Does not fail the test on timeout.
func (tc *TestClient) TryGetTreeSnapshot(timeout time.Duration) (protocol.TreeSnapshot, bool) {
	tc.t.Helper()

	select {
	case snapshot := <-tc.snapshots:
		return snapshot, true
	case err := <-tc.errors:
		tc.t.Fatalf("error while waiting for snapshot: %v", err)
	case <-time.After(timeout):
		return protocol.TreeSnapshot{}, false
	}
	return protocol.TreeSnapshot{}, false
}

// WaitForBufferDelta waits for a BufferDelta for the specified pane.
// Returns the delta or fails the test on timeout.
func (tc *TestClient) WaitForBufferDelta(paneID [16]byte, timeout time.Duration) protocol.BufferDelta {
	tc.t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			tc.t.Fatalf("timeout waiting for buffer delta for pane %x after %v", paneID[:4], timeout)
		}

		select {
		case delta := <-tc.deltas:
			if delta.PaneID == paneID {
				return delta
			}
			// Not the pane we're looking for, keep waiting
		case err := <-tc.errors:
			tc.t.Fatalf("error while waiting for delta: %v", err)
		case <-time.After(remaining):
			tc.t.Fatalf("timeout waiting for buffer delta for pane %x after %v", paneID[:4], timeout)
		}
	}
}

// WaitForAnyBufferDelta waits for any BufferDelta message.
// Returns the delta or fails the test on timeout.
func (tc *TestClient) WaitForAnyBufferDelta(timeout time.Duration) protocol.BufferDelta {
	tc.t.Helper()

	select {
	case delta := <-tc.deltas:
		return delta
	case err := <-tc.errors:
		tc.t.Fatalf("error while waiting for delta: %v", err)
	case <-time.After(timeout):
		tc.t.Fatalf("timeout waiting for any buffer delta after %v", timeout)
	}
	return protocol.BufferDelta{}
}

// ExpectNoDeltaWithin asserts that no buffer delta arrives within the timeout.
// Useful for validating that a single user action only produces one frame.
func (tc *TestClient) ExpectNoDeltaWithin(timeout time.Duration) {
	tc.t.Helper()

	select {
	case delta := <-tc.deltas:
		tc.t.Fatalf("expected no buffer delta within %v, but received pane %x rev %d", timeout, delta.PaneID[:4], delta.Revision)
	case err := <-tc.errors:
		tc.t.Fatalf("error while waiting for absence of delta: %v", err)
	case <-time.After(timeout):
		// Success: no delta observed within the interval.
	}
}

// DrainSnapshots drains any pending snapshots from the channel.
func (tc *TestClient) DrainSnapshots() {
	for {
		select {
		case <-tc.snapshots:
		default:
			return
		}
	}
}

// DrainDeltas drains any pending deltas from the channel.
func (tc *TestClient) DrainDeltas() {
	for {
		select {
		case <-tc.deltas:
		default:
			return
		}
	}
}

// AssertPaneCount checks that the cache has the expected number of panes.
func (tc *TestClient) AssertPaneCount(expected int) {
	tc.t.Helper()

	count := 0
	tc.cache.ForEachPaneSorted(func(p *client.PaneState) {
		count++
	})

	if count != expected {
		tc.t.Fatalf("expected %d panes, got %d", expected, count)
	}
}

// GetPaneCount returns the current number of panes in the cache.
func (tc *TestClient) GetPaneCount() int {
	tc.t.Helper()

	count := 0
	tc.cache.ForEachPaneSorted(func(p *client.PaneState) {
		count++
	})
	return count
}

// GetPaneGeometry returns the geometry of the specified pane.
// Returns an error if the pane is not found.
func (tc *TestClient) GetPaneGeometry(paneID [16]byte) (x, y, w, h int, err error) {
	tc.t.Helper()

	found := false
	tc.cache.ForEachPaneSorted(func(p *client.PaneState) {
		if p.ID == paneID {
			x = p.Rect.X
			y = p.Rect.Y
			w = p.Rect.Width
			h = p.Rect.Height
			found = true
		}
	})

	if !found {
		return 0, 0, 0, 0, fmt.Errorf("pane %x not found", paneID[:4])
	}
	return x, y, w, h, nil
}

// GetAllPanes returns a slice of all pane IDs currently in the cache.
func (tc *TestClient) GetAllPanes() [][16]byte {
	tc.t.Helper()

	var panes [][16]byte
	tc.cache.ForEachPaneSorted(func(p *client.PaneState) {
		panes = append(panes, p.ID)
	})
	return panes
}

// AssertPaneExists checks that a pane with the given ID exists.
func (tc *TestClient) AssertPaneExists(paneID [16]byte) {
	tc.t.Helper()

	found := false
	tc.cache.ForEachPaneSorted(func(p *client.PaneState) {
		if p.ID == paneID {
			found = true
		}
	})

	if !found {
		tc.t.Fatalf("expected pane %x to exist, but it doesn't", paneID[:4])
	}
}

// SessionID returns the current session ID.
func (tc *TestClient) SessionID() [16]byte {
	return tc.sessionID
}

// LastSequence returns the last acknowledged sequence number.
func (tc *TestClient) LastSequence() uint64 {
	return tc.lastSequence
}

// SnapshotCount returns the number of snapshots received.
func (tc *TestClient) SnapshotCount() int {
	return tc.snapshotCount
}

// DeltaCount returns the number of deltas received.
func (tc *TestClient) DeltaCount() int {
	return tc.deltaCount
}

// WaitForStateUpdate waits for a StateUpdate message with the given timeout.
// Returns the state update or fails the test on timeout.
func (tc *TestClient) WaitForStateUpdate(timeout time.Duration) protocol.StateUpdate {
	tc.t.Helper()

	select {
	case stateUpdate := <-tc.stateUpdates:
		return stateUpdate
	case err := <-tc.errors:
		tc.t.Fatalf("error while waiting for state update: %v", err)
	case <-time.After(timeout):
		tc.t.Fatalf("timeout waiting for state update after %v", timeout)
	}
	return protocol.StateUpdate{}
}

// DrainStateUpdates drains any pending state updates from the channel.
func (tc *TestClient) DrainStateUpdates() {
	for {
		select {
		case <-tc.stateUpdates:
		default:
			return
		}
	}
}

// Close stops the read loop and closes the connection.
func (tc *TestClient) Close() error {
	close(tc.stopCh)
	err := tc.conn.Close()
	<-tc.doneCh // Wait for read loop to finish
	return err
}
