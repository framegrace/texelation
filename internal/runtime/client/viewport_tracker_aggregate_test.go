// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import "testing"

func TestViewportTrackers_SnapshotAll(t *testing.T) {
	trackers := newViewportTrackers()
	id1 := [16]byte{1}
	id2 := [16]byte{2}
	vp1 := trackers.get(id1)
	vp1.mu.Lock()
	vp1.Rows = 24
	vp1.Cols = 80
	vp1.ViewBottomIdx = 100
	vp1.AutoFollow = false
	vp1.WrapSegmentIdx = 2
	vp1.mu.Unlock()
	vp2 := trackers.get(id2)
	vp2.mu.Lock()
	vp2.Rows = 10
	vp2.Cols = 40
	vp2.AltScreen = true
	vp2.mu.Unlock()

	got := trackers.snapshotAll()
	if len(got) != 2 {
		t.Fatalf("snapshotAll len: got %d want 2", len(got))
	}
	byID := make(map[[16]byte]paneViewportCopy, 2)
	for _, e := range got {
		byID[e.id] = e.vp
	}
	if byID[id1].ViewBottomIdx != 100 || byID[id1].WrapSegmentIdx != 2 {
		t.Fatalf("pane 1: got %+v", byID[id1])
	}
	if !byID[id2].AltScreen {
		t.Fatalf("pane 2 AltScreen: got false")
	}
}

func TestViewportTrackers_SnapshotAll_SkipsZeroDim(t *testing.T) {
	trackers := newViewportTrackers()
	trackers.get([16]byte{1}) // zero Rows/Cols — not yet initialised
	got := trackers.snapshotAll()
	if len(got) != 0 {
		t.Fatalf("snapshotAll len: got %d want 0 (zero-dim entries skipped)", len(got))
	}
}
