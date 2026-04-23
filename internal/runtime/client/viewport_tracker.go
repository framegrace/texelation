// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/viewport_tracker.go
// Summary: Per-pane client-side viewport tracker and flushFrame emission.
// Usage: Tracks each pane's current viewport (AltScreen, ViewTopIdx,
//
//	ViewBottomIdx, Rows, Cols, AutoFollow) and emits MsgViewportUpdate /
//	MsgFetchRange to the server once per render frame.
//
// Locking order: always acquire paneCachesMu (if needed) BEFORE any per-pane
//
//	viewportMu. Never hold any mutex across writeMessage calls — copy out
//	state first, then send.
package clientruntime

import (
	"log"
	"net"
	"sync"
	"sync/atomic"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/internal/debuglog"
	"github.com/framegrace/texelation/protocol"
)

// pendingFetchRange is a deferred MsgFetchRange request stashed because
// another fetch for the same pane was already inflight.  Replaced (not
// queued) when a newer viewport comes in — the client only ever needs the
// latest missing-rows window.
type pendingFetchRange struct {
	lo int64
	hi int64
}

// paneViewport is the per-pane viewport state tracked on the client.
type paneViewport struct {
	mu sync.Mutex

	AltScreen     bool
	ViewTopIdx    int64
	ViewBottomIdx int64
	Rows          uint16
	Cols          uint16
	AutoFollow    bool

	// bookkeeping
	dirty          bool
	inflightFetch  bool
	pendingFetch   *pendingFetchRange // nil when none
	knownBottomGid int64              // highest gid ever seen for this pane
}

// viewportTrackers holds all per-pane trackers plus a counter for FetchRange
// RequestIDs.
type viewportTrackers struct {
	mu      sync.RWMutex
	panes   map[[16]byte]*paneViewport
	fetchID atomic.Uint32
}

func newViewportTrackers() *viewportTrackers {
	return &viewportTrackers{
		panes: make(map[[16]byte]*paneViewport),
	}
}

// get returns the tracker for id, creating it on first access.
func (t *viewportTrackers) get(id [16]byte) *paneViewport {
	t.mu.RLock()
	vp, ok := t.panes[id]
	t.mu.RUnlock()
	if ok {
		return vp
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if vp, ok = t.panes[id]; ok {
		return vp
	}
	vp = &paneViewport{}
	t.panes[id] = vp
	return vp
}

// prune drops trackers for pane IDs not in live.
func (t *viewportTrackers) prune(live map[[16]byte]struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id := range t.panes {
		if _, ok := live[id]; !ok {
			delete(t.panes, id)
		}
	}
}

// snapshotDirty returns a shallow copy of all currently dirty trackers WITHOUT
// clearing their dirty flag. The caller is responsible for calling clearDirty
// after successful send — this split prevents a transient write failure from
// silently dropping a viewport update (the pane would otherwise stay dark
// with no retry until its viewport changed again).
func (t *viewportTrackers) snapshotDirty() ([]snapshotEntry, map[[16]byte]*paneViewport) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var entries []snapshotEntry
	raw := make(map[[16]byte]*paneViewport, len(t.panes))
	for id, vp := range t.panes {
		raw[id] = vp
		vp.mu.Lock()
		if vp.dirty {
			entries = append(entries, snapshotEntry{
				id: id,
				vp: paneViewportCopy{
					AltScreen:     vp.AltScreen,
					ViewTopIdx:    vp.ViewTopIdx,
					ViewBottomIdx: vp.ViewBottomIdx,
					Rows:          vp.Rows,
					Cols:          vp.Cols,
					AutoFollow:    vp.AutoFollow,
				},
			})
		}
		vp.mu.Unlock()
	}
	return entries, raw
}

// clearDirty clears the dirty flag for id iff the tracker's current state
// still matches expected. If the user scrolled between snapshotDirty and the
// successful send, the tracker will have been re-dirtied with new values —
// clearing unconditionally would drop that update.
func (t *viewportTrackers) clearDirty(id [16]byte, expected paneViewportCopy) {
	t.mu.RLock()
	vp, ok := t.panes[id]
	t.mu.RUnlock()
	if !ok {
		return
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if vp.AltScreen == expected.AltScreen &&
		vp.ViewTopIdx == expected.ViewTopIdx &&
		vp.ViewBottomIdx == expected.ViewBottomIdx &&
		vp.Rows == expected.Rows &&
		vp.Cols == expected.Cols &&
		vp.AutoFollow == expected.AutoFollow {
		vp.dirty = false
		return
	}
	debuglog.Printf("viewport clearDirty mismatch pane=%x: kept dirty (state changed since snapshot)", id[:4])
}

type snapshotEntry struct {
	id [16]byte
	vp paneViewportCopy
}

type paneViewportCopy struct {
	AltScreen     bool
	ViewTopIdx    int64
	ViewBottomIdx int64
	Rows          uint16
	Cols          uint16
	AutoFollow    bool
}

// onTreeSnapshot initialises per-pane trackers from a MsgTreeSnapshot.
// For any pane not yet tracked it sets a best-effort initial viewport.
func (s *clientState) onTreeSnapshot(snap protocol.TreeSnapshot) {
	live := make(map[[16]byte]struct{}, len(snap.Panes))
	for _, p := range snap.Panes {
		live[p.PaneID] = struct{}{}
		if p.Width <= 0 || p.Height <= 0 {
			continue
		}
		rows := uint16(p.Height)
		cols := uint16(p.Width)
		vp := s.viewports.get(p.PaneID)
		vp.mu.Lock()
		// Only initialise if this pane is brand-new.
		if vp.Rows == 0 {
			vp.AltScreen = false
			vp.ViewTopIdx = 0
			vp.ViewBottomIdx = int64(rows) - 1
			vp.Rows = rows
			vp.Cols = cols
			vp.AutoFollow = true
			vp.knownBottomGid = -1
			vp.dirty = true
		} else if vp.Rows != rows || vp.Cols != cols {
			// Geometry changed — update dims and mark dirty.
			vp.Rows = rows
			vp.Cols = cols
			vp.dirty = true
		}
		vp.mu.Unlock()
	}
	s.viewports.prune(live)
}

// onBufferDelta updates the viewport tracker for a pane after a delta arrives.
func (s *clientState) onBufferDelta(delta protocol.BufferDelta) {
	inAlt := delta.Flags&protocol.BufferDeltaAltScreen != 0
	vp := s.viewports.get(delta.PaneID)
	vp.mu.Lock()
	defer vp.mu.Unlock()

	// Alt-screen transitions.
	if inAlt != vp.AltScreen {
		vp.AltScreen = inAlt
		vp.dirty = true
		return
	}

	if inAlt {
		// Nothing more to do for alt-screen.
		return
	}

	if !vp.AutoFollow || vp.Rows == 0 {
		return
	}

	// AutoFollow on main-screen: advance view to track the live bottom.
	var maxGid int64 = -1
	for _, row := range delta.Rows {
		gid := delta.RowBase + int64(row.Row)
		if gid > maxGid {
			maxGid = gid
		}
	}
	if maxGid < 0 || maxGid <= vp.knownBottomGid {
		return
	}
	vp.knownBottomGid = maxGid
	vp.ViewBottomIdx = maxGid
	top := maxGid - int64(vp.Rows-1)
	if top < 0 {
		top = 0
	}
	vp.ViewTopIdx = top
	vp.dirty = true
}

// onFetchRangeResponse clears the inflight flag and emits a pending fetch if one
// was stashed. It returns a pending fetch request (lo, hi, true) if one should
// be sent, otherwise (0,0,false).
func (s *clientState) onFetchRangeResponse(paneID [16]byte) (lo, hi int64, send bool) {
	vp := s.viewports.get(paneID)
	vp.mu.Lock()
	defer vp.mu.Unlock()
	vp.inflightFetch = false
	if vp.pendingFetch != nil {
		lo = vp.pendingFetch.lo
		hi = vp.pendingFetch.hi
		vp.pendingFetch = nil
		vp.inflightFetch = true
		return lo, hi, true
	}
	return 0, 0, false
}

// restorePendingFetch clears the inflight reservation and re-stashes (lo, hi)
// into pendingFetch so the next flushFrame retries. Used after the response
// handler drained pendingFetch to re-issue a follow-up fetch and the write
// failed. If a newer pendingFetch has already been stashed (concurrent
// flushFrame observed missing rows), we leave it alone — the newer window
// supersedes ours.
func (s *clientState) restorePendingFetch(paneID [16]byte, lo, hi int64) {
	s.viewports.mu.RLock()
	vp, ok := s.viewports.panes[paneID]
	s.viewports.mu.RUnlock()
	if !ok {
		return
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	vp.inflightFetch = false
	if vp.pendingFetch == nil {
		vp.pendingFetch = &pendingFetchRange{lo: lo, hi: hi}
	}
}

// paneViewportFor returns a snapshot of the current viewport for a pane.
// Used by the renderer to decide whether to use PaneCache.
func (s *clientState) paneViewportFor(id [16]byte) (paneViewportCopy, bool) {
	s.viewports.mu.RLock()
	vp, ok := s.viewports.panes[id]
	s.viewports.mu.RUnlock()
	if !ok {
		return paneViewportCopy{}, false
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if vp.Rows == 0 {
		return paneViewportCopy{}, false
	}
	return paneViewportCopy{
		AltScreen:     vp.AltScreen,
		ViewTopIdx:    vp.ViewTopIdx,
		ViewBottomIdx: vp.ViewBottomIdx,
		Rows:          vp.Rows,
		Cols:          vp.Cols,
		AutoFollow:    vp.AutoFollow,
	}, true
}

// flushFrame is called at the top of each render iteration. It:
//  1. Snapshots dirty viewport trackers.
//  2. Pre-fetches PaneCaches for qualifying entries BEFORE taking any per-pane
//     viewport lock (honours the documented locking order: paneCachesMu must
//     never be acquired while holding a per-pane vp.mu).
//  3. Sends a MsgViewportUpdate for each dirty pane.
//  4. Checks for missing rows in the overscan window via PaneCache.MissingRows.
//  5. Issues MsgFetchRange when missing rows exist and no fetch is inflight.
//  6. Evicts rows outside the hysteresis band from PaneCache.
func flushFrame(
	state *clientState,
	conn net.Conn,
	writeMu *sync.Mutex,
	sessionID [16]byte,
) {
	if conn == nil {
		return
	}
	entries, rawPanes := state.viewports.snapshotDirty()

	// Pass 1: pre-fetch PaneCaches for every non-alt-screen entry with
	// non-zero dimensions. This ensures paneCachesMu is fully released before
	// we acquire any per-pane vp.mu below, matching the documented locking
	// order.
	caches := make(map[[16]byte]*client.PaneCache, len(entries))
	for _, e := range entries {
		if e.vp.Rows == 0 || e.vp.Cols == 0 {
			continue
		}
		if e.vp.AltScreen {
			continue
		}
		caches[e.id] = state.paneCacheFor(e.id)
	}

	// Pass 2: emit viewport updates and handle fetch/evict using the
	// pre-fetched caches. Per-pane vp.mu is only ever taken here.
	for _, e := range entries {
		id := e.id
		vc := e.vp
		if vc.Rows == 0 || vc.Cols == 0 {
			// Guard: never emit zero-dimension viewports.
			continue
		}
		// Encode and send MsgViewportUpdate.
		upd := protocol.ViewportUpdate{
			PaneID:         id,
			AltScreen:      vc.AltScreen,
			ViewTopIdx:     vc.ViewTopIdx,
			ViewBottomIdx:  vc.ViewBottomIdx,
			WrapSegmentIdx: 0,
			Rows:           vc.Rows,
			Cols:           vc.Cols,
			AutoFollow:     vc.AutoFollow,
		}
		payload, err := protocol.EncodeViewportUpdate(upd)
		if err != nil {
			log.Printf("encode viewport update: %v", err)
			continue
		}
		hdr := protocol.Header{
			Version:   protocol.Version,
			Type:      protocol.MsgViewportUpdate,
			Flags:     protocol.FlagChecksum,
			SessionID: sessionID,
		}
		if err := writeMessage(writeMu, conn, hdr, payload); err != nil {
			log.Printf("send viewport update: %v", err)
			continue
		}
		state.viewports.clearDirty(id, vc)

		if vc.AltScreen {
			continue
		}

		overscan := int64(vc.Rows)
		lo := vc.ViewTopIdx - overscan
		if lo < 0 {
			lo = 0
		}
		hi := vc.ViewBottomIdx + overscan

		pc := caches[id]
		miss := pc.MissingRows(lo, hi)

		if len(miss) > 0 {
			rawVP := rawPanes[id]
			rawVP.mu.Lock()
			if !rawVP.inflightFetch {
				// Claim the inflight slot before releasing the lock so
				// concurrent callers see the reservation.
				rawVP.inflightFetch = true
				rawVP.mu.Unlock()
				lo, hi := miss[0], miss[len(miss)-1]+1
				if !sendFetchRange(state, conn, writeMu, sessionID, id, lo, hi) {
					// Write failed — release the inflight slot so the next
					// frame can retry instead of staying wedged forever.
					rawVP.mu.Lock()
					rawVP.inflightFetch = false
					rawVP.mu.Unlock()
				}
			} else {
				// Stash as pending.
				pf := pendingFetchRange{lo: miss[0], hi: miss[len(miss)-1] + 1}
				rawVP.pendingFetch = &pf
				rawVP.mu.Unlock()
			}
		}

		// Evict rows outside hysteresis band.
		pc.Evict(vc.ViewTopIdx, vc.ViewBottomIdx, overscan)
	}
}

// sendFetchRange encodes and sends a MsgFetchRange to the server. Returns
// true on success.  On false the caller must roll back any reservation
// (e.g. inflightFetch) so the request can be retried on the next frame.
func sendFetchRange(
	state *clientState,
	conn net.Conn,
	writeMu *sync.Mutex,
	sessionID [16]byte,
	paneID [16]byte,
	lo, hi int64,
) bool {
	req := protocol.FetchRange{
		RequestID: state.viewports.fetchID.Add(1),
		PaneID:    paneID,
		LoIdx:     lo,
		HiIdx:     hi,
	}
	payload, err := protocol.EncodeFetchRange(req)
	if err != nil {
		log.Printf("encode fetch range: %v", err)
		return false
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgFetchRange,
		Flags:     protocol.FlagChecksum,
		SessionID: sessionID,
	}
	if err := writeMessage(writeMu, conn, hdr, payload); err != nil {
		log.Printf("send fetch range: %v", err)
		return false
	}
	return true
}
