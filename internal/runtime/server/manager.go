// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/manager.go
// Summary: Implements manager capabilities for the server runtime.
// Usage: Used by texel-server to coordinate manager when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"crypto/rand"
	"errors"
	"log"
	"sync"
	"time"
)

var (
	ErrSessionNotFound = errors.New("server: session not found")
)

// Manager tracks active sessions and coordinates creation/lookup.
type Manager struct {
	mu                sync.RWMutex
	sessions          map[[16]byte]*Session
	persistedSessions map[[16]byte]*StoredSession // populated at boot scan; consumed on first resume
	maxDiffs          int

	// Plan D2: when set, every new or rehydrated Session attaches an
	// atomicjson writer at <persistBasedir>/sessions/<hex-id>.json.
	persistBasedir  string
	persistDebounce time.Duration

	// Plan D2 17.B: per-ID "closing" markers serialize Close vs
	// LookupOrRehydrate for the same ID. Without this, Close drops
	// m.mu before flushing the atomicjson writer, and a concurrent
	// rehydrate could construct a fresh Session pointing at the
	// same on-disk path — two stores then race on rename(). Entries
	// are short-lived: created by Close on entry, deleted on exit.
	// LookupOrRehydrate waits while a marker exists for its ID.
	closingMu sync.Mutex
	closing   map[[16]byte]chan struct{}
}

func NewManager() *Manager {
	return &Manager{
		sessions:          make(map[[16]byte]*Session),
		persistedSessions: make(map[[16]byte]*StoredSession),
		maxDiffs:          512,
		closing:           make(map[[16]byte]chan struct{}),
	}
}

// markClosing records that id is in the middle of Manager.Close. Returns
// a channel that closes when the close completes. If id is already
// being closed, returns the existing channel (no-op).
func (m *Manager) markClosing(id [16]byte) chan struct{} {
	m.closingMu.Lock()
	defer m.closingMu.Unlock()
	if ch, ok := m.closing[id]; ok {
		return ch
	}
	ch := make(chan struct{})
	m.closing[id] = ch
	return ch
}

// unmarkClosing signals completion of Manager.Close for id and removes
// the marker.
func (m *Manager) unmarkClosing(id [16]byte) {
	m.closingMu.Lock()
	ch, ok := m.closing[id]
	if ok {
		delete(m.closing, id)
	}
	m.closingMu.Unlock()
	if ok {
		close(ch)
	}
}

// waitClosing blocks until any in-flight Close for id has completed.
// Returns immediately if no Close is in flight. Used by
// LookupOrRehydrate to avoid the 17.B rename race.
func (m *Manager) waitClosing(id [16]byte) {
	m.closingMu.Lock()
	ch, ok := m.closing[id]
	m.closingMu.Unlock()
	if ok {
		<-ch
	}
}

func (m *Manager) NewSession() (*Session, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		// Plan D2 17.C: log at the point of failure so operators
		// investigating "users can't connect" have a breadcrumb pointing
		// at the entropy pool. Without this, the error propagates up to
		// the handshake and surfaces as a generic "connect failed".
		log.Printf("session: crypto/rand failed: %v", err)
		return nil, err
	}

	m.mu.Lock()
	session := NewSession(id, m.maxDiffs)
	if m.persistBasedir != "" {
		session.AttachWriter(SessionFilePath(m.persistBasedir, id), m.persistDebounce)
	}
	m.sessions[id] = session
	m.mu.Unlock()
	return session, nil
}

// ErrSessionAlreadyExists is returned by NewSessionWithID when the
// requested ID is already in the live session map.
var ErrSessionAlreadyExists = errors.New("server: session already exists")

// NewSessionWithID creates a session with a caller-supplied ID. Used
// by:
//   - tests that need deterministic IDs.
//   - future Plan F session-recovery code that constructs a Session
//     from a persisted record.
//
// Returns ErrSessionAlreadyExists if a live session with that ID is
// already in the manager. Does NOT consume from the persistedSessions
// index — for that path, use LookupOrRehydrate.
func (m *Manager) NewSessionWithID(id [16]byte) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[id]; exists {
		return nil, ErrSessionAlreadyExists
	}
	session := NewSession(id, m.maxDiffs)
	if m.persistBasedir != "" {
		session.AttachWriter(SessionFilePath(m.persistBasedir, id), m.persistDebounce)
	}
	m.sessions[id] = session
	return session, nil
}

func (m *Manager) Lookup(id [16]byte) (*Session, error) {
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

// SetPersistedSessions seeds the rehydration index. Typically called
// once at boot from server_boot.go after ScanSessionsDir runs. Replaces
// any prior index — callers should pass the full result of the scan.
//
// In production code, prefer EnablePersistence (see Task 10), which
// performs scan + index seed + writer-path config atomically. This
// method is exposed primarily for tests that want to inject a
// hand-constructed index.
func (m *Manager) SetPersistedSessions(loaded map[[16]byte]*StoredSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistedSessions = make(map[[16]byte]*StoredSession, len(loaded))
	for id, s := range loaded {
		m.persistedSessions[id] = s
	}
}

// LookupOrRehydrate returns an existing live Session, or rehydrates
// one from the persisted index if present. The persisted entry is
// consumed (removed from the index) on rehydration; subsequent writes
// flow through the live Session's writer. Returns ErrSessionNotFound
// when the ID is unknown to both live and persisted maps.
//
// The rehydrated bool is true when a fresh Session was constructed
// from disk (the daemon-restart resume case), false when an existing
// live Session in the cache is returned (a regular in-process resume).
// Callers care about this distinction because rehydrated sessions
// have empty diff queues and revision/sequence counters that start at
// 0, while live sessions retain their accumulated counters across
// reconnects.
func (m *Manager) LookupOrRehydrate(id [16]byte) (sess *Session, rehydrated bool, err error) {
	// Plan D2 17.B: wait out any in-flight Close for the same ID
	// before consulting the persisted index. Otherwise a fresh
	// rehydrate could construct a new Session pointing at the same
	// on-disk file the closing session is still flushing, causing
	// two atomicjson stores to race on rename(). The wait is bounded
	// by Close's own duration (one synchronous flush).
	m.waitClosing(id)

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		return s, false, nil
	}
	stored, ok := m.persistedSessions[id]
	if !ok {
		return nil, false, ErrSessionNotFound
	}
	delete(m.persistedSessions, id)
	sess = NewSession(id, m.maxDiffs)
	if m.persistBasedir != "" {
		sess.AttachWriter(SessionFilePath(m.persistBasedir, id), m.persistDebounce)
	}
	// Pre-seed viewports from disk so the publisher has a clip window
	// even before the client's MsgResumeRequest arrives. The client's
	// fresher PaneViewports overwrite these via Session.ApplyResume.
	// Use the locked accessor — never write to byPaneID directly.
	sess.viewports.ApplyPreSeed(stored.PaneViewports)
	// Seed Plan F metadata from disk. Without this, the next write
	// after rehydrate (e.g. via ApplyViewportUpdate → schedulePersist)
	// would overwrite Pinned/Label/PaneCount/FirstPaneTitle with their
	// zero values, silently clobbering what was on disk.
	sess.storedMu.Lock()
	sess.storedMeta.pinned = stored.Pinned
	sess.storedMeta.label = stored.Label
	sess.storedMeta.paneCount = stored.PaneCount
	sess.storedMeta.firstPaneTitle = stored.FirstPaneTitle
	sess.storedMu.Unlock()
	m.sessions[id] = sess
	return sess, true, nil
}

// EnablePersistence is the single public entry point that wires Plan D2
// cross-restart persistence. Performs:
//
//  1. ScanSessionsDir(<basedir>) — disk I/O, runs OUTSIDE m.mu so a
//     slow filesystem cannot block other Manager methods. Safe because
//     this method is called once during boot before the listener
//     accepts any connection (see "Boot-scan-before-listener
//     invariant" in the spec). No concurrent caller exists at boot.
//  2. Under m.mu: install basedir/debounce on Manager and seed
//     persistedSessions from the scan result. The lock-protected
//     block is constant-time over the result-size copy.
//
// CALLERS MUST INVOKE THIS BEFORE STARTING THE LISTENER. Any
// MsgResumeRequest arriving during the scan window would otherwise
// falsely return ErrSessionNotFound and the client would wipe its
// persisted state — silently losing the very state D2 exists to
// preserve.
//
// debounce: typically 250ms in prod and 25ms in tests.
//
// Returns the boot-scan error (if any) so callers can decide whether to
// continue without persistence or abort startup. SetPersistedSessions
// is still exposed for tests that need to inject a hand-built index.
func (m *Manager) EnablePersistence(basedir string, debounce time.Duration) error {
	if basedir == "" {
		return nil
	}
	loaded, err := ScanSessionsDir(basedir)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistBasedir = basedir
	m.persistDebounce = debounce
	m.persistedSessions = make(map[[16]byte]*StoredSession, len(loaded))
	for id, s := range loaded {
		m.persistedSessions[id] = s
	}
	if len(loaded) > 0 {
		log.Printf("[BOOT] EnablePersistence: loaded %d persisted session(s) from %s", len(loaded), basedir)
	}
	return nil
}

// SetDiffRetentionLimit applies the new limit to all live sessions.
// Capture the slice under m.mu, then walk it without the lock — the
// per-session call may take a per-session lock and we don't want to
// block other Manager ops on that.
func (m *Manager) SetDiffRetentionLimit(limit int) {
	if limit < 0 {
		limit = 0
	}
	m.mu.Lock()
	m.maxDiffs = limit
	live := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		live = append(live, s)
	}
	m.mu.Unlock()
	for _, s := range live {
		s.setMaxDiffs(limit)
	}
}

// Close removes the session from the live map and tears it down. The
// teardown call (which now blocks on disk I/O via the atomicjson
// writer's flush) runs OUTSIDE m.mu so other Manager methods don't
// stall behind a slow flush.
//
// Plan D2 17.B: Close registers a per-ID "closing" marker before
// dropping m.mu and clears it after session.Close returns.
// LookupOrRehydrate consults the same marker so a fresh resume for
// the same ID waits out the disk flush instead of constructing a
// new Session pointing at the same on-disk path.
func (m *Manager) Close(id [16]byte) {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	if !ok {
		m.mu.Unlock()
		return
	}
	// Mark before dropping m.mu so any LookupOrRehydrate that grabs
	// m.mu next sees the marker via waitClosing on its way in.
	m.markClosing(id)
	m.mu.Unlock()
	defer m.unmarkClosing(id)
	session.Close() // disk flush — outside m.mu
}

// ShutdownSessions closes all live sessions, synchronously flushing
// each session's debounced atomicjson writer to disk. Called from
// Server.Stop so viewport updates debounced within the persistDebounce
// window (typically 250ms) before SIGINT/SIGTERM are preserved across
// a daemon restart. Without this, those updates would only exist in
// memory and the next boot would resume to a stale viewport — exactly
// the failure mode Plan D2 exists to prevent.
//
// The walk swaps the live map under m.mu, then drops the lock before
// per-session Close calls (matching the existing Close lock-discipline
// pattern). Callers should ensure the listener has stopped accepting
// new connections before invoking, otherwise a freshly-accepted
// connection's NewSession call could populate the now-empty map mid-
// shutdown. In production Server.Stop closes the listener first.
func (m *Manager) ShutdownSessions() {
	m.mu.Lock()
	live := m.sessions
	m.sessions = make(map[[16]byte]*Session)
	// Mark every live session as closing under m.mu so any concurrent
	// LookupOrRehydrate after we release m.mu blocks until the per-
	// session flush completes (Plan D2 17.B).
	for id := range live {
		m.markClosing(id)
	}
	m.mu.Unlock()

	for id, session := range live {
		session.Close() // disk flush — outside m.mu
		m.unmarkClosing(id)
	}
}

func (m *Manager) ActiveSessions() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *Manager) SessionStats() []SessionStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := make([]SessionStats, 0, len(m.sessions))
	for _, session := range m.sessions {
		stats = append(stats, session.Stats())
	}
	return stats
}
