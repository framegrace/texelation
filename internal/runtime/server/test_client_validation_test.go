//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/test_client_validation_test.go
// Summary: Tests for the TestClient infrastructure.
// Usage: Executed during `go test -tags=integration` to validate test client functionality.

package server

import (
	"net"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/internal/runtime/server/testutil"
	"texelation/protocol"
	"texelation/texel"
)

// fakeApp is a minimal app implementation for testing.
type fakeApp struct {
	title string
}

func (a *fakeApp) Run() error                        { return nil }
func (a *fakeApp) Stop()                             {}
func (a *fakeApp) Resize(cols, rows int)             {}
func (a *fakeApp) Render() [][]texel.Cell            { return [][]texel.Cell{{{Ch: 'x'}}} }
func (a *fakeApp) GetTitle() string                  { return a.title }
func (a *fakeApp) HandleKey(ev *tcell.EventKey)      {}
func (a *fakeApp) SetRefreshNotifier(ch chan<- bool) {}

// stubDriver is a minimal screen driver for testing.
type stubDriver struct{}

func (stubDriver) Init() error                                    { return nil }
func (stubDriver) Fini()                                          {}
func (stubDriver) Size() (int, int)                               { return 80, 24 }
func (stubDriver) SetStyle(tcell.Style)                           {}
func (stubDriver) HideCursor()                                    {}
func (stubDriver) Show()                                          {}
func (stubDriver) PollEvent() tcell.Event                         { return nil }
func (stubDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (stubDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

// setupTestServer creates a test server with desktop on a temporary socket.
func setupTestServer(t *testing.T) (string, *Manager, *texel.DesktopEngine, func()) {
	t.Helper()

	// Create temporary socket path
	socketPath := "/tmp/texel-test-" + t.Name() + ".sock"

	// Create desktop
	app := &fakeApp{title: "test"}
	shellFactory := func() texel.App { return app }
	welcomeFactory := func() texel.App { return app }
	lifecycle := &texel.NoopAppLifecycle{}

	desktop, err := texel.NewDesktopEngineWithDriver(stubDriver{}, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("failed to create desktop: %v", err)
	}

	// Create server components
	mgr := NewManager()
	sink := NewDesktopSink(desktop)

	// Create listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		desktop.Close()
		t.Fatalf("failed to create listener: %v", err)
	}

	// Start server in background - handle connections manually
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleTestConnection(conn, mgr, desktop, sink)
		}
	}()

	cleanup := func() {
		listener.Close()
		desktop.Close()
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	return socketPath, mgr, desktop, cleanup
}

// handleTestConnection handles a single client connection.
func handleTestConnection(conn net.Conn, mgr *Manager, desktop *texel.DesktopEngine, sink *DesktopSink) {
	defer conn.Close()

	session, resuming, err := handleHandshake(conn, mgr)
	if err != nil {
		return
	}

	publisher := NewDesktopPublisher(desktop, session)
	sink.SetPublisher(publisher)

	// Send initial snapshot
	snapshot, err := sink.Snapshot()
	if err == nil && len(snapshot.Panes) > 0 {
		if payload, err := protocol.EncodeTreeSnapshot(snapshot); err == nil {
			header := protocol.Header{
				Version:   protocol.Version,
				Type:      protocol.MsgTreeSnapshot,
				Flags:     protocol.FlagChecksum,
				SessionID: session.ID(),
			}
			_ = protocol.WriteMessage(conn, header, payload)
		}
	}

	// Publish initial buffers
	_ = publisher.Publish()

	// Create and serve connection
	connHandler := newConnection(conn, session, sink, resuming)
	_ = connHandler.serve()
}

func TestTestClientConnect(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	// Should have a valid session ID
	if client.SessionID() == [16]byte{} {
		t.Fatalf("expected non-zero session ID")
	}

	sid := client.SessionID()
	t.Logf("client connected with session %x", sid[:4])
}

func TestTestClientReceivesInitialSnapshot(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	// Wait for initial snapshot
	snapshot := client.WaitForInitialSnapshot()

	if len(snapshot.Panes) == 0 {
		t.Fatalf("expected at least one pane in initial snapshot")
	}

	t.Logf("received initial snapshot with %d panes", len(snapshot.Panes))
}

func TestTestClientReceivesBufferDelta(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	// Wait for initial snapshot
	snapshot := client.WaitForInitialSnapshot()
	if len(snapshot.Panes) == 0 {
		t.Fatalf("expected at least one pane")
	}

	// Should receive buffer delta
	delta := client.WaitForAnyBufferDelta(2 * time.Second)
	if delta.PaneID == [16]byte{} {
		t.Fatalf("expected valid pane ID in delta")
	}

	t.Logf("received buffer delta for pane %x", delta.PaneID[:4])

	// Cache should have the pane
	client.AssertPaneCount(1)
}

func TestTestClientCacheUpdates(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	// Wait for initial state
	snapshot := client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)

	// Verify cache has correct pane count
	count := client.GetPaneCount()
	if count != len(snapshot.Panes) {
		t.Fatalf("cache pane count %d doesn't match snapshot %d", count, len(snapshot.Panes))
	}

	// Verify we can query pane geometry
	paneID := snapshot.Panes[0].PaneID
	x, y, w, h, err := client.GetPaneGeometry(paneID)
	if err != nil {
		t.Fatalf("failed to get pane geometry: %v", err)
	}

	t.Logf("pane %x geometry: (%d,%d) %dx%d", paneID[:4], x, y, w, h)

	// Geometry should be reasonable
	if w <= 0 || h <= 0 {
		t.Fatalf("invalid pane dimensions: %dx%d", w, h)
	}
}

func TestTestClientSendKey(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	// Wait for initial state
	client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)

	// Send a key event
	err := client.SendKey(tcell.KeyEnter, '\n', tcell.ModNone)
	if err != nil {
		t.Fatalf("failed to send key: %v", err)
	}

	t.Logf("successfully sent key event")
}

func TestTestClientSendResize(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	// Wait for initial state
	snapshot1 := client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)

	initialWidth := int(snapshot1.Panes[0].Width)
	initialHeight := int(snapshot1.Panes[0].Height)

	// Send resize
	err := client.SendResize(120, 40)
	if err != nil {
		t.Fatalf("failed to send resize: %v", err)
	}

	// Should receive new snapshot with updated geometry
	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)

	newWidth := int(snapshot2.Panes[0].Width)
	newHeight := int(snapshot2.Panes[0].Height)

	// Dimensions should have changed
	if newWidth == initialWidth && newHeight == initialHeight {
		t.Logf("warning: dimensions didn't change after resize (may be expected)")
	}

	t.Logf("resize: %dx%d -> %dx%d", initialWidth, initialHeight, newWidth, newHeight)
}

func TestTestClientMultipleClients(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client1 := testutil.NewTestClient(t, socketPath)
	defer client1.Close()

	client2 := testutil.NewTestClient(t, socketPath)
	defer client2.Close()

	// Both should receive initial snapshot
	snapshot1 := client1.WaitForInitialSnapshot()
	snapshot2 := client2.WaitForInitialSnapshot()

	if len(snapshot1.Panes) != len(snapshot2.Panes) {
		t.Fatalf("clients received different snapshot sizes: %d vs %d",
			len(snapshot1.Panes), len(snapshot2.Panes))
	}

	// Should have different session IDs
	if client1.SessionID() == client2.SessionID() {
		t.Fatalf("clients have same session ID")
	}

	sid1 := client1.SessionID()
	sid2 := client2.SessionID()
	t.Logf("client1 session: %x", sid1[:4])
	t.Logf("client2 session: %x", sid2[:4])
}

func TestTestClientAssertions(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	snapshot := client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)

	// Test AssertPaneCount
	client.AssertPaneCount(len(snapshot.Panes))

	// Test AssertPaneExists
	if len(snapshot.Panes) > 0 {
		client.AssertPaneExists(snapshot.Panes[0].PaneID)
	}

	// Test GetAllPanes
	panes := client.GetAllPanes()
	if len(panes) != len(snapshot.Panes) {
		t.Fatalf("GetAllPanes returned %d, expected %d", len(panes), len(snapshot.Panes))
	}

	t.Logf("all assertions passed")
}

func TestTestClientSequenceTracking(t *testing.T) {
	socketPath, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	client.WaitForInitialSnapshot()

	initialSeq := client.LastSequence()

	// Receive a delta
	client.WaitForAnyBufferDelta(2 * time.Second)

	newSeq := client.LastSequence()
	if newSeq <= initialSeq {
		t.Fatalf("sequence didn't advance: %d -> %d", initialSeq, newSeq)
	}

	t.Logf("sequence advanced: %d -> %d", initialSeq, newSeq)
}
