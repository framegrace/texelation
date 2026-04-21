// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: client/pane_cache.go
// Summary: Sparse per-pane cell cache keyed by globalIdx for viewport-only rendering.
// Usage: Receives clipped BufferDelta and FetchRangeResponse from the server;
//
//	renderer reads rows by globalIdx instead of screen-row index.
//
// Notes: Main-screen rows are keyed by globalIdx; alt-screen rows are a flat
//
//	screen-sized 2D buffer indexed by screen row.

package client

import (
	"sync"

	"github.com/framegrace/texelation/protocol"
	"github.com/gdamore/tcell/v2"
)

// PaneCache is the client's local copy of a pane's displayable cells.
// Main-screen rows are keyed by globalIdx; alt-screen rows are a flat
// screen-sized 2D buffer.
type PaneCache struct {
	mu        sync.RWMutex
	altScreen bool
	main      map[int64][]Cell
	alt       [][]Cell
	revision  uint32
}

// NewPaneCache constructs an empty PaneCache.
func NewPaneCache() *PaneCache {
	return &PaneCache{main: make(map[int64][]Cell)}
}

// IsAltScreen reports whether the pane is currently in alt-screen mode.
func (c *PaneCache) IsAltScreen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.altScreen
}

// ApplyDelta merges a BufferDelta into the cache.
func (c *PaneCache) ApplyDelta(d protocol.BufferDelta) {
	c.mu.Lock()
	defer c.mu.Unlock()
	alt := d.Flags&protocol.BufferDeltaAltScreen != 0
	if alt != c.altScreen {
		// Mode changed — drop the other side's state.
		c.altScreen = alt
		if alt {
			c.main = make(map[int64][]Cell)
		} else {
			c.alt = nil
		}
	}
	if d.Revision > c.revision {
		c.revision = d.Revision
	}
	// NOTE: Styles are per-delta. We decode spans into concrete cells
	// eagerly so later reads don't need the style table.
	styles := d.Styles
	for _, row := range d.Rows {
		cells := decodeSpans(row.Spans, styles)
		if alt {
			c.putAlt(int(row.Row), cells)
		} else {
			gid := d.RowBase + int64(row.Row)
			c.main[gid] = cells
		}
	}
}

// ApplyFetchRange merges rows fetched on-demand.
func (c *PaneCache) ApplyFetchRange(r protocol.FetchRangeResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Flags&protocol.FetchRangeAltScreenActive != 0 {
		// Server says alt-screen is active — nothing to cache.
		return
	}
	// Coherence rule: drop stale responses.
	if r.Revision < c.revision {
		return
	}
	if r.Revision > c.revision {
		c.revision = r.Revision
	}
	styles := r.Styles
	for _, row := range r.Rows {
		cells := decodeSpans(row.Spans, styles)
		c.main[row.GlobalIdx] = cells
	}
}

// RowAt returns the main-screen row for globalIdx.
func (c *PaneCache) RowAt(globalIdx int64) ([]Cell, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	row, ok := c.main[globalIdx]
	return row, ok
}

// AltRowAt returns the alt-screen row for screenRow.
func (c *PaneCache) AltRowAt(screenRow int) ([]Cell, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if screenRow < 0 || screenRow >= len(c.alt) {
		return nil, false
	}
	return c.alt[screenRow], true
}

// Evict drops main-screen rows outside [lo − overscan×1.5, hi + overscan×1.5].
// Called after each viewport change. Small hysteresis prevents thrash on
// micro-scrolls.
func (c *PaneCache) Evict(lo, hi, overscan int64) {
	band := int64(float64(overscan) * 1.5)
	lowerBound := lo - band
	upperBound := hi + band
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.main {
		if k < lowerBound || k > upperBound {
			delete(c.main, k)
		}
	}
}

// MissingRows returns the globalIdxs in [lo, hi] not currently in cache.
// Caller uses this to issue a MsgFetchRange. Returned slice is sorted ascending.
func (c *PaneCache) MissingRows(lo, hi int64) []int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var miss []int64
	for gid := lo; gid <= hi; gid++ {
		if _, ok := c.main[gid]; !ok {
			miss = append(miss, gid)
		}
	}
	return miss
}

func (c *PaneCache) putAlt(row int, cells []Cell) {
	for row >= len(c.alt) {
		c.alt = append(c.alt, nil)
	}
	c.alt[row] = cells
}

// decodeSpans expands a slice of CellSpan into a concrete []Cell row.
// The returned slice length covers all columns written by the spans.
func decodeSpans(spans []protocol.CellSpan, styles []protocol.StyleEntry) []Cell {
	built := buildStyles(styles)
	var row []Cell
	for _, span := range spans {
		start := int(span.StartCol)
		textRunes := []rune(span.Text)
		needed := start + len(textRunes)
		row = ensureRowLength(row, needed)
		style := buildStyleAt(built, int(span.StyleIndex))
		var dynFG, dynBG protocol.DynColorDesc
		if int(span.StyleIndex) < len(styles) {
			entry := styles[span.StyleIndex]
			if entry.AttrFlags&protocol.AttrHasDynamic != 0 {
				dynFG = entry.DynFG
				dynBG = entry.DynBG
			}
		}
		for i, r := range textRunes {
			row[start+i] = Cell{Ch: r, Style: style, DynFG: dynFG, DynBG: dynBG}
		}
	}
	return row
}

// buildStyleAt returns the tcell.Style at index i, or StyleDefault when out of range.
func buildStyleAt(styles []tcell.Style, i int) tcell.Style {
	if i < 0 || i >= len(styles) {
		return tcell.StyleDefault
	}
	return styles[i]
}
