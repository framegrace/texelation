package server

import (
	"testing"

	"texelation/protocol"
)

func TestManagerLifecycle(t *testing.T) {
	m := NewManager()
	session, err := m.NewSession()
	if err != nil {
		t.Fatalf("new session failed: %v", err)
	}
	if m.ActiveSessions() != 1 {
		t.Fatalf("expected 1 active session")
	}

	found, err := m.Lookup(session.ID())
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if found != session {
		t.Fatalf("lookup returned different session")
	}

	m.Close(session.ID())
	if m.ActiveSessions() != 0 {
		t.Fatalf("expected 0 active sessions after close")
	}
}

func TestManagerDiffRetentionUpdate(t *testing.T) {
	m := NewManager()
	session, err := m.NewSession()
	if err != nil {
		t.Fatalf("new session failed: %v", err)
	}

	m.SetDiffRetentionLimit(1)
	if len(session.Pending(0)) != 0 {
		t.Fatalf("expected empty pending queue")
	}

	delta := protocol.BufferDelta{PaneID: session.ID(), Revision: 1}
	if err := session.EnqueueDiff(delta); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if err := session.EnqueueDiff(delta); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if len(session.Pending(0)) != 1 {
		t.Fatalf("retention limit not respected after manager update")
	}
}
