// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"math"
	"testing"

	"github.com/framegrace/texelation/protocol"
)

func TestClientViewports_ApplyResume(t *testing.T) {
	cv := NewClientViewports()
	states := []protocol.PaneViewportState{
		{PaneID: [16]byte{1}, ViewBottomIdx: 100, AutoFollow: false, ViewportRows: 24, ViewportCols: 80},
		{PaneID: [16]byte{2}, AltScreen: true, ViewportRows: 24, ViewportCols: 80},
	}
	cv.ApplyResume(states)
	got1, ok := cv.Get([16]byte{1})
	if !ok {
		t.Fatalf("pane 1 not stored")
	}
	if got1.ViewBottomIdx != 100 || got1.AutoFollow || got1.AltScreen {
		t.Fatalf("pane 1: got %+v", got1)
	}
	// ViewTopIdx derived from ViewBottomIdx - Rows + 1 = 100 - 23 = 77.
	if got1.ViewTopIdx != 77 {
		t.Fatalf("pane 1 ViewTopIdx: got %d want 77", got1.ViewTopIdx)
	}
	got2, ok := cv.Get([16]byte{2})
	if !ok {
		t.Fatalf("pane 2 not stored")
	}
	if !got2.AltScreen {
		t.Fatalf("pane 2 AltScreen: got false want true")
	}
}

func TestClientViewports_ApplyResume_ClampsNegativeTop(t *testing.T) {
	cv := NewClientViewports()
	cv.ApplyResume([]protocol.PaneViewportState{
		{PaneID: [16]byte{3}, ViewBottomIdx: 5 /* less than Rows-1 */, ViewportRows: 24, ViewportCols: 80},
	})
	got, _ := cv.Get([16]byte{3})
	if got.ViewTopIdx != 0 {
		t.Fatalf("ViewTopIdx: got %d want 0 (clamped)", got.ViewTopIdx)
	}
}

func TestClientViewports_ApplyResume_AutoFollowPreservesFlag(t *testing.T) {
	cv := NewClientViewports()
	cv.ApplyResume([]protocol.PaneViewportState{
		{PaneID: [16]byte{1}, AutoFollow: true, ViewBottomIdx: 500, ViewportRows: 24, ViewportCols: 80},
	})
	got, _ := cv.Get([16]byte{1})
	if !got.AutoFollow {
		t.Fatalf("AutoFollow flag lost: got %+v", got)
	}
	// ViewBottomIdx stored verbatim from payload; publisher ignores it when
	// AutoFollow=true (derives clip from snap.RowGlobalIdx instead).
	if got.ViewBottomIdx != 500 {
		t.Fatalf("ViewBottomIdx: got %d want 500 (stored verbatim)", got.ViewBottomIdx)
	}
}

func TestClientViewports_ApplyResume_NegativeViewBottomOverflowSafe(t *testing.T) {
	cv := NewClientViewports()
	// Malicious / malformed input: ViewBottomIdx = math.MinInt64 would overflow
	// the top-derivation arithmetic if unguarded. The overflow would produce a
	// large positive top that skips the < 0 clamp, leaving ClientViewport in a
	// degenerate state that drops every real row at publish time.
	cv.ApplyResume([]protocol.PaneViewportState{
		{PaneID: [16]byte{2}, AutoFollow: false, ViewBottomIdx: math.MinInt64, ViewportRows: 65535, ViewportCols: 80},
	})
	got, _ := cv.Get([16]byte{2})
	if got.ViewTopIdx < 0 || got.ViewTopIdx > got.ViewBottomIdx+1 {
		// Allow ViewTopIdx=0 (clamped) or anything <= ViewBottomIdx.
		t.Fatalf("overflow unguarded: ViewTopIdx=%d ViewBottomIdx=%d", got.ViewTopIdx, got.ViewBottomIdx)
	}
}
