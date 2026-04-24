// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
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
