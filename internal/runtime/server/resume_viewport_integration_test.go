//go:build integration
// +build integration

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	clientruntime "github.com/framegrace/texelation/internal/runtime/client"
	"github.com/framegrace/texelation/internal/runtime/server/testutil"
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

// TestIntegration_ResumeMultiPaneMixedAutoFollow exercises the mixed-autoFollow
// resume path: one pane resumes with AutoFollow=true (live-edge tracking) and
// one with AutoFollow=false (scrolled-back position). The test asserts that
// ClientViewports is populated with both entries and that each entry carries
// the correct AutoFollow value and the correctly seeded ViewBottomIdx.
func TestIntegration_ResumeMultiPaneMixedAutoFollow(t *testing.T) {
	h := newMemHarness(t, 80, 24)
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	h.fakeApp.FeedRows(0, lines)

	syntheticID := [16]byte{0xab, 0xcd, 0xef}

	// Real pane: AutoFollow=true (live-edge tracking).
	// Synthetic pane: AutoFollow=false (specific ViewBottomIdx, scrolled back).
	resume := protocol.ResumeRequest{
		SessionID: h.sessionID(),
		PaneViewports: []protocol.PaneViewportState{
			{PaneID: h.paneID, AutoFollow: true, ViewBottomIdx: 500 /* stale */, ViewportRows: 24, ViewportCols: 80},
			{PaneID: syntheticID, AutoFollow: false, ViewBottomIdx: 50, WrapSegmentIdx: 0, ViewportRows: 12, ViewportCols: 40},
		},
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// Poll for both entries in ClientViewports.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		realVP, okReal := h.session.Viewport(h.paneID)
		synVP, okSyn := h.session.Viewport(syntheticID)
		if okReal && okSyn {
			if !realVP.AutoFollow {
				t.Fatalf("real pane: AutoFollow got false want true")
			}
			// AutoFollow=true entries are stored verbatim (no sentinel). The
			// publisher ignores ViewBottomIdx when AutoFollow=true and derives
			// the clip from snap.RowGlobalIdx instead.
			if realVP.ViewBottomIdx != 500 {
				t.Fatalf("real pane AutoFollow ViewBottomIdx: got %d want 500 (stored verbatim)", realVP.ViewBottomIdx)
			}
			if synVP.AutoFollow {
				t.Fatalf("synthetic pane: AutoFollow got true want false")
			}
			if synVP.ViewBottomIdx != 50 {
				t.Fatalf("synthetic pane: ViewBottomIdx got %d want 50", synVP.ViewBottomIdx)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("both pane viewports not populated within 2s")
}

// TestIntegration_ResumeAtGidInWrappedChainRange verifies that a resume
// request targeting a globalIdx that HAPPENS to have a `Wrapped` flag set in
// the store still delivers that row in the post-resume delta. Does not
// exercise server-side wrap-segment reflow logic — the sparseFakeApp's
// RestoreViewport path calls `rebuildRenderFromStoreLocked` which is a flat
// 1:1 copy, not a reflowing walk. See `WalkUpwardFromBottom` unit tests for
// the actual wrap-segment reflow coverage.
func TestIntegration_ResumeAtGidInWrappedChainRange(t *testing.T) {
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

// TestIntegration_FullReconnectLifecycle exercises the full disconnect →
// reconnect flow: a first client connects, gets a snapshot, disconnects, then
// a second client connects using the same session ID and sends a
// MsgResumeRequest carrying PaneViewports. After the handler fires,
// session.Viewport must reflect the requested scroll position.
//
// This is the only test that drives the complete lifecycle through two
// independent protocol connections rather than re-using the memHarness
// write path.
func TestIntegration_FullReconnectLifecycle(t *testing.T) {
	// --- Phase 1: first connection via memHarness ---
	h := newMemHarness(t, 80, 24)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	h.fakeApp.FeedRows(0, lines)
	h.ApplyViewport(h.paneID, 26, 49, true, false)
	h.Publish()

	sessionID := h.sessionID()
	paneID := h.paneID

	// Close the first client connection; the serve goroutine will exit.
	h.clientConn.Close()
	// Wait for the reader loop to notice the close so subsequent close
	// in t.Cleanup doesn't race.
	select {
	case <-h.readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first client reader loop did not exit within 2s")
	}

	// --- Phase 2: reconnect with a fresh MemPipe ---
	newServerConn, newClientConn := testutil.NewMemPipe(64)
	t.Cleanup(func() {
		_ = newServerConn.Close()
		_ = newClientConn.Close()
	})

	// Channel to receive the resumed session from the server goroutine.
	resumedSessCh := make(chan *Session, 1)
	serveErrCh2 := make(chan error, 1)
	go func() {
		defer newServerConn.Close()
		sess, resuming, err := handleHandshake(newServerConn, h.mgr)
		if err != nil {
			serveErrCh2 <- err
			return
		}
		pub := NewDesktopPublisher(h.desktop, sess)
		h.sink.SetPublisher(pub)
		conn := newConnection(newServerConn, sess, h.sink, resuming)
		pub.SetNotifier(conn.nudge)
		resumedSessCh <- sess
		serveErrCh2 <- conn.serve()
	}()

	writeReconnect := func(msgType protocol.MessageType, payload []byte, sid [16]byte) {
		hdr := protocol.Header{
			Version:   protocol.Version,
			Type:      msgType,
			Flags:     protocol.FlagChecksum,
			SessionID: sid,
		}
		if err := protocol.WriteMessage(newClientConn, hdr, payload); err != nil {
			t.Fatalf("writeReconnect type=%v: %v", msgType, err)
		}
	}

	// Hello.
	helloPayload, _ := protocol.EncodeHello(protocol.Hello{ClientName: "reconnect-client"})
	writeReconnect(protocol.MsgHello, helloPayload, [16]byte{})

	// Read Welcome.
	if _, _, err := readMessageSkippingFocus(newClientConn); err != nil {
		t.Fatalf("read welcome on reconnect: %v", err)
	}

	// ConnectRequest with the original session ID (triggers resume path).
	connectReqPayload, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{SessionID: sessionID})
	writeReconnect(protocol.MsgConnectRequest, connectReqPayload, sessionID)

	// Read ConnectAccept.
	if _, _, err := readMessageSkippingFocus(newClientConn); err != nil {
		t.Fatalf("read connect accept on reconnect: %v", err)
	}

	// Confirm the server goroutine handed us the resumed session.
	var resumedSess *Session
	select {
	case resumedSess = <-resumedSessCh:
	case <-time.After(2 * time.Second):
		t.Fatal("resumed session did not materialize within 2s")
	}

	// ResumeRequest with PaneViewports pointing at a scrolled-back position.
	resume := protocol.ResumeRequest{
		SessionID:    sessionID,
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:         paneID,
				AltScreen:      false,
				AutoFollow:     false,
				ViewBottomIdx:  20,
				WrapSegmentIdx: 0,
				ViewportRows:   24,
				ViewportCols:   80,
			},
		},
	}
	resumePayload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode resume: %v", err)
	}
	writeReconnect(protocol.MsgResumeRequest, resumePayload, sessionID)

	// Poll session.Viewport until the handler fires and ApplyResume runs.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		vp, ok := resumedSess.Viewport(paneID)
		if ok && vp.ViewBottomIdx == 20 && !vp.AutoFollow {
			// Success: the viewport was seeded from the PaneViewports payload.
			newClientConn.Close()
			select {
			case err := <-serveErrCh2:
				if err != nil && err != io.EOF && !errors.Is(err, net.ErrClosed) {
					t.Logf("reconnect serve exit: %v", err)
				}
			case <-time.After(time.Second):
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	vp, ok := resumedSess.Viewport(paneID)
	t.Fatalf("viewport not seeded after full reconnect within 2s: ok=%v vp=%+v", ok, vp)
}

// TestIntegration_ResumeAutoFollowHighGlobalIdx_NoTruncation is a regression
// test for the uint16 truncation bug: when AutoFollow=true panes resume on a
// terminal whose live-edge gid is >= 65512, the publisher must not let the
// clip span exceed what RowDelta.Row (uint16) can encode. Pre-fix, the
// ApplyResume sentinel of 1<<62 caused lo to sit at -overscan while hi was
// near MaxInt64, so gid - lo wrapped to 0 on the wire and rows got cached at
// bogus negative gids on the client.
func TestIntegration_ResumeAutoFollowHighGlobalIdx_NoTruncation(t *testing.T) {
	h := newMemHarness(t, 80, 24)

	// Feed rows starting at gid=66000 — above the uint16 boundary.
	const startGid = int64(66000)
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "row"
	}
	h.fakeApp.FeedRows(startGid, lines)

	h.ApplyViewport(h.paneID, startGid+76, startGid+99, true, false)
	h.Publish()

	resume := protocol.ResumeRequest{
		SessionID: h.sessionID(),
		PaneViewports: []protocol.PaneViewportState{
			{PaneID: h.paneID, AutoFollow: true, ViewBottomIdx: 500 /* stale */, ViewportRows: 24, ViewportCols: 80},
		},
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// A near-live-edge row must arrive encoded correctly. The pane has
	// 1-cell borders top/bottom, so the gid=startGid+99 at the bottom edge
	// is covered by the border. Use startGid+97 — safely interior.
	//
	// Pre-fix bug: lo was -overscan while hi was ~MaxInt64; uint16(gid-lo)
	// wrapped to a small positive row, causing the client to key this row
	// under a bogus negative gid. The harness's client reader reconstructs
	// gid = RowBase + RowDelta.Row, so a correctly encoded row at high gid
	// must appear in rowsByGID under its real globalIdx.
	h.AwaitRow(h.paneID, startGid+97, 2*time.Second)
}

// TestIntegration_PersistedStateDrivesResumeRequest verifies the
// disk → wire → server boundary of Plan D persistence. We construct
// a ClientState in memory, Save+Load it through persistence.go (so
// JSON encoding/decoding is exercised), build a ResumeRequest from
// the loaded fields, push it through newMemHarness, and assert the
// server applied the resumed viewport correctly.
//
// This stops short of running a full clientruntime.Run; that path
// is covered by Task 20's manual end-to-end verification. The test
// here proves the wire shape is consistent end-to-end.
func TestIntegration_PersistedStateDrivesResumeRequest(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	h := newMemHarness(t, 80, 24)

	// Feed 200 rows so a scrolled-back gid=50 is in range.
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line"
	}
	h.fakeApp.FeedRows(0, lines)

	// Bootstrap the publisher's pre-resume viewport state — same shape
	// as TestIntegration_ResumeHonorsPaneViewport's setup. Without this,
	// the publisher's per-pane ClientViewport is empty and any post-resume
	// publish race could be observable as flakes.
	h.ApplyViewport(h.paneID, 176, 199, true /*autoFollow*/, false /*altScreen*/)
	h.Publish()

	// Build the ClientState a real client would have persisted: it
	// holds the same sessionID/paneID the harness allocated, plus a
	// scrolled-back PaneViewport.
	socketPath := "/tmp/test-d-rrq.sock" // sanity-check value; not actually opened
	statePath, err := clientruntime.ResolvePath(socketPath, "default")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	want := clientruntime.ClientState{
		SocketPath:   socketPath,
		SessionID:    h.sessionID(),
		LastSequence: 0,
		WrittenAt:    time.Now().UTC(),
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

	// Save + Load — exercises JSON encode/decode round-trip.
	if err := clientruntime.Save(statePath, &want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := clientruntime.Load(statePath, socketPath)
	if err != nil || got == nil {
		t.Fatalf("Load: state=%v err=%v", got, err)
	}

	// Construct the ResumeRequest as app.go would, from the loaded state.
	resume := protocol.ResumeRequest{
		SessionID:     got.SessionID,
		LastSequence:  got.LastSequence,
		PaneViewports: got.PaneViewports,
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode resume: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// Server should apply the resumed viewport — render buffer ends at gid 50.
	// Same assertion as the existing TestIntegration_ResumeHonorsPaneViewport
	// (which uses an in-memory-built request); this one proves the loaded-
	// from-disk shape is wire-equivalent.
	h.AwaitRow(h.paneID, 48, 2*time.Second)
}
