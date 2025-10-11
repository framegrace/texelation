package server

import (
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
}

func newConnection(conn net.Conn, session *Session, sink EventSink, awaitResume bool) *connection {
	if sink == nil {
		sink = nopSink{}
	}
	c := &connection{conn: conn, session: session, sink: sink, awaitResume: awaitResume}
	id := session.ID()
	if awaitResume {
		log.Printf("server: connection %x awaiting resume request", id[:4])
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
	return c
}

func (c *connection) serve() error {
	_ = c.conn.SetDeadline(time.Time{})
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
	}()
	defer c.session.MarkSnapshot(time.Now()) // placeholder for future persistence triggers
	for {
		if err := c.sendPending(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		_ = c.conn.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
		header, payload, err := protocol.ReadMessage(c.conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if err == io.EOF {
				return nil
			}
			return err
		}

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
		default:
			// Unknown messages are ignored for now.
		}
	}
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
	if err != nil || len(snapshot.Panes) == 0 {
		return
	}
	sink.Publish()
	payload, err := protocol.EncodeTreeSnapshot(snapshot)
	if err != nil {
		return
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: c.session.ID()}
	_ = c.writeMessage(header, payload)
	states := snapshotMergedPaneStates(snapshot, sink.Desktop())
	for _, pane := range states {
		c.sendPaneState(pane.ID, pane.Active, pane.Resizing)
	}
}

func snapshotMergedPaneStates(snapshot protocol.TreeSnapshot, desktop *texel.Desktop) []texel.PaneStateSnapshot {
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

func (c *connection) PaneStateChanged(id [16]byte, active bool, resizing bool) {
	c.sendPaneState(id, active, resizing)
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
	all := make([]int32, len(state.AllWorkspaces))
	for i, id := range state.AllWorkspaces {
		all[i] = int32(id)
	}
	r, g, b := state.DesktopBgColor.RGB()
	update := protocol.StateUpdate{
		WorkspaceID:   int32(state.WorkspaceID),
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
		c.sendPaneState(state.ID, state.Active, state.Resizing)
	}
}

func (c *connection) sendPaneState(id [16]byte, active, resizing bool) {
	var flags protocol.PaneStateFlags
	if active {
		flags |= protocol.PaneStateActive
	}
	if resizing {
		flags |= protocol.PaneStateResizing
	}
	payload, err := protocol.EncodePaneState(protocol.PaneState{PaneID: id, Flags: flags})
	if err != nil {
		return
	}
	_ = c.writeControlMessage(protocol.MsgPaneState, payload)
}

func (c *connection) handleResize(size protocol.Resize) {
	if sink, ok := c.sink.(*DesktopSink); ok {
		if desktop := sink.Desktop(); desktop != nil {
			desktop.SetViewportSize(int(size.Cols), int(size.Rows))
			if snapshot, err := sink.Snapshot(); err == nil {
				if payload, err := protocol.EncodeTreeSnapshot(snapshot); err == nil {
					header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: c.session.ID()}
					_ = c.writeMessage(header, payload)
				}
			}
			sink.Publish()
		}
	}
}
