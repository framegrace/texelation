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
	"sync"
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
	session := NewSession(id, m.maxDiffs)

	m.mu.Lock()
	defer m.mu.Unlock()
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
	// Pre-seed viewports from disk so the publisher has a clip window
	// even before the client's MsgResumeRequest arrives. The client's
	// fresher PaneViewports overwrite these via Session.ApplyResume.
	// Use the locked accessor — never write to byPaneID directly.
	sess.viewports.ApplyPreSeed(stored.PaneViewports)
	m.sessions[id] = sess
	return sess, nil
}

func (m *Manager) SetDiffRetentionLimit(limit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit < 0 {
		limit = 0
	}
	m.maxDiffs = limit
	for _, session := range m.sessions {
		session.setMaxDiffs(limit)
	}
}

func (m *Manager) Close(id [16]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[id]; ok {
		session.Close()
		delete(m.sessions, id)
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
