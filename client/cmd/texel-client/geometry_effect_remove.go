package main

import (
	"sync"
	"time"
)

type removeGeometryEffect struct {
	mu         sync.Mutex
	cfg        geometryConfig
	animations map[PaneID]*paneAnimation
}

func newRemoveGeometryEffect(cfg geometryConfig) GeometryEffect {
	return &removeGeometryEffect{
		cfg:        cfg,
		animations: make(map[PaneID]*paneAnimation),
	}
}

func (e *removeGeometryEffect) ID() string { return "geometry-remove" }

func (e *removeGeometryEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.animations) > 0
}

func (e *removeGeometryEffect) Update(now time.Time) {
	e.mu.Lock()
	for id, anim := range e.animations {
		if anim == nil {
			delete(e.animations, id)
			continue
		}
		if now.Sub(anim.startTime) >= anim.duration {
			delete(e.animations, id)
		}
	}
	e.mu.Unlock()
}

func (e *removeGeometryEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerPaneRemoved {
		return
	}
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	ghost := e.cfg.RemoveMode == removeModeGhost
	if trigger.Ghost {
		ghost = true
	}
	anim := &paneAnimation{
		start:     trigger.OldRect,
		end:       trigger.NewRect,
		startTime: now,
		duration:  e.cfg.RemoveDuration,
		buffer:    trigger.PaneBuffer,
		ghost:     ghost,
		forceTop:  ghost,
	}
	e.animations[trigger.PaneID] = anim
}

func (e *removeGeometryEffect) ApplyGeometry(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState) {
	e.mu.Lock()
	now := time.Now()
	for id, anim := range e.animations {
		if anim == nil {
			continue
		}
		progress := calcProgress(now, anim.startTime, anim.duration)
		rect := lerpRect(anim.start, anim.end, progress)
		state := panes[id]
		if state == nil {
			state = &geometryPaneState{Base: anim.end, Rect: rect, Ghost: anim.ghost}
			panes[id] = state
		}
		state.Rect = rect
		if anim.buffer != nil {
			state.Buffer = anim.buffer
		}
		if anim.ghost {
			state.Ghost = true
			state.ZIndex = 900
		}
	}
	e.mu.Unlock()
}
