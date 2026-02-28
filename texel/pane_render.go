// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/pane_render.go
// Summary: Rendering, dirty tracking, and geometry for panes.

package texel

import (
	"sync/atomic"

	"github.com/framegrace/texelation/internal/debuglog"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/gdamore/tcell/v2"
)

// markDirty flags this pane for re-render on the next snapshot cycle.
func (p *pane) markDirty() {
	atomic.AddInt32(&p.renderGen, 1)
}

// setupRefreshForwarder creates a per-pane channel that marks the pane dirty
// and sends a refresh signal to the desktop event loop. This enables per-pane
// render skipping: only panes whose app signalled a refresh (or whose state
// changed) will be re-rendered.
//
// The target parameter is kept for backward compatibility (avoids changing
// AttachApp, PrepareAppForRestore, and all their callers) but is unused.
func (p *pane) setupRefreshForwarder(target chan<- bool) chan<- bool {
	if p.refreshStop != nil {
		close(p.refreshStop)
	}

	ch := make(chan bool, 1)
	stop := make(chan struct{})
	p.refreshStop = stop

	go func() {
		for {
			select {
			case <-stop:
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				atomic.AddInt32(&p.renderGen, 1)
				// Send to desktop event loop for publishing
				if p.screen != nil && p.screen.desktop != nil {
					p.screen.desktop.SendRefresh()
				}
			}
		}
	}()

	return ch
}

// Render draws the pane's borders, title, and the hosted application's content including effects.
func (p *pane) Render() [][]Cell {
	return p.renderBuffer(true)
}

func (p *pane) renderBuffer(applyEffects bool) [][]Cell {
	w := p.Width()
	h := p.Height()

	// Return cached buffer when pane hasn't changed (Level 2 optimization).
	// Uses a generation counter: markDirty() increments renderGen, and we
	// only return the cache when it matches lastRendered. This avoids the
	// TOCTOU race where a binary flag set during rendering gets clobbered
	// by the post-render clear.
	currentTitle := p.getTitle()
	gen := atomic.LoadInt32(&p.renderGen)
	if gen == p.lastRendered && p.prevBuf != nil &&
		len(p.prevBuf) == h && (h == 0 || len(p.prevBuf[0]) == w) &&
		p.prevTitle == currentTitle {
		return p.prevBuf
	}

	// Refresh theme styles each time we actually render.
	p.refreshBorderStyles()

	tm := theme.Get()
	desktopBg := tm.GetSemanticColor("bg.base").TrueColor()
	desktopFg := tm.GetSemanticColor("text.primary").TrueColor()
	defstyle := tcell.StyleDefault.Background(desktopBg).Foreground(desktopFg)

	// Create the pane's buffer.
	buffer := make([][]Cell, h)
	for i := range buffer {
		buffer[i] = make([]Cell, w)
		for j := range buffer[i] {
			buffer[i][j] = Cell{Ch: ' ', Style: defstyle}
		}
	}

	// Don't draw decorations if the pane is too small.
	if w < 2 || h < 2 {
		debuglog.Printf("Render: Pane '%s' too small to draw decorations (%dx%d)", p.getTitle(), w, h)
		return buffer
	}

	// Reset graphics placements before rendering so only visible widgets
	// re-place their images. This mirrors the standalone runtime's
	// graphicsProvider.Reset() → app.Render() → Flush() cycle.
	if p.app != nil {
		if ua, ok := p.app.(interface{ UI() *texelcore.UIManager }); ok {
			if gp := ua.UI().GraphicsProvider(); gp != nil {
				gp.Reset()
			}
		}
	}

	// Render content from pipeline (or app as fallback).
	var appBuffer [][]Cell
	if p.pipeline != nil {
		appBuffer = p.pipeline.Render()
	} else if p.app != nil {
		appBuffer = p.app.Render()
	}

	// Update persistent border widget state.
	p.border.Resize(w, h)
	p.border.Title = p.getTitle()
	p.border.IsResizing = p.IsResizing
	if p.RoundedCorners {
		p.border.SetRoundedCorners()
	} else {
		p.border.SetSquareCorners()
	}

	// Update focus state only when it changes.
	if p.IsActive != p.wasActive {
		if p.IsActive {
			p.border.SetFocusable(true)
			p.border.BaseWidget.Focus()
		} else {
			p.border.BaseWidget.Blur()
		}
		p.wasActive = p.IsActive
	}

	// Swap buffer reference into the child widget.
	p.bufferWidget.SetBuffer(appBuffer)

	// Draw through the persistent widget tree.
	painter := texelcore.NewPainter(buffer, texelcore.Rect{X: 0, Y: 0, W: w, H: h})
	p.border.Draw(painter)

	// Cache the rendered buffer for reuse when pane hasn't changed.
	p.prevBuf = buffer
	p.prevTitle = currentTitle
	p.lastRendered = gen

	return buffer
}

func (p *pane) Width() int {
	w := p.absX1 - p.absX0
	if w < 0 {
		return 0
	}
	return w
}

func (p *pane) Height() int {
	h := p.absY1 - p.absY0
	if h < 0 {
		return 0
	}
	return h
}

// Rest of the methods remain the same...
func (p *pane) drawableWidth() int {
	w := p.Width() - 2
	if w < 0 {
		return 0
	}
	return w
}

func (p *pane) drawableHeight() int {
	h := p.Height() - 2
	if h < 0 {
		return 0
	}
	return h
}

func (p *pane) setDimensions(x0, y0, x1, y1 int) {
	if p.absX0 == x0 && p.absY0 == y0 && p.absX1 == x1 && p.absY1 == y1 {
		return
	}

	p.absX0, p.absY0, p.absX1, p.absY1 = x0, y0, x1, y1
	p.prevBuf = nil
	p.markDirty()

	drawableW := p.drawableWidth()
	drawableH := p.drawableHeight()

	// Resize pipeline (or app as fallback)
	if p.pipeline != nil {
		p.pipeline.Resize(drawableW, drawableH)
	} else if p.app != nil {
		p.app.Resize(drawableW, drawableH)
	} else {
		debuglog.Printf("setDimensions: Pane '%s' has no app yet!", p.getTitle())
	}
}

// contains reports whether the absolute pane bounds include the provided coordinates.
func (p *pane) contains(x, y int) bool {
	if p == nil {
		return false
	}
	if x < p.absX0 || x >= p.absX1 {
		return false
	}
	if y < p.absY0 || y >= p.absY1 {
		return false
	}
	return true
}
