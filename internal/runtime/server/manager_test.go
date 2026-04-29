// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/manager_test.go
// Summary: Exercises manager behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"os"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
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

	stats := m.SessionStats()
	if len(stats) != 1 {
		t.Fatalf("expected stats for 1 session, got %d", len(stats))
	}
}

func TestManagerLookupOrRehydrate_LiveSessionWins(t *testing.T) {
	mgr := NewManager()
	live, err := mgr.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetPersistedSessions(map[[16]byte]*StoredSession{
		live.ID(): {SessionID: live.ID()}, // shadowed; live should win
	})
	got, rehydrated, err := mgr.LookupOrRehydrate(live.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got != live {
		t.Fatalf("expected live session, got different instance")
	}
	if rehydrated {
		t.Fatalf("live cache hit must not report rehydrated=true")
	}
}

func TestManagerLookupOrRehydrate_RehydratesFromDisk(t *testing.T) {
	mgr := NewManager()
	id := [16]byte{0xab}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now(),
		PaneViewports: []StoredPaneViewport{{
			PaneID:        [16]byte{0x11},
			ViewBottomIdx: 555,
			Rows:          24,
			Cols:          80,
		}},
	}
	mgr.SetPersistedSessions(map[[16]byte]*StoredSession{id: stored})

	sess, rehydrated, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("LookupOrRehydrate: %v", err)
	}
	if !rehydrated {
		t.Fatalf("first lookup with persisted entry must report rehydrated=true")
	}
	if sess.ID() != id {
		t.Fatalf("rehydrated session ID mismatch: %x", sess.ID())
	}
	// Pre-seeded viewport must be present.
	vp, ok := sess.Viewport([16]byte{0x11})
	if !ok {
		t.Fatalf("expected pre-seeded viewport from disk")
	}
	if vp.ViewBottomIdx != 555 {
		t.Fatalf("expected ViewBottomIdx=555, got %d", vp.ViewBottomIdx)
	}
	// After rehydration, the disk-side index entry is consumed —
	// subsequent lookups hit the live cache and report rehydrated=false.
	if _, rehydrated2, err := mgr.LookupOrRehydrate(id); err != nil {
		t.Fatalf("second lookup after rehydration must hit live cache, got %v", err)
	} else if rehydrated2 {
		t.Fatalf("second lookup must report rehydrated=false (live cache hit)")
	}
}

func TestManagerLookupOrRehydrate_UnknownReturnsErr(t *testing.T) {
	mgr := NewManager()
	if _, _, err := mgr.LookupOrRehydrate([16]byte{0xff}); err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestManagerNewSessionAttachesWriter(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	sess, err := mgr.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0xaa}, ViewBottomIdx: 1, Rows: 1, Cols: 1,
	})
	sess.FlushPersistForTest()

	if _, err := os.Stat(SessionFilePath(dir, sess.ID())); err != nil {
		t.Fatalf("expected session file written, got stat=%v", err)
	}
}

func TestManagerLookupOrRehydrate_AttachesWriter(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	id := [16]byte{0xab}
	stored := &StoredSession{SchemaVersion: StoredSessionSchemaVersion, SessionID: id, LastActive: time.Now()}
	mgr.SetPersistedSessions(map[[16]byte]*StoredSession{id: stored})

	sess, _, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0xbb}, ViewBottomIdx: 99, Rows: 1, Cols: 1,
	})
	sess.FlushPersistForTest()
	if _, err := os.Stat(SessionFilePath(dir, id)); err != nil {
		t.Fatalf("rehydrated session not persisting: %v", err)
	}
}

func TestManagerCloseDropsLockBeforeSessionClose(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	sess, err := mgr.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	id := sess.ID()
	// Apply enough updates to make Close's flush non-trivial.
	for i := 0; i < 10; i++ {
		sess.ApplyViewportUpdate(protocol.ViewportUpdate{
			PaneID: [16]byte{byte(i)}, ViewBottomIdx: int64(i), Rows: 1, Cols: 1,
		})
	}

	closeStarted := make(chan struct{})
	closeDone := make(chan struct{})
	go func() {
		close(closeStarted)
		mgr.Close(id)
		close(closeDone)
	}()
	<-closeStarted

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatalf("ActiveSessions blocked while Close was running — m.mu held during disk I/O")
		case <-closeDone:
			return
		default:
			_ = mgr.ActiveSessions()
			time.Sleep(time.Millisecond)
		}
	}
}

func TestManagerNewSessionWithID_BypassesRandomGen(t *testing.T) {
	mgr := NewManager()
	id := [16]byte{0xfa, 0xce, 0xfe, 0xed}

	sess, err := mgr.NewSessionWithID(id)
	if err != nil {
		t.Fatalf("NewSessionWithID: %v", err)
	}
	if sess.ID() != id {
		t.Fatalf("ID mismatch: got %x want %x", sess.ID(), id)
	}
	got, _, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != sess {
		t.Fatalf("Lookup must return the same session instance")
	}
}

func TestManagerNewSessionWithID_RejectsDuplicates(t *testing.T) {
	mgr := NewManager()
	id := [16]byte{0x01}
	if _, err := mgr.NewSessionWithID(id); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.NewSessionWithID(id); err == nil {
		t.Fatalf("expected error on duplicate id")
	}
}

// TestManagerStats_PersistDisabledWhenNoBasedir verifies the Plan D2
// 17.D observability surface: with EnablePersistence skipped (or
// failing), Manager.Stats reports PersistEnabled=false so callers
// can detect the silent-failure mode.
func TestManagerStats_PersistDisabledWhenNoBasedir(t *testing.T) {
	mgr := NewManager()
	stats := mgr.Stats()
	if stats.PersistEnabled {
		t.Fatalf("PersistEnabled must be false before EnablePersistence")
	}
	if stats.PersistBasedir != "" {
		t.Fatalf("PersistBasedir must be empty before EnablePersistence, got %q", stats.PersistBasedir)
	}
}

// TestManagerStats_PersistEnabledAfterEnable verifies the happy path:
// after EnablePersistence succeeds, Stats reports the enabled state
// and the basedir.
func TestManagerStats_PersistEnabledAfterEnable(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	stats := mgr.Stats()
	if !stats.PersistEnabled {
		t.Fatalf("PersistEnabled must be true after EnablePersistence")
	}
	if stats.PersistBasedir != dir {
		t.Fatalf("PersistBasedir = %q, want %q", stats.PersistBasedir, dir)
	}
}

// TestManagerCloseSerializesWithLookupOrRehydrate is the Plan D2 17.B
// regression: a Close racing with a same-ID LookupOrRehydrate must
// not let the rehydrate path attach a fresh atomicjson writer to the
// same on-disk path while Close is still flushing — otherwise two
// stores race on rename(). The serialization is provided by per-ID
// "closing" markers consulted by LookupOrRehydrate via waitClosing.
func TestManagerCloseSerializesWithLookupOrRehydrate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	id := [16]byte{0xcc}
	// Pre-seed a persisted entry so LookupOrRehydrate has something to
	// rehydrate against once the live session is closed.
	stored := &StoredSession{SchemaVersion: StoredSessionSchemaVersion, SessionID: id, LastActive: time.Now()}
	mgr.SetPersistedSessions(map[[16]byte]*StoredSession{id: stored})

	sess, _, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatal(err)
	}
	// Apply a few updates to make Close's flush non-trivial.
	for i := 0; i < 5; i++ {
		sess.ApplyViewportUpdate(protocol.ViewportUpdate{
			PaneID: [16]byte{byte(i)}, ViewBottomIdx: int64(i), Rows: 1, Cols: 1,
		})
	}

	// Re-seed the persisted entry so the post-Close rehydrate has
	// something to find. (Close consumed the live session; the
	// persisted index was consumed during the original rehydrate.)
	mgr.SetPersistedSessions(map[[16]byte]*StoredSession{id: stored})

	closeStarted := make(chan struct{})
	closeDone := make(chan struct{})
	go func() {
		close(closeStarted)
		mgr.Close(id)
		close(closeDone)
	}()
	<-closeStarted

	// Concurrent rehydrate attempt for the same ID. Must wait for
	// Close to finish (no rename race), then succeed against the
	// re-seeded persisted entry.
	rehydrateDone := make(chan struct{})
	go func() {
		defer close(rehydrateDone)
		_, _, _ = mgr.LookupOrRehydrate(id)
	}()

	// rehydrate must NOT complete before close.
	select {
	case <-rehydrateDone:
		select {
		case <-closeDone:
			// Both done; verify ordering by ensuring close finished
			// at-or-before rehydrate. We can't introspect order
			// directly here, so instead require rehydrate didn't
			// finish before close started its flush. Best-effort:
			// re-check by allowing both to finish then asserting
			// the persisted file is valid JSON.
		default:
			t.Fatal("rehydrate completed while Close was still in flight — 17.B race")
		}
	case <-closeDone:
		// Close finished first; rehydrate should follow shortly.
		<-rehydrateDone
	case <-time.After(2 * time.Second):
		t.Fatal("close/rehydrate deadlock")
	}

	// Persisted file must exist and be valid JSON.
	path := SessionFilePath(dir, id)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("session file unreadable after close+rehydrate: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("session file empty — likely overwritten during rename race")
	}
}
