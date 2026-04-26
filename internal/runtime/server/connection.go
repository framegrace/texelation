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

	"github.com/framegrace/texelation/protocol"
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
	resumeProcessed     bool // set once MsgResumeRequest has been handled; blocks duplicate resumes
	attachListeners     func()
	incoming            chan protocolMessage
	readErr             chan error
	pending             chan struct{}
	stop                chan struct{}
	initialSnapshotSent bool // Track if we've sent the first snapshot
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

func (c *connection) nudge() {
	if c.pending == nil {
		return
	}
	select {
	case c.pending <- struct{}{}:
	default:
	}
}
