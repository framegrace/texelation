// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/testutil/memconn_test.go
// Summary: Exercises memconn behaviour to ensure the server runtime test utilities remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: Not shipped with production binaries; only used in test code.

package testutil

import (
	"net"
	"testing"
	"time"
)

func TestMemConnRoundTrip(t *testing.T) {
	left, right := NewMemPipe(4)
	defer left.Close()
	defer right.Close()

	payload := []byte("hello")
	if _, err := left.Write(payload); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	buf := make([]byte, 8)
	n, err := right.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("unexpected read %q", buf[:n])
	}

	if err := left.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if _, err := right.Read(buf); err == nil {
		t.Fatalf("expected EOF after close")
	}
}

func TestMemConnDeadline(t *testing.T) {
	left, right := NewMemPipe(1)
	defer left.Close()
	defer right.Close()

	if err := right.SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, 1)
	if _, err := right.Read(buf); err == nil {
		t.Fatalf("expected deadline error")
	}
}

func TestDummyAddrImplementsNetAddr(t *testing.T) {
	left, right := NewMemPipe(1)
	defer left.Close()
	defer right.Close()

	if _, ok := interface{}(left.LocalAddr()).(net.Addr); !ok {
		t.Fatalf("LocalAddr does not implement net.Addr")
	}
	if left.LocalAddr().Network() != "mem" {
		t.Fatalf("unexpected network: %s", left.LocalAddr().Network())
	}
}
