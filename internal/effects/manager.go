// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/manager.go
// Summary: Implements manager capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate manager visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"sync"
	"time"

	"github.com/framegrace/texelation/client"
)

// Binding associates an effect instance with a target and trigger.
type Binding struct {
	Effect Effect
	Target Target
	Event  EffectTriggerType
}

// Manager coordinates all configured effects, routing triggers to the appropriate bindings.
type Manager struct {
	mu               sync.RWMutex
	bindings         map[EffectTriggerType][]Effect
	paneEffects      []Effect
	workspaceEffects []Effect
	effectsClock     time.Time
	wakeCh           chan<- struct{}
	initializing     bool      // true during initial connect
	initTimestamp    time.Time // past timestamp for snapping effects

	// Cached active state — set by Update(), read by HasActive*() on same goroutine.
	cachedHasActive    bool
	cachedHasPane      bool
	cachedHasWorkspace bool
}

// NewManager constructs an empty effect manager.
func NewManager() *Manager {
	return &Manager{
		bindings:         make(map[EffectTriggerType][]Effect),
		paneEffects:      make([]Effect, 0),
		workspaceEffects: make([]Effect, 0),
		effectsClock:     time.Now(),
		initializing:     true,
		initTimestamp:    time.Now().Add(-10 * time.Second),
	}
}

// SetWakeChannel sets the channel used to wake the game loop when effects need attention.
func (m *Manager) SetWakeChannel(ch chan<- struct{}) {
	m.mu.Lock()
	m.wakeCh = ch
	m.mu.Unlock()
}

// RegisterBinding wires an effect to a trigger and target scope.
func (m *Manager) RegisterBinding(binding Binding) {
	if binding.Effect == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	switch binding.Target {
	case TargetPane:
		m.paneEffects = append(m.paneEffects, binding.Effect)
	case TargetWorkspace:
		m.workspaceEffects = append(m.workspaceEffects, binding.Effect)
	}
	m.bindings[binding.Event] = append(m.bindings[binding.Event], binding.Effect)
}

// Update ticks all effects so animations can advance.
// dt is the elapsed time since the last update; the manager converts it
// to a synthetic clock value that effects receive via Update(now time.Time).
func (m *Manager) Update(dt time.Duration) {
	if m == nil {
		return
	}
	if dt > 0 {
		// Tick render: advance by exact fixed timestep.
		m.effectsClock = m.effectsClock.Add(dt)
	} else {
		// Data render: sync to wall clock so timeline's Active() checks
		// (which use time.Now()) stay consistent with our timestamps.
		m.effectsClock = time.Now()
	}
	now := m.effectsClock

	m.mu.RLock()
	for _, eff := range m.paneEffects {
		eff.Update(now)
	}
	for _, eff := range m.workspaceEffects {
		eff.Update(now)
	}

	// Cache active state while still under lock
	m.cachedHasActive = false
	m.cachedHasPane = false
	m.cachedHasWorkspace = false
	for _, eff := range m.paneEffects {
		if eff.Active() {
			m.cachedHasActive = true
			m.cachedHasPane = true
			break
		}
	}
	for _, eff := range m.workspaceEffects {
		if eff.Active() {
			m.cachedHasActive = true
			m.cachedHasWorkspace = true
			break
		}
	}
	m.mu.RUnlock()
}

// HasActiveAnimations returns true if any effect (pane or workspace) is currently active.
// Uses cached state from the last Update() call.
func (m *Manager) HasActiveAnimations() bool {
	if m == nil {
		return false
	}
	return m.cachedHasActive
}

// HasActivePaneEffects returns true if any pane effect is currently active.
// Uses cached state from the last Update() call.
func (m *Manager) HasActivePaneEffects() bool {
	if m == nil {
		return false
	}
	return m.cachedHasPane
}

// ApplyPaneEffects mutates the pane buffer using the configured pane effects.
func (m *Manager) ApplyPaneEffects(pane *client.PaneState, buffer [][]client.Cell) {
	if m == nil {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, eff := range m.paneEffects {
		eff.ApplyPane(pane, buffer)
	}
}

// ApplyWorkspaceEffects mutates the workspace buffer using the configured workspace effects.
func (m *Manager) ApplyWorkspaceEffects(buffer [][]client.Cell) {
	if m == nil {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, eff := range m.workspaceEffects {
		eff.ApplyWorkspace(buffer)
	}
}

// HandleTrigger routes a trigger to the bound effects.
func (m *Manager) HandleTrigger(trigger EffectTrigger) {
	if m == nil {
		return
	}
	if trigger.Timestamp.IsZero() {
		trigger.Timestamp = m.effectsClock
	}
	m.mu.RLock()
	effects := m.bindings[trigger.Type]
	ch := m.wakeCh
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.HandleTrigger(trigger)
	}
	if len(effects) > 0 && ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// HasActiveWorkspaceEffects returns true if any workspace effect is currently active.
// Uses cached state from the last Update() call.
func (m *Manager) HasActiveWorkspaceEffects() bool {
	if m == nil {
		return false
	}
	return m.cachedHasWorkspace
}

// PaneStateTriggerTimestamp returns the timestamp to use for pane state triggers.
// During initial connect (before first render completes), returns a past timestamp
// so effects snap instantly. After that, returns the effectsClock for normal animation.
func (m *Manager) PaneStateTriggerTimestamp() time.Time {
	if m == nil {
		return time.Now()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.initializing {
		return m.initTimestamp
	}
	return m.effectsClock
}

// FinishInitialization marks the end of the initial connect phase.
// After this, pane state triggers use real timestamps for animation.
func (m *Manager) FinishInitialization() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.initializing = false
	m.mu.Unlock()
}

// ResetPaneStates primes pane effects with the current desktop state when the client connects.
// Uses a timestamp far in the past so animations snap to their target instantly
// rather than visibly animating on first connect.
func (m *Manager) ResetPaneStates(panes []*client.PaneState) {
	if m == nil {
		return
	}
	past := time.Now().Add(-10 * time.Second)
	for _, pane := range panes {
		if pane == nil {
			continue
		}
		m.HandleTrigger(EffectTrigger{Type: TriggerPaneActive, PaneID: pane.ID, Active: pane.Active, Timestamp: past})
		m.HandleTrigger(EffectTrigger{Type: TriggerPaneResizing, PaneID: pane.ID, Resizing: pane.Resizing, Timestamp: past})
	}
}
