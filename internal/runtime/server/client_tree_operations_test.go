//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/client_tree_operations_test.go
// Summary: Tests client behavior for tree operations (splits, closes, resizing).
// Usage: Executed during `go test -tags=integration` to validate client/server tree synchronization.
// Notes: Phase 2 of integration test gap analysis - basic tree operations.

package server

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/internal/runtime/server/testutil"
	"texelation/protocol"
	"texelation/texel"
)

// setupTreeTestServer creates a test server with desktop and shell factory.
func setupTreeTestServer(t *testing.T) (string, *Manager, *texel.DesktopEngine, func()) {
	t.Helper()

	socketPath := "/tmp/texel-tree-test-" + t.Name() + ".sock"

	// Create a simple app that returns content
	appCount := 0
	shellFactory := func() texel.App {
		appCount++
		return &treeTestApp{id: appCount}
	}
	welcomeFactory := func() texel.App {
		return &treeTestApp{id: 0}
	}
	lifecycle := &texel.NoopAppLifecycle{}

	desktop, err := texel.NewDesktopEngineWithDriver(treeTestDriver{}, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("failed to create desktop: %v", err)
	}

	mgr := NewManager()
	sink := NewDesktopSink(desktop)

	// Remove socket if it exists
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		desktop.Close()
		t.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleTreeTestConnection(conn, mgr, desktop, sink)
		}
	}()

	cleanup := func() {
		listener.Close()
		desktop.Close()
		_ = os.Remove(socketPath)
	}

	time.Sleep(50 * time.Millisecond)
	return socketPath, mgr, desktop, cleanup
}

func handleTreeTestConnection(conn net.Conn, mgr *Manager, desktop *texel.DesktopEngine, sink *DesktopSink) {
	defer conn.Close()

	session, resuming, err := handleHandshake(conn, mgr)
	if err != nil {
		return
	}

	publisher := NewDesktopPublisher(desktop, session)
	sink.SetPublisher(publisher)

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

	_ = publisher.Publish()

	connHandler := newConnection(conn, session, sink, resuming)
	_ = connHandler.serve()
}

type treeTestDriver struct{}

func (treeTestDriver) Init() error                                    { return nil }
func (treeTestDriver) Fini()                                          {}
func (treeTestDriver) Size() (int, int)                               { return 120, 40 }
func (treeTestDriver) SetStyle(tcell.Style)                           {}
func (treeTestDriver) HideCursor()                                    {}
func (treeTestDriver) Show()                                          {}
func (treeTestDriver) PollEvent() tcell.Event                         { return nil }
func (treeTestDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (treeTestDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type treeTestApp struct {
	id int
}

func (a *treeTestApp) Run() error            { return nil }
func (a *treeTestApp) Stop()                 {}
func (a *treeTestApp) Resize(cols, rows int) {}
func (a *treeTestApp) Render() [][]texel.Cell {
	// Return a simple buffer with the app ID
	return [][]texel.Cell{{{Ch: rune('0' + a.id), Style: tcell.StyleDefault}}}
}
func (a *treeTestApp) GetTitle() string {
	if a.id == 0 {
		return "welcome"
	}
	return "shell"
}
func (a *treeTestApp) HandleKey(ev *tcell.EventKey)      {}
func (a *treeTestApp) SetRefreshNotifier(ch chan<- bool) {}

func TestClientReceivesTreeSnapshotAfterVerticalSplit(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	// Wait for initial state
	snapshot1 := client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	if len(snapshot1.Panes) != 1 {
		t.Fatalf("expected 1 initial pane, got %d", len(snapshot1.Panes))
	}

	t.Logf("Initial state: 1 pane")

	// Trigger vertical split: Ctrl+A then '|'
	if err := client.SendKey(tcell.KeyCtrlA, 0, tcell.ModNone); err != nil {
		t.Fatalf("failed to send Ctrl+A: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send '|': %v", err)
	}

	// Client should receive TreeSnapshot with 2 panes
	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)

	if len(snapshot2.Panes) != 2 {
		t.Fatalf("expected 2 panes after vertical split, got %d", len(snapshot2.Panes))
	}

	t.Logf("After vertical split: %d panes", len(snapshot2.Panes))

	// Verify geometry: panes should be side-by-side (same Y, different X)
	pane0 := snapshot2.Panes[0]
	pane1 := snapshot2.Panes[1]

	if pane0.Y != pane1.Y {
		t.Logf("warning: vertical split panes have different Y coords: %d vs %d", pane0.Y, pane1.Y)
	}

	if pane0.X == pane1.X {
		t.Fatalf("vertical split panes should have different X coords, both are %d", pane0.X)
	}

	totalWidth := int(pane0.Width) + int(pane1.Width)
	if totalWidth > 120 {
		t.Fatalf("total pane width %d exceeds screen width 120", totalWidth)
	}

	t.Logf("Pane geometries: [0]=(%d,%d) %dx%d, [1]=(%d,%d) %dx%d",
		pane0.X, pane0.Y, pane0.Width, pane0.Height,
		pane1.X, pane1.Y, pane1.Width, pane1.Height)

	// Cache should reflect 2 panes
	client.AssertPaneCount(2)
}

func TestClientReceivesTreeSnapshotAfterHorizontalSplit(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	snapshot1 := client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	if len(snapshot1.Panes) != 1 {
		t.Fatalf("expected 1 initial pane, got %d", len(snapshot1.Panes))
	}

	// Trigger horizontal split: Ctrl+A then '-'
	if err := client.SendKey(tcell.KeyCtrlA, 0, tcell.ModNone); err != nil {
		t.Fatalf("failed to send Ctrl+A: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := client.SendKey(tcell.KeyRune, '-', tcell.ModNone); err != nil {
		t.Fatalf("failed to send '-': %v", err)
	}

	// Client should receive TreeSnapshot with 2 panes
	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)

	if len(snapshot2.Panes) != 2 {
		t.Fatalf("expected 2 panes after horizontal split, got %d", len(snapshot2.Panes))
	}

	t.Logf("After horizontal split: %d panes", len(snapshot2.Panes))

	// Verify geometry: panes should be stacked (same X, different Y)
	pane0 := snapshot2.Panes[0]
	pane1 := snapshot2.Panes[1]

	if pane0.X != pane1.X {
		t.Logf("warning: horizontal split panes have different X coords: %d vs %d", pane0.X, pane1.X)
	}

	if pane0.Y == pane1.Y {
		t.Fatalf("horizontal split panes should have different Y coords, both are %d", pane0.Y)
	}

	totalHeight := int(pane0.Height) + int(pane1.Height)
	if totalHeight > 40 {
		t.Fatalf("total pane height %d exceeds screen height 40", totalHeight)
	}

	t.Logf("Pane geometries: [0]=(%d,%d) %dx%d, [1]=(%d,%d) %dx%d",
		pane0.X, pane0.Y, pane0.Width, pane0.Height,
		pane1.X, pane1.Y, pane1.Width, pane1.Height)

	client.AssertPaneCount(2)
}

func TestClientReceivesTreeSnapshotAfterPaneClose(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	// First, create a split so we have 2 panes
	if err := client.SendKey(tcell.KeyCtrlA, 0, tcell.ModNone); err != nil {
		t.Fatalf("failed to send Ctrl+A: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send '|': %v", err)
	}

	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot2.Panes) != 2 {
		t.Fatalf("expected 2 panes after split, got %d", len(snapshot2.Panes))
	}

	// Drain any buffer deltas from the new pane
	time.Sleep(100 * time.Millisecond)
	client.DrainDeltas()
	client.DrainSnapshots()

	t.Logf("Created split: now have %d panes", len(snapshot2.Panes))

	// Now close the active pane: Ctrl+A then 'x'
	if err := client.SendKey(tcell.KeyCtrlA, 0, tcell.ModNone); err != nil {
		t.Fatalf("failed to send Ctrl+A: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := client.SendKey(tcell.KeyRune, 'x', tcell.ModNone); err != nil {
		t.Fatalf("failed to send 'x': %v", err)
	}

	// Client should receive TreeSnapshot with 1 pane
	snapshot3 := client.WaitForTreeSnapshot(2 * time.Second)

	if len(snapshot3.Panes) != 1 {
		t.Fatalf("expected 1 pane after close, got %d", len(snapshot3.Panes))
	}

	t.Logf("After close: back to %d pane", len(snapshot3.Panes))

	client.AssertPaneCount(1)
}

func TestClientReceivesBufferDeltasForNewPanes(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	snapshot1 := client.WaitForInitialSnapshot()
	firstPaneID := snapshot1.Panes[0].PaneID

	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Trigger split
	if err := client.SendKey(tcell.KeyCtrlA, 0, tcell.ModNone); err != nil {
		t.Fatalf("failed to send Ctrl+A: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send '|': %v", err)
	}

	// Wait for snapshot with new pane
	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot2.Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(snapshot2.Panes))
	}

	// Identify the new pane
	var newPaneID [16]byte
	for _, pane := range snapshot2.Panes {
		if pane.PaneID != firstPaneID {
			newPaneID = pane.PaneID
			break
		}
	}

	if newPaneID == [16]byte{} {
		t.Fatalf("couldn't identify new pane")
	}

	t.Logf("First pane: %x, New pane: %x", firstPaneID[:4], newPaneID[:4])

	// Client should receive buffer delta for the new pane
	delta := client.WaitForBufferDelta(newPaneID, 3*time.Second)

	if delta.PaneID != newPaneID {
		t.Fatalf("received delta for wrong pane: expected %x, got %x",
			newPaneID[:4], delta.PaneID[:4])
	}

	t.Logf("Received buffer delta for new pane %x", newPaneID[:4])
}

func TestClientCacheUpdateAfterTreeChange(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Initial state
	count1 := client.GetPaneCount()
	if count1 != 1 {
		t.Fatalf("expected 1 pane initially, got %d", count1)
	}

	panes1 := client.GetAllPanes()
	if len(panes1) != 1 {
		t.Fatalf("expected 1 pane ID, got %d", len(panes1))
	}

	// Perform split
	if err := client.SendKey(tcell.KeyCtrlA, 0, tcell.ModNone); err != nil {
		t.Fatalf("failed to send Ctrl+A: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send '|': %v", err)
	}

	client.WaitForTreeSnapshot(2 * time.Second)
	time.Sleep(200 * time.Millisecond) // Wait for buffer deltas
	client.DrainDeltas()

	// Cache should now have 2 panes
	count2 := client.GetPaneCount()
	if count2 != 2 {
		t.Fatalf("expected 2 panes after split, cache has %d", count2)
	}

	panes2 := client.GetAllPanes()
	if len(panes2) != 2 {
		t.Fatalf("expected 2 pane IDs, got %d", len(panes2))
	}

	// Verify both panes exist in cache
	for _, paneID := range panes2 {
		client.AssertPaneExists(paneID)
	}

	// Verify geometry is available for both panes
	for i, paneID := range panes2 {
		x, y, w, h, err := client.GetPaneGeometry(paneID)
		if err != nil {
			t.Fatalf("failed to get geometry for pane %d: %v", i, err)
		}
		t.Logf("Pane %d (%x): geometry (%d,%d) %dx%d", i, paneID[:4], x, y, w, h)
	}

	t.Logf("Cache correctly updated: %d -> %d panes", count1, count2)
}
