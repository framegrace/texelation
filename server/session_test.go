package server

import (
	"testing"
	"time"

	"texelation/protocol"
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
			Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{Text: text}}}},
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
