// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/desktop_publisher.go
// Summary: Implements desktop publisher capabilities for the server runtime.
// Usage: Used by texel-server to coordinate desktop publisher when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"encoding/binary"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/texel"
)

// DesktopPublisher captures desktop pane buffers and enqueues them as buffer
// deltas on the associated session.
type DesktopPublisher struct {
	desktop   *texel.DesktopEngine
	session   *Session
	revisions map[[16]byte]uint32
	digests   map[[16]byte]uint64
	observer  PublishObserver
	mu        sync.Mutex
	notify    func()

	paneListeners []PanePublishListener
	dirtyAll      bool
	dirtyPanes    map[[16]byte]struct{}
}

// PublishObserver records desktop publish metrics for instrumentation.
type PublishObserver interface {
	ObservePublish(session *Session, paneCount int, duration time.Duration)
}

func NewDesktopPublisher(desktop *texel.DesktopEngine, session *Session) *DesktopPublisher {
	return &DesktopPublisher{
		desktop:   desktop,
		session:   session,
		revisions: make(map[[16]byte]uint32),
		digests:   make(map[[16]byte]uint64),
		dirtyPanes: make(map[[16]byte]struct{}),
	}
}

// SetObserver registers an optional metrics observer invoked after each publish.
func (p *DesktopPublisher) SetObserver(observer PublishObserver) {
	p.observer = observer
}

// SetNotifier registers a callback invoked after diffs are enqueued.
func (p *DesktopPublisher) SetNotifier(fn func()) {
	p.notify = fn
}

func (p *DesktopPublisher) Publish() error {
	if p.desktop == nil || p.session == nil {
		return nil
	}
	dirty, dirtyAll := p.consumeDirty()
	var filter func([16]byte) bool
	if !dirtyAll && len(dirty) > 0 {
		filter = func(id [16]byte) bool {
			_, ok := dirty[id]
			return ok
		}
	}

	start := time.Now()
	snapshots := p.desktop.SnapshotBuffersFiltered(filter)
	published := make([][16]byte, 0)
	for _, snap := range snapshots {
		currentDigest := hashPaneBuffer(snap.Buffer)
		p.mu.Lock()
		prev, ok := p.digests[snap.ID]
		if ok && prev == currentDigest {
			p.mu.Unlock()
			continue
		}
		rev := p.revisions[snap.ID] + 1
		p.revisions[snap.ID] = rev
		p.digests[snap.ID] = currentDigest
		p.mu.Unlock()
		delta := bufferToDelta(snap, rev)
		if err := p.session.EnqueueDiff(delta); err != nil {
			return err
		}
		published = append(published, snap.ID)
	}
	if p.observer != nil {
		p.observer.ObservePublish(p.session, len(snapshots), time.Since(start))
	}
	if p.notify != nil {
		p.notify()
	}
	for _, id := range published {
		p.notifyPanePublished(id)
	}
	return nil
}

func (p *DesktopPublisher) addPaneListener(listener PanePublishListener) {
	p.mu.Lock()
	p.paneListeners = append(p.paneListeners, listener)
	p.mu.Unlock()
}

func (p *DesktopPublisher) removePaneListener(listener PanePublishListener) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := make([]PanePublishListener, 0, len(p.paneListeners))
	for _, l := range p.paneListeners {
		if l == listener {
			continue
		}
		n = append(n, l)
	}
	p.paneListeners = n
}

func (p *DesktopPublisher) notifyPanePublished(id [16]byte) {
	listeners := make([]PanePublishListener, 0)
	p.mu.Lock()
	listeners = append(listeners, p.paneListeners...)
	p.mu.Unlock()
	for _, listener := range listeners {
		listener.OnPanePublished(id)
	}
}

func (p *DesktopPublisher) MarkPaneDirty(id [16]byte) {
	if isZeroPaneID(id) {
		return
	}
	p.mu.Lock()
	p.dirtyPanes[id] = struct{}{}
	p.mu.Unlock()
}

func (p *DesktopPublisher) MarkAllDirty() {
	p.mu.Lock()
	p.dirtyAll = true
	p.dirtyPanes = make(map[[16]byte]struct{})
	p.mu.Unlock()
}

func (p *DesktopPublisher) consumeDirty() (map[[16]byte]struct{}, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dirtyAll || len(p.dirtyPanes) == 0 {
		p.dirtyAll = false
		p.dirtyPanes = make(map[[16]byte]struct{})
		return nil, true
	}
	out := make(map[[16]byte]struct{}, len(p.dirtyPanes))
	for id := range p.dirtyPanes {
		out[id] = struct{}{}
	}
	p.dirtyPanes = make(map[[16]byte]struct{})
	return out, false
}

func bufferToDelta(snap texel.PaneSnapshot, revision uint32) protocol.BufferDelta {
	styleMap := make(map[styleKey]uint16)
	styles := make([]protocol.StyleEntry, 0)

	rows := make([]protocol.RowDelta, 0, len(snap.Buffer))
	for y, row := range snap.Buffer {
		if len(row) == 0 {
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
	r, g, b := color.RGB()
	return protocol.ColorModelRGB, (uint32(r)&0xff)<<16 | (uint32(g)&0xff)<<8 | (uint32(b) & 0xff)
}

func hashPaneBuffer(buffer [][]texel.Cell) uint64 {
	hasher := fnv.New64a()
	var scratch [8]byte

	writeUint32 := func(v uint32) {
		binary.LittleEndian.PutUint32(scratch[:4], v)
		hasher.Write(scratch[:4])
	}
	writeUint16 := func(v uint16) {
		binary.LittleEndian.PutUint16(scratch[:2], v)
		hasher.Write(scratch[:2])
	}

	writeUint32(uint32(len(buffer)))
	for _, row := range buffer {
		writeUint32(uint32(len(row)))
		for _, cell := range row {
			writeUint32(uint32(cell.Ch))
			key, _ := convertStyle(cell.Style)
			writeUint16(key.attrFlags)
			writeUint32(uint32(key.fgModel))
			writeUint32(key.fgValue)
			writeUint32(uint32(key.bgModel))
			writeUint32(key.bgValue)
		}
	}
	return hasher.Sum64()
}
