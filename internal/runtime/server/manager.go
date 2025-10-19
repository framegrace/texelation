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
	mu       sync.RWMutex
	sessions map[[16]byte]*Session
	maxDiffs int
}

func NewManager() *Manager {
	return &Manager{sessions: make(map[[16]byte]*Session), maxDiffs: 512}
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
