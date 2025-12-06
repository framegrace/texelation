// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/testutil/memconn.go
// Summary: Implements memconn capabilities for the server runtime test utilities.
// Usage: Imported by server tests when they need memconn helpers.
// Notes: Not shipped with production binaries; only used in test code.

package testutil

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// MemConn implements net.Conn using in-memory channels, allowing predictable
// behaviour without relying on OS sockets.
type MemConn struct {
	readCh   <-chan []byte
	writeCh  chan []byte
	mu       sync.Mutex
	closed   bool
	deadline time.Time
}

// NewMemPipe returns two endpoints backed by mirrored channels.
func NewMemPipe(buffer int) (*MemConn, *MemConn) {
	if buffer <= 0 {
		buffer = 16
	}
	leftChan := make(chan []byte, buffer)
	rightChan := make(chan []byte, buffer)
	left := &MemConn{readCh: rightChan, writeCh: leftChan}
	right := &MemConn{readCh: leftChan, writeCh: rightChan}
	return left, right
}

func (m *MemConn) Read(b []byte) (int, error) {
	m.mu.Lock()
	closed := m.closed
	deadline := m.deadline
	m.mu.Unlock()
	if closed {
		return 0, io.EOF
	}

	var timer <-chan time.Time
	if !deadline.IsZero() {
		timer = time.After(time.Until(deadline))
	}

	select {
	case data, ok := <-m.readCh:
		if !ok {
			return 0, io.EOF
		}
		n := copy(b, data)
		return n, nil
	case <-timer:
		return 0, errors.New("memconn: read deadline reached")
	}
}

func (m *MemConn) Write(b []byte) (int, error) {
	m.mu.Lock()
	closed := m.closed
	deadline := m.deadline
	m.mu.Unlock()
	if closed {
		return 0, io.EOF
	}

	payload := make([]byte, len(b))
	copy(payload, b)

	var timer <-chan time.Time
	if !deadline.IsZero() {
		timer = time.After(time.Until(deadline))
	}

	select {
	case m.writeCh <- payload:
		return len(b), nil
	case <-timer:
		return 0, errors.New("memconn: write deadline reached")
	}
}

func (m *MemConn) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	close(m.writeCh)
	return nil
}

func (m *MemConn) LocalAddr() net.Addr  { return dummyAddr("mem") }
func (m *MemConn) RemoteAddr() net.Addr { return dummyAddr("mem") }

func (m *MemConn) SetDeadline(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deadline = t
	return nil
}

func (m *MemConn) SetReadDeadline(t time.Time) error  { return m.SetDeadline(t) }
func (m *MemConn) SetWriteDeadline(t time.Time) error { return m.SetDeadline(t) }

// dummyAddr implements net.Addr for in-memory connections.
type dummyAddr string

func (d dummyAddr) Network() string { return string(d) }
func (d dummyAddr) String() string  { return string(d) }
