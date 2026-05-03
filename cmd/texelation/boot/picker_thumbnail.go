// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker_thumbnail.go
// Summary: Lazy thumbnail fetch + widgets.Image instantiation for the
// picker. Rendering is delegated to texelui/widgets.Image which
// handles Kitty + half-block + alt-text fallback internally.

package boot

import (
	"bytes"
	"image/png"
	"log"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
	core "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// maxThumbnailDim caps PNG dimensions on the decode path. A 480×270
// PNG is well within this; anything larger is either corrupt or
// hostile (the 16 MiB protocol cap leaves room for adversarial
// dimension declarations within a small payload). Decoding without
// this check can OOM the picker on corrupted inputs.
const maxThumbnailDim = 4096

// ThumbCached returns true iff a PNG for id is in the local cache.
// Locked accessor for tests; production read sites also take p.mu.
func (p *Picker) ThumbCached(id [16]byte) bool {
	p.mu.Lock()
	_, ok := p.thumbCache[id]
	p.mu.Unlock()
	return ok
}

// IsPending reports whether a fetch is in flight for id. Locked
// accessor for tests.
func (p *Picker) IsPending(id [16]byte) bool {
	p.mu.Lock()
	v := p.pending[id]
	p.mu.Unlock()
	return v
}

// markFetchFailed bumps the failure counter for id. Called from the
// goroutine's error branches and the panic recover so a deterministic
// failure exhausts attempts after maxFetchAttempts.
func (p *Picker) markFetchFailed(id [16]byte) {
	p.mu.Lock()
	p.failedFetches[id]++
	p.mu.Unlock()
}

// imageWidgetFor returns the widgets.Image bound to id's cached PNG,
// constructing one on first use. nil if no PNG cached. The widget is
// keyed not just by id but also by the byte length of the cached PNG —
// for live sessions the bytes change between renders, so a stale
// widget would keep showing the old surface; we discard the old
// widget when the bytes change.
func (p *Picker) imageWidgetFor(id [16]byte) *widgets.Image {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, ok := p.thumbCache[id]
	if !ok {
		return nil
	}
	if w, exists := p.imgCache[id]; exists {
		// For live sessions the bytes may differ between fetches.
		// Cheap signal: byte-length mismatch → rebuild. PNG bytes
		// are not deterministic for the same image (timestamps,
		// compression order) so a length match is "probably same"
		// and a mismatch is "definitely different." Good enough.
		if len(data) > 0 && p.liveIDs[id] {
			// Always rebuild for live IDs so even same-length re-renders
			// land. Cost: re-decode per render frame, negligible at
			// thumbnail dims.
			w = widgets.NewImage(data, "session-thumbnail")
			p.imgCache[id] = w
		}
		return w
	}
	w := widgets.NewImage(data, "session-thumbnail")
	p.imgCache[id] = w
	return w
}

// maybeFetchThumbnail kicks off a non-blocking fetch for id if we
// haven't cached or pending one already, and haven't already failed
// maxFetchAttempts times for that id.
//
// Live sessions bypass the cache check — their state changes
// continuously, so we always re-fetch. The pending guard still
// prevents in-flight duplicates, so each render only spawns one
// goroutine per visible card.
func (p *Picker) maybeFetchThumbnail(id [16]byte, hasThumb bool) {
	if !p.hasGraphics() || !hasThumb {
		return
	}
	p.mu.Lock()
	isLive := p.liveIDs[id]
	if !isLive {
		if _, cached := p.thumbCache[id]; cached {
			p.mu.Unlock()
			return
		}
	}
	if p.pending[id] {
		p.mu.Unlock()
		return
	}
	if p.failedFetches[id] >= maxFetchAttempts {
		p.mu.Unlock()
		return
	}
	p.pending[id] = true
	p.mu.Unlock()

	go func(targetID [16]byte) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("picker: thumbnail fetch panic: %v", rec)
				p.markFetchFailed(targetID)
			}
			p.mu.Lock()
			delete(p.pending, targetID)
			p.mu.Unlock()
		}()
		data, err := p.client.FetchThumbnail(targetID)
		if err != nil {
			// All four server-side OK=false branches (path empty,
			// file missing, oversize, IO error) collapse into err
			// here. Any of those is permanent — the file will not
			// magically appear next render — so we count toward the
			// limiter. A purely transient socket blip would also
			// count, but maxFetchAttempts=3 means three blips in a
			// row to give up, which is acceptable and prevents a
			// render-storm against a hopeless ID.
			log.Printf("picker: thumbnail fetch %x: %v", targetID[:4], err)
			p.markFetchFailed(targetID)
			return
		}
		if len(data) == 0 {
			log.Printf("picker: thumbnail fetch %x: empty response", targetID[:4])
			p.markFetchFailed(targetID)
			return
		}
		// First-pass: header check + dimension cap. Cheap and
		// catches non-PNG / huge-canvas attacks before full decode.
		// Decode-stage errors are bytes-permanent (the same bytes
		// will fail the same way next time), so they count toward
		// failedFetches.
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			log.Printf("picker: thumbnail decode-config %x: %v", targetID[:4], err)
			p.markFetchFailed(targetID)
			return
		}
		if cfg.Width > maxThumbnailDim || cfg.Height > maxThumbnailDim {
			log.Printf("picker: thumbnail %x: refusing %dx%d (cap %d)",
				targetID[:4], cfg.Width, cfg.Height, maxThumbnailDim)
			p.markFetchFailed(targetID)
			return
		}
		// Second-pass: full decode. Catches truncated IDAT, bad
		// CRCs, etc. that DecodeConfig accepts. Required because
		// widgets.Image silently falls back to alt text on decode
		// failure, and the picker has no retry signal.
		if _, err := png.Decode(bytes.NewReader(data)); err != nil {
			log.Printf("picker: thumbnail decode %x: %v", targetID[:4], err)
			p.markFetchFailed(targetID)
			return
		}
		p.mu.Lock()
		p.thumbCache[targetID] = data
		p.mu.Unlock()
		// Wake the picker's run loop so the new thumbnail renders
		// without waiting for the user to press a key. PostEvent is
		// non-blocking; on a contested screen it returns an error
		// we can safely ignore.
		if p.screen != nil {
			_ = p.screen.PostEvent(tcell.NewEventInterrupt(nil))
		}
	}(id)
}

// drawThumbnail paints the thumbnail rect for a session card. Branches:
//   - Graphics-capable + cached PNG  → widgets.Image (Kitty or half-block)
//   - Otherwise                       → ASCII tree from TreeNodeSnapshot
//
// We never half-block-render the structural layout because at thumbnail
// resolution it produces noise; the box-drawing characters convey
// structure cleanly even at 22×8.
func (p *Picker) drawThumbnail(painter *core.Painter, rect core.Rect, s protocol.SessionSummary, bgStyle tcell.Style) {
	if p.hasGraphics() {
		if w := p.imageWidgetFor(s.SessionID); w != nil {
			w.SetPosition(rect.X, rect.Y)
			w.Resize(rect.W, rect.H)
			w.Draw(painter)
			p.maybeFetchThumbnail(s.SessionID, s.HasThumbnail)
			return
		}
	}
	p.drawASCIILayoutAt(painter, rect, s.Layout, bgStyle)
	p.maybeFetchThumbnail(s.SessionID, s.HasThumbnail)
}
