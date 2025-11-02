// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/message_sender.go
// Summary: Protocol message encoding and sending for client runtime.
// Usage: Provides functions to encode and send various message types to the server.

package clientruntime

import (
	"log"
	"net"
	"sync"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
)

func sendResize(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, screen tcell.Screen) {
	cols, rows := screen.Size()
	sendResizeMessage(writeMu, conn, sessionID, protocol.Resize{Cols: uint16(cols), Rows: uint16(rows)})
}

func sendResizeMessage(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, resize protocol.Resize) {
	if resize.Cols == 0 || resize.Rows == 0 {
		return
	}
	payload, err := protocol.EncodeResize(resize)
	if err != nil {
		log.Printf("encode resize failed: %v", err)
		return
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgResize, Flags: protocol.FlagChecksum, SessionID: sessionID}
	if err := writeMessage(writeMu, conn, header, payload); err != nil {
		log.Printf("send resize failed: %v", err)
	}
}

func sendKeyEvent(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, key tcell.Key, r rune, mods tcell.ModMask) error {
	event := protocol.KeyEvent{KeyCode: uint32(key), RuneValue: r, Modifiers: uint16(mods)}
	// log.Printf("send key: key=%v rune=%q mods=%v", key, r, mods)
	payload, err := protocol.EncodeKeyEvent(event)
	if err != nil {
		return err
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgKeyEvent, Flags: protocol.FlagChecksum, SessionID: sessionID}
	return writeMessage(writeMu, conn, header, payload)
}

func sendPaste(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, data []byte) error {
	payload, err := protocol.EncodePaste(protocol.Paste{Data: data})
	if err != nil {
		return err
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgPaste, Flags: protocol.FlagChecksum, SessionID: sessionID}
	return writeMessage(writeMu, conn, header, payload)
}

func sendClipboardSet(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, mime string, data []byte) {
	msg := protocol.ClipboardSet{MimeType: mime, Data: data}
	payload, err := protocol.EncodeClipboardSet(msg)
	if err != nil {
		log.Printf("encode clipboard set failed: %v", err)
		return
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgClipboardSet, Flags: protocol.FlagChecksum, SessionID: sessionID}
	if err := writeMessage(writeMu, conn, header, payload); err != nil {
		log.Printf("send clipboard set failed: %v", err)
	}
}

func writeMessage(mu *sync.Mutex, conn net.Conn, header protocol.Header, payload []byte) error {
	mu.Lock()
	defer mu.Unlock()
	log.Printf("client tx type=%d seq=%d len=%d", header.Type, header.Sequence, len(payload))
	return protocol.WriteMessage(conn, header, payload)
}
