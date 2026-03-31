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
	renderCh         chan<- struct{}
	frameMu          sync.Mutex
	frameTimer       *time.Timer
	initializing     bool      // true during initial connect
	initTimestamp    time.Time // past timestamp for snapping effects
}

// NewManager constructs an empty effect manager.
func NewManager() *Manager {
	return &Manager{
		bindings:         make(map[EffectTriggerType][]Effect),
		paneEffects:      make([]Effect, 0),
		workspaceEffects: make([]Effect, 0),
		initializing:     true,
		initTimestamp:    time.Now().Add(-10 * time.Second),
	}
}

// AttachRenderChannel allows the manager to request additional frames for animated effects.
func (m *Manager) AttachRenderChannel(ch chan<- struct{}) {
	m.frameMu.Lock()
	m.renderCh = ch
	if m.frameTimer != nil {
		m.frameTimer.Stop()
		m.frameTimer = nil
	}
	m.frameMu.Unlock()
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

func (m *Manager) requestFrame() {
	m.frameMu.Lock()
	if m.renderCh == nil {
		m.frameMu.Unlock()
		return
	}
	if m.frameTimer != nil {
		m.frameMu.Unlock()
		return
	}
	ch := m.renderCh
	m.frameTimer = time.AfterFunc(16*time.Millisecond, func() {
		// Clear timer BEFORE sending so RequestFrame() can schedule the
		// next frame immediately when the render goroutine calls it.
		m.frameMu.Lock()
		m.frameTimer = nil
		m.frameMu.Unlock()
		select {
		case ch <- struct{}{}:
		default:
		}
	})
	m.frameMu.Unlock()
}

// RequestFrame schedules a render after one frame interval (~16ms).
// Safe to call multiple times; only one timer runs at a time.
func (m *Manager) RequestFrame() {
	m.requestFrame()
}

// Update ticks all effects so animations can advance.
func (m *Manager) Update(now time.Time) {
	if m == nil {
		return
	}
	m.mu.RLock()
	panes := append([]Effect(nil), m.paneEffects...)
	workspaces := append([]Effect(nil), m.workspaceEffects...)
	m.mu.RUnlock()

	needsFrame := false
	for _, eff := range panes {
		eff.Update(now)
		if eff.Active() {
			needsFrame = true
		}
	}
	for _, eff := range workspaces {
		eff.Update(now)
		if eff.Active() {
			needsFrame = true
		}
	}
	if needsFrame {
		m.requestFrame()
	}
}

// HasActivePaneEffects returns true if any pane effect is currently active.
func (m *Manager) HasActivePaneEffects() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, eff := range m.paneEffects {
		if eff.Active() {
			return true
		}
	}
	return false
}

// ApplyPaneEffects mutates the pane buffer using the configured pane effects.
func (m *Manager) ApplyPaneEffects(pane *client.PaneState, buffer [][]client.Cell) {
	if m == nil {
		return
	}
	m.mu.RLock()
	effects := append([]Effect(nil), m.paneEffects...)
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.ApplyPane(pane, buffer)
	}
}

// ApplyWorkspaceEffects mutates the workspace buffer using the configured workspace effects.
func (m *Manager) ApplyWorkspaceEffects(buffer [][]client.Cell) {
	if m == nil {
		return
	}
	m.mu.RLock()
	effects := append([]Effect(nil), m.workspaceEffects...)
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.ApplyWorkspace(buffer)
	}
}

// HandleTrigger routes a trigger to the bound effects.
func (m *Manager) HandleTrigger(trigger EffectTrigger) {
	if m == nil {
		return
	}
	if trigger.Timestamp.IsZero() {
		trigger.Timestamp = time.Now()
	}
	m.mu.RLock()
	effects := append([]Effect(nil), m.bindings[trigger.Type]...)
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.HandleTrigger(trigger)
	}
	if len(effects) > 0 {
		m.requestFrame()
	}
}

// HasActiveWorkspaceEffects returns true if any workspace effect is currently active.
func (m *Manager) HasActiveWorkspaceEffects() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, eff := range m.workspaceEffects {
		if eff.Active() {
			return true
		}
	}
	return false
}

// PaneStateTriggerTimestamp returns the timestamp to use for pane state triggers.
// During initial connect (before first render completes), returns a past timestamp
// so effects snap instantly. After that, returns time.Now() for normal animation.
func (m *Manager) PaneStateTriggerTimestamp() time.Time {
	if m == nil {
		return time.Now()
	}
	m.frameMu.Lock()
	defer m.frameMu.Unlock()
	if m.initializing {
		return m.initTimestamp
	}
	return time.Now()
}

// FinishInitialization marks the end of the initial connect phase.
// After this, pane state triggers use real timestamps for animation.
func (m *Manager) FinishInitialization() {
	if m == nil {
		return
	}
	m.frameMu.Lock()
	m.initializing = false
	m.frameMu.Unlock()
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
