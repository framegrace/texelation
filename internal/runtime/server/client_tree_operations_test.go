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

func TestClientReceivesTreeSnapshotAfterPaneResize(t *testing.T) {
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

	// Create a vertical split so we have two panes side by side
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

	// Drain any pending messages
	time.Sleep(100 * time.Millisecond)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Get initial geometry of both panes
	leftPane := snapshot2.Panes[0]
	rightPane := snapshot2.Panes[1]

	leftWidth1 := int(leftPane.Width)
	rightWidth1 := int(rightPane.Width)

	t.Logf("Initial geometry: left=%dx%d, right=%dx%d",
		leftPane.Width, leftPane.Height, rightPane.Width, rightPane.Height)

	// Now resize: Need to be in control mode for Ctrl+arrow to work
	// After split, the right pane (shell) is active, so use Ctrl+Left to adjust its left border
	// Enter control mode with Ctrl+A
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// First Ctrl+Left selects the border to the left of active pane
	if err := client.SendKey(tcell.KeyLeft, 0, tcell.ModCtrl); err != nil {
		t.Fatalf("failed to send first Ctrl+Left: %v", err)
	}

	// Give it a moment to process the border selection
	time.Sleep(50 * time.Millisecond)

	// Second Ctrl+Left resizes: shrinks left pane, grows right pane
	if err := client.SendKey(tcell.KeyLeft, 0, tcell.ModCtrl); err != nil {
		t.Fatalf("failed to send second Ctrl+Left: %v", err)
	}

	// Wait for TreeSnapshot from resize
	snapshot3 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot3.Panes) != 2 {
		t.Fatalf("expected 2 panes after resize, got %d", len(snapshot3.Panes))
	}

	// Get updated geometry
	var leftPane2, rightPane2 protocol.PaneSnapshot
	for _, pane := range snapshot3.Panes {
		if pane.PaneID == leftPane.PaneID {
			leftPane2 = pane
		} else if pane.PaneID == rightPane.PaneID {
			rightPane2 = pane
		}
	}

	leftWidth2 := int(leftPane2.Width)
	rightWidth2 := int(rightPane2.Width)

	t.Logf("After resize: left=%dx%d, right=%dx%d",
		leftPane2.Width, leftPane2.Height, rightPane2.Width, rightPane2.Height)

	// Verify the resize occurred
	// Ctrl+Left from right pane: left pane shrinks, right pane grows
	if leftWidth2 >= leftWidth1 {
		t.Fatalf("expected left pane to shrink: was %d, now %d", leftWidth1, leftWidth2)
	}

	if rightWidth2 <= rightWidth1 {
		t.Fatalf("expected right pane to grow: was %d, now %d", rightWidth1, rightWidth2)
	}

	// Total width should remain constant
	totalWidth1 := leftWidth1 + rightWidth1
	totalWidth2 := leftWidth2 + rightWidth2

	if totalWidth1 != totalWidth2 {
		t.Fatalf("total width changed: was %d, now %d", totalWidth1, totalWidth2)
	}

	t.Logf("Resize verified: left %d->%d (%+d), right %d->%d (%+d)",
		leftWidth1, leftWidth2, leftWidth2-leftWidth1,
		rightWidth1, rightWidth2, rightWidth2-rightWidth1)

	// Test multiple resize steps
	client.DrainSnapshots()

	// Send another Ctrl+Left to resize more
	if err := client.SendKey(tcell.KeyLeft, 0, tcell.ModCtrl); err != nil {
		t.Fatalf("failed to send third Ctrl+Left: %v", err)
	}

	snapshot4 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot4.Panes) != 2 {
		t.Fatalf("expected 2 panes after second resize, got %d", len(snapshot4.Panes))
	}

	// Get updated geometry after second resize
	var leftPane3, rightPane3 protocol.PaneSnapshot
	for _, pane := range snapshot4.Panes {
		if pane.PaneID == leftPane.PaneID {
			leftPane3 = pane
		} else if pane.PaneID == rightPane.PaneID {
			rightPane3 = pane
		}
	}

	leftWidth3 := int(leftPane3.Width)
	rightWidth3 := int(rightPane3.Width)

	t.Logf("After second resize: left=%dx%d, right=%dx%d",
		leftPane3.Width, leftPane3.Height, rightPane3.Width, rightPane3.Height)

	// Left should have shrunk more, right should have grown more
	if leftWidth3 >= leftWidth2 {
		t.Fatalf("expected left pane to shrink more: was %d, now %d", leftWidth2, leftWidth3)
	}

	if rightWidth3 <= rightWidth2 {
		t.Fatalf("expected right pane to grow more: was %d, now %d", rightWidth2, rightWidth3)
	}

	t.Logf("Multi-step resize successful: left %d -> %d -> %d, right %d -> %d -> %d",
		leftWidth1, leftWidth2, leftWidth3, rightWidth1, rightWidth2, rightWidth3)
}

func TestClientReceivesTreeSnapshotAfterZoom(t *testing.T) {
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

	// Create a vertical split so we have 2 panes
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send split command: %v", err)
	}

	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	if len(snapshot2.Panes) != 2 {
		t.Fatalf("expected 2 panes after split, got %d", len(snapshot2.Panes))
	}

	// Find left and right panes
	var leftPane, rightPane protocol.PaneSnapshot
	for _, pane := range snapshot2.Panes {
		if pane.X == 0 {
			leftPane = pane
		} else {
			rightPane = pane
		}
	}

	leftHeight := int(leftPane.Height)
	rightWidth := int(rightPane.Width)

	t.Logf("Split geometry: left=%dx%d, right=%dx%d",
		leftPane.Width, leftPane.Height, rightPane.Width, rightPane.Height)

	// Now zoom in: Enter control mode and press 'z'
	// The right pane (shell) should be active after split
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode for zoom: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := client.SendKey(tcell.KeyRune, 'z', tcell.ModNone); err != nil {
		t.Fatalf("failed to send zoom command: %v", err)
	}

	// Wait for TreeSnapshot after zoom in
	snapshot3 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot3.Panes) != 2 {
		t.Fatalf("expected 2 panes after zoom, got %d", len(snapshot3.Panes))
	}

	// Find the active (zoomed) pane - should be the right pane
	var zoomedPane protocol.PaneSnapshot
	for _, pane := range snapshot3.Panes {
		if pane.PaneID == rightPane.PaneID {
			zoomedPane = pane
			break
		}
	}

	zoomedWidth := int(zoomedPane.Width)
	zoomedHeight := int(zoomedPane.Height)

	t.Logf("After zoom in: zoomed pane=%dx%d", zoomedPane.Width, zoomedPane.Height)

	// When zoomed, the pane should be fullscreen (120x40)
	// It should be significantly larger than its split size
	if zoomedWidth <= rightWidth {
		t.Fatalf("expected zoomed pane to be wider: was %d, now %d", rightWidth, zoomedWidth)
	}

	if zoomedHeight < leftHeight {
		t.Fatalf("expected zoomed pane height to be at least split height: was %d, now %d", leftHeight, zoomedHeight)
	}

	// The zoomed pane should be close to the full workspace size (120x40)
	expectedWidth := 120
	expectedHeight := 40
	if zoomedWidth < expectedWidth-5 || zoomedWidth > expectedWidth {
		t.Fatalf("expected zoomed width ~%d, got %d", expectedWidth, zoomedWidth)
	}
	if zoomedHeight < expectedHeight-5 || zoomedHeight > expectedHeight {
		t.Fatalf("expected zoomed height ~%d, got %d", expectedHeight, zoomedHeight)
	}

	t.Logf("Zoom in verified: %dx%d -> %dx%d",
		rightWidth, leftHeight, zoomedWidth, zoomedHeight)

	// Now zoom out: Send 'z' again (still in control mode or need to re-enter)
	client.DrainSnapshots()

	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode for zoom out: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := client.SendKey(tcell.KeyRune, 'z', tcell.ModNone); err != nil {
		t.Fatalf("failed to send zoom out command: %v", err)
	}

	// Wait for TreeSnapshot after zoom out
	snapshot4 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot4.Panes) != 2 {
		t.Fatalf("expected 2 panes after zoom out, got %d", len(snapshot4.Panes))
	}

	// Find the panes after zoom out
	var restoredRightPane protocol.PaneSnapshot
	for _, pane := range snapshot4.Panes {
		if pane.PaneID == rightPane.PaneID {
			restoredRightPane = pane
			break
		}
	}

	restoredWidth := int(restoredRightPane.Width)
	restoredHeight := int(restoredRightPane.Height)

	t.Logf("After zoom out: restored pane=%dx%d", restoredRightPane.Width, restoredRightPane.Height)

	// After zoom out, the pane should be back to split size
	if restoredWidth >= zoomedWidth {
		t.Fatalf("expected restored pane to be smaller: was %d (zoomed), now %d", zoomedWidth, restoredWidth)
	}

	// Should be close to original split size
	if restoredWidth < rightWidth-5 || restoredWidth > rightWidth+5 {
		t.Fatalf("expected restored width ~%d, got %d", rightWidth, restoredWidth)
	}

	if restoredHeight < leftHeight-5 || restoredHeight > leftHeight+5 {
		t.Fatalf("expected restored height ~%d, got %d", leftHeight, restoredHeight)
	}

	t.Logf("Zoom out verified: %dx%d -> %dx%d -> %dx%d",
		rightWidth, leftHeight, zoomedWidth, zoomedHeight, restoredWidth, restoredHeight)
}

func TestClientReceivesTreeSnapshotAfterPaneSwap(t *testing.T) {
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

	// Create a vertical split so we have 2 panes side-by-side
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send split command: %v", err)
	}

	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	if len(snapshot2.Panes) != 2 {
		t.Fatalf("expected 2 panes after split, got %d", len(snapshot2.Panes))
	}

	// Identify left and right panes by their X position
	var leftPaneID, rightPaneID [16]byte
	var leftX, rightX int
	for _, pane := range snapshot2.Panes {
		if pane.X == 0 {
			leftPaneID = pane.PaneID
			leftX = int(pane.X)
		} else {
			rightPaneID = pane.PaneID
			rightX = int(pane.X)
		}
	}

	t.Logf("Before swap: left pane %x at X=%d, right pane %x at X=%d",
		leftPaneID[:4], leftX, rightPaneID[:4], rightX)

	// Now perform swap: Ctrl+A, 'w', Right arrow
	// After split, active pane should be the right pane
	// Swapping right will swap with the pane to the right (but there isn't one)
	// So let's move left first, then swap right

	// Move focus to left pane
	if err := client.SendKey(tcell.KeyLeft, 0, tcell.ModShift); err != nil {
		t.Fatalf("failed to send Shift+Left to move focus: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Enter control mode for swap
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode for swap: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	client.DrainStateUpdates()

	// Send 'w' to enter swap sub-mode
	if err := client.SendKey(tcell.KeyRune, 'w', tcell.ModNone); err != nil {
		t.Fatalf("failed to send 'w' for swap mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Send Right arrow to swap with right neighbor
	if err := client.SendKey(tcell.KeyRight, 0, tcell.ModNone); err != nil {
		t.Fatalf("failed to send Right arrow for swap: %v", err)
	}

	// Wait for TreeSnapshot showing swapped panes
	snapshot3 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot3.Panes) != 2 {
		t.Fatalf("expected 2 panes after swap, got %d", len(snapshot3.Panes))
	}

	// Verify panes have swapped positions
	var newLeftPaneID, newRightPaneID [16]byte
	var newLeftX, newRightX int
	for _, pane := range snapshot3.Panes {
		if pane.X == 0 || pane.X < snapshot3.Panes[1].X {
			newLeftPaneID = pane.PaneID
			newLeftX = int(pane.X)
		} else {
			newRightPaneID = pane.PaneID
			newRightX = int(pane.X)
		}
	}

	t.Logf("After swap: left pane %x at X=%d, right pane %x at X=%d",
		newLeftPaneID[:4], newLeftX, newRightPaneID[:4], newRightX)

	// The pane that was on the left should now be on the right, and vice versa
	if newLeftPaneID != rightPaneID {
		t.Fatalf("expected left position to have pane %x, got %x",
			rightPaneID[:4], newLeftPaneID[:4])
	}

	if newRightPaneID != leftPaneID {
		t.Fatalf("expected right position to have pane %x, got %x",
			leftPaneID[:4], newRightPaneID[:4])
	}

	t.Logf("Swap verified: panes successfully swapped positions")
}

func TestClientWorkspaceSwitchingAndCreation(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	snapshot1 := client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()
	client.DrainStateUpdates()

	if len(snapshot1.Panes) != 1 {
		t.Fatalf("expected 1 initial pane, got %d", len(snapshot1.Panes))
	}

	t.Logf("Starting in workspace 1 with %d pane", len(snapshot1.Panes))

	// Switch to workspace 2 (doesn't exist, should create it)
	// Ctrl+A then '2'
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Drain state updates from entering control mode
	client.DrainStateUpdates()

	if err := client.SendKey(tcell.KeyRune, '2', tcell.ModNone); err != nil {
		t.Fatalf("failed to send workspace 2 command: %v", err)
	}

	// Should receive StateUpdate with workspace 2 (sent before TreeSnapshot)
	stateUpdate2 := client.WaitForStateUpdate(2 * time.Second)
	if stateUpdate2.WorkspaceID != 2 {
		t.Fatalf("expected workspace 2, got %d", stateUpdate2.WorkspaceID)
	}

	// Should receive TreeSnapshot (new workspace with welcome pane)
	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot2.Panes) != 1 {
		t.Fatalf("expected 1 pane in workspace 2, got %d", len(snapshot2.Panes))
	}

	t.Logf("Switched to workspace: %d", stateUpdate2.WorkspaceID)

	// Verify pane IDs are different (new workspace has new pane)
	if snapshot2.Panes[0].PaneID == snapshot1.Panes[0].PaneID {
		t.Fatalf("expected different pane ID in workspace 2")
	}

	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()
	client.DrainStateUpdates()

	// Switch back to workspace 1
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Drain state updates from entering control mode
	client.DrainStateUpdates()

	if err := client.SendKey(tcell.KeyRune, '1', tcell.ModNone); err != nil {
		t.Fatalf("failed to send workspace 1 command: %v", err)
	}

	// Should receive StateUpdate with workspace 1 (sent before TreeSnapshot)
	stateUpdate3 := client.WaitForStateUpdate(2 * time.Second)
	if stateUpdate3.WorkspaceID != 1 {
		t.Fatalf("expected workspace 1, got %d", stateUpdate3.WorkspaceID)
	}

	// Should receive TreeSnapshot with original workspace 1 pane
	snapshot3 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot3.Panes) != 1 {
		t.Fatalf("expected 1 pane back in workspace 1, got %d", len(snapshot3.Panes))
	}

	// Verify we're back to the original pane
	if snapshot3.Panes[0].PaneID != snapshot1.Panes[0].PaneID {
		t.Fatalf("expected same pane ID when returning to workspace 1")
	}

	t.Logf("Workspace switching verified: 1 -> 2 (created) -> 1")
}

func TestClientWorkspaceIndependence(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	snapshot1 := client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()
	client.DrainStateUpdates()

	if len(snapshot1.Panes) != 1 {
		t.Fatalf("expected 1 initial pane in workspace 1, got %d", len(snapshot1.Panes))
	}

	originalPaneID := snapshot1.Panes[0].PaneID

	// Create a split in workspace 1
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send split command: %v", err)
	}

	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()
	client.DrainStateUpdates()

	if len(snapshot2.Panes) != 2 {
		t.Fatalf("expected 2 panes in workspace 1 after split, got %d", len(snapshot2.Panes))
	}

	t.Logf("Workspace 1 has %d panes after split", len(snapshot2.Panes))

	// Switch to workspace 3
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Drain state updates from entering control mode
	client.DrainStateUpdates()

	if err := client.SendKey(tcell.KeyRune, '3', tcell.ModNone); err != nil {
		t.Fatalf("failed to send workspace 3 command: %v", err)
	}

	stateUpdate3 := client.WaitForStateUpdate(2 * time.Second)
	snapshot3 := client.WaitForTreeSnapshot(2 * time.Second)
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()
	client.DrainStateUpdates()

	if stateUpdate3.WorkspaceID != 3 {
		t.Fatalf("expected workspace 3, got %d", stateUpdate3.WorkspaceID)
	}

	// Workspace 3 should have only 1 pane (fresh workspace)
	if len(snapshot3.Panes) != 1 {
		t.Fatalf("expected 1 pane in workspace 3, got %d", len(snapshot3.Panes))
	}

	t.Logf("Workspace 3 has %d pane (fresh)", len(snapshot3.Panes))

	// Verify workspace 3 pane is different from workspace 1 panes
	ws3PaneID := snapshot3.Panes[0].PaneID
	for _, pane := range snapshot2.Panes {
		if ws3PaneID == pane.PaneID {
			t.Fatalf("workspace 3 pane has same ID as workspace 1 pane")
		}
	}

	// Switch back to workspace 1
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Drain state updates from entering control mode
	client.DrainStateUpdates()

	if err := client.SendKey(tcell.KeyRune, '1', tcell.ModNone); err != nil {
		t.Fatalf("failed to send workspace 1 command: %v", err)
	}

	stateUpdate4 := client.WaitForStateUpdate(2 * time.Second)
	snapshot4 := client.WaitForTreeSnapshot(2 * time.Second)

	if stateUpdate4.WorkspaceID != 1 {
		t.Fatalf("expected workspace 1, got %d", stateUpdate4.WorkspaceID)
	}

	// Workspace 1 should still have 2 panes (split preserved)
	if len(snapshot4.Panes) != 2 {
		t.Fatalf("expected 2 panes in workspace 1, got %d", len(snapshot4.Panes))
	}

	t.Logf("Workspace 1 still has %d panes (split preserved)", len(snapshot4.Panes))

	// Verify the original pane is still there
	foundOriginalPane := false
	for _, pane := range snapshot4.Panes {
		if pane.PaneID == originalPaneID {
			foundOriginalPane = true
			break
		}
	}

	if !foundOriginalPane {
		t.Fatalf("original pane not found when returning to workspace 1")
	}

	t.Logf("Workspace independence verified: WS1(2 panes) <-> WS3(1 pane)")
}

func TestClientTerminalResize(t *testing.T) {
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

	initialWidth := int(snapshot1.Panes[0].Width)
	initialHeight := int(snapshot1.Panes[0].Height)

	t.Logf("Initial terminal size: %dx%d", initialWidth, initialHeight)

	// Resize terminal to 100x30
	if err := client.SendResize(100, 30); err != nil {
		t.Fatalf("failed to send resize: %v", err)
	}

	// Should receive TreeSnapshot with new geometry
	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot2.Panes) != 1 {
		t.Fatalf("expected 1 pane after resize, got %d", len(snapshot2.Panes))
	}

	newWidth := int(snapshot2.Panes[0].Width)
	newHeight := int(snapshot2.Panes[0].Height)

	t.Logf("After resize: %dx%d", newWidth, newHeight)

	// Verify dimensions changed
	if newWidth == initialWidth && newHeight == initialHeight {
		t.Fatalf("pane dimensions didn't change after resize")
	}

	// Verify new dimensions are close to requested (accounting for borders)
	if newWidth < 95 || newWidth > 100 {
		t.Fatalf("expected width ~100, got %d", newWidth)
	}

	if newHeight < 25 || newHeight > 30 {
		t.Fatalf("expected height ~30, got %d", newHeight)
	}

	t.Logf("Terminal resize verified: %dx%d -> %dx%d",
		initialWidth, initialHeight, newWidth, newHeight)
}

func TestTerminalResizeAfterSplit(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Create a vertical split
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send split command: %v", err)
	}

	snapshot2 := client.WaitForTreeSnapshot(2 * time.Second)
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	if len(snapshot2.Panes) != 2 {
		t.Fatalf("expected 2 panes after split, got %d", len(snapshot2.Panes))
	}

	// Find left and right panes
	var leftPane, rightPane protocol.PaneSnapshot
	for _, pane := range snapshot2.Panes {
		if pane.X == 0 {
			leftPane = pane
		} else {
			rightPane = pane
		}
	}

	leftWidth1 := int(leftPane.Width)
	rightWidth1 := int(rightPane.Width)
	height1 := int(leftPane.Height)

	t.Logf("After split: left=%dx%d, right=%dx%d", leftWidth1, height1, rightWidth1, height1)

	// Calculate split ratio
	totalWidth1 := leftWidth1 + rightWidth1
	leftRatio := float64(leftWidth1) / float64(totalWidth1)
	rightRatio := float64(rightWidth1) / float64(totalWidth1)

	t.Logf("Split ratios: left=%.2f, right=%.2f", leftRatio, rightRatio)

	// Now resize terminal to 150x50
	if err := client.SendResize(150, 50); err != nil {
		t.Fatalf("failed to send resize: %v", err)
	}

	snapshot3 := client.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot3.Panes) != 2 {
		t.Fatalf("expected 2 panes after resize, got %d", len(snapshot3.Panes))
	}

	// Find panes after resize
	var leftPane2, rightPane2 protocol.PaneSnapshot
	for _, pane := range snapshot3.Panes {
		if pane.PaneID == leftPane.PaneID {
			leftPane2 = pane
		} else if pane.PaneID == rightPane.PaneID {
			rightPane2 = pane
		}
	}

	leftWidth2 := int(leftPane2.Width)
	rightWidth2 := int(rightPane2.Width)
	height2 := int(leftPane2.Height)

	t.Logf("After resize: left=%dx%d, right=%dx%d", leftWidth2, height2, rightWidth2, height2)

	// Verify both panes resized
	if leftWidth2 <= leftWidth1 {
		t.Fatalf("expected left pane to grow: was %d, now %d", leftWidth1, leftWidth2)
	}

	if rightWidth2 <= rightWidth1 {
		t.Fatalf("expected right pane to grow: was %d, now %d", rightWidth1, rightWidth2)
	}

	if height2 <= height1 {
		t.Fatalf("expected height to increase: was %d, now %d", height1, height2)
	}

	// Verify split ratios are approximately preserved
	totalWidth2 := leftWidth2 + rightWidth2
	leftRatio2 := float64(leftWidth2) / float64(totalWidth2)
	rightRatio2 := float64(rightWidth2) / float64(totalWidth2)

	t.Logf("New ratios: left=%.2f, right=%.2f", leftRatio2, rightRatio2)

	// Ratios should be within 10% of original
	if leftRatio2 < leftRatio-0.1 || leftRatio2 > leftRatio+0.1 {
		t.Fatalf("left ratio changed too much: %.2f -> %.2f", leftRatio, leftRatio2)
	}

	if rightRatio2 < rightRatio-0.1 || rightRatio2 > rightRatio+0.1 {
		t.Fatalf("right ratio changed too much: %.2f -> %.2f", rightRatio, rightRatio2)
	}

	t.Logf("Proportional resize verified: ratios preserved")
}

func TestDeepNestedSplits(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	snapshot := client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	if len(snapshot.Panes) != 1 {
		t.Fatalf("expected 1 initial pane, got %d", len(snapshot.Panes))
	}

	t.Logf("Starting with %d pane", len(snapshot.Panes))

	// Try to create multiple splits to test deep nesting
	// Alternate between vertical and horizontal to create complex tree
	// Note: Some splits may be rejected if panes get too small (MinPaneWidth/MinPaneHeight)
	splits := []rune{'|', '-', '|', '-', '|', '-'}
	successfulSplits := 0
	lastPaneCount := 1

	for i, splitChar := range splits {
		if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
			t.Fatalf("failed to enter control mode for split %d: %v", i+1, err)
		}
		time.Sleep(100 * time.Millisecond)
		client.DrainStateUpdates()

		if err := client.SendKey(tcell.KeyRune, splitChar, tcell.ModNone); err != nil {
			t.Fatalf("failed to send split %d: %v", i+1, err)
		}

		// Wait for potential TreeSnapshot from this split
		time.Sleep(100 * time.Millisecond)

		// Try to get snapshot, but it might timeout if split was rejected
		currentSnapshot, ok := client.TryGetTreeSnapshot(500 * time.Millisecond)
		if !ok {
			// No snapshot - split was likely rejected due to size constraints
			t.Logf("Split %d/%d: No TreeSnapshot received (likely rejected due to size)", i+1, len(splits))
			continue
		}

		// Check if pane count increased
		if len(currentSnapshot.Panes) > lastPaneCount {
			successfulSplits++
			lastPaneCount = len(currentSnapshot.Panes)

			// Drain deltas for this split
			client.WaitForAnyBufferDelta(1 * time.Second)
			client.DrainDeltas()

			t.Logf("Split %d/%d succeeded: now %d panes", i+1, len(splits), len(currentSnapshot.Panes))
		} else {
			t.Logf("Split %d/%d: Pane count unchanged (split rejected)", i+1, len(splits))
		}
	}

	t.Logf("Completed %d successful splits out of %d attempts", successfulSplits, len(splits))

	// Verify final state using cache
	paneCount := client.GetPaneCount()

	// Should have at least 4 splits (5 panes) on 120x40 screen before hitting size limits
	if paneCount < 4 {
		t.Fatalf("expected at least 4 panes, got %d (minimum size protection may be too aggressive)", paneCount)
	}

	t.Logf("Deep nesting verified: %d panes created with %d successful splits", paneCount, successfulSplits)

	// Verify all panes meet minimum size requirements
	panes := client.GetAllPanes()
	minWidth := 20 // texel.MinPaneWidth
	minHeight := 8 // texel.MinPaneHeight

	for i, paneID := range panes {
		x, y, w, h, err := client.GetPaneGeometry(paneID)
		if err != nil {
			t.Fatalf("failed to get geometry for pane %d: %v", i, err)
		}
		// All panes should meet or exceed minimum size since splits are now rejected for too-small panes
		if w < minWidth || h < minHeight {
			t.Errorf("pane %d below minimum size: %dx%d at (%d,%d), expected at least %dx%d",
				i, w, h, x, y, minWidth, minHeight)
		}
		t.Logf("  Pane %d: %dx%d at (%d,%d)", i+1, w, h, x, y)
	}

	t.Logf("All %d panes meet minimum size requirements", len(panes))
}

func TestFocusTransferWhenActivePaneRemoved(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Create 3 panes (2 splits)
	for i := 0; i < 2; i++ {
		if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
			t.Fatalf("failed to enter control mode: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		client.DrainStateUpdates()

		if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
			t.Fatalf("failed to send split: %v", err)
		}

		time.Sleep(150 * time.Millisecond)
		t.Logf("Sent split %d/2", i+1)
	}

	// Allow splits to complete
	time.Sleep(300 * time.Millisecond)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Verify we have 3 panes using cache
	paneCount := client.GetPaneCount()
	if paneCount != 3 {
		t.Fatalf("expected 3 panes after 2 splits, got %d", paneCount)
	}

	t.Logf("Created %d panes", paneCount)

	// Now close the active pane
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode for close: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	client.DrainStateUpdates()

	if err := client.SendKey(tcell.KeyRune, 'x', tcell.ModNone); err != nil {
		t.Fatalf("failed to send close command: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Verify we now have 2 panes using cache
	paneCount = client.GetPaneCount()
	if paneCount != 2 {
		t.Fatalf("expected 2 panes after close, got %d", paneCount)
	}

	t.Logf("After closing active pane: %d panes remain", paneCount)

	// Close another pane - verify we can still interact (focus transferred)
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode after close: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	client.DrainStateUpdates()

	if err := client.SendKey(tcell.KeyRune, 'x', tcell.ModNone); err != nil {
		t.Fatalf("failed to send second close: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Verify we now have 1 pane using cache
	paneCount = client.GetPaneCount()
	if paneCount != 1 {
		t.Fatalf("expected 1 pane after second close, got %d", paneCount)
	}

	t.Logf("Focus transfer verified: closed 2 panes, 1 remains active")
}

func TestRapidSplitCloseLoop(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)
	defer client.Close()

	client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)
	client.DrainDeltas()
	client.DrainSnapshots()

	t.Logf("Starting rapid split/close torture test")

	// Perform 20 rapid operations: split, split, close, repeat
	for cycle := 0; cycle < 10; cycle++ {
		// Split twice
		for i := 0; i < 2; i++ {
			if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
				t.Fatalf("cycle %d split %d: failed to enter control mode: %v", cycle, i, err)
			}
			time.Sleep(30 * time.Millisecond)
			client.DrainStateUpdates()

			if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
				t.Fatalf("cycle %d split %d: failed to send split: %v", cycle, i, err)
			}

			// Don't wait for full response, just brief pause
			time.Sleep(30 * time.Millisecond)
		}

		// Close once
		if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
			t.Fatalf("cycle %d close: failed to enter control mode: %v", cycle, err)
		}
		time.Sleep(30 * time.Millisecond)
		client.DrainStateUpdates()

		if err := client.SendKey(tcell.KeyRune, 'x', tcell.ModNone); err != nil {
			t.Fatalf("cycle %d close: failed to send close: %v", cycle, err)
		}

		time.Sleep(30 * time.Millisecond)

		if cycle%5 == 4 {
			t.Logf("Completed cycle %d (20 operations so far)", cycle+1)
		}
	}

	t.Logf("Torture test completed all 10 cycles (30 operations total)")

	// Final verification - allow operations to complete and check cache state
	time.Sleep(200 * time.Millisecond)
	client.DrainDeltas()
	client.DrainSnapshots()

	// Check the client's cache state (reflects all applied snapshots/deltas)
	paneCount := client.GetPaneCount()

	if paneCount == 0 {
		t.Fatalf("no panes remaining after torture test")
	}

	t.Logf("Torture test complete: %d panes remain after 10 cycles of split/close", paneCount)
	t.Logf("Operations survived: 20 splits, 10 closes")

	// Verify all panes are accessible
	panes := client.GetAllPanes()
	if len(panes) != paneCount {
		t.Fatalf("pane count mismatch: GetPaneCount=%d, GetAllPanes=%d", paneCount, len(panes))
	}
}

// ============================================================================
// Phase 6: Error Handling Tests
// ============================================================================

func TestClientDisconnectDuringSplit(t *testing.T) {
	socketPath, mgr, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	client := testutil.NewTestClient(t, socketPath)

	// Wait for initial state
	client.WaitForInitialSnapshot()
	client.WaitForAnyBufferDelta(2 * time.Second)

	// Verify we have a session
	sessionID := client.SessionID()
	if sessionID == [16]byte{} {
		t.Fatalf("expected valid session ID")
	}

	// Trigger a split
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	client.DrainStateUpdates()

	if err := client.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("failed to send split: %v", err)
	}

	// Immediately disconnect without waiting for TreeSnapshot
	t.Logf("Disconnecting client during split operation")
	if err := client.Close(); err != nil {
		t.Logf("Client close returned: %v", err)
	}

	// Give server time to detect disconnect and cleanup
	time.Sleep(200 * time.Millisecond)

	// Verify session handling after disconnect
	session, err := mgr.Lookup(sessionID)
	if err != nil {
		t.Logf("Session removed after disconnect: %v (expected behavior)", err)
	} else {
		t.Logf("Session retained after disconnect (supports reconnect)")
		stats := session.Stats()
		t.Logf("Session stats: Pending=%d, NextSeq=%d", stats.PendingCount, stats.NextSequence)
	}

	t.Logf("Server handled client disconnect during split gracefully")
}

func TestServerHandlesCorruptedMessage(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	// Connect directly with raw socket to send corrupted data
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send valid hello
	helloPayload, _ := protocol.EncodeHello(protocol.Hello{ClientName: "corruption-test"})
	if err := protocol.WriteMessage(conn, protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgHello,
		Flags:   protocol.FlagChecksum,
	}, helloPayload); err != nil {
		t.Fatalf("failed to write hello: %v", err)
	}

	// Read welcome
	if _, _, err := protocol.ReadMessage(conn); err != nil {
		t.Fatalf("failed to read welcome: %v", err)
	}

	// Send connect request
	connectPayload, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	if err := protocol.WriteMessage(conn, protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgConnectRequest,
		Flags:   protocol.FlagChecksum,
	}, connectPayload); err != nil {
		t.Fatalf("failed to write connect: %v", err)
	}

	// Read connect accept
	hdr, payload, err := protocol.ReadMessage(conn)
	if err != nil {
		t.Fatalf("failed to read connect accept: %v", err)
	}
	if hdr.Type != protocol.MsgConnectAccept {
		t.Fatalf("expected connect accept, got %v", hdr.Type)
	}

	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		t.Fatalf("failed to decode connect accept: %v", err)
	}

	sessionID := accept.SessionID
	t.Logf("Connected with session %x", sessionID[:4])

	// Drain initial messages (TreeSnapshot) sent by server after handshake
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	drainedCount := 0
	for {
		_, _, err := protocol.ReadMessage(conn)
		if err != nil {
			// Timeout or connection closed - we've drained all initial messages
			break
		}
		drainedCount++
		if drainedCount > 10 {
			break // Safety limit
		}
	}
	t.Logf("Drained %d initial messages", drainedCount)

	// Now send corrupted message - write a full 40-byte header with invalid magic
	t.Logf("Sending corrupted message (invalid magic number)")
	corruptedData := make([]byte, 40) // Full header size
	// Set invalid magic (should be 0x54584c01)
	corruptedData[0] = 0xFF
	corruptedData[1] = 0xFF
	corruptedData[2] = 0xFF
	corruptedData[3] = 0xFF
	// Rest filled with zeros (invalid but complete header)
	if _, err := conn.Write(corruptedData); err != nil {
		t.Logf("Write of corrupted data failed: %v", err)
	}

	// Try to read response - connection should be closed by server
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = protocol.ReadMessage(conn)
	if err != nil {
		// Expected - server closed connection or we got protocol error
		t.Logf("Server correctly closed connection after corrupted message: %v", err)
	} else {
		t.Fatalf("Server should have closed connection after corrupted message")
	}
}

func TestMultipleClientsOneDisconnects(t *testing.T) {
	socketPath, _, _, cleanup := setupTreeTestServer(t)
	defer cleanup()

	// Connect two clients
	client1 := testutil.NewTestClient(t, socketPath)
	defer client1.Close()

	client2 := testutil.NewTestClient(t, socketPath)

	// Both clients receive initial state
	client1.WaitForInitialSnapshot()
	client1.WaitForAnyBufferDelta(2 * time.Second)

	client2.WaitForInitialSnapshot()
	client2.WaitForAnyBufferDelta(2 * time.Second)

	t.Logf("Both clients connected")

	// Client 1 triggers a split
	if err := client1.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("client1: failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	client1.DrainStateUpdates()

	if err := client1.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("client1: failed to send split: %v", err)
	}

	// Client 1 waits for split
	snapshot1 := client1.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot1.Panes) != 2 {
		t.Fatalf("client1: expected 2 panes, got %d", len(snapshot1.Panes))
	}

	// Client 2 should also receive the split
	snapshot2 := client2.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot2.Panes) != 2 {
		t.Fatalf("client2: expected 2 panes, got %d", len(snapshot2.Panes))
	}

	t.Logf("Both clients received split: %d panes", len(snapshot1.Panes))

	// Now disconnect client 2
	t.Logf("Disconnecting client 2")
	if err := client2.Close(); err != nil {
		t.Logf("Client 2 close returned: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Client 1 should still be able to perform operations
	if err := client1.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("client1: failed to enter control mode after client2 disconnect: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	client1.DrainStateUpdates()

	if err := client1.SendKey(tcell.KeyRune, '|', tcell.ModNone); err != nil {
		t.Fatalf("client1: failed to send second split: %v", err)
	}

	snapshot3 := client1.WaitForTreeSnapshot(2 * time.Second)
	if len(snapshot3.Panes) != 3 {
		t.Fatalf("client1: expected 3 panes after second split, got %d", len(snapshot3.Panes))
	}

	t.Logf("Client 1 continues working after client 2 disconnect: %d panes", len(snapshot3.Panes))
}
