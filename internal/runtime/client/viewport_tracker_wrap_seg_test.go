// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import "testing"

func TestSnapshotEntry_CarriesWrapSegment(t *testing.T) {
	trackers := newViewportTrackers()
	id := [16]byte{0x22}
	vp := trackers.get(id)
	vp.mu.Lock()
	vp.Rows = 24
	vp.Cols = 80
	vp.ViewBottomIdx = 50
	vp.WrapSegmentIdx = 7 // future renderer will set this; for now test the plumbing
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
