// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: client/cmd/texel-headless/main.go
// Summary: Implements main capabilities for the headless client harness.
// Usage: Used in CI and automated tests to validate protocol flows without opening a tcell screen.
// Notes: Provides a minimal client for scripted scenarios.

package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"texelation/client"
	"texelation/protocol"
)

type headlessState struct {
	cache        *client.BufferCache
	sessionID    [16]byte
	lastSequence uint64

	conn    net.Conn
	writeMu sync.Mutex

	deltaCount    uint64
	snapshotCount uint64
	lastDelta     time.Time
	logEvery      int
}

func main() {
	socketPath := flag.String("socket", "/tmp/texelation.sock", "Unix domain socket path")
	sessionStr := flag.String("session", "", "Existing session ID to resume (hex, optional)")
	lastSeq := flag.Uint64("resume-seq", 0, "Last acknowledged sequence when resuming")
	resizeCols := flag.Int("cols", 120, "Advertised terminal columns")
	resizeRows := flag.Int("rows", 40, "Advertised terminal rows")
	logEvery := flag.Int("log-every", 500, "Log every N buffer deltas (0 disables periodic logging)")
	flag.Parse()

	logger := log.New(os.Stdout, "[headless] ", log.LstdFlags|log.Lmicroseconds)

	var sessionID [16]byte
	if *sessionStr != "" {
		if err := parseSessionID(*sessionStr, &sessionID); err != nil {
			logger.Fatalf("invalid session id: %v", err)
		}
	}

	simple := client.NewSimpleClient(*socketPath)
	accept, conn, err := simple.Connect(&sessionID)
	if err != nil {
		logger.Fatalf("connect failed: %v", err)
	}
	sessionID = accept.SessionID
	logger.Printf("connected to session %s", client.FormatUUID(sessionID))

	state := &headlessState{
		cache:     client.NewBufferCache(),
		sessionID: sessionID,
		conn:      conn,
		logEvery:  *logEvery,
	}

	// Handle resume if requested.
	if *sessionStr != "" || *lastSeq > 0 {
		hdr, payload, err := simple.RequestResume(conn, sessionID, *lastSeq)
		if err != nil {
			logger.Fatalf("resume request failed: %v", err)
		}
		if err := state.handleMessage(logger, hdr, payload); err != nil {
			logger.Fatalf("resume payload error: %v", err)
		}
		state.lastSequence = *lastSeq
	}

	// Send initial resize to keep server layout deterministic.
	if err := state.sendResize(*resizeCols, *resizeRows); err != nil {
		logger.Printf("send resize failed: %v", err)
	}

	ctxDone := make(chan struct{})
	go func() {
		state.readLoop(logger)
		close(ctxDone)
	}()

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigC:
		logger.Printf("received signal %v, closing connection", sig)
		_ = conn.Close()
	case <-ctxDone:
	}

	<-ctxDone
	logger.Printf("exiting: snapshots=%d deltas=%d last-seq=%d", state.snapshotCount, state.deltaCount, state.lastSequence)
}

func (s *headlessState) readLoop(logger *log.Logger) {
	for {
		hdr, payload, err := protocol.ReadMessage(s.conn)
		if err != nil {
			if err == io.EOF {
				logger.Printf("server closed connection")
			} else {
				logger.Printf("read error: %v", err)
			}
			return
		}
		if err := s.handleMessage(logger, hdr, payload); err != nil {
			logger.Printf("handle %v failed: %v", hdr.Type, err)
			return
		}
	}
}

func (s *headlessState) handleMessage(logger *log.Logger, hdr protocol.Header, payload []byte) error {
	switch hdr.Type {
	case protocol.MsgTreeSnapshot:
		snapshot, err := protocol.DecodeTreeSnapshot(payload)
		if err != nil {
			return fmt.Errorf("decode snapshot: %w", err)
		}
		s.cache.ApplySnapshot(snapshot)
		s.snapshotCount++
	case protocol.MsgBufferDelta:
		delta, err := protocol.DecodeBufferDelta(payload)
		if err != nil {
			return fmt.Errorf("decode delta: %w", err)
		}
		s.cache.ApplyDelta(delta)
		if err := s.sendAck(hdr.Sequence); err != nil {
			return fmt.Errorf("send ack: %w", err)
		}
		s.lastSequence = hdr.Sequence
		s.deltaCount++
		if s.logEvery > 0 && s.deltaCount%uint64(s.logEvery) == 0 {
			logger.Printf("deltas=%d last-seq=%d panes=%d", s.deltaCount, s.lastSequence, len(s.cache.SortedPanes()))
		}
	case protocol.MsgStateUpdate:
		_, err := protocol.DecodeStateUpdate(payload)
		if err != nil {
			return fmt.Errorf("decode state update: %w", err)
		}
	case protocol.MsgPaneState:
		state, err := protocol.DecodePaneState(payload)
		if err != nil {
			return fmt.Errorf("decode pane state: %w", err)
		}
		active := state.Flags&protocol.PaneStateActive != 0
		resizing := state.Flags&protocol.PaneStateResizing != 0
		s.cache.SetPaneFlags(state.PaneID, active, resizing, state.ZOrder)
	case protocol.MsgPing:
		ping, err := protocol.DecodePing(payload)
		if err != nil {
			return fmt.Errorf("decode ping: %w", err)
		}
		pongPayload, err := protocol.EncodePong(protocol.Pong{Timestamp: ping.Timestamp})
		if err != nil {
			return fmt.Errorf("encode pong: %w", err)
		}
		return s.writeControl(protocol.MsgPong, pongPayload)
	case protocol.MsgThemeUpdate:
		update, err := protocol.DecodeThemeUpdate(payload)
		if err != nil {
			return fmt.Errorf("decode theme update: %w", err)
		}
		ackPayload, err := protocol.EncodeThemeAck(protocol.ThemeAck(update))
		if err != nil {
			return fmt.Errorf("encode theme ack: %w", err)
		}
		return s.writeControl(protocol.MsgThemeAck, ackPayload)
	case protocol.MsgClipboardGet:
		request, err := protocol.DecodeClipboardGet(payload)
		if err != nil {
			return fmt.Errorf("decode clipboard get: %w", err)
		}
		dataPayload, err := protocol.EncodeClipboardData(protocol.ClipboardData{MimeType: request.MimeType, Data: nil})
		if err != nil {
			return fmt.Errorf("encode clipboard data: %w", err)
		}
		return s.writeControl(protocol.MsgClipboardData, dataPayload)
	case protocol.MsgClipboardSet:
		// No-op; server is pushing clipboard contents to the client.
	case protocol.MsgDisconnectNotice:
		logger.Printf("server sent disconnect notice; closing")
		return io.EOF
	default:
		// Ignore other message types (focus, theme ack, etc).
	}
	return nil
}

func (s *headlessState) sendAck(sequence uint64) error {
	payload, err := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: sequence})
	if err != nil {
		return err
	}
	return s.writeControl(protocol.MsgBufferAck, payload)
}

func (s *headlessState) sendResize(cols, rows int) error {
	payload, err := protocol.EncodeResize(protocol.Resize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return err
	}
	return s.writeControl(protocol.MsgResize, payload)
}

func (s *headlessState) writeControl(msgType protocol.MessageType, payload []byte) error {
	header := protocol.Header{
		Version:   protocol.Version,
		Type:      msgType,
		Flags:     protocol.FlagChecksum,
		SessionID: s.sessionID,
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return protocol.WriteMessage(s.conn, header, payload)
}

func parseSessionID(value string, out *[16]byte) error {
	clean := strings.ReplaceAll(value, "-", "")
	if len(clean) != 32 {
		return fmt.Errorf("expected 32 hex characters, got %d", len(clean))
	}
	data, err := hex.DecodeString(clean)
	if err != nil {
		return err
	}
	copy(out[:], data)
	return nil
}
