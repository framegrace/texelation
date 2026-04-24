// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
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
// AutoFollow=true entries are seeded with ViewTopIdx=0, ViewBottomIdx=1<<62
// (an effectively unbounded window). Using the saved, potentially-stale
// ViewBottomIdx would cause publisher clip to drop rows at the actual live
// edge during the first post-resume publish, leaving the pane blank until
// new output arrives. The client's first post-resume MsgViewportUpdate
// replaces these sentinel values with exact coordinates.
//
// AutoFollow=false entries compute ViewTopIdx = ViewBottomIdx - Rows + 1,
// with an overflow guard: if the subtraction wraps (ViewBottomIdx near
// MinInt64) the result would be a large positive number that skips the <0
// clamp. Both <0 and >ViewBottomIdx are clamped to 0.
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
func (c *ClientViewports) ApplyResume(states []protocol.PaneViewportState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ps := range states {
		var top, bottom int64
		if ps.AutoFollow {
			// AutoFollow=true resumes mean "track live edge". The publisher
			// derives its clip window from ViewTopIdx/ViewBottomIdx; using
			// the saved (potentially stale) value would cause rows at the
			// actual live edge to be clipped out. Seed an effectively
			// unbounded window. The client's first post-resume
			// MsgViewportUpdate replaces these values with exact ones.
			top = 0
			bottom = int64(1) << 62 // well above any realistic globalIdx; not MaxInt64 to avoid + overscan overflow in publisher
		} else {
			top = ps.ViewBottomIdx - int64(ps.ViewportRows) + 1
			// Overflow guard: subtraction can wrap when ViewBottomIdx is
			// near MinInt64, producing a large positive `top` that skips the
			// `top < 0` clamp. Detect wrap via `top > ps.ViewBottomIdx`
			// (impossible without overflow when ViewportRows >= 1) and clamp
			// both top and bottom to 0 so the publisher's clip window is a
			// valid [0, 0] range rather than [largePositive, MinInt64] which
			// would invert the clip direction.
			if top > ps.ViewBottomIdx {
				// Overflow: clamp everything to origin.
				top = 0
				bottom = 0
			} else {
				if top < 0 {
					top = 0
				}
				bottom = ps.ViewBottomIdx
			}
		}
		c.byPaneID[ps.PaneID] = ClientViewport{
			AltScreen:     ps.AltScreen,
			ViewTopIdx:    top,
			ViewBottomIdx: bottom,
			Rows:          ps.ViewportRows,
			Cols:          ps.ViewportCols,
			AutoFollow:    ps.AutoFollow,
		}
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
