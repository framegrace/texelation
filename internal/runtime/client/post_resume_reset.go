// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// post_resume_reset.go: one-shot synchronization barrier for daemon-restart
// resume. See spec at
// docs/superpowers/specs/2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md.

package clientruntime

import "sync/atomic"

// applyPostResumeReset zeros per-pane Revision counters and the
// top-level lastSequence iff state.resetOnNextSnapshot was set. The CAS
// guarantees one-shot semantics: only the FIRST snapshot after the flag
// is armed consumes it. Steady-state snapshots (workspace ops, splits)
// pass through untouched. The reset matches the new daemon's
// restart-from-zero numbering so the BufferCache stops dedup-dropping
// fresh deltas as "stale."
//
// lastSequence may be nil — caller's choice (some test paths). When
// nil, only the cache is reset.
func applyPostResumeReset(state *clientState, lastSequence *atomic.Uint64) {
	if !state.resetOnNextSnapshot.CompareAndSwap(true, false) {
		return
	}
	state.cache.ResetRevisions()
	if lastSequence != nil {
		lastSequence.Store(0)
	}
}
