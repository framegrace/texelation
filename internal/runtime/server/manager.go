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
}

func NewManager() *Manager {
	return &Manager{
		sessions:          make(map[[16]byte]*Session),
		persistedSessions: make(map[[16]byte]*StoredSession),
		maxDiffs:          512,
	}
}

func (m *Manager) NewSession() (*Session, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
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
func (m *Manager) LookupOrRehydrate(id [16]byte) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		return s, nil
	}
	stored, ok := m.persistedSessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	delete(m.persistedSessions, id)
	sess := NewSession(id, m.maxDiffs)
	if m.persistBasedir != "" {
		sess.AttachWriter(SessionFilePath(m.persistBasedir, id), m.persistDebounce)
	}
	// Pre-seed viewports from disk so the publisher has a clip window
	// even before the client's MsgResumeRequest arrives. The client's
	// fresher PaneViewports overwrite these via Session.ApplyResume.
	// Use the locked accessor — never write to byPaneID directly.
	sess.viewports.ApplyPreSeed(stored.PaneViewports)
	m.sessions[id] = sess
	return sess, nil
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
func (m *Manager) Close(id [16]byte) {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		session.Close() // disk flush — outside m.mu
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
