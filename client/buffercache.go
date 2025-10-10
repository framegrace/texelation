package client

import (
	"sort"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
)

// PaneState represents the locally cached state of a pane.
type PaneState struct {
	ID        [16]byte
	Revision  uint32
	UpdatedAt time.Time
	rows      map[int][]Cell
	Title     string
	Rect      clientRect
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
	return p.rows[row]
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
		pane = &PaneState{ID: delta.PaneID, rows: make(map[int][]Cell)}
		c.panes[delta.PaneID] = pane
	}
	if delta.Revision < pane.Revision {
		return pane
	}

	styles := buildStyles(delta.Styles)
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
	seen := make(map[[16]byte]struct{}, len(snapshot.Panes))
	for _, paneSnap := range snapshot.Panes {
		pane := c.panes[paneSnap.PaneID]
		if pane == nil {
			pane = &PaneState{ID: paneSnap.PaneID}
			c.panes[paneSnap.PaneID] = pane
		}
		pane.Title = paneSnap.Title
		pane.Revision = paneSnap.Revision
		pane.UpdatedAt = time.Now().UTC()
		pane.rows = make(map[int][]Cell, len(paneSnap.Rows))
		for idx, row := range paneSnap.Rows {
			pane.rows[idx] = stringToCells(row)
		}
		pane.Rect = clientRect{X: int(paneSnap.X), Y: int(paneSnap.Y), Width: int(paneSnap.Width), Height: int(paneSnap.Height)}
		c.trackOrdering(paneSnap.PaneID, pane.UpdatedAt)
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

// AllPanes returns panes in order of last update.
func (c *BufferCache) AllPanes() []*PaneState {
	panes := make([]*PaneState, len(c.order))
	for i, ord := range c.order {
		panes[i] = c.panes[ord.id]
	}
	return panes
}

// LayoutPanes returns panes sorted by their recorded geometry so renderers can
// draw them deterministically.
func (c *BufferCache) LayoutPanes() []*PaneState {
	if len(c.panes) == 0 {
		return nil
	}
	panes := make([]*PaneState, 0, len(c.panes))
	for _, pane := range c.panes {
		panes = append(panes, pane)
	}
	sort.Slice(panes, func(i, j int) bool {
		pi, pj := panes[i], panes[j]
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
