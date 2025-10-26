//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/tview_tree_operations_test.go
// Summary: Tests tree operations (split, zoom, resize, swap) with tview apps.
// Usage: Executed during `go test -tags=integration` to validate tview app behavior during tree operations.
// Notes: Tests that tview apps handle tree operations without crashing or flickering.

package server

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/welcome"
	"texelation/internal/runtime/server/testutil"
	"texelation/protocol"
	"texelation/texel"
)

// setupTViewTreeTestServer creates a test server with tview welcome apps.
func setupTViewTreeTestServer(t *testing.T) (string, *Manager, *texel.DesktopEngine, func()) {
	t.Helper()

	socketPath := "/tmp/texel-tview-tree-test-" + t.Name() + ".sock"

	// Use real tview welcome app for shell and welcome
	shellFactory := func() texel.App {
		return welcome.NewStaticTView()
	}
	welcomeFactory := func() texel.App {
		return welcome.NewStaticTView()
	}
	lifecycle := &texel.LocalAppLifecycle{}

	// Use simple test driver (same as client_tree_operations_test.go)
	driver := tviewTestDriver{}

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
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
			go handleTViewTreeTestConnection(conn, mgr, desktop, sink)
		}
	}()

	cleanup := func() {
		listener.Close()
		desktop.Close()
		_ = os.Remove(socketPath)
	}

	time.Sleep(100 * time.Millisecond)
	return socketPath, mgr, desktop, cleanup
}

func handleTViewTreeTestConnection(conn net.Conn, mgr *Manager, desktop *texel.DesktopEngine, sink *DesktopSink) {
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

// tviewTestDriver is a minimal screen driver for testing tview apps
type tviewTestDriver struct{}

func (tviewTestDriver) Init() error                                    { return nil }
func (tviewTestDriver) Fini()                                          {}
func (tviewTestDriver) Size() (int, int)                               { return 120, 40 }
func (tviewTestDriver) SetStyle(tcell.Style)                           {}
func (tviewTestDriver) HideCursor()                                    {}
func (tviewTestDriver) Show()                                          {}
func (tviewTestDriver) PollEvent() tcell.Event                         { return nil }
func (tviewTestDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (tviewTestDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

func TestTViewAppVerticalSplit(t *testing.T) {
	socketPath, _, _, cleanup := setupTViewTreeTestServer(t)
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

	t.Logf("Initial state: 1 tview pane")

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
		t.Fatalf("expected 2 tview panes after vertical split, got %d", len(snapshot2.Panes))
	}

	t.Logf("After vertical split: %d tview panes", len(snapshot2.Panes))

	// Verify geometry
	pane0 := snapshot2.Panes[0]
	pane1 := snapshot2.Panes[1]

	if pane0.Y != pane1.Y {
		t.Logf("warning: vertical split panes have different Y coords: %d vs %d", pane0.Y, pane1.Y)
	}

	if pane0.X == pane1.X {
		t.Fatalf("vertical split panes should have different X coords, both are %d", pane0.X)
	}

	t.Logf("TView vertical split successful")

	// Cache should reflect 2 panes
	client.AssertPaneCount(2)
}

func TestTViewAppHorizontalSplit(t *testing.T) {
	socketPath, _, _, cleanup := setupTViewTreeTestServer(t)
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
		t.Fatalf("expected 2 tview panes after horizontal split, got %d", len(snapshot2.Panes))
	}

	t.Logf("After horizontal split: %d tview panes", len(snapshot2.Panes))

	// Verify geometry: panes should be stacked (same X, different Y)
	pane0 := snapshot2.Panes[0]
	pane1 := snapshot2.Panes[1]

	if pane0.X != pane1.X {
		t.Logf("warning: horizontal split panes have different X coords: %d vs %d", pane0.X, pane1.X)
	}

	if pane0.Y == pane1.Y {
		t.Fatalf("horizontal split panes should have different Y coords, both are %d", pane0.Y)
	}

	t.Logf("TView horizontal split successful")

	client.AssertPaneCount(2)
}

func TestTViewAppZoom(t *testing.T) {
	socketPath, _, _, cleanup := setupTViewTreeTestServer(t)
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

	rightWidth := int(rightPane.Width)
	rightHeight := int(rightPane.Height)

	t.Logf("Split geometry: left=%dx%d, right=%dx%d",
		leftPane.Width, leftPane.Height, rightPane.Width, rightPane.Height)

	// Now zoom in: Enter control mode and press 'z'
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

	// Find the zoomed pane - should be the right pane
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

	// When zoomed, the pane should be significantly wider
	// (Height may not change if split was vertical and pane already had full height)
	if zoomedWidth <= rightWidth {
		t.Fatalf("expected zoomed pane to be wider: was %d, now %d", rightWidth, zoomedWidth)
	}

	// Height should be at least as tall as before
	if zoomedHeight < rightHeight {
		t.Fatalf("expected zoomed pane height to be at least %d, got %d", rightHeight, zoomedHeight)
	}

	t.Logf("TView zoom in verified: %dx%d -> %dx%d",
		rightWidth, rightHeight, zoomedWidth, zoomedHeight)

	// Now zoom out: Send 'z' again
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

	t.Logf("TView zoom out verified: %dx%d -> %dx%d -> %dx%d",
		rightWidth, rightHeight, zoomedWidth, zoomedHeight, restoredWidth, restoredHeight)
}

func TestTViewAppPaneResize(t *testing.T) {
	socketPath, _, _, cleanup := setupTViewTreeTestServer(t)
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

	// Now resize: Enter control mode with Ctrl+A
	if err := client.SendKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl); err != nil {
		t.Fatalf("failed to enter control mode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// First Ctrl+Left selects the border
	if err := client.SendKey(tcell.KeyLeft, 0, tcell.ModCtrl); err != nil {
		t.Fatalf("failed to send first Ctrl+Left: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Second Ctrl+Left resizes
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
	if leftWidth2 >= leftWidth1 {
		t.Fatalf("expected left pane to shrink: was %d, now %d", leftWidth1, leftWidth2)
	}

	if rightWidth2 <= rightWidth1 {
		t.Fatalf("expected right pane to grow: was %d, now %d", rightWidth1, rightWidth2)
	}

	t.Logf("TView resize verified: left %d->%d (%+d), right %d->%d (%+d)",
		leftWidth1, leftWidth2, leftWidth2-leftWidth1,
		rightWidth1, rightWidth2, rightWidth2-rightWidth1)
}

func TestTViewAppPaneSwap(t *testing.T) {
	socketPath, _, _, cleanup := setupTViewTreeTestServer(t)
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

	t.Logf("TView swap verified: panes successfully swapped positions")
}

func TestTViewAppPaneClose(t *testing.T) {
	socketPath, _, _, cleanup := setupTViewTreeTestServer(t)
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

func TestTViewAppTerminalResize(t *testing.T) {
	socketPath, _, _, cleanup := setupTViewTreeTestServer(t)
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

	// Verify new dimensions are close to requested
	if newWidth < 95 || newWidth > 100 {
		t.Fatalf("expected width ~100, got %d", newWidth)
	}

	if newHeight < 25 || newHeight > 30 {
		t.Fatalf("expected height ~30, got %d", newHeight)
	}

	t.Logf("TView terminal resize verified: %dx%d -> %dx%d",
		initialWidth, initialHeight, newWidth, newHeight)
}
