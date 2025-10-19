package main

import (
	"sync"
	"time"
)

type geometryManager struct {
	mu         sync.RWMutex
	effects    []GeometryEffect
	renderCh   chan<- struct{}
	frameMu    sync.Mutex
	frameTimer *time.Timer
}

func newGeometryManager() *geometryManager {
	return &geometryManager{
		effects: make([]GeometryEffect, 0),
	}
}

func (m *geometryManager) attachRenderChannel(ch chan<- struct{}) {
	m.frameMu.Lock()
	m.renderCh = ch
	if m.frameTimer != nil {
		m.frameTimer.Stop()
		m.frameTimer = nil
	}
	m.frameMu.Unlock()
}

func (m *geometryManager) registerEffect(effect GeometryEffect) {
	if m == nil || effect == nil {
		return
	}
	m.mu.Lock()
	m.effects = append(m.effects, effect)
	m.mu.Unlock()
}

func (m *geometryManager) HandleTrigger(trigger EffectTrigger) {
	if m == nil {
		return
	}
	m.mu.RLock()
	effects := append([]GeometryEffect(nil), m.effects...)
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.HandleTrigger(trigger)
	}
	m.requestFrame()
}

func (m *geometryManager) Update(now time.Time) {
	if m == nil {
		return
	}
	m.mu.RLock()
	effects := append([]GeometryEffect(nil), m.effects...)
	m.mu.RUnlock()
	requireFrame := false
	for _, eff := range effects {
		eff.Update(now)
		if eff.Active() {
			requireFrame = true
		}
	}
	if requireFrame {
		m.requestFrame()
	}
}

func (m *geometryManager) Apply(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState) {
	if m == nil {
		return
	}
	m.mu.RLock()
	effects := append([]GeometryEffect(nil), m.effects...)
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.ApplyGeometry(panes, workspace)
	}
}

func (m *geometryManager) requestFrame() {
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
