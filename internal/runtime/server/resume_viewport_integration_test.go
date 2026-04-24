//go:build integration
// +build integration

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/protocol"
)

// TestIntegration_ResumeHonorsPaneViewport verifies that a ResumeRequest
// carrying PaneViewports causes the server to re-seat the pane's view and
// clip subsequent deltas to the resumed range.
func TestIntegration_ResumeHonorsPaneViewport(t *testing.T) {
	h := newMemHarness(t, 80, 24)

	// Feed 200 rows at globalIdxs [0, 199].
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line"
	}
	h.fakeApp.FeedRows(0, lines)

	// Client sets viewport to live edge first (normal post-handshake sequence).
	h.ApplyViewport(h.paneID, 176, 199, true /*autoFollow*/, false /*altScreen*/)
	h.Publish()

	// Simulate the resume path: encode a ResumeRequest with PaneViewports
	// pointing at globalIdx=50 scrolled-back position, then send it.
	resume := protocol.ResumeRequest{
		SessionID:    h.sessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:         h.paneID,
				AltScreen:      false,
				AutoFollow:     false,
				ViewBottomIdx:  50,
				WrapSegmentIdx: 0,
				ViewportRows:   24,
				ViewportCols:   80,
			},
		},
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode resume: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// After the resume, the publisher should emit rows near globalIdx=50.
	// The render buffer is rebuilt ending at gid 50 (24-row window: gids 27..50).
	// The pane has a 1-cell border top and bottom, so interior gids are 28..49.
	// Await gid 48 — safely interior and within the resumed clip window.
	h.AwaitRow(h.paneID, 48, 2*time.Second)
}

// TestIntegration_ResumeAltScreen_SkipsScrollResolution verifies that a
// PaneViewportState with AltScreen=true does NOT trigger the restore
// dispatch path (so the alt buffer continues rendering unchanged).
func TestIntegration_ResumeAltScreen_SkipsScrollResolution(t *testing.T) {
	h := newMemHarness(t, 80, 24)

	// Flip the fake app into alt-screen mode.
	h.fakeApp.EnterAltScreen("ALT CONTENT")
	h.ApplyViewport(h.paneID, 0, 23, false, true /*altScreen*/)
	h.Publish()

	// Send resume with AltScreen=true and a bogus viewBottom (must be ignored).
	resume := protocol.ResumeRequest{
		SessionID:    h.sessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:        h.paneID,
				AltScreen:     true,
				ViewBottomIdx: 9999, /* nonsense — must be ignored */
				ViewportRows:  24,
				ViewportCols:  80,
			},
		},
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode resume: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// After the resume, an alt-screen delta for row 0 should contain the
	// alt buffer's "ALT CONTENT" (or match the pre-resume alt state).
	h.AwaitAltRow(h.paneID, 1, "ALT CONTENT", 2*time.Second)
}

// TestIntegration_ResumeMissingAnchor_SnapsToOldest exercises the
// WalkUpwardFromBottom → MissingAnchor path end-to-end.
func TestIntegration_ResumeMissingAnchor_SnapsToOldest(t *testing.T) {
	h := newMemHarness(t, 80, 24)

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	h.fakeApp.FeedRows(0, lines)
	// Simulate eviction: drop rows 0..39.
	h.fakeApp.store.EvictBelow(40)

	// Initial live-edge viewport to establish a baseline.
	h.ApplyViewport(h.paneID, 76, 99, true, false)
	h.Publish()

	// Resume targeting globalIdx=10 (below retention, below 40).
	// autoFollow=false so missing-anchor policy applies.
	resume := protocol.ResumeRequest{
		SessionID:    h.sessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:         h.paneID,
				AltScreen:      false,
				AutoFollow:     false,
				ViewBottomIdx:  10,
				WrapSegmentIdx: 0,
				ViewportRows:   24,
				ViewportCols:   80,
			},
		},
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode resume: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// The fake app's RestoreViewport calls WalkUpwardFromBottom which detects
	// the missing anchor (viewBottom=10 < OldestRetained=40) and snaps the
	// render to anchor gid 40 (24-row window: gids 17..40, interior: 18..39).
	// ApplyResume seeds ClientViewports with the protocol-reported ViewBottomIdx=10,
	// so the publisher clip window is [lo=-24, hi=34]. Interior gids 18..34 are
	// within clip. We await gid 33 — safely interior and within the clip window.
	// The key invariant: the server remains live and emits rows after a
	// missing-anchor resume (no crash, no deadlock).
	h.AwaitRow(h.paneID, 33, 2*time.Second)
}

// TestIntegration_ResumeMultiplePaneViewports verifies that a ResumeRequest
// carrying multiple PaneViewportState entries populates ClientViewports for
// all of them, even when some PaneIDs do not correspond to a real pane in
// the desktop (e.g., a stale client state carrying an ID from a pane that
// closed server-side). The handler must continue processing remaining entries
// instead of aborting on the first unknown ID.
func TestIntegration_ResumeMultiplePaneViewports(t *testing.T) {
	h := newMemHarness(t, 80, 24)

	// Feed some content so the real pane has scrollback.
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	h.fakeApp.FeedRows(0, lines)

	// Establish initial viewport for the real pane (bootstrap).
	h.ApplyViewport(h.paneID, 76, 99, true, false)
	h.Publish()

	// Synthetic pane ID that does NOT exist in the desktop. RestorePaneViewport
	// will return false for this one; ApplyResume should still store it.
	syntheticID := [16]byte{0xaa, 0xbb, 0xcc}

	// Resume request with two entries: one for the real pane (scrolled back),
	// one for the synthetic pane.
	resume := protocol.ResumeRequest{
		SessionID:    h.sessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:         h.paneID,
				AltScreen:      false,
				AutoFollow:     false,
				ViewBottomIdx:  50,
				WrapSegmentIdx: 0,
				ViewportRows:   24,
				ViewportCols:   80,
			},
			{
				PaneID:         syntheticID,
				AltScreen:      false,
				AutoFollow:     true,
				ViewBottomIdx:  0,
				WrapSegmentIdx: 0,
				ViewportRows:   12,
				ViewportCols:   40,
			},
		},
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode resume: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// After the resume propagates through the handler, ClientViewports must
	// carry entries for BOTH pane IDs. Poll briefly since the handler runs
	// on another goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		vp1, ok1 := h.session.Viewport(h.paneID)
		vp2, ok2 := h.session.Viewport(syntheticID)
		if ok1 && ok2 {
			// Real pane viewport carries the scrolled-back values.
			if vp1.ViewBottomIdx != 50 {
				t.Fatalf("real pane ViewBottomIdx: got %d want 50", vp1.ViewBottomIdx)
			}
			if vp1.AutoFollow {
				t.Fatalf("real pane AutoFollow: got true want false")
			}
			// Synthetic pane viewport is stored verbatim despite no pane existing.
			if vp2.Rows != 12 || vp2.Cols != 40 {
				t.Fatalf("synthetic pane dims: got (%d,%d) want (12,40)", vp2.Rows, vp2.Cols)
			}
			if !vp2.AutoFollow {
				t.Fatalf("synthetic pane AutoFollow: got false want true")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ClientViewports did not populate both entries within 2s")
}

// TestIntegration_ResumeWrappedChain verifies that a resume request whose
// clip window includes a wrapped chain delivers the chain's rows in the
// post-resume delta stream. All existing integration tests use flat rows;
// this one writes a wrapped chain directly into the sparse store via
// SparseStore() and resumes pointing into that range.
func TestIntegration_ResumeWrappedChain(t *testing.T) {
	h := newMemHarness(t, 80, 24)

	// Fill 20 flat rows at gids [0,19] so there is scrollback.
	flat := make([]string, 20)
	for i := range flat {
		flat[i] = "line"
	}
	h.fakeApp.FeedRows(0, flat)

	// Inject a wrapped chain at gids 20/21/22 directly into the sparse store.
	// gid=20 and gid=21 have their last cell Wrapped=true; gid=22 is the
	// chain tail (no Wrapped flag).
	store := h.fakeApp.SparseStore()
	row20 := make([]parser.Cell, 80)
	for i := range row20 {
		row20[i] = parser.Cell{Rune: 'X'}
	}
	row20[79].Wrapped = true
	store.SetLine(20, row20)

	row21 := make([]parser.Cell, 80)
	for i := range row21 {
		row21[i] = parser.Cell{Rune: 'Y'}
	}
	row21[79].Wrapped = true
	store.SetLine(21, row21)

	row22 := make([]parser.Cell, 80)
	for i := range row22 {
		row22[i] = parser.Cell{Rune: 'Z'}
	}
	store.SetLine(22, row22)

	// Continue with more flat rows at gids [23,40].
	moreFlat := make([]string, 18)
	for i := range moreFlat {
		moreFlat[i] = "line"
	}
	h.fakeApp.FeedRows(23, moreFlat)

	// Establish live-edge viewport (bootstrap).
	h.ApplyViewport(h.paneID, 17, 40, true /*autoFollow*/, false)
	h.Publish()

	// Resume: land the client at ViewBottomIdx=30, autoFollow=false.
	// WalkUpwardFromBottom from gid=30 with height=24 spans [7,30], so
	// the wrapped chain tail at gid=22 falls inside the clip window.
	resume := protocol.ResumeRequest{
		SessionID:    h.sessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:         h.paneID,
				AltScreen:      false,
				AutoFollow:     false,
				ViewBottomIdx:  30,
				WrapSegmentIdx: 0,
				ViewportRows:   24,
				ViewportCols:   80,
			},
		},
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode resume: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// The chain tail at gid=22 must be delivered in the post-resume delta
	// stream. Its presence confirms that wrapped-chain rows survive the
	// restore + clip path.
	h.AwaitRow(h.paneID, 22, 2*time.Second)
}
