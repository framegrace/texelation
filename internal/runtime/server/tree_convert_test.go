// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/tree_convert_test.go
// Summary: Exercises protocol<->tree capture conversions for the server runtime.
// Usage: Executed during `go test` to guard against regressions.

package server

import (
	"testing"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

func TestProtocolToTreeCapturePopulatesRowGlobalIdx(t *testing.T) {
	paneID := [16]byte{0x01, 0x02, 0x03}
	snap := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{
			{
				PaneID: paneID,
				Title:  "pane",
				Rows:   []string{"abc", "def", "ghi"},
				X:      0, Y: 0, Width: 3, Height: 3,
			},
		},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	}

	capture := protocolToTreeCapture(snap)
	if len(capture.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(capture.Panes))
	}
	pane := capture.Panes[0]
	if len(pane.Buffer) != 3 {
		t.Fatalf("expected buffer length 3, got %d", len(pane.Buffer))
	}
	if len(pane.RowGlobalIdx) != len(pane.Buffer) {
		t.Fatalf("RowGlobalIdx must parallel Buffer: len(RowGlobalIdx)=%d len(Buffer)=%d", len(pane.RowGlobalIdx), len(pane.Buffer))
	}
	for i, idx := range pane.RowGlobalIdx {
		if idx != -1 {
			t.Fatalf("expected reconstructed RowGlobalIdx[%d] == -1, got %d", i, idx)
		}
	}
}

func TestProtocolToTreeCaptureEmptyRows(t *testing.T) {
	// A pane with no rows produces a zero-length Buffer; the invariant
	// holds trivially (len(RowGlobalIdx) == len(Buffer) == 0) whether
	// RowGlobalIdx is nil or an empty slice.
	snap := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{
			{PaneID: [16]byte{0xaa}, Title: "empty", Rows: nil, Width: 0, Height: 0},
		},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	}

	capture := protocolToTreeCapture(snap)
	pane := capture.Panes[0]
	if len(pane.RowGlobalIdx) != len(pane.Buffer) {
		t.Fatalf("invariant violated: len(RowGlobalIdx)=%d len(Buffer)=%d", len(pane.RowGlobalIdx), len(pane.Buffer))
	}
}

func TestTreeCaptureToProtocol_PassesContentBounds(t *testing.T) {
	capture := texel.TreeCapture{
		Panes: []texel.PaneSnapshot{{
			ID:             [16]byte{0xab},
			Title:          "t",
			ContentTopRow:  2,
			NumContentRows: 16,
		}},
	}
	snap := treeCaptureToProtocol(capture)
	if len(snap.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(snap.Panes))
	}
	if snap.Panes[0].ContentTopRow != 2 || snap.Panes[0].NumContentRows != 16 {
		t.Fatalf("content bounds not passed through forward: top=%d num=%d",
			snap.Panes[0].ContentTopRow, snap.Panes[0].NumContentRows)
	}

	roundTrip := protocolToTreeCapture(snap)
	if len(roundTrip.Panes) != 1 {
		t.Fatalf("expected 1 pane after reverse, got %d", len(roundTrip.Panes))
	}
	if roundTrip.Panes[0].ContentTopRow != 2 || roundTrip.Panes[0].NumContentRows != 16 {
		t.Fatalf("content bounds not passed through reverse: top=%d num=%d",
			roundTrip.Panes[0].ContentTopRow, roundTrip.Panes[0].NumContentRows)
	}
}
