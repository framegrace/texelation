package main

import (
	"sync"
	"time"
)

type ghostGeometryEffect struct {
	mu    sync.Mutex
	cfg   geometryConfig
	panes map[PaneID]*paneAnimation
}

func newGhostGrowEffect(cfg geometryConfig) GeometryEffect {
	return &ghostGeometryEffect{
		cfg:   cfg,
		panes: make(map[PaneID]*paneAnimation),
	}
}

func (e *ghostGeometryEffect) ID() string { return geometryEffectGhost }

func (e *ghostGeometryEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.panes) > 0
}

func (e *ghostGeometryEffect) Update(now time.Time) {
	e.mu.Lock()
	for id, anim := range e.panes {
		if anim == nil {
			delete(e.panes, id)
			continue
		}
		if now.Sub(anim.startTime) >= anim.duration {
			delete(e.panes, id)
		}
	}
	e.mu.Unlock()
}

func (e *ghostGeometryEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerPaneCreated && trigger.Type != TriggerPaneRemoved {
		return
	}
	now := time.Now()
	e.mu.Lock()
	anim := &paneAnimation{
		start:     trigger.OldRect,
		end:       trigger.NewRect,
		startTime: now,
		duration:  e.durationForTrigger(trigger.Type),
		buffer:    trigger.PaneBuffer,
		ghost:     true,
		forceTop:  true,
	}
	e.panes[trigger.PaneID] = anim
	e.mu.Unlock()
}

func (e *ghostGeometryEffect) durationForTrigger(t EffectTriggerType) time.Duration {
	switch t {
	case TriggerPaneCreated:
		return e.cfg.SplitDuration
	case TriggerPaneRemoved:
		return e.cfg.RemoveDuration
	default:
		return 160 * time.Millisecond
	}
}

func (e *ghostGeometryEffect) ApplyGeometry(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState) {
	e.mu.Lock()
	now := time.Now()
	for id, anim := range e.panes {
		if anim == nil {
			continue
		}
		progress := calcProgress(now, anim.startTime, anim.duration)
		rect := lerpRect(anim.start, anim.end, progress)
		state := panes[id]
		if state == nil {
			state = &geometryPaneState{Pane: nil, Base: rect, Rect: rect, Ghost: true}
			panes[id] = state
		}
		state.Rect = rect
		state.Ghost = true
		state.ZIndex = maxInt(state.ZIndex, 900)
		if anim.buffer != nil {
			state.Buffer = anim.buffer
		}
	}
	e.mu.Unlock()
}
