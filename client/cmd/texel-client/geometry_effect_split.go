package main

import (
	"sync"
	"time"
)

type splitGeometryEffect struct {
	mu         sync.Mutex
	cfg        geometryConfig
	animations map[PaneID]*paneAnimation
}

func newSplitGeometryEffect(cfg geometryConfig) GeometryEffect {
	return &splitGeometryEffect{
		cfg:        cfg,
		animations: make(map[PaneID]*paneAnimation),
	}
}

func (e *splitGeometryEffect) ID() string { return "geometry-split" }

func (e *splitGeometryEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.animations) > 0
}

func (e *splitGeometryEffect) Update(now time.Time) {
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

func (e *splitGeometryEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerPaneCreated && trigger.Type != TriggerPaneGeometry {
		return
	}

	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()

	anim := &paneAnimation{
		start:     trigger.OldRect,
		end:       trigger.NewRect,
		startTime: now,
		duration:  e.cfg.SplitDuration,
		ghost:     trigger.Ghost,
		forceTop:  trigger.Ghost,
	}
	e.animations[trigger.PaneID] = anim
}

func (e *splitGeometryEffect) ApplyGeometry(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState) {
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
			state = &geometryPaneState{Pane: nil, Base: rect, Rect: rect}
			panes[id] = state
		}
		state.Rect = rect
		if anim.ghost {
			state.Ghost = true
			state.ZIndex = 800
		}
	}
	e.mu.Unlock()
}
