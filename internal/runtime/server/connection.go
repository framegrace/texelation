// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection.go
// Summary: Implements connection capabilities for the server runtime.
// Usage: Used by texel-server to coordinate connection when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"texelation/protocol"
	"texelation/texel"
)

type connection struct {
	conn                net.Conn
	session             *Session
	lastSent            uint64
	lastAcked           uint64
	sink                EventSink
	writeMu             sync.Mutex
	unregisterFocus     func()
	unregisterState     func()
	unregisterPaneState func()
	awaitResume         bool
	attachListeners     func()
	incoming            chan protocolMessage
	readErr             chan error
	pending             chan struct{}
	stop                chan struct{}
}

type protocolMessage struct {
	header  protocol.Header
	payload []byte
}

func newConnection(conn net.Conn, session *Session, sink EventSink, awaitResume bool) *connection {
	if sink == nil {
		sink = nopSink{}
	}
	c := &connection{conn: conn, session: session, sink: sink, awaitResume: awaitResume}
	c.incoming = make(chan protocolMessage, 32)
	c.readErr = make(chan error, 1)
	c.pending = make(chan struct{}, 1)
	c.stop = make(chan struct{})
	id := session.ID()
	if awaitResume {
		debugLog.Printf("server: connection %x awaiting resume request", id[:4])
	}
	if ds, ok := sink.(*DesktopSink); ok {
		if desktop := ds.Desktop(); desktop != nil {
			attach := func() {
				desktop.RegisterFocusListener(c)
				c.unregisterFocus = func() { desktop.UnregisterFocusListener(c) }
				desktop.Subscribe(c)
				c.unregisterState = func() { desktop.Unsubscribe(c) }
				desktop.RegisterPaneStateListener(c)
				c.unregisterPaneState = func() { desktop.UnregisterPaneStateListener(c) }
				c.sendStateUpdate(desktop.CurrentStatePayload())
				c.sendPaneStateSnapshots(desktop.PaneStates())
			}
			if awaitResume {
				c.attachListeners = func() {
					attach()
					c.attachListeners = nil
				}
			} else {
				attach()
			}
		}
	}

	go c.readMessages()
	c.nudge()
	return c
}

func (c *connection) serve() (retErr error) {
	connID := c.session.ID()
	prefix := fmt.Sprintf("connection %x", connID[:4])
	_ = c.conn.SetDeadline(time.Time{})
	defer close(c.stop)
	defer func() {
		if c.unregisterFocus != nil {
			c.unregisterFocus()
		}
		if c.unregisterState != nil {
			c.unregisterState()
		}
		if c.unregisterPaneState != nil {
			c.unregisterPaneState()
		}
		if retErr != nil {
			debugLog.Printf("%s exiting with error: %v", prefix, retErr)
		} else {
			debugLog.Printf("%s exiting cleanly", prefix)
		}
	}()
	defer c.session.MarkSnapshot(time.Now())
	for {
		if err := c.sendPending(); err != nil {
			if err == io.EOF {
				debugLog.Printf("%s sendPending reached EOF", prefix)
				return nil
			}
			log.Printf("%s sendPending error: %v", prefix, err)
			retErr = err
			return err
		}

		select {
		case <-c.pending:
			continue
		case err := <-c.readErr:
			if err == io.EOF {
				debugLog.Printf("%s read EOF", prefix)
				return nil
			}
			if err != nil {
				log.Printf("%s read error: %v", prefix, err)
				retErr = err
				return err
			}
			return nil
		case msg, ok := <-c.incoming:
			if !ok {
				err := c.awaitReadError()
				if err == io.EOF {
					debugLog.Printf("%s read EOF", prefix)
					return nil
				}
				if err != nil {
					log.Printf("%s read error: %v", prefix, err)
					retErr = err
					return err
				}
				return nil
			}
			debugLog.Printf("%s recv type=%d seq=%d len=%d", prefix, msg.header.Type, msg.header.Sequence, len(msg.payload))
			if err := c.handleMessage(prefix, msg.header, msg.payload); err != nil {
				retErr = err
				return err
			}
		}
	}
}

func (c *connection) readMessages() {
	defer close(c.incoming)
	for {
		header, payload, err := protocol.ReadMessage(c.conn)
		if err != nil {
			c.reportReadError(err)
			return
		}
		msg := protocolMessage{header: header, payload: payload}
		select {
		case c.incoming <- msg:
		case <-c.stop:
			return
		}
	}
}

func (c *connection) reportReadError(err error) {
	if err == nil {
		return
	}
	select {
	case c.readErr <- err:
	default:
	}
}

func (c *connection) awaitReadError() error {
	select {
	case err := <-c.readErr:
		return err
	default:
		return nil
	}
}

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
				encoded, err := protocol.EncodeClipboardSet(protocol.ClipboardSet{MimeType: mime, Data: data})
				if err != nil {
					return err
				}
				if err := c.writeControlMessage(protocol.MsgClipboardSet, encoded); err != nil {
					return err
				}
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
		}
		c.nudge()
	default:
		debugLog.Printf("%s ignoring message type %d", prefix, header.Type)
	}
	return nil
}

func (c *connection) OnEvent(event texel.Event) {
	switch event.Type {
	case texel.EventStateUpdate:
		payload, ok := event.Payload.(texel.StatePayload)
		if !ok {
			return
		}
		c.sendStateUpdate(payload)
	case texel.EventTreeChanged:
		c.sendTreeSnapshot()
	}
}

func (c *connection) sendTreeSnapshot() {
	sink, ok := c.sink.(*DesktopSink)
	if !ok {
		return
	}
	snapshot, err := sink.Snapshot()
	if err != nil {
		return
	}
	// Always send snapshot, even if empty - client needs to clear old panes during workspace switches
	sink.Publish()
	geometrySnapshot := geometryOnlySnapshot(snapshot)

	payload, err := protocol.EncodeTreeSnapshot(geometrySnapshot)
	if err != nil {
		return
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: c.session.ID()}
	_ = c.writeMessage(header, payload)
	states := snapshotMergedPaneStates(snapshot, sink.Desktop())
	for _, pane := range states {
		c.sendPaneState(pane.ID, pane.Active, pane.Resizing, pane.ZOrder, pane.HandlesSelection)
	}
}

func snapshotMergedPaneStates(snapshot protocol.TreeSnapshot, desktop *texel.DesktopEngine) []texel.PaneStateSnapshot {
	if desktop == nil {
		return nil
	}
	byID := make(map[[16]byte]texel.PaneStateSnapshot)
	for _, state := range desktop.PaneStates() {
		byID[state.ID] = state
	}
	merged := make([]texel.PaneStateSnapshot, 0, len(snapshot.Panes))
	for _, pane := range snapshot.Panes {
		if state, ok := byID[pane.PaneID]; ok {
			merged = append(merged, state)
		} else {
			merged = append(merged, texel.PaneStateSnapshot{ID: pane.PaneID})
		}
	}
	return merged
}

func (c *connection) PaneStateChanged(id [16]byte, active bool, resizing bool, z int, handlesSelection bool) {
	c.sendPaneState(id, active, resizing, z, handlesSelection)
}

func (c *connection) sendPending() error {
	if c.awaitResume {
		return nil
	}
	pending := c.session.Pending(c.lastAcked)
	for _, diff := range pending {
		if diff.Sequence <= c.lastSent {
			continue
		}
		header := diff.Message
		header.Sequence = diff.Sequence
		header.SessionID = c.session.ID()
		if err := c.writeMessage(header, diff.Payload); err != nil {
			return err
		}
		c.lastSent = diff.Sequence
	}
	return nil
}

func (c *connection) writeControlMessage(msgType protocol.MessageType, payload []byte) error {
	header := protocol.Header{
		Version:   protocol.Version,
		Type:      msgType,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(header, payload)
}

func (c *connection) writeMessage(header protocol.Header, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return protocol.WriteMessage(c.conn, header, payload)
}

func (c *connection) requestClipboardData(mime string) []byte {
	if sink, ok := c.sink.(*DesktopSink); ok {
		if desktop := sink.Desktop(); desktop != nil {
			return desktop.HandleClipboardGet(mime)
		}
	}
	return nil
}

func (c *connection) PaneFocused(paneID [16]byte) {
	payload, err := protocol.EncodePaneFocus(protocol.PaneFocus{PaneID: paneID})
	if err != nil {
		return
	}
	if err := c.writeControlMessage(protocol.MsgPaneFocus, payload); err != nil {
		// Ignore errors when the connection is closing.
	}
}

func (c *connection) sendStateUpdate(state texel.StatePayload) {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)

	all := make([]int32, 0, len(state.AllWorkspaces))
	for _, id := range state.AllWorkspaces {
		if id < minInt32 || id > maxInt32 {
			log.Printf("connection: workspace id %d out of int32 range; skipping", id)
			continue
		}
		all = append(all, int32(id))
	}

	workspaceID := int32(0)
	if state.WorkspaceID < minInt32 || state.WorkspaceID > maxInt32 {
		log.Printf("connection: active workspace id %d out of int32 range; defaulting to 0", state.WorkspaceID)
	} else {
		workspaceID = int32(state.WorkspaceID)
	}

	r, g, b := state.DesktopBgColor.RGB()
	update := protocol.StateUpdate{
		WorkspaceID:   workspaceID,
		AllWorkspaces: all,
		InControlMode: state.InControlMode,
		SubMode:       state.SubMode,
		ActiveTitle:   state.ActiveTitle,
		DesktopBgRGB:  colorToRGB(r, g, b),
	}
	payload, err := protocol.EncodeStateUpdate(update)
	if err != nil {
		return
	}
	if err := c.writeControlMessage(protocol.MsgStateUpdate, payload); err != nil {
		// Ignore write failures; connection loop will surface them.
	}
}

func colorToRGB(r, g, b int32) uint32 {
	return ((uint32(r) & 0xFF) << 16) | ((uint32(g) & 0xFF) << 8) | (uint32(b) & 0xFF)
}

func (c *connection) sendPaneStateSnapshots(states []texel.PaneStateSnapshot) {
	for _, state := range states {
		c.sendPaneState(state.ID, state.Active, state.Resizing, state.ZOrder, state.HandlesSelection)
	}
}

func (c *connection) sendPaneState(id [16]byte, active, resizing bool, z int, handlesSelection bool) {
	var flags protocol.PaneStateFlags
	if active {
		flags |= protocol.PaneStateActive
	}
	if resizing {
		flags |= protocol.PaneStateResizing
	}
	if handlesSelection {
		flags |= protocol.PaneStateSelectionDelegated
	}
	payload, err := protocol.EncodePaneState(protocol.PaneState{PaneID: id, Flags: flags, ZOrder: int32(z)})
	if err != nil {
		return
	}
	_ = c.writeControlMessage(protocol.MsgPaneState, payload)
}

func (c *connection) nudge() {
	if c.pending == nil {
		return
	}
	select {
	case c.pending <- struct{}{}:
	default:
	}
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

	snapshot, err := sink.Snapshot()
	if err != nil {
		sink.Publish()
		return
	}
	if len(snapshot.Panes) == 0 {
		sink.Publish()
		return
	}

	sink.Publish()

	payload, err := protocol.EncodeTreeSnapshot(snapshot)
	if err != nil {
		return
	}

	header := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgTreeSnapshot,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	if err := c.writeMessage(header, payload); err != nil {
		return
	}

	states := snapshotMergedPaneStates(snapshot, desktop)
	for _, state := range states {
		c.sendPaneState(state.ID, state.Active, state.Resizing, state.ZOrder, state.HandlesSelection)
	}
}

func geometryOnlySnapshot(snapshot protocol.TreeSnapshot) protocol.TreeSnapshot {
	out := protocol.TreeSnapshot{
		Panes: make([]protocol.PaneSnapshot, len(snapshot.Panes)),
		Root:  snapshot.Root,
	}
	for i, pane := range snapshot.Panes {
		cloned := pane
		cloned.Rows = nil
		out.Panes[i] = cloned
	}
	return out
}
