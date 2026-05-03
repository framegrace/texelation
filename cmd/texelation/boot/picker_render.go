// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
	core "github.com/framegrace/texelui/core"
)

const (
	cardThumbW = 22
	cardThumbH = 8
	cardGap    = 1
)

// Render paints the picker. Builds a fresh cell buffer per frame,
// runs the widget tree (cards + tabs + banner + action-bar) through
// a Painter wired to the graphics provider, copies the buffer cells
// to the tcell.Screen, and flushes any queued APC sequences.
func (p *Picker) Render() {
	w, h := p.screen.Size()
	if w <= 0 || h <= 0 {
		return
	}
	if len(p.cellBuf) != h || (len(p.cellBuf) > 0 && len(p.cellBuf[0]) != w) {
		p.cellBuf = make([][]core.Cell, h)
		for y := range p.cellBuf {
			p.cellBuf[y] = make([]core.Cell, w)
		}
	} else {
		for y := range p.cellBuf {
			for x := range p.cellBuf[y] {
				p.cellBuf[y][x] = core.Cell{Ch: ' ', Style: tcell.StyleDefault}
			}
		}
	}
	clip := core.Rect{X: 0, Y: 0, W: w, H: h}
	painter := core.NewPainterWithGraphics(p.cellBuf, clip, p.gp)
	painter.SetScreenSize(w, h)

	// Reset graphics provider's placement set each frame so stale
	// images from the previous frame don't linger when scrolled away
	// or replaced by an upgrade.
	if p.gp != nil {
		p.gp.Reset()
	}

	p.drawHeader(painter, w)
	p.drawTabs(painter, w)
	p.drawCards(painter, w, h)
	p.drawErrorBanner(painter, w, h)
	p.drawActionBar(painter, w, h)
	if p.mode == modeRename {
		p.drawRenameOverlay(painter, w, h)
	}
	if p.mode == modeDeleteConfirm {
		p.drawDeleteConfirmOverlay(painter, w, h)
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := p.cellBuf[y][x]
			ch := c.Ch
			if ch == 0 {
				ch = ' '
			}
			p.screen.SetContent(x, y, ch, nil, c.Style)
		}
	}
	// Flush queued graphics commands. KittyProvider has Flush; the
	// HalfBlockProvider does not (its writes already landed in
	// cellBuf via Place→SetCell), so the type assertion is
	// intentionally Kitty-only — when it fails, no flush is needed.
	if p.gp != nil && p.gpFlush != nil {
		if flusher, ok := p.gp.(interface{ Flush(io.Writer) error }); ok {
			if err := flusher.Flush(p.gpFlush); err != nil {
				log.Printf("picker: graphics flush failed: %v", err)
				p.flushErrMsg = "Thumbnails unavailable: " + err.Error()
			} else {
				p.flushErrMsg = ""
			}
		}
	}
	p.screen.Show()
}

func (p *Picker) drawErrorBanner(painter *core.Painter, w, h int) {
	msg := p.errMsg
	if msg == "" {
		msg = p.flushErrMsg
	}
	if msg == "" {
		return
	}
	style := tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	if len(msg) > w-4 {
		msg = msg[:w-5] + "…"
	}
	painter.DrawText(2, h-3, msg, style)
}

func (p *Picker) drawHeader(painter *core.Painter, w int) {
	style := tcell.StyleDefault.Bold(true)
	title := "texelation — recover session"
	startX := (w - len(title)) / 2
	if startX < 0 {
		startX = 0
	}
	painter.DrawText(startX, 0, title, style)
}

func (p *Picker) drawTabs(painter *core.Painter, w int) {
	live := fmt.Sprintf("[ Live (%d) ]", len(p.response.Live))
	stored := fmt.Sprintf("[ Stored (%d) ]", len(p.response.Stored))
	liveStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	if p.activeTab == tabLive {
		liveStyle = tcell.StyleDefault.Bold(true)
	}
	storedStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	if p.activeTab == tabStored {
		storedStyle = tcell.StyleDefault.Bold(true)
	}
	painter.DrawText(2, 2, live, liveStyle)
	painter.DrawText(2+len(live)+2, 2, stored, storedStyle)
}

func (p *Picker) drawCards(painter *core.Painter, w, h int) {
	startY := 4
	if p.activeTab == tabLive {
		// F.1: render live sessions as lightweight cards. We synthesize
		// a SessionSummary from each LiveSummary so drawCard can stay
		// uniform. No layout/thumbnail data — that belongs to Stored.
		for i, live := range p.response.Live {
			cardY := startY + i*(cardThumbH+cardGap)
			if cardY+cardThumbH+cardGap > h-2 {
				break
			}
			summary := protocol.SessionSummary{
				SessionID:  live.SessionID,
				Label:      live.Label,
				LastActive: live.LastInputAt,
				PaneCount:  live.PaneCount,
			}
			p.drawCard(painter, 2, cardY, summary, i == p.selectedIdx)
		}
		return
	}
	for i, summary := range p.response.Stored {
		cardY := startY + i*(cardThumbH+cardGap)
		if cardY+cardThumbH+cardGap > h-2 {
			break
		}
		p.drawCard(painter, 2, cardY, summary, i == p.selectedIdx)
	}
}

func (p *Picker) drawCard(painter *core.Painter, x, y int, s protocol.SessionSummary, selected bool) {
	bgStyle := tcell.StyleDefault
	if selected {
		bgStyle = bgStyle.Background(tcell.ColorDarkBlue)
	}
	thumbRect := core.Rect{X: x, Y: y, W: cardThumbW, H: cardThumbH}
	p.drawThumbnail(painter, thumbRect, s, bgStyle)

	metaX := x + cardThumbW + 2
	painter.DrawText(metaX, y, fmt.Sprintf("Label:   %s", labelOrUntitled(s.Label)), bgStyle.Bold(true))
	painter.DrawText(metaX, y+1, fmt.Sprintf("Active:  %s", relativeTime(s.LastActive)), bgStyle)
	painter.DrawText(metaX, y+2, fmt.Sprintf("Panes:   %d", s.PaneCount), bgStyle)
	painter.DrawText(metaX, y+3, fmt.Sprintf("Title:   %s", truncate(s.FirstPaneTitle, 40)), bgStyle)
	if s.Pinned {
		painter.DrawText(metaX, y+4, "Pinned:  ★", bgStyle)
	}
}

// drawASCIILayoutAt paints the box-drawing tree for s.Layout into the
// rect via the Painter. Shared between Task 16 (no-graphics or no-
// cached-thumbnail path) and Task 17 (called when the widget isn't
// cached yet).
func (p *Picker) drawASCIILayoutAt(painter *core.Painter, rect core.Rect, layout *protocol.TreeNodeSnapshot, bgStyle tcell.Style) {
	grid := renderASCIILayoutGrid(rect.W, rect.H, layout)
	for cy := 0; cy < rect.H && cy < len(grid); cy++ {
		for cx := 0; cx < rect.W && cx < len(grid[cy]); cx++ {
			painter.SetCell(rect.X+cx, rect.Y+cy, grid[cy][cx], bgStyle)
		}
	}
}

func (p *Picker) drawActionBar(painter *core.Painter, w, h int) {
	bar := "[Enter] recover   [n] new   [r] rename   [d] delete   [q] quit"
	style := tcell.StyleDefault.Foreground(tcell.ColorGray)
	startX := (w - len(bar)) / 2
	if startX < 0 {
		startX = 0
	}
	painter.DrawText(startX, h-2, bar, style)
}

func (p *Picker) drawRenameOverlay(painter *core.Painter, w, h int) {
	prompt := fmt.Sprintf("Rename: %s", string(p.renameBuf))
	painter.DrawText(2, h-4, prompt, tcell.StyleDefault.Bold(true))
}

func (p *Picker) drawDeleteConfirmOverlay(painter *core.Painter, w, h int) {
	if len(p.response.Stored) == 0 {
		return
	}
	prompt := fmt.Sprintf("Delete '%s'? [y/N]", labelOrUntitled(p.response.Stored[p.selectedIdx].Label))
	painter.DrawText(2, h-4, prompt, tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true))
}

func labelOrUntitled(s string) string {
	if s == "" {
		return "Untitled"
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func relativeTime(unixSec int64) string {
	if unixSec == 0 {
		return "—"
	}
	d := time.Since(time.Unix(unixSec, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
	}
}
