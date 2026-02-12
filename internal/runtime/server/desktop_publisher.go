// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/desktop_publisher.go
// Summary: Implements desktop publisher capabilities for the server runtime.
// Usage: Used by texel-server to coordinate desktop publisher when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
	"github.com/framegrace/texelui/theme"
)

// DesktopPublisher captures desktop pane buffers and enqueues them as buffer
// deltas on the associated session.
type DesktopPublisher struct {
	desktop     *texel.DesktopEngine
	session     *Session
	revisions   map[[16]byte]uint32
	prevBuffers map[[16]byte][][]texel.Cell
	observer    PublishObserver
	mu          sync.Mutex
	notify      func()
}

// PublishObserver records desktop publish metrics for instrumentation.
type PublishObserver interface {
	ObservePublish(session *Session, paneCount int, duration time.Duration)
}

func NewDesktopPublisher(desktop *texel.DesktopEngine, session *Session) *DesktopPublisher {
	return &DesktopPublisher{
		desktop:     desktop,
		session:     session,
		revisions:   make(map[[16]byte]uint32),
		prevBuffers: make(map[[16]byte][][]texel.Cell),
	}
}

// SetObserver registers an optional metrics observer invoked after each publish.
func (p *DesktopPublisher) SetObserver(observer PublishObserver) {
	p.observer = observer
}

// ResetDiffState clears the previous-frame buffers so the next Publish
// sends a full snapshot. Call after sending a TreeSnapshot to a new client.
func (p *DesktopPublisher) ResetDiffState() {
	p.mu.Lock()
	p.prevBuffers = make(map[[16]byte][][]texel.Cell)
	p.mu.Unlock()
}

// SetNotifier registers a callback invoked after diffs are enqueued.
func (p *DesktopPublisher) SetNotifier(fn func()) {
	p.notify = fn
}

func (p *DesktopPublisher) Publish() error {
	if p.desktop == nil || p.session == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	start := time.Now()
	buffers := p.desktop.SnapshotBuffers()
	// if len(buffers) > 0 { log.Printf("DesktopPublisher: Publishing %d buffers", len(buffers)) }

	for _, snap := range buffers {
		rev := p.revisions[snap.ID] + 1
		p.revisions[snap.ID] = rev
		prev := p.prevBuffers[snap.ID]
		delta := bufferToDelta(snap, prev, rev)
		p.prevBuffers[snap.ID] = snap.Buffer
		if len(delta.Rows) == 0 {
			continue
		}
		if err := p.session.EnqueueDiff(delta); err != nil {
			return err
		}
	}
	elapsed := time.Since(start)
	p.desktop.SetLastPublishDuration(elapsed)
	if p.observer != nil {
		p.observer.ObservePublish(p.session, len(buffers), elapsed)
	}
	if p.notify != nil {
		p.notify()
	}
	return nil
}

func bufferToDelta(snap texel.PaneSnapshot, prev [][]texel.Cell, revision uint32) protocol.BufferDelta {
	styleMap := make(map[styleKey]uint16)
	styles := make([]protocol.StyleEntry, 0)

	rows := make([]protocol.RowDelta, 0, len(snap.Buffer))
	for y, row := range snap.Buffer {
		if len(row) == 0 {
			continue
		}
		if y < len(prev) && rowsEqual(row, prev[y]) {
			continue
		}
		spans := make([]protocol.CellSpan, 0)
		builders := make([]*strings.Builder, 0)

		for x, cell := range row {
			key, entry := convertStyle(cell.Style)
			index, ok := styleMap[key]
			if !ok {
				styles = append(styles, entry)
				index = uint16(len(styles) - 1)
				styleMap[key] = index
			}

			if len(spans) == 0 || spans[len(spans)-1].StyleIndex != index {
				spans = append(spans, protocol.CellSpan{StartCol: uint16(x), StyleIndex: index})
				builders = append(builders, &strings.Builder{})
			}
			builders[len(builders)-1].WriteRune(cell.Ch)
		}

		for i := range spans {
			spans[i].Text = builders[i].String()
		}

		rows = append(rows, protocol.RowDelta{Row: uint16(y), Spans: spans})
	}

	return protocol.BufferDelta{
		PaneID:   snap.ID,
		Revision: revision,
		Flags:    protocol.BufferDeltaNone,
		Styles:   styles,
		Rows:     rows,
	}
}

type styleKey struct {
	attrFlags uint16
	fgModel   protocol.ColorModel
	fgValue   uint32
	bgModel   protocol.ColorModel
	bgValue   uint32
}

func convertStyle(style tcell.Style) (styleKey, protocol.StyleEntry) {
	fg, bg, attrs := style.Decompose()

	attrFlags := uint16(0)
	if attrs&tcell.AttrBold != 0 {
		attrFlags |= protocol.AttrBold
	}
	if attrs&tcell.AttrUnderline != 0 {
		attrFlags |= protocol.AttrUnderline
	}
	if attrs&tcell.AttrReverse != 0 {
		attrFlags |= protocol.AttrReverse
	}
	if attrs&tcell.AttrBlink != 0 {
		attrFlags |= protocol.AttrBlink
	}
	if attrs&tcell.AttrDim != 0 {
		attrFlags |= protocol.AttrDim
	}
	if attrs&tcell.AttrItalic != 0 {
		attrFlags |= protocol.AttrItalic
	}

	fgModel, fgValue := convertColor(fg)
	bgModel, bgValue := convertColor(bg)

	key := styleKey{attrFlags: attrFlags, fgModel: fgModel, fgValue: fgValue, bgModel: bgModel, bgValue: bgValue}
	entry := protocol.StyleEntry{
		AttrFlags: attrFlags,
		FgModel:   fgModel,
		FgValue:   fgValue,
		BgModel:   bgModel,
		BgValue:   bgValue,
	}
	return key, entry
}

func convertColor(color tcell.Color) (protocol.ColorModel, uint32) {
	if color == tcell.ColorDefault {
		return protocol.ColorModelDefault, 0
	}

	// Intercept standard ANSI colors (0-15) and map them to theme colors
	if color >= tcell.ColorBlack && color <= tcell.ColorWhite {
		// We map standard tcell colors to their palette names
		// tcell.ColorBlack (0) -> "surface1" (or explicit ansi black)
		// tcell.ColorRed (1) -> "red"
		// ...
		var paletteName string
		switch color {
		case tcell.ColorBlack:
			paletteName = "surface1" // Often better than true black for TUI
		case tcell.ColorMaroon:
			paletteName = "maroon"
		case tcell.ColorGreen:
			paletteName = "green"
		case tcell.ColorOlive:
			paletteName = "yellow"
		case tcell.ColorNavy:
			paletteName = "blue"
		case tcell.ColorPurple:
			paletteName = "pink"
		case tcell.ColorTeal:
			paletteName = "teal"
		case tcell.ColorSilver:
			paletteName = "subtext1"
		case tcell.ColorGray:
			paletteName = "surface2"
		case tcell.ColorRed:
			paletteName = "red"
		case tcell.ColorLime:
			paletteName = "green"
		case tcell.ColorYellow:
			paletteName = "yellow"
		case tcell.ColorBlue:
			paletteName = "blue"
		case tcell.ColorFuchsia:
			paletteName = "pink"
		case tcell.ColorAqua:
			paletteName = "teal"
		case tcell.ColorWhite:
			paletteName = "text"
		}
		
		if paletteName != "" {
			themeColor := theme.ResolveColorName(paletteName)
			if themeColor != tcell.ColorDefault {
				color = themeColor
			}
		}
	}

	r, g, b := color.RGB()
	return protocol.ColorModelRGB, (uint32(r)&0xff)<<16 | (uint32(g)&0xff)<<8 | (uint32(b) & 0xff)
}

func rowsEqual(a, b []texel.Cell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Ch != b[i].Ch || a[i].Style != b[i].Style {
			return false
		}
	}
	return true
}
