package server

import "testing"

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
