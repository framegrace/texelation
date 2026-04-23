// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func makeRow(n int, ch rune) []parser.Cell {
	out := make([]parser.Cell, n)
	for i := range out {
		out[i] = parser.Cell{Rune: ch}
	}
	return out
}

// Populate a store with N unwrapped rows of content at globalIdx [0, N-1].
// Each row is NoWrap-set so reflow leaves them 1:1.
func makeFlatStore(t *testing.T, n int, width int) *Store {
	t.Helper()
	s := NewStore(width)
	for i := 0; i < n; i++ {
		s.SetLineWithNoWrap(int64(i), makeRow(width, 'a'+rune(i%26)), true)
	}
	return s
}

func TestWalkUpwardFromBottom_FlatRows_TailAligned(t *testing.T) {
	// 100 unwrapped rows, viewport 24x80. viewBottom=99, wrapSeg=0.
	// Walk should land viewAnchor=99-23=76, offset=0, policy=AnchorInStore.
	s := makeFlatStore(t, 100, 80)
	anchor, offset, policy := WalkUpwardFromBottom(s, 99, 0, 24, 80, false)
	if policy != WalkPolicyAnchorInStore {
		t.Fatalf("policy: got %v want WalkPolicyAnchorInStore", policy)
	}
	if anchor != 76 {
		t.Fatalf("anchor: got %d want 76", anchor)
	}
	if offset != 0 {
		t.Fatalf("offset: got %d want 0", offset)
	}
}

func TestWalkUpwardFromBottom_MissingAnchor_SnapsToOldest(t *testing.T) {
	// 100 unwrapped rows but oldestRetained=50 (simulated by EvictBelow).
	// Ask viewBottom=10 (below oldest). Expect MissingAnchor + anchor=50.
	s := makeFlatStore(t, 100, 80)
	s.EvictBelow(50)
	anchor, _, policy := WalkUpwardFromBottom(s, 10, 0, 24, 80, false)
	if policy != WalkPolicyMissingAnchor {
		t.Fatalf("policy: got %v want WalkPolicyMissingAnchor", policy)
	}
	if anchor != 50 {
		t.Fatalf("anchor: got %d want 50 (oldest retained)", anchor)
	}
}

func TestWalkUpwardFromBottom_WrappedChain_TailSubRow(t *testing.T) {
	// 20 flat rows [0,19], one wrapped chain spanning gids [20,22] reflowing
	// to 3 sub-rows at width 80, then 3 flat NoWrap rows at gids [23,25].
	//
	// The chain is built with Wrapped=true on the last cell of gids 20 and 21,
	// and Wrapped=false (default) on the last cell of gid 22. This is the
	// correct sparse semantics: walkChain walks forward while the last cell of
	// the current row has Wrapped=true and the next row exists in the store.
	//
	// viewBottom=25 (flat), wrapSeg=0, viewport height=5 at width=80.
	// Display: ..., row 23 (flat), row 24 (flat), row 25 (flat) occupy
	// bottom 3 rows; above: 2 sub-rows of the chain (sub-rows 2 and 1).
	// Top anchor = chain head gid=20 with offset=1.
	s := NewStore(80)
	for i := 0; i < 20; i++ {
		s.SetLineWithNoWrap(int64(i), makeRow(80, 'a'), true)
	}
	// Build a 3-sub-row wrapped chain: gid=20 and gid=21 each have their
	// last cell Wrapped=true; gid=22 has Wrapped=false (plain content).
	row20 := makeRow(80, 'W')
	row20[79].Wrapped = true
	s.SetLine(20, row20)

	row21 := makeRow(80, 'W')
	row21[79].Wrapped = true
	s.SetLine(21, row21)

	row22 := makeRow(80, 'W')
	s.SetLine(22, row22)

	for i := 0; i < 3; i++ {
		s.SetLineWithNoWrap(int64(23+i), makeRow(80, 'z'), true)
	}
	anchor, offset, policy := WalkUpwardFromBottom(s, 25, 0, 5, 80, false)
	if policy != WalkPolicyAnchorInStore {
		t.Fatalf("policy: got %v want WalkPolicyAnchorInStore", policy)
	}
	if anchor != 20 {
		t.Fatalf("anchor: got %d want 20 (chain head)", anchor)
	}
	if offset != 1 {
		t.Fatalf("offset: got %d want 1", offset)
	}
}

func TestViewWindow_SetAutoFollow(t *testing.T) {
	v := NewViewWindow(80, 24)
	if !v.IsFollowing() {
		t.Fatalf("IsFollowing default: got false want true")
	}
	v.SetAutoFollow(false)
	if v.IsFollowing() {
		t.Fatalf("after SetAutoFollow(false): got true want false")
	}
	v.SetAutoFollow(true)
	if !v.IsFollowing() {
		t.Fatalf("after SetAutoFollow(true): got false want true")
	}
}

func TestViewWindow_SetViewBottom(t *testing.T) {
	v := NewViewWindow(80, 24)
	v.SetViewBottom(100)
	top, bottom := v.VisibleRange()
	if bottom != 100 {
		t.Fatalf("VisibleRange bottom: got %d want 100", bottom)
	}
	if top != 100-24+1 {
		t.Fatalf("VisibleRange top: got %d want %d", top, 100-24+1)
	}
}
