package server

import (
	"io"
	"net"
	"time"

	"texelation/protocol"
)

type connection struct {
	conn      net.Conn
	session   *Session
	lastSent  uint64
	lastAcked uint64
	sink      EventSink
}

func newConnection(conn net.Conn, session *Session, sink EventSink) *connection {
	if sink == nil {
		sink = nopSink{}
	}
	return &connection{conn: conn, session: session, sink: sink}
}

func (c *connection) serve() error {
	defer c.session.MarkSnapshot(time.Now()) // placeholder for future persistence triggers
	for {
		if err := c.sendPending(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		header, payload, err := protocol.ReadMessage(c.conn)
		if err != nil {
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
			if err := protocol.WriteMessage(c.conn, pongHeader, pongPayload); err != nil {
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
		case protocol.MsgClipboardSet:
			clipSet, err := protocol.DecodeClipboardSet(payload)
			if err != nil {
				return err
			}
			c.sink.HandleClipboardSet(c.session, clipSet)
	case protocol.MsgClipboardGet:
		clipGet, err := protocol.DecodeClipboardGet(payload)
		if err != nil {
			return err
		}
		c.sink.HandleClipboardGet(c.session, clipGet)
	case protocol.MsgThemeUpdate:
		themeUpdate, err := protocol.DecodeThemeUpdate(payload)
		if err != nil {
			return err
		}
		c.sink.HandleThemeUpdate(c.session, themeUpdate)
	case protocol.MsgResumeRequest:
		request, err := protocol.DecodeResumeRequest(payload)
		if err != nil {
			return err
		}
		c.lastAcked = request.LastSequence
		if provider, ok := c.sink.(SnapshotProvider); ok {
			snapshot, err := provider.Snapshot()
			if err == nil {
				if payload, err := protocol.EncodeTreeSnapshot(snapshot); err == nil {
					header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: c.session.ID()}
					if err := protocol.WriteMessage(c.conn, header, payload); err != nil {
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

func (c *connection) sendPending() error {
	pending := c.session.Pending(c.lastAcked)
	for _, diff := range pending {
		if diff.Sequence <= c.lastSent {
			continue
		}
		header := diff.Message
		header.Sequence = diff.Sequence
		header.SessionID = c.session.ID()
		if err := protocol.WriteMessage(c.conn, header, diff.Payload); err != nil {
			return err
		}
		c.lastSent = diff.Sequence
	}
	return nil
}
