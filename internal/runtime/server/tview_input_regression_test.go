//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/welcome"
	"texelation/internal/runtime/server/testutil"
	"texelation/protocol"
	"texelation/texel"
)

// TestInteractiveDemoPublishesSingleFramePerKey verifies that the interactive demo
// only produces a single buffer delta for each keypress once focus is on the form.
// This guards against the flicker/regression where an immediate publish races with
// tview's eventual Show() frame.
func TestInteractiveDemoPublishesSingleFramePerKey(t *testing.T) {
	lifecycle := &texel.LocalAppLifecycle{}
	driver := tviewTestDriver{}
	desktop, err := texel.NewDesktopEngineWithDriver(driver, welcome.NewInteractiveDemo, welcome.NewInteractiveDemo, lifecycle)
	if err != nil {
		t.Fatalf("failed to create desktop: %v", err)
	}
	defer desktop.Close()

	mgr := NewManager()
	sink := NewDesktopSink(desktop)

	clientConn, serverConn := net.Pipe()

	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		session, resuming, err := handleHandshake(serverConn, mgr)
		if err != nil {
			serverDone <- err
			return
		}
		publisher := NewDesktopPublisher(desktop, session)
		sink.SetPublisher(publisher)

		if snapshot, err := sink.Snapshot(); err == nil && len(snapshot.Panes) > 0 {
			if payload, err := protocol.EncodeTreeSnapshot(snapshot); err == nil {
				header := protocol.Header{
					Version:   protocol.Version,
					Type:      protocol.MsgTreeSnapshot,
					Flags:     protocol.FlagChecksum,
					SessionID: session.ID(),
				}
				_ = protocol.WriteMessage(serverConn, header, payload)
			}
		}
		_ = publisher.Publish()

		conn := newConnection(serverConn, session, sink, resuming)
		serverDone <- conn.serve()
	}()

	client := testutil.NewTestClientWithConn(t, clientConn)

	snapshot := client.WaitForInitialSnapshot()
	if len(snapshot.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(snapshot.Panes))
	}
	paneID := snapshot.Panes[0].PaneID

	// Drain any immediate renders without blocking. The initial snapshot already
	// carries full buffer content, so we don't require a delta here.
	client.DrainDeltas()
	client.DrainSnapshots()

	// Tab once to move focus to the form (second widget), matching manual repro steps.
	if err := client.SendKey(tcell.KeyTab, '\t', tcell.ModNone); err != nil {
		t.Fatalf("failed to send tab key: %v", err)
	}
	client.WaitForBufferDelta(paneID, 2*time.Second)
	client.DrainDeltas()

	verifyKeyFrame := func(r rune) {
		if err := client.SendKey(tcell.KeyRune, r, tcell.ModNone); err != nil {
			t.Fatalf("failed to send rune %q: %v", r, err)
		}
		delta := client.WaitForBufferDelta(paneID, 2*time.Second)
		if delta.PaneID != paneID {
			t.Fatalf("expected delta for pane %x, got %x", paneID[:4], delta.PaneID[:4])
		}
		if !client.PaneContains(paneID, "Interactive") {
			t.Fatalf("pane %x missing expected content after key %q", paneID[:4], r)
		}
	}

	for _, ch := range []rune{'a', 'b', 'c', 'd'} {
		verifyKeyFrame(ch)
	}

	// Close client and ensure server goroutine exits.
	client.Close()
	select {
	case err := <-serverDone:
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("server connection error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not exit after client closed")
	}

	if sink.scheduler != nil {
		if count := sink.scheduler.FallbackCount(); count != 0 {
			t.Fatalf("fallback publishes triggered (%d)", count)
		}
	}

}
