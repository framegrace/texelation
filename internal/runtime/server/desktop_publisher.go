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
	"github.com/framegrace/texelui/color"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
)

// DesktopPublisher captures desktop pane buffers and enqueues them as buffer
// deltas on the associated session.
//
// Per-pane revision counters live on the *Session* (not on the publisher) so
// they survive across publisher lifetimes. A new client connection on an
// existing session creates a new publisher, but the session's pending diff
// queue still carries diffs the previous publisher emitted at high revision
// numbers; if the new publisher's revisions started over at 1 the client's
// BufferCache would reject every fresh delta as stale (delta.Revision <
// pane.Revision) until enough writes pushed past the prior cap.
type DesktopPublisher struct {
	desktop      *texel.DesktopEngine
	session      *Session
	prevBuffers  map[[16]byte][][]texel.Cell
	lastViewport map[[16]byte]ClientViewport
	observer     PublishObserver
	mu           sync.RWMutex
	notify       func()
}

// PublishObserver records desktop publish metrics for instrumentation.
type PublishObserver interface {
	ObservePublish(session *Session, paneCount int, duration time.Duration)
}

func NewDesktopPublisher(desktop *texel.DesktopEngine, session *Session) *DesktopPublisher {
	pub := &DesktopPublisher{
		desktop:      desktop,
		session:      session,
		prevBuffers:  make(map[[16]byte][][]texel.Cell),
		lastViewport: make(map[[16]byte]ClientViewport),
	}

	// Set up graphics provider factory so panes can send image messages
	desktop.SetGraphicsProviderFactory(func(paneID [16]byte) texelcore.GraphicsProvider {
		return NewRemoteGraphicsProvider(paneID, func(msgType uint8, payload []byte) {
			_ = session.EnqueueImage(msgType, payload)
		})
	})

	return pub
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
	p.lastViewport = make(map[[16]byte]ClientViewport)
	p.mu.Unlock()
}

// SetNotifier registers a callback invoked after diffs are enqueued.
func (p *DesktopPublisher) SetNotifier(fn func()) {
	p.notify = fn
}

// RevisionFor returns the latest revision stamped for paneID. Returns 0 if
// the pane has not been published yet under this publisher's session.
func (p *DesktopPublisher) RevisionFor(paneID [16]byte) uint32 {
	if p.session == nil {
		return 0
	}
	return p.session.RevisionFor(paneID)
}

// Publish reads SnapshotBuffers from the desktop engine, then delegates
// to publishSnapshotsLocked for the per-pane encode + enqueue loop. The
// split lets tests drive the encode path with synthetic snapshots without
// spinning up a live desktop engine.
func (p *DesktopPublisher) Publish() error {
	if p.desktop == nil || p.session == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	start := time.Now()
	buffers := p.desktop.SnapshotBuffers()
	// if len(buffers) > 0 { log.Printf("DesktopPublisher: Publishing %d buffers", len(buffers)) }

	if err := p.publishSnapshotsLocked(buffers); err != nil {
		return err
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

// publishSnapshotsLocked runs the per-pane encode + enqueue loop for the
// given snapshots. The caller must hold p.mu.
func (p *DesktopPublisher) publishSnapshotsLocked(buffers []texel.PaneSnapshot) error {
	for _, snap := range buffers {
		vp, haveVP := p.session.Viewport(snap.ID)
		// Main-screen panes require a viewport before we clip rows: the
		// client will send one right after handshake. Alt-screen (or
		// non-terminal / placeholder) panes don't need clipping and are
		// always emitted.
		if !snap.AltScreen && !haveVP {
			// Silent-skip is deliberate: the client will send a ViewportUpdate
			// right after handshake, and connection_handler nudges Publish
			// then.  Logging surfaces the transient state for diagnosis.
			debugLog.Printf("publisher: pane %x waiting for viewport", snap.ID[:4])
			continue
		}
		// Narrow prev-buffer invalidation: rows that were OUT of window
		// and are now IN window with unchanged content would be wrongly
		// skipped by rowsEqual, so we must force a re-emit in cases where
		// that can happen. The only cases that require invalidation are:
		//   - first viewport for this pane (no prior state);
		//   - geometry change (Rows or Cols differ);
		//   - AutoFollow mode toggled (window shape semantics change);
		//   - manual scroll (AutoFollow=false and View indices shifted),
		//     where the new window may expose unchanged content.
		// In AutoFollow-to-AutoFollow with unchanged Rows/Cols, View
		// indices advance every frame; we deliberately do NOT invalidate
		// because new bottom rows differ in content and diff naturally
		// via rowsEqual, and no previously-hidden row can become visible
		// without content change.
		if haveVP {
			prevVP, hadPrev := p.lastViewport[snap.ID]
			shouldInvalidate := !hadPrev ||
				prevVP.AutoFollow != vp.AutoFollow ||
				prevVP.Rows != vp.Rows ||
				prevVP.Cols != vp.Cols
			if !shouldInvalidate && !vp.AutoFollow {
				if prevVP.ViewTopIdx != vp.ViewTopIdx || prevVP.ViewBottomIdx != vp.ViewBottomIdx {
					shouldInvalidate = true
				}
			}
			if shouldInvalidate {
				p.prevBuffers[snap.ID] = nil
			}
			p.lastViewport[snap.ID] = vp
		}
		rev := p.session.NextRevision(snap.ID)
		prev := p.prevBuffers[snap.ID]
		delta := bufferToDelta(snap, prev, rev, vp)
		// Allow decoration-only deltas (e.g. focus change repaints just the
		// borders): a delta is meaningful if either content rows or
		// decoration rows changed since the previous frame.
		if len(delta.Rows) == 0 && len(delta.DecorRows) == 0 {
			continue
		}
		// Only clone when there are actual changes — avoids massive GC
		// pressure from cloning every pane buffer every frame.
		p.prevBuffers[snap.ID] = cloneBuffer(snap.Buffer)
		if err := p.session.EnqueueDiff(delta); err != nil {
			return err
		}
	}
	return nil
}

func cloneBuffer(buf [][]texel.Cell) [][]texel.Cell {
	clone := make([][]texel.Cell, len(buf))
	for y, row := range buf {
		clone[y] = make([]texel.Cell, len(row))
		copy(clone[y], row)
	}
	return clone
}

// bufferToDelta encodes a pane snapshot into a BufferDelta, emitting only
// rows that (a) changed since prev and (b) fall inside the client's
// resident window. Alt-screen (or non-terminal) panes bypass clipping:
// Flags sets BufferDeltaAltScreen, RowBase stays 0, and Row is the flat
// buffer index.
//
// Main-screen panes use RowGlobalIdx to key rows by globalIdx and clip to
// [lo, hi] = [ViewTopIdx - overscan, ViewBottomIdx + overscan] where
// overscan = vp.Rows (1× viewport). In AutoFollow we still use
// ViewTopIdx/ViewBottomIdx since the client keeps them in sync with the
// live bottom — no extra bookkeeping needed here.
func bufferToDelta(snap texel.PaneSnapshot, prev [][]texel.Cell, revision uint32, vp ClientViewport) protocol.BufferDelta {
	styleMap := make(map[styleKey]uint16)
	styles := make([]protocol.StyleEntry, 0)

	encodeRow := func(row []texel.Cell) []protocol.CellSpan {
		spans := make([]protocol.CellSpan, 0)
		builders := make([]*strings.Builder, 0)
		for x, cell := range row {
			key, entry := convertCell(cell)
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
		return spans
	}

	rows := make([]protocol.RowDelta, 0, len(snap.Buffer))
	delta := protocol.BufferDelta{
		PaneID:   snap.ID,
		Revision: revision,
		Flags:    protocol.BufferDeltaNone,
	}

	if snap.AltScreen {
		delta.Flags |= protocol.BufferDeltaAltScreen
		for y, row := range snap.Buffer {
			if len(row) == 0 {
				continue
			}
			if y < len(prev) && rowsEqual(row, prev[y]) {
				continue
			}
			rows = append(rows, protocol.RowDelta{Row: uint16(y), Spans: encodeRow(row)})
		}
		delta.Styles = styles
		delta.Rows = rows
		return delta
	}

	overscan := int64(vp.Rows)
	lo := vp.ViewTopIdx - overscan
	hi := vp.ViewBottomIdx + overscan
	delta.RowBase = lo

	if vp.AutoFollow {
		// AutoFollow=true means the client tracks the live edge. The saved
		// ViewBottomIdx in ClientViewport can be stale (e.g., from a
		// pre-resume tracker state). Derive the clip from the pane's actual
		// rendered globalIdx range instead. This also keeps (hi-lo) bounded
		// to roughly 2*Rows+overscan, comfortably within the uint16
		// RowDelta.Row encoding.
		var maxGid int64 = -1
		for y := range snap.Buffer {
			if y >= len(snap.RowGlobalIdx) {
				break
			}
			gid := snap.RowGlobalIdx[y]
			if gid > maxGid {
				maxGid = gid
			}
		}
		if maxGid >= 0 {
			hi = maxGid + overscan
			lo = maxGid - int64(vp.Rows) - overscan + 1
			if lo < 0 {
				lo = 0
			}
			delta.RowBase = lo
		}
		// If maxGid < 0 (all rows are borders/padding/non-terminal), fall
		// through with the ViewTopIdx-derived lo/hi — no rows will pass the
		// `gid >= 0 && gid in [lo,hi]` gate anyway.
	}

	var decorRows []protocol.DecorRowDelta
	for y, row := range snap.Buffer {
		if len(row) == 0 {
			continue
		}
		if y >= len(snap.RowGlobalIdx) {
			continue
		}
		gid := snap.RowGlobalIdx[y]
		// Alt-screen panes have RowGlobalIdx all -1; skip decoration emission
		// before the rowsEqual cost. The existing alt-screen positional path
		// (BufferDeltaAltScreen flag) handles them.
		if snap.AltScreen && gid < 0 {
			continue
		}
		if y < len(prev) && rowsEqual(row, prev[y]) {
			continue
		}
		if gid < 0 {
			// Decoration row (border or app statusbar) — positional.
			decorRows = append(decorRows, protocol.DecorRowDelta{
				RowIdx: uint16(y),
				Spans:  encodeRow(row),
			})
			continue
		}
		if gid < lo || gid > hi {
			continue
		}
		rows = append(rows, protocol.RowDelta{Row: uint16(gid - lo), Spans: encodeRow(row)})
	}
	delta.Styles = styles
	delta.Rows = rows
	delta.DecorRows = decorRows
	return delta
}

type styleKey struct {
	attrFlags uint16
	fgModel   protocol.ColorModel
	fgValue   uint32
	bgModel   protocol.ColorModel
	bgValue   uint32
	dynFGType uint8
	dynBGType uint8
}

func convertCell(cell texel.Cell) (styleKey, protocol.StyleEntry) {
	fg, bg, attrs := cell.Style.Decompose()

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

	if cell.DynFG.IsDynamic() || cell.DynBG.IsDynamic() {
		entry.AttrFlags |= protocol.AttrHasDynamic
		key.attrFlags |= protocol.AttrHasDynamic
		entry.DynFG = convertDynDesc(cell.DynFG)
		entry.DynBG = convertDynDesc(cell.DynBG)
		key.dynFGType = cell.DynFG.Type
		key.dynBGType = cell.DynBG.Type
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

func convertDynDesc(d color.DynamicColorDesc) protocol.DynColorDesc {
	pd := protocol.DynColorDesc{
		Type: d.Type, Base: d.Base, Target: d.Target,
		Easing: d.Easing, Speed: d.Speed, Min: d.Min, Max: d.Max,
	}
	if len(d.Stops) > 0 {
		pd.Stops = make([]protocol.DynColorStopDesc, len(d.Stops))
		for i, s := range d.Stops {
			pd.Stops[i] = protocol.DynColorStopDesc{
				Position: s.Position,
				Color: protocol.DynColorDesc{
					Type: s.Color.Type, Base: s.Color.Base, Target: s.Color.Target,
					Easing: s.Color.Easing, Speed: s.Color.Speed, Min: s.Color.Min, Max: s.Color.Max,
				},
			}
		}
	}
	return pd
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
