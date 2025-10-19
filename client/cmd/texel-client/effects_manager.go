package main

import (
	"sync"
	"time"

	"texelation/client"
)

type effectManager struct {
	mu               sync.RWMutex
	paneEffects      []PaneEffect
	workspaceEffects []WorkspaceEffect
	renderCh         chan<- struct{}
	frameMu          sync.Mutex
	frameTimer       *time.Timer
}

func newEffectManager() *effectManager {
	return &effectManager{
		paneEffects:      make([]PaneEffect, 0),
		workspaceEffects: make([]WorkspaceEffect, 0),
	}
}

func (m *effectManager) attachRenderChannel(ch chan<- struct{}) {
	m.frameMu.Lock()
	m.renderCh = ch
	if m.frameTimer != nil {
		m.frameTimer.Stop()
		m.frameTimer = nil
	}
	m.frameMu.Unlock()
}

func (m *effectManager) registerPaneEffect(effect PaneEffect) {
	m.mu.Lock()
	m.paneEffects = append(m.paneEffects, effect)
	m.mu.Unlock()
}

func (m *effectManager) registerWorkspaceEffect(effect WorkspaceEffect) {
	m.mu.Lock()
	m.workspaceEffects = append(m.workspaceEffects, effect)
	m.mu.Unlock()
}

func (m *effectManager) requestFrame() {
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
		select {
		case ch <- struct{}{}:
		default:
		}
		m.frameMu.Lock()
		m.frameTimer = nil
		m.frameMu.Unlock()
	})
	m.frameMu.Unlock()
}

func (m *effectManager) Update(now time.Time) {
	if m == nil {
		return
	}
	m.mu.RLock()
	paneEffects := append([]PaneEffect(nil), m.paneEffects...)
	workspaceEffects := append([]WorkspaceEffect(nil), m.workspaceEffects...)
	m.mu.RUnlock()
	needsFrame := false
	for _, eff := range paneEffects {
		eff.Update(now)
		if eff.Active() {
			needsFrame = true
		}
	}
	for _, eff := range workspaceEffects {
		eff.Update(now)
		if eff.Active() {
			needsFrame = true
		}
	}
	if needsFrame {
		m.requestFrame()
	}
}

func (m *effectManager) ApplyPaneEffects(pane *client.PaneState, buffer [][]client.Cell) {
	if m == nil {
		return
	}
	m.mu.RLock()
	effects := append([]PaneEffect(nil), m.paneEffects...)
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.ApplyPane(pane, buffer)
	}
}

func (m *effectManager) ApplyWorkspaceEffects(buffer [][]client.Cell) {
	if m == nil {
		return
	}
	m.mu.RLock()
	effects := append([]WorkspaceEffect(nil), m.workspaceEffects...)
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.ApplyWorkspace(buffer)
	}
}

func (m *effectManager) HandleTrigger(trigger EffectTrigger) {
	if m == nil {
		return
	}
	if trigger.Timestamp.IsZero() {
		trigger.Timestamp = time.Now()
	}
	m.mu.RLock()
	paneEffects := append([]PaneEffect(nil), m.paneEffects...)
	workspaceEffects := append([]WorkspaceEffect(nil), m.workspaceEffects...)
	m.mu.RUnlock()
	for _, eff := range paneEffects {
		eff.HandleTrigger(trigger)
	}
	for _, eff := range workspaceEffects {
		eff.HandleTrigger(trigger)
	}
	m.requestFrame()
}

func (m *effectManager) ResetPaneStates(panes []*client.PaneState) {
	if m == nil {
		return
	}
	now := time.Now()
	for _, pane := range panes {
		if pane == nil {
			continue
		}
		m.HandleTrigger(EffectTrigger{Type: TriggerPaneActive, PaneID: pane.ID, Active: pane.Active, Timestamp: now})
		m.HandleTrigger(EffectTrigger{Type: TriggerPaneResizing, PaneID: pane.ID, Resizing: pane.Resizing, Timestamp: now})
	}
}
