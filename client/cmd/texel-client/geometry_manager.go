package main

import (
	"sync"
	"time"
)

type geometryManager struct {
	mu         sync.RWMutex
	cfg        geometryConfig
	effects    map[string]GeometryEffect
	factories  map[string]func(geometryConfig) GeometryEffect
	renderCh   chan<- struct{}
	frameMu    sync.Mutex
	frameTimer *time.Timer
}

func newGeometryManager(cfg geometryConfig) *geometryManager {
	return &geometryManager{
		cfg:       cfg,
		effects:   make(map[string]GeometryEffect),
		factories: make(map[string]func(geometryConfig) GeometryEffect),
	}
}

func (m *geometryManager) registerEffectFactory(id string, factory func(geometryConfig) GeometryEffect) {
	if id == "" || factory == nil {
		return
	}
	m.mu.Lock()
	m.factories[id] = factory
	m.mu.Unlock()
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

func (m *geometryManager) HandleTrigger(trigger EffectTrigger) {
	if m == nil {
		return
	}
	effectID := m.effectIDForTrigger(trigger.Type)
	if effectID == "" {
		return
	}
	eff := m.ensureEffect(effectID)
	if eff == nil {
		return
	}
	eff.HandleTrigger(trigger)
	m.requestFrame()
}

func (m *geometryManager) Update(now time.Time) {
	if m == nil {
		return
	}
	m.mu.RLock()
	effects := make([]GeometryEffect, 0, len(m.effects))
	for _, eff := range m.effects {
		if eff != nil {
			effects = append(effects, eff)
		}
	}
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
	effects := make([]GeometryEffect, 0, len(m.effects))
	for _, eff := range m.effects {
		if eff != nil {
			effects = append(effects, eff)
		}
	}
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.ApplyGeometry(panes, workspace)
	}
}

func (m *geometryManager) effectIDForTrigger(t EffectTriggerType) string {
	switch t {
	case TriggerPaneCreated, TriggerPaneGeometry:
		return m.cfg.SplitEffect
	case TriggerPaneRemoved:
		return m.cfg.RemoveEffect
	case TriggerWorkspaceZoom:
		return m.cfg.ZoomEffect
	default:
		return ""
	}
}

func (m *geometryManager) ensureEffect(id string) GeometryEffect {
	m.mu.Lock()
	defer m.mu.Unlock()
	if eff, ok := m.effects[id]; ok && eff != nil {
		return eff
	}
	factory, ok := m.factories[id]
	if !ok {
		return nil
	}
	eff := factory(m.cfg)
	m.effects[id] = eff
	return eff
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
