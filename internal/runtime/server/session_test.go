// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/session_test.go
// Summary: Exercises session behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

func TestSessionEnqueueAckPending(t *testing.T) {
	var id [16]byte
	copy(id[:], []byte("session-0000001"))
	session := NewSession(id, 0)

	delta := protocol.BufferDelta{PaneID: id, Revision: 1}
	if err := session.EnqueueDiff(delta); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	pending := session.Pending(0)
	if len(pending) != 1 {
		t.Fatalf("expected one pending diff, got %d", len(pending))
	}
	if pending[0].Sequence != 1 {
		t.Fatalf("expected sequence 1, got %d", pending[0].Sequence)
	}

	session.Ack(1)
	if len(session.Pending(0)) != 0 {
		t.Fatalf("expected queue empty after ack")
	}
}

func TestSessionSnapshotTracking(t *testing.T) {
	var id [16]byte
	copy(id[:], []byte("session-0000002"))
	session := NewSession(id, 2)
	now := time.Now()
	session.MarkSnapshot(now)
	if got := session.LastSnapshot(); !got.Equal(now) {
		t.Fatalf("expected snapshot time %v, got %v", now, got)
	}
}

func TestSessionRetentionLimit(t *testing.T) {
	var id [16]byte
	copy(id[:], []byte("session-retent"))
	session := NewSession(id, 2)

	makeDelta := func(seq uint32, text string) protocol.BufferDelta {
		return protocol.BufferDelta{
			PaneID:   id,
			Revision: seq,
			Styles:   []protocol.StyleEntry{{}},
			Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{Text: text, StyleIndex: 0}}}},
		}
	}

	_ = session.EnqueueDiff(makeDelta(1, "one"))
	_ = session.EnqueueDiff(makeDelta(2, "two"))
	_ = session.EnqueueDiff(makeDelta(3, "three"))

	pending := session.Pending(0)
	if len(pending) != 2 {
		t.Fatalf("expected retention to keep 2 diffs, got %d", len(pending))
	}
	if pending[0].Sequence != 2 || pending[1].Sequence != 3 {
		t.Fatalf("unexpected sequences after retention: %d, %d", pending[0].Sequence, pending[1].Sequence)
	}

	session.setMaxDiffs(1)
	_ = session.EnqueueDiff(makeDelta(4, "four"))
	pending = session.Pending(0)
	if len(pending) != 1 || pending[0].Sequence != 4 {
		t.Fatalf("expected retention update to keep only latest diff, got %v", pending)
	}
	stats := session.Stats()
	if stats.DroppedDiffs == 0 || stats.LastDroppedSeq == 0 {
		t.Fatalf("expected dropped diff metrics, got %+v", stats)
	}
}

func TestSessionStatsReporter(t *testing.T) {
	var id [16]byte
	copy(id[:], []byte("session-report"))
	session := NewSession(id, 1)

	ch := make(chan SessionStats, 1)
	SetSessionStatsReporter(func(stats SessionStats) {
		ch <- stats
	})
	defer SetSessionStatsReporter(nil)

	delta := protocol.BufferDelta{PaneID: id, Revision: 1}
	if err := session.EnqueueDiff(delta); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	_ = session.EnqueueDiff(protocol.BufferDelta{PaneID: id, Revision: 2})

	select {
	case stats := <-ch:
		if stats.DroppedDiffs == 0 {
			t.Fatalf("expected drop count in reporter")
		}
	default:
		t.Fatalf("expected reporter to be invoked")
	}
}

func TestSessionWriterPersistsViewportUpdate(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xde, 0xad}
	sess := NewSession(id, 100)
	sess.AttachWriter(SessionFilePath(dir, id), 25*time.Millisecond)
	defer sess.Close()

	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID:        [16]byte{0xaa},
		ViewBottomIdx: 12345,
		Rows:          24,
		Cols:          80,
	})
	sess.FlushPersistForTest()

	data, err := os.ReadFile(SessionFilePath(dir, id))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var got StoredSession
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionID != id {
		t.Fatalf("expected sessionID %x, got %x", id, got.SessionID)
	}
	if got.SchemaVersion != StoredSessionSchemaVersion {
		t.Fatalf("schema version: got %d", got.SchemaVersion)
	}
	if len(got.PaneViewports) != 1 || got.PaneViewports[0].ViewBottomIdx != 12345 {
		t.Fatalf("pane viewports mismatch: %+v", got.PaneViewports)
	}
	if got.LastActive.IsZero() {
		t.Fatalf("LastActive should be set")
	}
}

func TestSessionWriterCloseFlushes(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xbe, 0xef}
	sess := NewSession(id, 100)
	sess.AttachWriter(SessionFilePath(dir, id), 1*time.Hour) // long debounce
	sess.ApplyViewportUpdate(protocol.ViewportUpdate{PaneID: [16]byte{0x01}, ViewBottomIdx: 1, Rows: 1, Cols: 1})
	sess.Close() // must flush

	if _, err := os.Stat(SessionFilePath(dir, id)); err != nil {
		t.Fatalf("Close did not flush: %v", err)
	}
}
