package main

import (
	"sync"
	"time"
)

type expandGeometryEffect struct {
	mu   sync.Mutex
	cfg  geometryConfig
	zoom *zoomAnimation
}

func newExpandEffect(cfg geometryConfig) GeometryEffect {
	return &expandGeometryEffect{cfg: cfg}
}

func (e *expandGeometryEffect) ID() string { return geometryEffectExpand }

func (e *expandGeometryEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.zoom != nil && e.zoom.active
}

func (e *expandGeometryEffect) Update(now time.Time) {
	e.mu.Lock()
	if e.zoom != nil && e.zoom.active && now.Sub(e.zoom.startTime) >= e.zoom.duration {
		e.zoom.active = false
	}
	e.mu.Unlock()
}

func (e *expandGeometryEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerWorkspaceZoom {
		return
	}
	now := time.Now()
	e.mu.Lock()
	e.zoom = &zoomAnimation{
		paneID:    trigger.PaneID,
		start:     trigger.OldRect,
		end:       trigger.NewRect,
		startTime: now,
		duration:  e.cfg.ZoomDuration,
		active:    true,
	}
	if !trigger.Active {
		e.zoom.start, e.zoom.end = trigger.OldRect, trigger.NewRect
	}
	e.mu.Unlock()
}

func (e *expandGeometryEffect) ApplyGeometry(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState) {
	e.mu.Lock()
	zoom := e.zoom
	e.mu.Unlock()
	if zoom == nil || !zoom.active {
		return
	}
	progress := calcProgress(time.Now(), zoom.startTime, zoom.duration)
	rect := lerpRect(zoom.start, zoom.end, progress)
	if state := panes[zoom.paneID]; state != nil {
		state.Rect = rect
		state.ZIndex = maxInt(state.ZIndex, 1000)
	}
}
