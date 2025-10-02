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
}

func NewManager() *Manager {
	return &Manager{sessions: make(map[[16]byte]*Session)}
}

func (m *Manager) NewSession() (*Session, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	session := NewSession(id)

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
