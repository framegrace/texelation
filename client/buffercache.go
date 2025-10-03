package client

import (
	"sort"
	"time"

	"texelation/protocol"
)

// PaneState represents the locally cached state of a pane.
type PaneState struct {
	ID        [16]byte
	Revision  uint32
	UpdatedAt time.Time
	rows      map[int][]rune
	Title     string
	Rect      clientRect
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
			out[i] = string(row)
		}
	}
	return out
}

// BufferCache maintains pane states keyed by pane ID.
type BufferCache struct {
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
func (c *BufferCache) ApplyDelta(delta protocol.BufferDelta) *PaneState {
	if c.panes == nil {
		c.panes = make(map[[16]byte]*PaneState)
	}
	pane := c.panes[delta.PaneID]
	if pane == nil {
		pane = &PaneState{ID: delta.PaneID, rows: make(map[int][]rune)}
		c.panes[delta.PaneID] = pane
	}
	if delta.Revision < pane.Revision {
		return pane
	}

	for _, rowDelta := range delta.Rows {
		rowIdx := int(rowDelta.Row)
		row := pane.rows[rowIdx]
		for _, span := range rowDelta.Spans {
			start := int(span.StartCol)
			textRunes := []rune(span.Text)
			needed := start + len(textRunes)
			row = ensureRowLength(row, needed)
			copy(row[start:], textRunes)
		}
		pane.rows[rowIdx] = trimTrailingSpaces(row)
	}
	pane.Revision = delta.Revision
	pane.UpdatedAt = time.Now().UTC()

	c.trackOrdering(delta.PaneID, pane.UpdatedAt)
	return pane
}

// ApplySnapshot replaces local state with the provided snapshot.
func (c *BufferCache) ApplySnapshot(snapshot protocol.TreeSnapshot) {
	if c.panes == nil {
		c.panes = make(map[[16]byte]*PaneState)
	}
	for _, paneSnap := range snapshot.Panes {
		pane := c.panes[paneSnap.PaneID]
		if pane == nil {
			pane = &PaneState{ID: paneSnap.PaneID, rows: make(map[int][]rune)}
			c.panes[paneSnap.PaneID] = pane
		}
		pane.Title = paneSnap.Title
		pane.Revision = paneSnap.Revision
		pane.UpdatedAt = time.Now().UTC()
		pane.rows = make(map[int][]rune, len(paneSnap.Rows))
		for idx, row := range paneSnap.Rows {
			pane.rows[idx] = []rune(row)
		}
		pane.Rect = clientRect{X: int(paneSnap.X), Y: int(paneSnap.Y), Width: int(paneSnap.Width), Height: int(paneSnap.Height)}
		c.trackOrdering(paneSnap.PaneID, pane.UpdatedAt)
	}
}

// AllPanes returns panes in order of last update.
func (c *BufferCache) AllPanes() []*PaneState {
	panes := make([]*PaneState, len(c.order))
	for i, ord := range c.order {
		panes[i] = c.panes[ord.id]
	}
	return panes
}

// LatestPane returns the most recently updated pane.
func (c *BufferCache) LatestPane() *PaneState {
	if len(c.order) == 0 {
		return nil
	}
	id := c.order[len(c.order)-1].id
	return c.panes[id]
}

func (c *BufferCache) trackOrdering(id [16]byte, ts time.Time) {
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

func ensureRowLength(row []rune, n int) []rune {
	if len(row) >= n {
		return row
	}
	out := make([]rune, n)
	copy(out, row)
	for i := len(row); i < n; i++ {
		out[i] = ' '
	}
	return out
}

func trimTrailingSpaces(row []rune) []rune {
	last := len(row) - 1
	for last >= 0 && row[last] == ' ' {
		last--
	}
	return row[:last+1]
}
