// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_handler.go
// Summary: Implements message dispatch and individual message handlers for connections.
// Usage: Used by texel-server to handle incoming protocol messages from clients.
// Notes: Split from connection.go for clarity; methods remain on *connection.

package server

import (
	"errors"
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
		if c.resumeProcessed {
			// Duplicate MsgResumeRequest on this connection. Ignore silently
			// to prevent ApplyResume clobbering viewport state, RestoreViewport
			// jitter, and redundant TreeSnapshot spam.
			debugLog.Printf("connection %x: ignoring duplicate MsgResumeRequest", c.session.ID())
			break
		}
		if request.SessionID != c.session.ID() {
			// SessionID mismatch: reject unconditionally. A client that
			// connected freshly (no awaitResume) has no business sending a
			// resume bound to a different session — any such frame is stale
			// or malicious, and silently accepting it would let it clobber
			// this session's viewports.
			debugLog.Printf("connection %x: MsgResumeRequest session mismatch (got %x)", c.session.ID(), request.SessionID)
			return errors.New("server: resume request session mismatch")
		}
		c.resumeProcessed = true
		// Plan D2: a rehydrated session (one reconstructed from disk
		// after a daemon restart) has an empty diff queue and
		// nextSequence == 0, so the client's claimed LastSequence is
		// from a prior daemon's lifetime and is meaningless here.
		// Honoring it would make Session.Pending(after:LastSequence)
		// filter out every fresh delta (all of which start at seq=1)
		// and the client would appear frozen — no scroll updates, no
		// keystroke echoes, no control-mode text would ever reach the
		// client.
		//
		// For an in-process resume (live cache hit), the diff queue
		// retains its accumulated entries; honoring the client's
		// LastSequence is correct so we don't replay already-acked
		// diffs.
		if c.rehydrated {
			c.lastAcked = 0
		} else {
			c.lastAcked = request.LastSequence
		}
		c.awaitResume = false
		if c.attachListeners != nil {
			c.attachListeners()
		}
		// Plan D: hoist the desktop lookup so we can build a paneExists
		// predicate once and use it BEFORE ApplyResume runs (otherwise
		// ClientViewports accumulates phantom entries on cross-restart resumes).
		// When the sink isn't a DesktopSink (test harnesses, fake sinks), we
		// fall through with the unpruned slice — ApplyResume will accept it
		// and downstream lookups handle missing panes gracefully.
		viewportsToApply := request.PaneViewports
		sink, sinkOK := c.sink.(*DesktopSink)
		if sinkOK && sink.Desktop() != nil {
			desktop := sink.Desktop()
			pruned := make([]protocol.PaneViewportState, 0, len(request.PaneViewports))
			for _, ps := range request.PaneViewports {
				if desktop.AppByID(ps.PaneID) != nil {
					pruned = append(pruned, ps)
				}
			}
			if dropped := len(request.PaneViewports) - len(pruned); dropped > 0 {
				debugLog.Printf("connection %x: pruned %d phantom paneID entries from resume payload", c.session.ID(), dropped)
			}
			viewportsToApply = pruned
		}

		// Plan D2: prune phantom pre-seed viewports against the live pane
		// tree so dead PaneIDs from a prior daemon's lifetime don't persist
		// back to disk (Plan B review-findings #4 close-out).
		if sinkOK && sink.Desktop() != nil {
			desktop := sink.Desktop()
			if pruned := c.session.viewports.PrunePhantoms(func(p [16]byte) bool {
				return desktop.AppByID(p) != nil
			}); pruned > 0 {
				debugLog.Printf("connection %x: pruned %d phantom pre-seed viewport(s)", c.session.ID(), pruned)
			}
		}

		// Seed ClientViewports from the resume payload FIRST: this is a
		// pure data copy and cannot fail, so the publisher has a valid
		// clip window even if a per-pane RestoreViewport call below
		// panics or no-ops. Without this ordering, a pane-side failure
		// would leave ClientViewports empty and the publisher would
		// silently skip every main-screen pane until the client's next
		// MsgViewportUpdate.
		c.session.ApplyResume(viewportsToApply, nil)

		// Then re-seat each non-alt-screen pane's ViewWindow so the
		// pane's renderer produces rows inside the resumed range on the
		// first post-resume publish. Alt-screen panes keep their own
		// buffer; skipping the restore call avoids a no-op through the
		// alt-screen guard in TexelTerm.RestoreViewport. Wrapped in a
		// deferred recover so a panicking app cannot crash the connection.
		if sinkOK && sink.Desktop() != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("server: RestorePaneViewport panic: %v", r)
					}
				}()
				for _, ps := range viewportsToApply {
					if !ps.AltScreen {
						sink.Desktop().RestorePaneViewport(ps.PaneID, ps.ViewBottomIdx, ps.WrapSegmentIdx, ps.AutoFollow)
					}
				}
			}()
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
					c.recordSnapshotActivity(snapshot)
				}
			}
			if sinkOK {
				// ORDER-SENSITIVE: ResetDiffState MUST come before sink.Publish.
				// During the handler, the publisher goroutine can fire with the
				// new ClientViewport (seeded by ApplyResume above) against old
				// pane content, emitting a stale/empty intermediate delta.
				// ResetDiffState clears prevBuffers + lastViewport so the
				// subsequent Publish treats every pane as "first viewport" and
				// emits a full buffer, repairing any earlier interleave.
				// Do not reorder.
				if pub := sink.Publisher(); pub != nil {
					pub.ResetDiffState()
				}
				sink.Publish()
			}
		}
		// Plan D2: for a rehydrated session, leave initialSnapshotSent
		// false so handleClientReady runs when MsgClientReady arrives.
		// The resume branch above shipped a snapshot but ran with a
		// 0×0 desktop (the new daemon hasn't received the client's
		// viewport size yet), so handleClientReady is needed for
		// SetViewportSize, the geometry-correct re-snapshot, the
		// per-pane sendPaneState loop (active/zorder/handlesMouse),
		// and the statusbar layout pass. Skipping these leaves the
		// client with no pane focus, no borders, no statusbar, and
		// publishes that emit against 0×0 buffers.
		//
		// For a fresh-session or in-process resume, the original
		// behavior stands: we already sent a usable snapshot from a
		// well-dimensioned desktop, so handleClientReady's repeat
		// work is wasteful.
		if !c.rehydrated {
			c.initialSnapshotSent = true
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
		// Publish so main-screen panes that were skipped by the no-viewport
		// gate in publishSnapshotsLocked now render, and so scroll/resize
		// viewport changes re-clip prev-buffer state without waiting for
		// unrelated content to change.
		if sink, ok := c.sink.(*DesktopSink); ok {
			sink.Publish()
		}
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
	c.recordSnapshotActivity(snapshot)

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
	c.recordSnapshotActivity(snapshot)

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

// recordSnapshotActivity updates the session's stored pane-activity
// metadata after a TreeSnapshot is dispatched. Cheap (no I/O on the
// hot path; the writer debounces). Plan F consumes the resulting
// PaneCount / FirstPaneTitle fields.
func (c *connection) recordSnapshotActivity(snap protocol.TreeSnapshot) {
	if c.session == nil {
		return
	}
	count, title := paneActivityFromSnapshot(snap)
	c.session.RecordPaneActivity(count, title)
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
		debugLog.Printf("handleFetchRange pane %x: sink is not *DesktopSink (%T)", req.PaneID[:4], c.sink)
		return sendStub(protocol.FetchRangeEmpty)
	}
	desktop := sink.Desktop()
	if desktop == nil {
		debugLog.Printf("handleFetchRange pane %x: desktop is nil", req.PaneID[:4])
		return sendStub(protocol.FetchRangeEmpty)
	}

	app := desktop.AppByID(req.PaneID)
	if app == nil {
		debugLog.Printf("handleFetchRange pane %x: no app found for pane", req.PaneID[:4])
		return sendStub(protocol.FetchRangeEmpty)
	}

	provider, ok := app.(fetchRangeProvider)
	if !ok {
		debugLog.Printf("handleFetchRange pane %x: app %T does not implement fetchRangeProvider", req.PaneID[:4], app)
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
		// Real server-side fault (programmer bug or store corruption).
		// Don't mask as FetchRangeEmpty — the client would hot-loop
		// re-requesting. Drop the connection; resume will recover.
		return fmt.Errorf("ServeFetchRange pane %x: %w", req.PaneID[:4], err)
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
