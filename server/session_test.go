package server

import (
	"testing"
	"time"

	"texelation/protocol"
)

func TestSessionEnqueueAckPending(t *testing.T) {
	var id [16]byte
	copy(id[:], []byte("session-0000001"))
	session := NewSession(id)

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
	session := NewSession(id)
	now := time.Now()
	session.MarkSnapshot(now)
	if got := session.LastSnapshot(); !got.Equal(now) {
		t.Fatalf("expected snapshot time %v, got %v", now, got)
	}
}
