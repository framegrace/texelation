// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import "testing"

func TestViewportTrackers_SetBottomWrapSegment(t *testing.T) {
	trackers := newViewportTrackers()
	id := [16]byte{0x11}
	vp := trackers.get(id)
	vp.mu.Lock()
	vp.Rows = 24
	vp.Cols = 80
	vp.ViewBottomIdx = 100
	vp.mu.Unlock()

	trackers.SetBottomWrapSegment(id, 3)

	vp2 := trackers.get(id)
	vp2.mu.Lock()
	got := vp2.WrapSegmentIdx
	vp2.mu.Unlock()
	if got != 3 {
		t.Fatalf("WrapSegmentIdx: got %d want 3", got)
	}
}

func TestViewportTrackers_SetBottomWrapSegment_MissingPane_NoCrash(t *testing.T) {
	trackers := newViewportTrackers()
	trackers.SetBottomWrapSegment([16]byte{0xff}, 5) // pane doesn't exist — must not panic
}

func TestSnapshotEntry_CarriesWrapSegment(t *testing.T) {
	trackers := newViewportTrackers()
	id := [16]byte{0x22}
	vp := trackers.get(id)
	vp.mu.Lock()
	vp.Rows = 24
	vp.Cols = 80
	vp.ViewBottomIdx = 50
	vp.WrapSegmentIdx = 7
	vp.dirty = true
	vp.mu.Unlock()

	entries, _ := trackers.snapshotDirty()
	if len(entries) != 1 {
		t.Fatalf("snapshotDirty: got %d entries want 1", len(entries))
	}
	if entries[0].vp.WrapSegmentIdx != 7 {
		t.Fatalf("WrapSegmentIdx in snapshot: got %d want 7", entries[0].vp.WrapSegmentIdx)
	}
}
