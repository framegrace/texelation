// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Regression: a selection that reaches into rows the in-memory sparse
// store no longer holds (because they were dropped on session reload
// rather than being faulted in eagerly) must still copy correctly.
// GetContentText falls back to the PageStore for any gid the sparse
// store can't serve. Without the fallback, ReadLine returns nil and the
// row would be silently dropped from the copied text.

package parser

import (
	"strings"
	"testing"
)

func TestGetContentText_FaultsEvictedRowsFromPageStore(t *testing.T) {
	dir := t.TempDir()
	id := "evict-fault"

	// Session 1: write 200 lines, then close so everything lands in
	// the PageStore.
	v1 := newTestVTerm(t, 80, 24, dir, id)
	p := NewParser(v1)
	const total = 200
	for i := 0; i < total; i++ {
		s := "row " + intToString(i) + "\r\n"
		for _, r := range s {
			p.Parse(r)
		}
	}
	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// Session 2: reopen the same disk-backed buffer. Recovery loads a
	// recent window into the sparse store; gids well below the recent
	// edge typically aren't in memory anymore.
	v2 := newTestVTerm(t, 80, 24, dir, id)
	defer v2.CloseMemoryBuffer()

	if v2.mainScreenPageStore == nil {
		t.Fatal("mainScreenPageStore nil; cannot exercise fault path")
	}
	if line, err := v2.mainScreenPageStore.ReadLine(10); err != nil || line == nil {
		t.Fatalf("PageStore.ReadLine(10) after reload: line=%v err=%v", line, err)
	}

	// Drop gids 0..49 from the in-memory sparse store WITHOUT touching
	// persistence — ClearRange (rather than ClearRangePersistent)
	// preserves the PageStore copies, which is exactly the eviction
	// scenario we want to exercise.
	v2.mainScreen.ClearRange(0, 49)
	if cells := v2.mainScreen.ReadLine(10); cells != nil {
		t.Fatalf("expected gid=10 evicted from sparse store; got %d cells", len(cells))
	}

	// Selection spans the eviction boundary: gids 5..15. Without the
	// fault path, gids 5..15 (all evicted) drop and the captured text
	// is empty.
	text := v2.GetContentText(5, 0, 15, 0)
	for i := 5; i < 15; i++ {
		marker := "row " + intToString(i)
		if !strings.Contains(text, marker) {
			t.Errorf("captured text missing gid=%d marker %q\nfull text:\n%s", i, marker, text)
			return
		}
	}
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	negative := n < 0
	if negative {
		n = -n
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
