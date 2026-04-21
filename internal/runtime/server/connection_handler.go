// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_handler.go
// Summary: Implements message dispatch and individual message handlers for connections.
// Usage: Used by texel-server to handle incoming protocol messages from clients.
// Notes: Split from connection.go for clarity; methods remain on *connection.

package server

import (
	"fmt"
	"log"

	"github.com/framegrace/texelation/protocol"
)

func (c *connection) handleMessage(prefix string, header protocol.Header, payload []byte) error {
	switch header.Type {
	case protocol.MsgBufferAck:
		ack, err := protocol.DecodeBufferAck(payload)
		if err != nil {
			return err
		}
		c.session.Ack(ack.Sequence)
		if ack.Sequence > c.lastAcked {
			c.lastAcked = ack.Sequence
		}
	case protocol.MsgPing:
		ping, err := protocol.DecodePing(payload)
		if err != nil {
			return err
		}
		pongPayload, err := protocol.EncodePong(protocol.Pong{Timestamp: ping.Timestamp})
		if err != nil {
			return err
		}
		pongHeader := protocol.Header{
			Version:   protocol.Version,
			Type:      protocol.MsgPong,
			Flags:     protocol.FlagChecksum,
			SessionID: c.session.ID(),
			Sequence:  c.lastSent,
		}
		if err := c.writeMessage(pongHeader, pongPayload); err != nil {
			return err
		}
	case protocol.MsgKeyEvent:
		keyEvent, err := protocol.DecodeKeyEvent(payload)
		if err != nil {
			return err
		}
		c.sink.HandleKeyEvent(c.session, keyEvent)
	case protocol.MsgMouseEvent:
		mouseEvent, err := protocol.DecodeMouseEvent(payload)
		if err != nil {
			return err
		}
		c.sink.HandleMouseEvent(c.session, mouseEvent)
		if popper, ok := c.sink.(interface{ PopPendingClipboard() (string, []byte, bool) }); ok {
			if mime, data, ok := popper.PopPendingClipboard(); ok && len(data) > 0 {
				log.Printf("CLIPBOARD DEBUG: Sending clipboard to client: mime=%s, len=%d", mime, len(data))
				encoded, err := protocol.EncodeClipboardSet(protocol.ClipboardSet{MimeType: mime, Data: data})
				if err != nil {
					return err
				}
				if err := c.writeControlMessage(protocol.MsgClipboardSet, encoded); err != nil {
					return err
				}
			} else if ok {
				log.Printf("CLIPBOARD DEBUG: PopPendingClipboard returned ok=true but empty data")
			}
		}
	case protocol.MsgResize:
		size, err := protocol.DecodeResize(payload)
		if err != nil {
			return err
		}
		c.handleResize(size)
	case protocol.MsgClipboardSet:
		clipSet, err := protocol.DecodeClipboardSet(payload)
		if err != nil {
			return err
		}
		c.sink.HandleClipboardSet(c.session, clipSet)
		if data := c.requestClipboardData(clipSet.MimeType); data != nil {
			encoded, err := protocol.EncodeClipboardData(protocol.ClipboardData{MimeType: clipSet.MimeType, Data: data})
			if err != nil {
				return err
			}
			if err := c.writeControlMessage(protocol.MsgClipboardData, encoded); err != nil {
				return err
			}
		}
	case protocol.MsgClipboardGet:
		clipGet, err := protocol.DecodeClipboardGet(payload)
		if err != nil {
			return err
		}
		data := c.sink.HandleClipboardGet(c.session, clipGet)
		encoded, err := protocol.EncodeClipboardData(protocol.ClipboardData{MimeType: clipGet.MimeType, Data: data})
		if err != nil {
			return err
		}
		if err := c.writeControlMessage(protocol.MsgClipboardData, encoded); err != nil {
			return err
		}
	case protocol.MsgThemeUpdate:
		themeUpdate, err := protocol.DecodeThemeUpdate(payload)
		if err != nil {
			return err
		}
		c.sink.HandleThemeUpdate(c.session, themeUpdate)
		encoded, err := protocol.EncodeThemeAck(protocol.ThemeAck(themeUpdate))
		if err != nil {
			return err
		}
		if err := c.writeControlMessage(protocol.MsgThemeAck, encoded); err != nil {
			return err
		}
	case protocol.MsgPaste:
		paste, err := protocol.DecodePaste(payload)
		if err != nil {
			return err
		}
		c.sink.HandlePaste(c.session, paste)
	case protocol.MsgResumeRequest:
		request, err := protocol.DecodeResumeRequest(payload)
		if err != nil {
			return err
		}
		c.lastAcked = request.LastSequence
		c.awaitResume = false
		if c.attachListeners != nil {
			c.attachListeners()
		}
		if provider, ok := c.sink.(SnapshotProvider); ok {
			snapshot, err := provider.Snapshot()
			if err != nil {
				log.Printf("server: resume snapshot error: %v", err)
			} else {
				if payload, err := protocol.EncodeTreeSnapshot(snapshot); err != nil {
					log.Printf("server: encode snapshot error: %v", err)
				} else {
					header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: c.session.ID()}
					if err := c.writeMessage(header, payload); err != nil {
						return err
					}
				}
			}
			if sink, ok := c.sink.(*DesktopSink); ok {
				if pub := sink.Publisher(); pub != nil {
					pub.ResetDiffState()
				}
				sink.Publish()
			}
		}
		c.nudge()
	case protocol.MsgClientReady:
		ready, err := protocol.DecodeClientReady(payload)
		if err != nil {
			return err
		}
		c.handleClientReady(ready)
	case protocol.MsgViewportUpdate:
		u, err := protocol.DecodeViewportUpdate(payload)
		if err != nil {
			return fmt.Errorf("decode viewport update: %w", err)
		}
		c.session.ApplyViewportUpdate(u)
	case protocol.MsgFetchRange:
		if err := c.handleFetchRange(payload); err != nil {
			return fmt.Errorf("fetch range: %w", err)
		}
	default:
		debugLog.Printf("%s ignoring message type %d", prefix, header.Type)
	}
	return nil
}

func (c *connection) handleClientReady(ready protocol.ClientReady) {
	if c.initialSnapshotSent {
		return // Already sent initial snapshot
	}

	sink, ok := c.sink.(*DesktopSink)
	if !ok {
		return
	}
	desktop := sink.Desktop()
	if desktop == nil {
		return
	}

	// Set viewport size with client's actual dimensions
	desktop.SetViewportSize(int(ready.Cols), int(ready.Rows))

	// Now send the snapshot with correct dimensions
	snapshot, err := sink.Snapshot()
	if err != nil {
		sink.Publish()
		c.initialSnapshotSent = true
		return
	}

	sink.Publish()

	payload, err := protocol.EncodeTreeSnapshot(snapshot)
	if err != nil {
		c.initialSnapshotSent = true
		return
	}

	header := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgTreeSnapshot,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	if err := c.writeMessage(header, payload); err != nil {
		c.initialSnapshotSent = true
		return
	}

	// Reset publisher diff state so the next publish sends full frames.
	// The TreeSnapshot overwrites client rows with unstyled text, so the
	// follow-up publish must include all rows with correct styles.
	if pub := sink.Publisher(); pub != nil {
		pub.ResetDiffState()
	}
	sink.Publish()

	states := snapshotMergedPaneStates(snapshot, desktop)
	for _, state := range states {
		c.sendPaneState(state.ID, state.Active, state.Resizing, state.ZOrder, state.HandlesMouse)
	}

	c.initialSnapshotSent = true
	id := c.session.ID()
	debugLog.Printf("connection %x: sent initial snapshot after ClientReady (%dx%d)",
		id[:4], ready.Cols, ready.Rows)
}

func (c *connection) handleResize(size protocol.Resize) {
	sink, ok := c.sink.(*DesktopSink)
	if !ok {
		return
	}
	desktop := sink.Desktop()
	if desktop == nil {
		return
	}

	desktop.SetViewportSize(int(size.Cols), int(size.Rows))

	// For backward compatibility: if old client sends Resize without ClientReady,
	// treat this as the initial ready signal and send snapshot.
	if !c.initialSnapshotSent {
		c.initialSnapshotSent = true
		id := c.session.ID()
		debugLog.Printf("connection %x: sent initial snapshot on first Resize (backward compat)",
			id[:4])
	}

	// Build a geometry-only tree snapshot (pane positions + tree structure,
	// no buffer rendering).  This is cheap and avoids the wasteful full
	// render that sink.Snapshot() would trigger.
	snapshot, err := sink.GeometrySnapshot()
	if err != nil {
		sink.Publish()
		return
	}
	if len(snapshot.Panes) == 0 {
		sink.Publish()
		return
	}

	// Send geometry snapshot FIRST so the client updates pane positions
	// before content arrives.
	payload, err := protocol.EncodeTreeSnapshot(snapshot)
	if err != nil {
		sink.Publish()
		return
	}

	header := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgTreeSnapshot,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	if err := c.writeMessage(header, payload); err != nil {
		sink.Publish()
		return
	}

	states := snapshotMergedPaneStates(snapshot, desktop)
	for _, state := range states {
		c.sendPaneState(state.ID, state.Active, state.Resizing, state.ZOrder, state.HandlesMouse)
	}

	// Reset diff state so the publish sends full buffers instead of diffs
	// against stale pre-resize content.
	if pub := sink.Publisher(); pub != nil {
		pub.ResetDiffState()
	}

	// Now publish and flush buffer deltas. The client already has correct
	// pane positions, so the new content renders at the right location.
	sink.Publish()
	c.sendPending()
}

func (c *connection) requestClipboardData(mime string) []byte {
	if sink, ok := c.sink.(*DesktopSink); ok {
		if desktop := sink.Desktop(); desktop != nil {
			return desktop.HandleClipboardGet(mime)
		}
	}
	return nil
}

func (c *connection) handleFetchRange(payload []byte) error {
	req, err := protocol.DecodeFetchRange(payload)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	// sendStub sends a minimal response with the given flags and no rows.
	sendStub := func(flags protocol.FetchRangeFlags) error {
		stub := protocol.FetchRangeResponse{
			RequestID: req.RequestID,
			PaneID:    req.PaneID,
			Flags:     flags,
		}
		enc, encErr := protocol.EncodeFetchRangeResponse(stub)
		if encErr != nil {
			return fmt.Errorf("encode stub: %w", encErr)
		}
		hdr := protocol.Header{
			Version:   protocol.Version,
			Type:      protocol.MsgFetchRangeResponse,
			Flags:     protocol.FlagChecksum,
			SessionID: c.session.ID(),
		}
		return c.writeMessage(hdr, enc)
	}

	sink, ok := c.sink.(*DesktopSink)
	if !ok {
		return sendStub(protocol.FetchRangeEmpty)
	}
	desktop := sink.Desktop()
	if desktop == nil {
		return sendStub(protocol.FetchRangeEmpty)
	}

	app := desktop.AppByID(req.PaneID)
	if app == nil {
		return sendStub(protocol.FetchRangeEmpty)
	}

	provider, ok := app.(fetchRangeProvider)
	if !ok {
		return sendStub(protocol.FetchRangeEmpty)
	}

	if provider.InAltScreen() {
		return sendStub(protocol.FetchRangeAltScreenActive)
	}

	st := provider.SparseStore()
	if st == nil {
		return sendStub(protocol.FetchRangeEmpty)
	}

	pub := sink.Publisher()
	var revision uint32
	if pub != nil {
		revision = pub.RevisionFor(req.PaneID)
	}

	resp, err := ServeFetchRange(st, req, revision)
	if err != nil {
		log.Printf("server: ServeFetchRange pane %x: %v", req.PaneID[:4], err)
		return sendStub(protocol.FetchRangeEmpty)
	}

	enc, err := protocol.EncodeFetchRangeResponse(resp)
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}

	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgFetchRangeResponse,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, enc)
}
