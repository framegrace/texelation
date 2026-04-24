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
// ViewTopIdx is derived as ViewBottomIdx - Rows + 1, clamped to 0 for panes
// whose saved bottom is close to the origin. Publisher clipping uses this
// for first-paint; once rendering settles, the client's normal
// MsgViewportUpdate (via flushFrame) reconciles exact values.
func (c *ClientViewports) ApplyResume(states []protocol.PaneViewportState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ps := range states {
		top := ps.ViewBottomIdx - int64(ps.ViewportRows) + 1
		if top < 0 {
			top = 0
		}
		c.byPaneID[ps.PaneID] = ClientViewport{
			AltScreen:     ps.AltScreen,
			ViewTopIdx:    top,
			ViewBottomIdx: ps.ViewBottomIdx,
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
