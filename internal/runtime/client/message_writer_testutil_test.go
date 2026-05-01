// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Test helpers for the async message writer. The writer drains queued
// messages from a goroutine; tests inspecting the underlying conn need
// (a) deterministic shutdown (Close) and (b) a synchronization barrier
// after each Send (flushPending) before reading.

package clientruntime

import (
	"net"
	"testing"
)

// newTestWriter wraps conn with a messageWriter and registers a Cleanup
// hook so the writer goroutine exits when the test ends.
func newTestWriter(t testing.TB, conn net.Conn) *messageWriter {
	t.Helper()
	w := newMessageWriter(conn, 256)
	t.Cleanup(func() { w.Close() })
	return w
}
