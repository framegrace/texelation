// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"log"
	"sync"

	"github.com/framegrace/texelation/protocol"
)

// ClientViewport is the server's view of a single pane's state inside a client
// session. Publisher uses it to clip BufferDelta rows at emit time.
// ViewportUpdate.WrapSegmentIdx is intentionally not mirrored here — it is reserved
// for Plan B resume; clipping only needs ViewTopIdx / ViewBottomIdx.
type ClientViewport struct {
	AltScreen     bool
	ViewTopIdx    int64
	ViewBottomIdx int64
	Rows          uint16
	Cols          uint16
	AutoFollow    bool
}

// ClientViewports is the per-Session map of pane -> viewport.
type ClientViewports struct {
	mu       sync.RWMutex
	byPaneID map[[16]byte]ClientViewport
}

func NewClientViewports() *ClientViewports {
	return &ClientViewports{byPaneID: make(map[[16]byte]ClientViewport)}
}

func (c *ClientViewports) Apply(u protocol.ViewportUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byPaneID[u.PaneID] = ClientViewport{
		AltScreen:     u.AltScreen,
		ViewTopIdx:    u.ViewTopIdx,
		ViewBottomIdx: u.ViewBottomIdx,
		Rows:          u.Rows,
		Cols:          u.Cols,
		AutoFollow:    u.AutoFollow,
	}
}

func (c *ClientViewports) Get(paneID [16]byte) (ClientViewport, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.byPaneID[paneID]
	return v, ok
}

// ApplyResume seeds the viewport map from a ResumeRequest.PaneViewports list.
// Publisher clipping uses these values on the first post-resume publish; once
// rendering settles, the client's normal MsgViewportUpdate (via flushFrame)
// reconciles exact values.
//
// Derivation: top = ViewBottomIdx - ViewportRows + 1, with an overflow guard.
// If the subtraction wraps (ViewBottomIdx near MinInt64), the result would be
// a large positive number that skips the <0 clamp; in that case both top and
// bottom are clamped to 0.
//
// AutoFollow is stored verbatim on the ClientViewport. Note that for
// AutoFollow=true resume entries, ViewBottomIdx is the value the client last
// sent — which may be stale relative to the live edge. The publisher knows
// to IGNORE ClientViewport.ViewBottomIdx when AutoFollow=true and instead
// derive the clip window from the pane's rendered snap.RowGlobalIdx (see
// bufferToDelta in desktop_publisher.go). This avoids leaking a sentinel
// (previously 1<<62) into the clip math, where it would combine with
// overscan to produce a (hi-lo) span far exceeding uint16 and wrap
// RowDelta.Row on the wire.
//
// Known first-paint approximations, both reconciled by the first
// flushFrame-driven MsgViewportUpdate after resume:
//
//  1. Wrapped-chain terminals: a single globalIdx at the bottom can span
//     multiple display rows, so ViewTopIdx = ViewBottomIdx - Rows + 1 sits
//     at a higher globalIdx than the true top of the visible region.
//     Publisher overscan absorbs most of the gap; the MsgViewportUpdate
//     after resume tightens it.
//
//  2. Missing-anchor scrollback: if the payload's ViewBottomIdx is below
//     the store's OldestRetained, Terminal.RestoreViewport snaps the pane
//     to OldestRetained (Policy A). This function still writes the raw
//     protocol ViewBottomIdx into the ClientViewport, so the publisher's
//     clip window is briefly narrower than the pane's render buffer —
//     some rows the pane emits above OldestRetained will be dropped by
//     the publisher until the client's next MsgViewportUpdate. Acceptable
//     because the client's flushFrame fires on the very next render
//     frame after the resumed snapshot applies.
//
// If either approximation becomes a visible regression in practice,
// propagate the post-snap effective bottom from RestorePaneViewport back
// into ApplyResume (return value plumbing) rather than rederiving here.
//
// The paneExists predicate filters out phantom paneIDs — IDs the client
// supplied that no longer exist server-side (closed pane during offline
// time, cross-restart drift, or a recovered persistence file pointing
// at a long-gone pane). Without pruning, the map grows unboundedly with
// stale entries on every cross-restart resume. Pass nil to disable
// pruning (tests). Logs once per call when entries are dropped, to
// surface the drop count for Plan F (session recovery) debugging.
func (c *ClientViewports) ApplyResume(states []protocol.PaneViewportState, paneExists func(id [16]byte) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	dropped := 0
	for _, ps := range states {
		if paneExists != nil && !paneExists(ps.PaneID) {
			dropped++
			continue
		}
		top := ps.ViewBottomIdx - int64(ps.ViewportRows) + 1
		// Overflow guard: subtraction can wrap when ViewBottomIdx is near
		// MinInt64. Clamp both on overflow; only clamp top on a plain
		// negative underflow (ViewBottomIdx small but in-range).
		bottom := ps.ViewBottomIdx
		if top > ps.ViewBottomIdx {
			top, bottom = 0, 0
		} else if top < 0 {
			top = 0
		}
		c.byPaneID[ps.PaneID] = ClientViewport{
			AltScreen:     ps.AltScreen,
			ViewTopIdx:    top,
			ViewBottomIdx: bottom,
			Rows:          ps.ViewportRows,
			Cols:          ps.ViewportCols,
			AutoFollow:    ps.AutoFollow, // publisher uses this as the "derive clip from snap" signal
		}
	}
	if dropped > 0 {
		log.Printf("ClientViewports.ApplyResume: dropped %d phantom paneID entries (cross-restart or closed-pane race)", dropped)
	}
}

// Snapshot returns a shallow copy of all viewports. Intended for publisher
// fan-out; callers must treat the result as read-only.
func (c *ClientViewports) Snapshot() map[[16]byte]ClientViewport {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[[16]byte]ClientViewport, len(c.byPaneID))
	for k, v := range c.byPaneID {
		out[k] = v
	}
	return out
}
