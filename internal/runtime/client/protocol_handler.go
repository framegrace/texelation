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

	"texelation/internal/effects"
	"texelation/protocol"
)

func readLoop(conn net.Conn, state *uiState, sessionID [16]byte, lastSequence *uint64, renderCh chan<- struct{}, doneCh chan<- struct{}, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal chan<- struct{}) {
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

func handleControlMessage(state *uiState, conn net.Conn, hdr protocol.Header, payload []byte, sessionID [16]byte, lastSequence *uint64, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal chan<- struct{}) bool {
	cache := state.cache
	switch hdr.Type {
	case protocol.MsgTreeSnapshot:
		snap, err := protocol.DecodeTreeSnapshot(payload)
		if err != nil {
			log.Printf("decode snapshot failed: %v", err)
			return false
		}
		if len(snap.Panes) == 0 {
			if existing := cache.SortedPanes(); len(existing) > 0 {
				log.Printf("ignoring empty snapshot; retaining %d cached panes", len(existing))
				return false
			}
		}
		cache.ApplySnapshot(snap)
		if state.effects != nil {
			state.effects.ResetPaneStates(cache.SortedPanes())
		}
		return true
	case protocol.MsgBufferDelta:
		delta, err := protocol.DecodeBufferDelta(payload)
		if err != nil {
			log.Printf("decode delta failed: %v", err)
			return false
		}
		cache.ApplyDelta(delta)
		scheduleAck(pendingAck, ackSignal, hdr.Sequence)
		if lastSequence != nil && hdr.Sequence > *lastSequence {
			*lastSequence = hdr.Sequence
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
			ts := time.Now()
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
		log.Printf("state update: control=%v sub=%q zoom=%v", update.InControlMode, update.SubMode, update.Zoomed)
		state.applyStateUpdate(update)
		return true
	}
	return false
}
