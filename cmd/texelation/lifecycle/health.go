// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/lifecycle/health.go
// Summary: Health checking for texelation server daemon.

package lifecycle

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// HealthChecker verifies the server is responsive
type HealthChecker interface {
	// Check performs a health check against the server
	// Returns nil if healthy, error otherwise
	Check(ctx context.Context, socketPath string) error
}

// SocketHealthChecker performs health checks by connecting to the socket
type SocketHealthChecker struct {
	timeout time.Duration
}

// NewSocketHealthChecker creates a health checker with the given timeout
func NewSocketHealthChecker(timeout time.Duration) HealthChecker {
	return &SocketHealthChecker{timeout: timeout}
}

// Check verifies the server is accepting connections
func (h *SocketHealthChecker) Check(ctx context.Context, socketPath string) error {
	// Create a deadline based on context or timeout
	deadline := time.Now().Add(h.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	// Dial with timeout
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to socket: %w", err)
	}
	defer conn.Close()

	// Set read/write deadlines
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	// Socket connection succeeded - server is accepting connections
	// For a more thorough check, we could do a full handshake, but
	// accepting connections is sufficient to prove the server is alive
	return nil
}

// ProtocolHealthChecker performs health checks using the ping/pong protocol
type ProtocolHealthChecker struct {
	timeout time.Duration
}

// NewProtocolHealthChecker creates a health checker that uses ping/pong
func NewProtocolHealthChecker(timeout time.Duration) HealthChecker {
	return &ProtocolHealthChecker{timeout: timeout}
}

// Check verifies the server responds to ping with pong
func (h *ProtocolHealthChecker) Check(ctx context.Context, socketPath string) error {
	// Create a deadline based on context or timeout
	deadline := time.Now().Add(h.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	// Dial with timeout
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to socket: %w", err)
	}
	defer conn.Close()

	// Set read/write deadlines
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	// Send ping
	hdr := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgPing,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(conn, hdr, nil); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}

	// Read response
	respHdr, _, err := protocol.ReadMessage(conn)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if respHdr.Type != protocol.MsgPong {
		return fmt.Errorf("unexpected response type: %v", respHdr.Type)
	}

	return nil
}
