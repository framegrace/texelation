//go:build integration
// +build integration

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/viewport_integration_test.go
// Summary: End-to-end memconn integration coverage for viewport clipping +
//   FetchRange (Plan A / issue #199).
// Usage: go test -tags=integration ./internal/runtime/server/ -run TestIntegration_
// Notes: Uses a sparse-backed fake app so FetchRange has a real sparse.Store
//   to serve from. The app satisfies RowGlobalIdxProvider, AltScreenProvider,
//   and the private fetchRangeProvider interface; that's enough for the
//   publisher clip path and the FetchRange handler.

package server

import (
	"io"
	"sync"
	"testing"
	"time"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/parser/sparse"
	"github.com/framegrace/texelation/internal/runtime/server/testutil"
	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

// Compile-time assertion: sparseFakeApp must satisfy texel.ViewportRestorer.
var _ texel.ViewportRestorer = (*sparseFakeApp)(nil)

// nonZeroStyle is the style we use for all cells in the fake app's buffers.
// We deliberately avoid tcell.StyleDefault because the pane's BufferWidget
// skips cells whose Style equals the zero-value tcell.Style{} — and
// tcell.StyleDefault IS the zero value, so using it would silently drop
// every cell we write. Setting any non-default attribute (color, bold, etc.)
// makes the style non-zero and the cells render.
var nonZeroStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite)

// sparseFakeApp is a minimal App impl that:
//   - Holds a sparse.Store so the FetchRange handler has something to serve.
//   - Exposes RowGlobalIdxProvider so the publisher emits main-screen deltas.
//   - Exposes AltScreenProvider so we can flip alt-screen for the second test.
//   - Renders a constant per-row label ("row-<gid>") plus its globalIdx so
//     we can identify which rows the publisher emitted.
type sparseFakeApp struct {
	mu         sync.Mutex
	width      int
	height     int
	renderRows [][]texelcore.Cell
	rowGIDs    []int64 // parallel to renderRows; -1 if no main-screen gid
	store      *sparse.Store
	altScreen  bool
	altBuf     [][]texelcore.Cell
	notify     chan<- bool
}

func newSparseFakeApp(cols, rows int) *sparseFakeApp {
	a := &sparseFakeApp{
		width:  cols,
		height: rows,
		store:  sparse.NewStore(cols),
	}
	a.resetAltBuf()
	return a
}

func (a *sparseFakeApp) resetAltBuf() {
	a.altBuf = make([][]texelcore.Cell, a.height)
	for y := range a.altBuf {
		row := make([]texelcore.Cell, a.width)
		for x := range row {
			row[x] = texelcore.Cell{Ch: ' ', Style: nonZeroStyle}
		}
		a.altBuf[y] = row
	}
}

// FeedRows appends main-screen rows at the given starting globalIdx. Each
// row string is placed at (startGID + i). Both the render buffer (as the
// most-recent <height> rows) and the sparse store are updated.
func (a *sparseFakeApp) FeedRows(startGID int64, rows []string) {
	a.mu.Lock()
	for i, s := range rows {
		gid := startGID + int64(i)
		cells := stringToParserCells(s, a.width)
		a.store.SetLine(gid, cells)
	}
	// Rebuild render slice from the last <height> rows of the written range.
	maxGID := startGID + int64(len(rows)-1)
	a.rebuildRenderFromStoreLocked(maxGID)
	a.mu.Unlock()
	a.markDirty()
}

// ScrollTo sets the render buffer to show the <height> rows ending at
// bottomGID. Used to simulate the cursor being at a specific globalIdx.
func (a *sparseFakeApp) ScrollTo(bottomGID int64) {
	a.mu.Lock()
	a.rebuildRenderFromStoreLocked(bottomGID)
	a.mu.Unlock()
	a.markDirty()
}

// RestoreViewport satisfies texel.ViewportRestorer. For the fake app, we
// translate the request into a direct rebuildRenderFromStoreLocked — enough to
// exercise the Desktop→App dispatch and the publisher's re-clip. wrapSeg
// is ignored (fake app doesn't reflow). autoFollow snaps to the live edge.
// When viewBottom is below retention, WalkUpwardFromBottom returns the oldest
// retained gid so we render from there rather than from a gap.
func (a *sparseFakeApp) RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool) {
	a.mu.Lock()
	if autoFollow {
		// Snap to live edge.
		a.rebuildRenderFromStoreLocked(a.store.Max())
		a.mu.Unlock()
		a.markDirty()
		return
	}
	// Honor missing-anchor: if viewBottom < OldestRetained, snap up.
	anchor, _, policy := sparse.WalkUpwardFromBottom(a.store, viewBottom, wrapSeg, a.height, a.width, false)
	if policy == sparse.WalkPolicyMissingAnchor {
		a.rebuildRenderFromStoreLocked(anchor)
	} else {
		a.rebuildRenderFromStoreLocked(viewBottom)
	}
	a.mu.Unlock()
	a.markDirty()
}

func (a *sparseFakeApp) rebuildRenderFromStoreLocked(bottomGID int64) {
	a.renderRows = make([][]texelcore.Cell, a.height)
	a.rowGIDs = make([]int64, a.height)
	topGID := bottomGID - int64(a.height-1)
	for y := 0; y < a.height; y++ {
		gid := topGID + int64(y)
		a.rowGIDs[y] = gid
		cells := a.store.GetLine(gid)
		row := make([]texelcore.Cell, a.width)
		for x := range row {
			row[x] = texelcore.Cell{Ch: ' ', Style: nonZeroStyle}
		}
		for x, c := range cells {
			if x >= a.width {
				break
			}
			row[x] = texelcore.Cell{Ch: c.Rune, Style: nonZeroStyle}
		}
		a.renderRows[y] = row
	}
}

// EnterAltScreen flips to alt-screen mode and writes a single-line label.
func (a *sparseFakeApp) EnterAltScreen(text string) {
	a.mu.Lock()
	a.altScreen = true
	a.resetAltBuf()
	for x, r := range text {
		if x >= a.width {
			break
		}
		a.altBuf[0][x] = texelcore.Cell{Ch: r, Style: nonZeroStyle}
	}
	a.mu.Unlock()
	a.markDirty()
}

// App interface.
func (a *sparseFakeApp) Run() error            { return nil }
func (a *sparseFakeApp) Stop()                 {}
func (a *sparseFakeApp) Resize(cols, rows int) {}
func (a *sparseFakeApp) Render() [][]texelcore.Cell {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.altScreen {
		out := make([][]texelcore.Cell, len(a.altBuf))
		for y, row := range a.altBuf {
			out[y] = make([]texelcore.Cell, len(row))
			copy(out[y], row)
		}
		return out
	}
	if a.renderRows == nil {
		// Default blank buffer so the pane has something to render before
		// the test feeds it.
		out := make([][]texelcore.Cell, a.height)
		for y := range out {
			out[y] = make([]texelcore.Cell, a.width)
			for x := range out[y] {
				out[y][x] = texelcore.Cell{Ch: ' ', Style: nonZeroStyle}
			}
		}
		return out
	}
	out := make([][]texelcore.Cell, len(a.renderRows))
	for y, row := range a.renderRows {
		out[y] = make([]texelcore.Cell, len(row))
		copy(out[y], row)
	}
	return out
}
func (a *sparseFakeApp) GetTitle() string             { return "sparseFake" }
func (a *sparseFakeApp) HandleKey(ev *tcell.EventKey) {}
func (a *sparseFakeApp) SetRefreshNotifier(ch chan<- bool) {
	a.mu.Lock()
	a.notify = ch
	a.mu.Unlock()
}

// markDirty nudges the pane's refresh forwarder so it drops its cached
// render and re-reads Render(). The send is blocking because we want the
// pane's renderGen to have definitely ticked before the test proceeds to
// take a snapshot; a non-blocking send could race with an in-flight
// forwarder drain and be dropped silently.
func (a *sparseFakeApp) markDirty() {
	a.mu.Lock()
	ch := a.notify
	a.mu.Unlock()
	if ch != nil {
		ch <- true
	}
}

// RowGlobalIdxProvider.
func (a *sparseFakeApp) RowGlobalIdx() []int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.altScreen {
		// No main-screen gids when in alt-screen.
		out := make([]int64, a.height)
		for i := range out {
			out[i] = -1
		}
		return out
	}
	if a.rowGIDs == nil {
		out := make([]int64, a.height)
		for i := range out {
			out[i] = -1
		}
		return out
	}
	out := make([]int64, len(a.rowGIDs))
	copy(out, a.rowGIDs)
	return out
}

// AltScreenProvider.
func (a *sparseFakeApp) InAltScreen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.altScreen
}

// Satisfies fetchRangeProvider (unexported — this file lives in the same
// package, so that is fine).
func (a *sparseFakeApp) SparseStore() *sparse.Store { return a.store }

func stringToParserCells(s string, width int) []parser.Cell {
	cells := make([]parser.Cell, 0, len(s))
	for _, r := range s {
		cells = append(cells, parser.Cell{Rune: r})
		if len(cells) >= width {
			break
		}
	}
	return cells
}

// memHarness owns a server + single-client memconn and drives both sides.
type memHarness struct {
	t       *testing.T
	desktop *texel.DesktopEngine
	sink    *DesktopSink
	mgr     *Manager
	srv     *Server
	session *Session
	pub     *DesktopPublisher
	fakeApp *sparseFakeApp
	paneID  [16]byte

	serverConn *testutil.MemConn
	clientConn *testutil.MemConn

	readerDone chan struct{}

	mu             sync.Mutex
	rowsByGID      map[int64]protocol.RowDelta    // main-screen rows keyed by globalIdx
	altRowsByIdx   map[uint16][]protocol.CellSpan // alt-screen rows keyed by flat row index
	fetchByReqID   map[uint32]protocol.FetchRangeResponse
	rowBasesByPane map[[16]byte][]int64 // every RowBase observed per pane, in order
	writeMu        sync.Mutex           // client-side write serialization
}

type vpScreenDriver struct {
	cols, rows int
}

func (d vpScreenDriver) Init() error                                    { return nil }
func (d vpScreenDriver) Fini()                                          {}
func (d vpScreenDriver) Size() (int, int)                               { return d.cols, d.rows }
func (d vpScreenDriver) SetStyle(tcell.Style)                           {}
func (d vpScreenDriver) HideCursor()                                    {}
func (d vpScreenDriver) Show()                                          {}
func (d vpScreenDriver) PollEvent() tcell.Event                         { return nil }
func (d vpScreenDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (d vpScreenDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, nonZeroStyle, 1
}

// newMemHarness wires everything up and performs the initial handshake so
// the test can immediately push viewports / feed rows / call FetchRange.
func newMemHarness(t *testing.T, cols, rows int) *memHarness {
	t.Helper()
	fakeApp := newSparseFakeApp(cols, rows)
	driver := vpScreenDriver{cols: cols, rows: rows}
	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texelcore.App { return fakeApp }
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "texelterm", &lifecycle)
	if err != nil {
		t.Fatalf("NewDesktopEngineWithDriver: %v", err)
	}
	t.Cleanup(func() {
		desktop.Close()
	})
	// Create the default workspace and attach the shell app so a pane
	// exists for the rest of the wiring to target.
	desktop.SwitchToWorkspace(1)
	desktop.SetViewportSize(cols, rows)

	mgr := NewManager()
	sink := NewDesktopSink(desktop)
	srv := &Server{manager: mgr, sink: sink, desktopSink: sink}

	h := &memHarness{
		t:              t,
		desktop:        desktop,
		sink:           sink,
		mgr:            mgr,
		srv:            srv,
		fakeApp:        fakeApp,
		rowsByGID:      make(map[int64]protocol.RowDelta),
		altRowsByIdx:   make(map[uint16][]protocol.CellSpan),
		fetchByReqID:   make(map[uint32]protocol.FetchRangeResponse),
		rowBasesByPane: make(map[[16]byte][]int64),
		readerDone:     make(chan struct{}),
	}

	// Find the pane ID assigned to the shell app by the desktop.
	var foundID [16]byte
	for _, snap := range desktop.SnapshotBuffers() {
		if snap.Title == fakeApp.GetTitle() {
			foundID = snap.ID
			break
		}
	}
	if foundID == ([16]byte{}) {
		t.Fatalf("could not locate pane hosting sparseFakeApp")
	}
	h.paneID = foundID

	h.serverConn, h.clientConn = testutil.NewMemPipe(64)
	t.Cleanup(func() {
		_ = h.serverConn.Close()
		_ = h.clientConn.Close()
	})

	serveErrCh := make(chan error, 1)
	sessCh := make(chan *Session, 1)

	go func() {
		defer h.serverConn.Close()
		sess, resuming, err := handleHandshake(h.serverConn, mgr)
		if err != nil {
			serveErrCh <- err
			return
		}
		pub := NewDesktopPublisher(desktop, sess)
		sink.SetPublisher(pub)
		conn := newConnection(h.serverConn, sess, sink, resuming)
		// Wire nudge so sendPending fires when publisher queues diffs.
		pub.SetNotifier(conn.nudge)
		h.mu.Lock()
		h.session = sess
		h.pub = pub
		h.mu.Unlock()
		sessCh <- sess
		serveErrCh <- conn.serve()
	}()

	// Client-side handshake.
	helloPayload, _ := protocol.EncodeHello(protocol.Hello{ClientName: "intg-client"})
	h.writeFrame(protocol.MsgHello, helloPayload, [16]byte{})
	if _, _, err := protocol.ReadMessage(h.clientConn); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	connectReq, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	h.writeFrame(protocol.MsgConnectRequest, connectReq, [16]byte{})

	// Read MsgConnectAccept (skip any non-target frames in case).
	for {
		hdr, payload, err := protocol.ReadMessage(h.clientConn)
		if err != nil {
			t.Fatalf("read connect accept: %v", err)
		}
		if hdr.Type == protocol.MsgConnectAccept {
			if _, err := protocol.DecodeConnectAccept(payload); err != nil {
				t.Fatalf("decode connect accept: %v", err)
			}
			break
		}
	}

	select {
	case <-sessCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("session did not materialize after handshake")
	}

	// Now spin up the client reader that stashes inbound frames for lookup.
	go h.clientReadLoop()

	// Collect the server-side teardown error on cleanup.
	t.Cleanup(func() {
		_ = h.clientConn.Close()
		// Drain the serve goroutine.
		select {
		case err := <-serveErrCh:
			if err != nil && err != io.EOF {
				// Informational — shutdown races are benign.
				t.Logf("server serve exit: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Log("server serve goroutine did not exit cleanly")
		}
		<-h.readerDone
	})

	return h
}

// writeFrame writes one protocol frame from the client side. Serialised.
func (h *memHarness) writeFrame(msgType protocol.MessageType, payload []byte, sessionID [16]byte) {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      msgType,
		Flags:     protocol.FlagChecksum,
		SessionID: sessionID,
	}
	if err := protocol.WriteMessage(h.clientConn, hdr, payload); err != nil {
		h.t.Fatalf("write frame type=%v: %v", msgType, err)
	}
}

func (h *memHarness) sessionID() [16]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil {
		return [16]byte{}
	}
	return h.session.ID()
}

// clientReadLoop reads inbound frames forever and files them into the
// per-pane row maps / fetch response map.
func (h *memHarness) clientReadLoop() {
	defer close(h.readerDone)
	for {
		hdr, payload, err := protocol.ReadMessage(h.clientConn)
		if err != nil {
			return
		}
		switch hdr.Type {
		case protocol.MsgBufferDelta:
			delta, derr := protocol.DecodeBufferDelta(payload)
			if derr != nil {
				h.t.Logf("client decode buffer delta: %v", derr)
				continue
			}
			if delta.PaneID != h.paneID {
				continue
			}
			h.mu.Lock()
			if delta.Flags&protocol.BufferDeltaAltScreen != 0 {
				for _, row := range delta.Rows {
					spans := make([]protocol.CellSpan, len(row.Spans))
					copy(spans, row.Spans)
					h.altRowsByIdx[row.Row] = spans
				}
			} else {
				// Record the RowBase so tests can assert the documented
				// clip-offset invariant (RowBase == ViewTopIdx - Rows).
				h.rowBasesByPane[delta.PaneID] = append(h.rowBasesByPane[delta.PaneID], delta.RowBase)
				for _, row := range delta.Rows {
					gid := delta.RowBase + int64(row.Row)
					spansCopy := make([]protocol.CellSpan, len(row.Spans))
					copy(spansCopy, row.Spans)
					h.rowsByGID[gid] = protocol.RowDelta{Row: row.Row, Spans: spansCopy}
				}
			}
			h.mu.Unlock()
			// Ack the delta so the session queue stays bounded.
			ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
			h.writeFrame(protocol.MsgBufferAck, ackPayload, h.sessionID())
		case protocol.MsgFetchRangeResponse:
			resp, derr := protocol.DecodeFetchRangeResponse(payload)
			if derr != nil {
				h.t.Logf("client decode fetch response: %v", derr)
				continue
			}
			h.mu.Lock()
			h.fetchByReqID[resp.RequestID] = resp
			h.mu.Unlock()
		default:
			// Silently discard other messages (PaneFocus, StateUpdate, etc.).
		}
	}
}

// ApplyViewport sends MsgViewportUpdate and waits for the server to record it.
func (h *memHarness) ApplyViewport(paneID [16]byte, top, bottom int64, autoFollow bool, altScreen bool) {
	vp := protocol.ViewportUpdate{
		PaneID:        paneID,
		AltScreen:     altScreen,
		ViewTopIdx:    top,
		ViewBottomIdx: bottom,
		Rows:          uint16(bottom - top + 1),
		Cols:          uint16(h.fakeApp.width),
		AutoFollow:    autoFollow,
	}
	payload, err := protocol.EncodeViewportUpdate(vp)
	if err != nil {
		h.t.Fatalf("encode viewport update: %v", err)
	}
	h.writeFrame(protocol.MsgViewportUpdate, payload, h.sessionID())

	// Poll until the session shows the viewport — the server applies it
	// asynchronously in the serve loop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := h.session.Viewport(paneID); ok {
			if got.ViewTopIdx == top && got.ViewBottomIdx == bottom && got.AutoFollow == autoFollow {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("server did not record viewport top=%d bottom=%d within 2s", top, bottom)
}

// Publish triggers the publisher and flushes pending diffs onto the wire.
func (h *memHarness) Publish() {
	h.sink.Publish()
}

// AwaitRow waits for a main-screen row with globalIdx == gid to arrive from
// the server. Returns the first matching RowDelta.
func (h *memHarness) AwaitRow(paneID [16]byte, gid int64, timeout time.Duration) protocol.RowDelta {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		rd, ok := h.rowsByGID[gid]
		h.mu.Unlock()
		if ok {
			return rd
		}
		// Nudge publish every few polls in case the server was waiting on us.
		h.Publish()
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.Lock()
	seen := make([]int64, 0, len(h.rowsByGID))
	for k := range h.rowsByGID {
		seen = append(seen, k)
	}
	h.mu.Unlock()
	h.t.Fatalf("did not receive row for globalIdx=%d within %v (saw %d rows, first gids=%v)", gid, timeout, len(seen), truncate(seen, 20))
	return protocol.RowDelta{}
}

func truncate(xs []int64, n int) []int64 {
	if len(xs) <= n {
		return xs
	}
	return xs[:n]
}

// AwaitAltRow waits for an alt-screen row with the given flat index and
// whose decoded text contains want.
func (h *memHarness) AwaitAltRow(paneID [16]byte, row uint16, want string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		spans, ok := h.altRowsByIdx[row]
		h.mu.Unlock()
		if ok && spansContain(spans, want) {
			return
		}
		h.Publish()
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.Lock()
	keys := make([]uint16, 0, len(h.altRowsByIdx))
	for k := range h.altRowsByIdx {
		keys = append(keys, k)
	}
	// Dump row=1 content if we have it.
	var r1 string
	if spans, ok := h.altRowsByIdx[1]; ok {
		for _, s := range spans {
			r1 += s.Text
		}
	}
	nRows := len(h.rowsByGID)
	h.mu.Unlock()
	h.t.Fatalf("did not receive alt row=%d containing %q within %v (altKeys=%v row1=%q mainRows=%d)", row, want, timeout, keys, r1, nRows)
}

func spansContain(spans []protocol.CellSpan, want string) bool {
	// Concatenate all spans in the row and check for want anywhere inside.
	// Pane renders add border characters at column 0 / width-1, so we can't
	// rely on want being at the start of the first span.
	var joined string
	for _, s := range spans {
		joined += s.Text
	}
	return containsSubstring(joined, want)
}

func containsSubstring(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// FetchRangeSync blocks until a FetchRangeResponse matching the RequestID we
// send arrives on the wire.
func (h *memHarness) FetchRangeSync(paneID [16]byte, lo, hi int64) protocol.FetchRangeResponse {
	reqID := uint32(time.Now().UnixNano() & 0xffffffff)
	req := protocol.FetchRange{
		RequestID: reqID,
		PaneID:    paneID,
		LoIdx:     lo,
		HiIdx:     hi,
	}
	payload, err := protocol.EncodeFetchRange(req)
	if err != nil {
		h.t.Fatalf("encode fetch range: %v", err)
	}
	h.writeFrame(protocol.MsgFetchRange, payload, h.sessionID())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		resp, ok := h.fetchByReqID[reqID]
		h.mu.Unlock()
		if ok {
			return resp
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("no fetch response for request %d within 2s", reqID)
	return protocol.FetchRangeResponse{}
}

func TestIntegration_ClipsAndFetches(t *testing.T) {
	const cols, rows = 80, 24
	h := newMemHarness(t, cols, rows)
	paneID := h.paneID

	// Feed 5000 rows (gids 0..4999) and point the cursor at row 4999.
	feed := make([]string, 5000)
	for i := range feed {
		feed[i] = "row-content"
	}
	h.fakeApp.FeedRows(0, feed)

	// Scroll the app so that gid 4923 is the bottom-most interior row.
	// With a 1-cell border and 24 total rows, interior rows are 1..22
	// (22 rows). rebuildRenderFromStoreLocked sets rowGIDs[i] = topGID+i
	// over 0..23, then capturePaneSnapshot drops the first and last (the
	// borders), so interior gids become [topGID+0 .. topGID+21] where
	// topGID = bottomGID - (height-1) = bottomGID - 23. If we ScrollTo
	// bottom=4945, interior gids span [4922..4943]. 4923 falls in that
	// range.
	h.fakeApp.ScrollTo(4945)

	// Client viewport says we're looking at main-screen rows 4900..4923
	// (24 rows), AutoFollow=true. This gives an overscan window of
	// [4900-24, 4923+24] = [4876, 4947], so all interior gids qualify.
	h.ApplyViewport(paneID, 4900, 4923, true, false)
	if _, ok := h.session.Viewport(paneID); !ok {
		t.Fatalf("server did not record viewport for pane")
	}
	h.Publish()

	// Assert the publisher emitted row 4923, which sits at the bottom
	// edge of the client's reported viewport, with the expected content.
	rd := h.AwaitRow(paneID, 4923, 3*time.Second)
	if len(rd.Spans) == 0 {
		t.Fatalf("row 4923 arrived but had no spans")
	}
	var joined string
	for _, s := range rd.Spans {
		joined += s.Text
	}
	if !containsSubstring(joined, "row-content") {
		t.Fatalf("row 4923 missing expected content %q; got %q", "row-content", joined)
	}

	// Negative-assertion phase: prove the publisher actually clips rather
	// than emitting the full render buffer. The render buffer still spans
	// interior gids [4923..4944] (22 rows after border trim). Shrink the
	// viewport so the overscan window is a strict subset of that range:
	// with top=4930, bottom=4931, Rows=2, the publisher clips to
	// [lo, hi] = [ViewTopIdx-Rows, ViewBottomIdx+Rows] = [4928, 4933].
	// Interior gids 4923..4927 and 4934..4944 MUST be suppressed.
	h.mu.Lock()
	h.rowsByGID = make(map[int64]protocol.RowDelta)
	h.rowBasesByPane[paneID] = nil
	h.mu.Unlock()

	const narrowTop, narrowBottom = int64(4930), int64(4931)
	h.ApplyViewport(paneID, narrowTop, narrowBottom, false, false)
	h.Publish()

	// Wait for at least one in-window row to confirm the publisher ran
	// under the narrow viewport. Row 4930 sits inside [4928, 4933].
	h.AwaitRow(paneID, 4930, 3*time.Second)

	// The Rows-geometry change from 24 to 2 invalidates prev-buffer dedup
	// (see publishSnapshotsLocked), so every in-window gid should have
	// re-emitted. Any out-of-window gid appearing here would prove the
	// publisher ignored the viewport clip.
	const narrowRows = int64(narrowBottom - narrowTop + 1)
	lo := narrowTop - narrowRows
	hi := narrowBottom + narrowRows
	interiorLo, interiorHi := int64(4923), int64(4944)
	h.mu.Lock()
	for gid := interiorLo; gid <= interiorHi; gid++ {
		if gid >= lo && gid <= hi {
			continue
		}
		if _, present := h.rowsByGID[gid]; present {
			h.mu.Unlock()
			t.Fatalf("gid %d is outside clip window [%d,%d] but was still delivered — publisher did not clip", gid, lo, hi)
		}
	}
	// Belt-and-suspenders: assert the wire-level RowBase invariant. The
	// most recent delta for this pane must carry RowBase == ViewTopIdx -
	// Rows (= lo). Paired with the Row = uint16(gid - lo) contract, this
	// proves the clip math end-to-end.
	bases := h.rowBasesByPane[paneID]
	if len(bases) == 0 {
		h.mu.Unlock()
		t.Fatalf("no main-screen BufferDelta observed under narrow viewport")
	}
	latestBase := bases[len(bases)-1]
	h.mu.Unlock()
	if latestBase != lo {
		t.Fatalf("latest RowBase = %d, want %d (ViewTopIdx - Rows)", latestBase, lo)
	}

	// Clear out collected rows for the fetch-back assertion so we don't
	// false-positive on a stale earlier delta.
	h.mu.Lock()
	h.rowsByGID = make(map[int64]protocol.RowDelta)
	h.mu.Unlock()

	// Scroll back. With AutoFollow=false the server does not walk the
	// render window backwards for us — the client must issue FetchRange
	// to get the back-scrolled rows. Do that explicitly.
	h.ApplyViewport(paneID, 3900, 3923, false, false)
	resp := h.FetchRangeSync(paneID, 3900, 3924)
	if resp.Flags&protocol.FetchRangeAltScreenActive != 0 {
		t.Fatalf("unexpected AltScreenActive flag on main-screen fetch")
	}
	if len(resp.Rows) == 0 {
		t.Fatalf("fetch returned no rows for [3900,3924) — flags=%v", resp.Flags)
	}
	found := false
	for _, r := range resp.Rows {
		if r.GlobalIdx == 3900 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fetch response did not include globalIdx=3900; gids=%v", gidList(resp.Rows))
	}
}

func TestIntegration_AltScreenOptsOut(t *testing.T) {
	const cols, rows = 80, 24
	h := newMemHarness(t, cols, rows)
	paneID := h.paneID

	// Push the app into alt-screen with a single labelled row.
	h.fakeApp.EnterAltScreen("hello alt")

	// Alt-screen panes do not need a viewport to emit, but Plan A's
	// connection_handler accepts one either way; send one for completeness.
	h.ApplyViewport(paneID, 0, int64(rows-1), true, true)
	h.Publish()

	// Row 1 of the alt buffer (the pane renders the app inside a 1-cell
	// border, so app row 0 lands at pane row 1) should carry "hello alt".
	h.AwaitAltRow(paneID, 1, "hello alt", 3*time.Second)

	// A FetchRange against a pane in alt-screen must come back with
	// FetchRangeAltScreenActive set and no rows.
	resp := h.FetchRangeSync(paneID, 0, 100)
	if resp.Flags&protocol.FetchRangeAltScreenActive == 0 {
		t.Fatalf("expected FetchRangeAltScreenActive flag; got flags=%v", resp.Flags)
	}
	if len(resp.Rows) != 0 {
		t.Fatalf("alt-screen fetch should return no rows; got %d", len(resp.Rows))
	}
}

func gidList(rows []protocol.LogicalRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.GlobalIdx
	}
	return out
}
