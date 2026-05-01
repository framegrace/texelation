// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/background_tasks.go
// Summary: Background goroutines for keep-alive and acknowledgment handling.
// Usage: Ping/pong keep-alive and buffer acknowledgment loops.

package clientruntime

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/framegrace/texelation/protocol"
)

func pingLoop(sessionID [16]byte, done <-chan struct{}, stop <-chan struct{}, writer *messageWriter) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-stop:
			return
		case <-ticker.C:
			ping := protocol.Ping{Timestamp: time.Now().UnixNano()}
			payload, err := protocol.EncodePing(ping)
			if err != nil {
				log.Printf("encode ping failed: %v", err)
				continue
			}
			header := protocol.Header{Version: protocol.Version, Type: protocol.MsgPing, Flags: protocol.FlagChecksum, SessionID: sessionID}
			if err := writer.Send(header, payload); err != nil {
				log.Printf("send ping failed: %v", err)
				return
			}
		}
	}
}

func scheduleAck(pending *atomic.Uint64, signal chan<- struct{}, seq uint64) {
	for {
		current := pending.Load()
		if seq <= current {
			break
		}
		if pending.CompareAndSwap(current, seq) {
			break
		}
	}
	select {
	case signal <- struct{}{}:
	default:
	}
}

func ackLoop(sessionID [16]byte, writer *messageWriter, done <-chan struct{}, pending *atomic.Uint64, lastAck *atomic.Uint64, signal <-chan struct{}) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-signal:
		case <-ticker.C:
		}
		target := pending.Load()
		if target == 0 || target == lastAck.Load() {
			continue
		}
		payload, err := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: target})
		if err != nil {
			log.Printf("ack encode failed: %v", err)
			continue
		}
		header := protocol.Header{
			Version:   protocol.Version,
			Type:      protocol.MsgBufferAck,
			Flags:     protocol.FlagChecksum,
			SessionID: sessionID,
		}
		if err := writer.Send(header, payload); err != nil {
			log.Printf("ack send failed: %v", err)
			return
		}
		lastAck.Store(target)
	}
}
