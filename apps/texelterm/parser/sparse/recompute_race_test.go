// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"sync"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// TestRecomputeLiveAnchor_RespectsConcurrentAutoFollowFlip is a regression
// test for the race where RecomputeLiveAnchor reads autoFollow=true at
// entry, does its unlocked walk, and then clobbers viewAnchor that was set
// by a concurrent ApplyResumeState(..., autoFollow=false).
//
// The test runs a tight loop: half the iterations run RecomputeLiveAnchor,
// half run ApplyResumeState with a distinguishable anchor. After the loop,
// the ViewWindow must be in one of two legitimate states (either the
// RecomputeLiveAnchor result OR the ApplyResumeState result) — never a
// hybrid where autoFollow=false but viewAnchor was clobbered to live edge.
func TestRecomputeLiveAnchor_RespectsConcurrentAutoFollowFlip(t *testing.T) {
	s := NewStore(80)
	// Seed some content so the walk has material.
	for i := int64(0); i < 100; i++ {
		row := make([]parser.Cell, 80)
		for j := range row {
			row[j] = parser.Cell{Rune: 'a'}
		}
		s.SetLineWithNoWrap(i, row, true)
	}
	v := NewViewWindow(80, 24)
	const resumedAnchor = int64(42)

	var wg sync.WaitGroup
	const iters = 1000
	for i := 0; i < iters; i++ {
		v.SetAutoFollow(true)
		wg.Add(2)
		go func() {
			defer wg.Done()
			v.RecomputeLiveAnchor(s, 99, 0, 0)
		}()
		go func() {
			defer wg.Done()
			v.ApplyResumeState(resumedAnchor, 0, resumedAnchor, false)
		}()
		wg.Wait()
		// Post-condition: if autoFollow is false, viewAnchor MUST be resumedAnchor.
		// (It is legal for RecomputeLiveAnchor to have fully won, leaving autoFollow=true
		// and anchor at live edge — since the goroutines race, either order is fine.
		// The bug manifests as autoFollow=false + viewAnchor != resumedAnchor.)
		if !v.IsFollowing() {
			anchor, _ := v.Anchor()
			if anchor != resumedAnchor {
				t.Fatalf("iter %d: autoFollow=false but anchor=%d (want %d — RecomputeLiveAnchor clobbered ApplyResumeState write)", i, anchor, resumedAnchor)
			}
		}
	}
}
