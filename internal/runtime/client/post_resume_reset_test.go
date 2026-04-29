// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import (
	"sync/atomic"
	"testing"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/protocol"
)

func newStateWithCache(cache *client.BufferCache) *clientState {
	return &clientState{
		cache:      cache,
		paneCaches: make(map[[16]byte]*client.PaneCache),
	}
}

// seedRevision sets a pane's Revision to v in the cache. Using the
// minimal BufferDelta shape required by ApplyDelta — only PaneID and
// Revision are needed to populate the per-pane cache entry that
// PaneRevision reads. Other BufferDelta fields are not relevant.
func seedRevision(t *testing.T, cache *client.BufferCache, paneID [16]byte, v uint32) {
	t.Helper()
	cache.ApplyDelta(protocol.BufferDelta{PaneID: paneID, Revision: v})
}

func TestApplyPostResumeReset_FlagSet_ResetsRevisionAndSequence(t *testing.T) {
	cache := client.NewBufferCache()
	seedRevision(t, cache, [16]byte{0xaa}, 50)
	if got := cache.PaneRevision([16]byte{0xaa}); got != 50 {
		t.Fatalf("seed: got revision=%d want 50", got)
	}

	state := newStateWithCache(cache)
	state.resetOnNextSnapshot.Store(true)
	var lastSeq atomic.Uint64
	lastSeq.Store(9999)

	applyPostResumeReset(state, &lastSeq)

	if state.resetOnNextSnapshot.Load() {
		t.Fatalf("flag must be cleared after consumption")
	}
	if got := cache.PaneRevision([16]byte{0xaa}); got != 0 {
		t.Fatalf("revision: got %d want 0", got)
	}
	if got := lastSeq.Load(); got != 0 {
		t.Fatalf("lastSequence: got %d want 0", got)
	}
}

func TestApplyPostResumeReset_FlagUnset_NoReset(t *testing.T) {
	cache := client.NewBufferCache()
	seedRevision(t, cache, [16]byte{0xaa}, 7)

	state := newStateWithCache(cache)
	var lastSeq atomic.Uint64
	lastSeq.Store(123)

	applyPostResumeReset(state, &lastSeq)

	if got := cache.PaneRevision([16]byte{0xaa}); got != 7 {
		t.Fatalf("revision should not be touched: got %d want 7", got)
	}
	if got := lastSeq.Load(); got != 123 {
		t.Fatalf("lastSequence should not be touched: got %d want 123", got)
	}
}

func TestApplyPostResumeReset_FiresExactlyOnce(t *testing.T) {
	cache := client.NewBufferCache()
	seedRevision(t, cache, [16]byte{0xaa}, 99)
	state := newStateWithCache(cache)
	state.resetOnNextSnapshot.Store(true)

	var lastSeq atomic.Uint64
	lastSeq.Store(500)

	applyPostResumeReset(state, &lastSeq)
	if got := cache.PaneRevision([16]byte{0xaa}); got != 0 {
		t.Fatalf("first call should reset revision: got %d", got)
	}
	if got := lastSeq.Load(); got != 0 {
		t.Fatalf("first call should reset sequence: got %d", got)
	}

	seedRevision(t, cache, [16]byte{0xaa}, 1)
	lastSeq.Store(42)

	applyPostResumeReset(state, &lastSeq)
	if got := cache.PaneRevision([16]byte{0xaa}); got != 1 {
		t.Fatalf("second call must not zero revision: got %d want 1", got)
	}
	if got := lastSeq.Load(); got != 42 {
		t.Fatalf("second call must not zero sequence: got %d want 42", got)
	}
}

func TestApplyPostResumeReset_NilSequenceIsNotADereference(t *testing.T) {
	cache := client.NewBufferCache()
	seedRevision(t, cache, [16]byte{0xaa}, 3)
	state := newStateWithCache(cache)
	state.resetOnNextSnapshot.Store(true)
	applyPostResumeReset(state, nil)
	if got := cache.PaneRevision([16]byte{0xaa}); got != 0 {
		t.Fatalf("nil lastSeq should still reset cache: got %d", got)
	}
}

// TestPostResumeReset_ClearsDecorationMissTracker verifies the
// decoration-miss dedup tracker is cleared when the post-resume reset
// fires. Issue #199 Task 11 / Task 9 Step 4.
func TestPostResumeReset_ClearsDecorationMissTracker(t *testing.T) {
	cache := client.NewBufferCache()
	state := newStateWithCache(cache)
	state.logDecorationMissOnce([16]byte{0xab}, 5)
	if len(state.decorMissSeen) != 1 {
		t.Fatalf("expected 1 entry pre-reset, got %d", len(state.decorMissSeen))
	}
	state.resetOnNextSnapshot.Store(true)
	applyPostResumeReset(state, nil)
	if len(state.decorMissSeen) != 0 {
		t.Fatalf("expected decorMissSeen cleared, got %d entries", len(state.decorMissSeen))
	}
}
