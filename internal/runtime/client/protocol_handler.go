// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/protocol_handler.go
// Summary: Protocol message decoding and handling for client runtime.
// Usage: Reads messages from server connection and updates client state.

package clientruntime

import (
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/framegrace/texelation/internal/debuglog"
	"github.com/framegrace/texelation/internal/effects"
	"github.com/framegrace/texelation/protocol"
)

func readLoop(conn net.Conn, state *clientState, sessionID [16]byte, lastSequence *atomic.Uint64, renderCh chan<- struct{}, doneCh chan<- struct{}, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal chan<- struct{}) {
	for {
		hdr, payload, err := protocol.ReadMessage(conn)
		if err != nil {
			if !isNetworkClosed(err) {
				log.Printf("read failed: %v", err)
			}
			close(doneCh)
			return
		}
		if handleControlMessage(state, conn, hdr, payload, sessionID, lastSequence, writeMu, pendingAck, ackSignal) {
			select {
			case renderCh <- struct{}{}:
			default:
			}
		}
	}
}

func handleControlMessage(state *clientState, conn net.Conn, hdr protocol.Header, payload []byte, sessionID [16]byte, lastSequence *atomic.Uint64, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal chan<- struct{}) bool {
	cache := state.cache
	switch hdr.Type {
	case protocol.MsgTreeSnapshot:
		snap, err := protocol.DecodeTreeSnapshot(payload)
		if err != nil {
			log.Printf("decode snapshot failed: %v", err)
			return false
		}
		// Always apply snapshots - empty snapshots clear the cache (e.g., when switching to empty workspace)
		cache.ApplySnapshot(snap)
		state.fullRenderNeeded = true
		if state.effects != nil {
			state.effects.ResetPaneStates(cache.SortedPanes())
		}
		// Prune pane caches that are no longer in the snapshot.
		livePanes := make(map[[16]byte]struct{}, len(snap.Panes))
		for _, p := range snap.Panes {
			livePanes[p.PaneID] = struct{}{}
		}
		state.paneCachesMu.Lock()
		for id := range state.paneCaches {
			if _, live := livePanes[id]; !live {
				delete(state.paneCaches, id)
			}
		}
		state.paneCachesMu.Unlock()
		// Initialise per-pane viewport trackers from snapshot geometry.
		if state.viewports != nil {
			state.onTreeSnapshot(snap)
		}
		return true
	case protocol.MsgBufferDelta:
		delta, err := protocol.DecodeBufferDelta(payload)
		if err != nil {
			log.Printf("decode delta failed: %v", err)
			return false
		}
		cache.ApplyDelta(delta)
		state.paneCacheFor(delta.PaneID).ApplyDelta(delta)
		// Update viewport tracker: alt-screen transitions + AutoFollow advance.
		if state.viewports != nil {
			state.onBufferDelta(delta)
		}
		scheduleAck(pendingAck, ackSignal, hdr.Sequence)
		if lastSequence != nil {
			// Atomic-safe check-then-set. Plan D's only writer is this
			// loop, so any race against persistSnapshot's Load() is a
			// benign read of "either the old or new value", both of which
			// are valid sequences to persist. If a future change adds a
			// second writer, switch to CompareAndSwap.
			cur := lastSequence.Load()
			if hdr.Sequence > cur {
				lastSequence.Store(hdr.Sequence)
			}
		}
		return true
	case protocol.MsgFetchRangeResponse:
		resp, err := protocol.DecodeFetchRangeResponse(payload)
		if err != nil {
			log.Printf("decode fetch range response failed: %v", err)
			return false
		}
		// FetchRangeResponse is a targeted reply, not a broadcast delta.
		// It does not participate in the seq/ack stream.
		state.paneCacheFor(resp.PaneID).ApplyFetchRange(resp)
		// Mark the BufferCache pane dirty — incrementalComposite skips panes
		// with Dirty=false, so without this the newly-fetched rows would sit
		// in PaneCache unrendered until unrelated content marked the pane
		// dirty.
		state.cache.MarkPaneDirty(resp.PaneID)
		// Clear inflight flag and emit pending fetch if one was stashed.
		if state.viewports != nil {
			if lo, hi, send := state.onFetchRangeResponse(resp.PaneID); send {
				if !sendFetchRange(state, conn, writeMu, sessionID, resp.PaneID, lo, hi) {
					// Write failed after we drained pendingFetch — restore
					// the window so flushFrame retries instead of silently
					// losing the request.
					state.restorePendingFetch(resp.PaneID, lo, hi)
				}
			}
		}
		return true
	case protocol.MsgPing:
		pong, _ := protocol.EncodePong(protocol.Pong{Timestamp: time.Now().UnixNano()})
		if err := writeMessage(writeMu, conn, protocol.Header{
			Version:   protocol.Version,
			Type:      protocol.MsgPong,
			Flags:     protocol.FlagChecksum,
			SessionID: sessionID,
		}, pong); err != nil {
			log.Printf("send pong failed: %v", err)
		}
		return false
	case protocol.MsgClipboardSet:
		clip, err := protocol.DecodeClipboardSet(payload)
		if err != nil {
			log.Printf("decode clipboard failed: %v", err)
			return false
		}
		debuglog.Printf("CLIPBOARD DEBUG: Client received MsgClipboardSet: mime=%s, len=%d", clip.MimeType, len(clip.Data))
		state.setClipboard(protocol.ClipboardData{MimeType: clip.MimeType, Data: clip.Data})
		return true
	case protocol.MsgClipboardData:
		clip, err := protocol.DecodeClipboardData(payload)
		if err != nil {
			log.Printf("decode clipboard data failed: %v", err)
			return false
		}
		state.setClipboard(clip)
		return true
	case protocol.MsgThemeUpdate:
		themeUpdate, err := protocol.DecodeThemeUpdate(payload)
		if err != nil {
			log.Printf("decode theme update failed: %v", err)
			return false
		}
		state.theme = protocol.ThemeAck(themeUpdate)
		state.hasTheme = true
		state.updateTheme(themeUpdate.Section, themeUpdate.Key, themeUpdate.Value)
		state.applyEffectConfig()
		return true
	case protocol.MsgThemeAck:
		ack, err := protocol.DecodeThemeAck(payload)
		if err != nil {
			log.Printf("decode theme ack failed: %v", err)
			return false
		}
		state.theme = ack
		state.hasTheme = true
		state.updateTheme(ack.Section, ack.Key, ack.Value)
		state.applyEffectConfig()
		return true
	case protocol.MsgPaneFocus:
		focus, err := protocol.DecodePaneFocus(payload)
		if err != nil {
			log.Printf("decode pane focus failed: %v", err)
			return false
		}
		state.focus = focus
		state.hasFocus = true
		return true
	case protocol.MsgPaneState:
		paneFlags, err := protocol.DecodePaneState(payload)
		if err != nil {
			log.Printf("decode pane state failed: %v", err)
			return false
		}
		active := paneFlags.Flags&protocol.PaneStateActive != 0
		resizing := paneFlags.Flags&protocol.PaneStateResizing != 0
		handlesSelection := paneFlags.Flags&protocol.PaneStateSelectionDelegated != 0
		state.cache.SetPaneFlags(paneFlags.PaneID, active, resizing, paneFlags.ZOrder, handlesSelection)
		if state.effects != nil {
			// Use past timestamp during initial connect so effects snap
			// instantly rather than visibly animating.
			ts := state.effects.PaneStateTriggerTimestamp()
			state.effects.HandleTrigger(effects.EffectTrigger{Type: effects.TriggerPaneActive, PaneID: paneFlags.PaneID, Active: active, Timestamp: ts})
			state.effects.HandleTrigger(effects.EffectTrigger{Type: effects.TriggerPaneResizing, PaneID: paneFlags.PaneID, Resizing: resizing, Timestamp: ts})
		}
		return true
	case protocol.MsgStateUpdate:
		update, err := protocol.DecodeStateUpdate(payload)
		if err != nil {
			log.Printf("decode state update failed: %v", err)
			return false
		}
		debuglog.Printf("state update: control=%v sub=%q zoom=%v", update.InControlMode, update.SubMode, update.Zoomed)
		state.applyStateUpdate(update)
		return true
	case protocol.MsgImageUpload:
		upload, err := protocol.DecodeImageUpload(payload)
		if err != nil {
			log.Printf("decode image upload failed: %v", err)
			return false
		}
		cache.ImageCache().Upload(upload.PaneID, upload.SurfaceID,
			int(upload.Width), int(upload.Height), upload.Data)
		return true
	case protocol.MsgImagePlace:
		place, err := protocol.DecodeImagePlace(payload)
		if err != nil {
			log.Printf("decode image place failed: %v", err)
			return false
		}
		cache.ImageCache().Place(place.PaneID, place.SurfaceID,
			int(place.X), int(place.Y), int(place.W), int(place.H), int(place.ZIndex))
		return true
	case protocol.MsgImageDelete:
		del, err := protocol.DecodeImageDelete(payload)
		if err != nil {
			log.Printf("decode image delete failed: %v", err)
			return false
		}
		cache.ImageCache().Delete(del.PaneID, del.SurfaceID)
		if state.kitty != nil {
			state.kitty.deleteImage(del.SurfaceID)
		}
		return true
	case protocol.MsgImageReset:
		reset, err := protocol.DecodeImageReset(payload)
		if err != nil {
			log.Printf("decode image reset failed: %v", err)
			return false
		}
		cache.ImageCache().ResetPlacements(reset.PaneID)
		return true
	}
	return false
}
