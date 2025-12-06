// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: client/buffercache.go
// Summary: Implements buffercache capabilities for the client runtime support library.
// Usage: Imported by the remote renderer to manage buffercache during live sessions.
// Notes: Shared across multiple client binaries and tests.

package client

import (
	"sort"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
)

// PaneState represents the locally cached state of a pane.
type PaneState struct {
	ID               [16]byte
	Revision         uint32
	UpdatedAt        time.Time
	rowsMu           sync.RWMutex
	rows             map[int][]Cell
	Title            string
	Rect             clientRect
	Active           bool
	Resizing         bool
	ZOrder           int
	HandlesSelection bool
}

// Cell mirrors texel.Cell but keeps the remote client decoupled from desktop internals.
type Cell struct {
	Ch    rune
	Style tcell.Style
}

type clientRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Rows returns the pane contents as a slice of strings in row order.
func (p *PaneState) Rows() []string {
	if p == nil {
		return nil
	}
	p.rowsMu.RLock()
	defer p.rowsMu.RUnlock()
	if len(p.rows) == 0 {
		return nil
	}
	maxRow := 0
	for idx := range p.rows {
		if idx > maxRow {
			maxRow = idx
		}
	}
	out := make([]string, maxRow+1)
	for i := 0; i <= maxRow; i++ {
		row := p.rows[i]
		if len(row) == 0 {
			out[i] = ""
		} else {
			runes := make([]rune, len(row))
			for idx, cell := range row {
				runes[idx] = cell.Ch
			}
			out[i] = trimRightSpaces(string(runes))
		}
	}
	return out
}

// RowCells returns the styled cells for the given row, if present.
func (p *PaneState) RowCells(row int) []Cell {
	if p == nil || p.rows == nil {
		return nil
	}
	p.rowsMu.RLock()
	defer p.rowsMu.RUnlock()
	src := p.rows[row]
	if len(src) == 0 {
		return nil
	}
	out := make([]Cell, len(src))
	copy(out, src)
	return out
}

// BufferCache maintains pane states keyed by pane ID.
type BufferCache struct {
	mu    sync.RWMutex
	panes map[[16]byte]*PaneState
	order []paneOrder
}

type paneOrder struct {
	id   [16]byte
	seen time.Time
}

// NewBufferCache constructs an empty cache.
func NewBufferCache() *BufferCache {
	return &BufferCache{panes: make(map[[16]byte]*PaneState)}
}

// ApplyDelta merges the buffer delta into the cache and returns the updated pane.

func (c *BufferCache) ApplyDelta(delta protocol.BufferDelta) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.panes == nil {
		c.panes = make(map[[16]byte]*PaneState)
	}
	pane := c.panes[delta.PaneID]
	if pane == nil {
		pane = &PaneState{ID: delta.PaneID, rows: make(map[int][]Cell)}
		c.panes[delta.PaneID] = pane
	}
	if delta.Revision < pane.Revision {
		return
	}

	styles := buildStyles(delta.Styles)
	pane.rowsMu.Lock()
	for _, rowDelta := range delta.Rows {
		rowIdx := int(rowDelta.Row)
		row := pane.rows[rowIdx]
		for _, span := range rowDelta.Spans {
			start := int(span.StartCol)
			textRunes := []rune(span.Text)
			needed := start + len(textRunes)
			row = ensureRowLength(row, needed)
			style := tcell.StyleDefault
			if int(span.StyleIndex) < len(styles) {
				style = styles[span.StyleIndex]
			}
			for i, r := range textRunes {
				row[start+i] = Cell{Ch: r, Style: style}
			}
		}
		pane.rows[rowIdx] = row
	}
	pane.rowsMu.Unlock()
	pane.Revision = delta.Revision
	pane.UpdatedAt = time.Now().UTC()

	c.trackOrderingLocked(delta.PaneID, pane.UpdatedAt)
}

// ApplySnapshot replaces local state with the provided snapshot.
func (c *BufferCache) ApplySnapshot(snapshot protocol.TreeSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.panes == nil {
		c.panes = make(map[[16]byte]*PaneState)
	}
	seen := make(map[[16]byte]struct{}, len(snapshot.Panes))
	for _, paneSnap := range snapshot.Panes {
		pane := c.panes[paneSnap.PaneID]
		if pane == nil {
			pane = &PaneState{ID: paneSnap.PaneID}
			c.panes[paneSnap.PaneID] = pane
		}
		pane.Title = paneSnap.Title
		pane.UpdatedAt = time.Now().UTC()
		if paneSnap.Revision != 0 || pane.Revision == 0 {
			pane.Revision = paneSnap.Revision
		}
		pane.rowsMu.Lock()
		prevRows := pane.rows
		pane.rows = applySnapshotRows(prevRows, paneSnap.Rows, int(paneSnap.Height), int(paneSnap.Width))
		pane.rowsMu.Unlock()
		pane.Rect = clientRect{X: int(paneSnap.X), Y: int(paneSnap.Y), Width: int(paneSnap.Width), Height: int(paneSnap.Height)}
		c.trackOrderingLocked(paneSnap.PaneID, pane.UpdatedAt)
		seen[paneSnap.PaneID] = struct{}{}
	}
	if len(seen) > 0 && len(c.panes) > len(seen) {
		for id := range c.panes {
			if _, ok := seen[id]; !ok {
				delete(c.panes, id)
			}
		}
		filtered := c.order[:0]
		for _, ord := range c.order {
			if _, ok := c.panes[ord.id]; ok {
				filtered = append(filtered, ord)
			}
		}
		c.order = filtered
	}
}

// SetPaneFlags updates tracked pane flags, creating an entry if necessary.
func (c *BufferCache) SetPaneFlags(id [16]byte, active, resizing bool, zOrder int32, handlesSelection bool) *PaneState {
	c.mu.Lock()
	defer c.mu.Unlock()
	pane := c.panes[id]
	if pane == nil {
		pane = &PaneState{ID: id, rows: make(map[int][]Cell)}
		c.panes[id] = pane
	}
	pane.Active = active
	pane.Resizing = resizing
	pane.ZOrder = int(zOrder)
	pane.HandlesSelection = handlesSelection
	return pane
}

// AllPanes returns panes in order of last update.
func (c *BufferCache) AllPanes() []*PaneState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	panes := make([]*PaneState, len(c.order))
	for i, ord := range c.order {
		panes[i] = c.panes[ord.id]
	}
	return panes
}

// Pane returns the cached pane state by ID, if present.
func (c *BufferCache) Pane(id [16]byte) *PaneState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.panes[id]
}

// LayoutPanes returns panes sorted by their recorded geometry so renderers can
// draw them deterministically.
func (c *BufferCache) ForEachPaneSorted(fn func(*PaneState)) {
	for _, pane := range c.SortedPanes() {
		fn(pane)
	}
}

// PaneByID returns the cached pane for the given identifier, if present.
func (c *BufferCache) PaneByID(id [16]byte) *PaneState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.panes[id]
}

// PaneAt returns the topmost pane containing the provided workspace coordinates.
func (c *BufferCache) PaneAt(x, y int) *PaneState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.panes) == 0 {
		return nil
	}
	var best *PaneState
	for _, pane := range c.panes {
		if pane == nil {
			continue
		}
		rect := pane.Rect
		if rect.Width <= 0 || rect.Height <= 0 {
			continue
		}
		if x < rect.X || x >= rect.X+rect.Width || y < rect.Y || y >= rect.Y+rect.Height {
			continue
		}
		if best == nil {
			best = pane
			continue
		}
		if pane.ZOrder > best.ZOrder {
			best = pane
			continue
		}
		if pane.ZOrder == best.ZOrder {
			if pane.Active && !best.Active {
				best = pane
				continue
			}
			if pane.Rect.Y > best.Rect.Y || (pane.Rect.Y == best.Rect.Y && pane.Rect.X > best.Rect.X) {
				best = pane
			}
		}
	}
	return best
}

// SortedPanes returns the panes ordered by geometry (top-to-bottom, left-to-right).
func (c *BufferCache) SortedPanes() []*PaneState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.panes) == 0 {
		return nil
	}
	panes := make([]*PaneState, 0, len(c.panes))
	for _, pane := range c.panes {
		panes = append(panes, pane)
	}
	sort.Slice(panes, func(i, j int) bool {
		pi, pj := panes[i], panes[j]
		if pi == nil || pj == nil {
			return i < j
		}
		if pi.ZOrder != pj.ZOrder {
			return pi.ZOrder < pj.ZOrder
		}
		if pi.Rect.Y != pj.Rect.Y {
			return pi.Rect.Y < pj.Rect.Y
		}
		if pi.Rect.X != pj.Rect.X {
			return pi.Rect.X < pj.Rect.X
		}
		return compareBytes(pi.ID[:], pj.ID[:]) < 0
	})
	return panes
}

// LatestPane returns the most recently updated pane.
func (c *BufferCache) LatestPane() *PaneState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.order) == 0 {
		return nil
	}
	id := c.order[len(c.order)-1].id
	return c.panes[id]
}

func (c *BufferCache) trackOrdering(id [16]byte, ts time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trackOrderingLocked(id, ts)
}

func (c *BufferCache) trackOrderingLocked(id [16]byte, ts time.Time) {
	found := false
	for i := range c.order {
		if c.order[i].id == id {
			c.order[i].seen = ts
			found = true
			break
		}
	}
	if !found {
		c.order = append(c.order, paneOrder{id: id, seen: ts})
	}
	sort.Slice(c.order, func(i, j int) bool {
		return c.order[i].seen.Before(c.order[j].seen)
	})
}

func applySnapshotRows(existing map[int][]Cell, rows []string, height, width int) map[int][]Cell {
	if len(rows) > 0 {
		newRows := make(map[int][]Cell, len(rows))
		for idx, row := range rows {
			cells := stringToCells(row)
			if width > 0 {
				cells = resizeRowToWidth(cells, width)
			}
			newRows[idx] = cells
		}
		return newRows
	}
	return resizePaneRows(existing, height, width)
}

func resizePaneRows(existing map[int][]Cell, height, width int) map[int][]Cell {
	if height < 0 {
		height = 0
	}
	if width <= 0 {
		return make(map[int][]Cell)
	}
	out := make(map[int][]Cell, height)
	for row := 0; row < height; row++ {
		var src []Cell
		if existing != nil {
			src = existing[row]
		}
		out[row] = resizeRowToWidth(src, width)
	}
	return out
}

func resizeRowToWidth(row []Cell, width int) []Cell {
	if width <= 0 {
		return nil
	}
	if len(row) >= width {
		out := make([]Cell, width)
		copy(out, row[:width])
		return out
	}
	return ensureRowLength(row, width)
}

func ensureRowLength(row []Cell, n int) []Cell {
	if len(row) >= n {
		return row
	}
	out := make([]Cell, n)
	copy(out, row)
	for i := len(row); i < n; i++ {
		out[i] = Cell{Ch: ' ', Style: tcell.StyleDefault}
	}
	return out
}

func buildStyles(entries []protocol.StyleEntry) []tcell.Style {
	styles := make([]tcell.Style, len(entries))
	for i, entry := range entries {
		styles[i] = styleFromEntry(entry)
	}
	return styles
}

func styleFromEntry(entry protocol.StyleEntry) tcell.Style {
	style := tcell.StyleDefault
	fg := colorFromModel(entry.FgModel, entry.FgValue)
	bg := colorFromModel(entry.BgModel, entry.BgValue)
	style = style.Foreground(fg).Background(bg)
	if entry.AttrFlags&protocol.AttrBold != 0 {
		style = style.Bold(true)
	}
	if entry.AttrFlags&protocol.AttrUnderline != 0 {
		style = style.Underline(true)
	}
	if entry.AttrFlags&protocol.AttrReverse != 0 {
		style = style.Reverse(true)
	}
	if entry.AttrFlags&protocol.AttrBlink != 0 {
		style = style.Blink(true)
	}
	if entry.AttrFlags&protocol.AttrDim != 0 {
		style = style.Dim(true)
	}
	if entry.AttrFlags&protocol.AttrItalic != 0 {
		style = style.Italic(true)
	}
	return style
}

func colorFromModel(model protocol.ColorModel, value uint32) tcell.Color {
	switch model {
	case protocol.ColorModelDefault:
		return tcell.ColorDefault
	case protocol.ColorModelRGB:
		r := int32(value >> 16 & 0xFF)
		g := int32(value >> 8 & 0xFF)
		b := int32(value & 0xFF)
		return tcell.NewRGBColor(r, g, b)
	case protocol.ColorModelANSI16, protocol.ColorModelANSI256:
		return tcell.PaletteColor(int(value))
	default:
		return tcell.ColorDefault
	}
}

func stringToCells(row string) []Cell {
	runes := []rune(row)
	cells := make([]Cell, len(runes))
	for i, r := range runes {
		cells[i] = Cell{Ch: r, Style: tcell.StyleDefault}
	}
	return cells
}

func trimRightSpaces(s string) string {
	runes := []rune(s)
	end := len(runes)
	for end > 0 && runes[end-1] == ' ' {
		end--
	}
	return string(runes[:end])
}

func compareBytes(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}
