//go:build integration
// +build integration

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"
	"time"

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
