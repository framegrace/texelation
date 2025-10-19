package main

import (
	"sync"
	"time"
)

type zoomGeometryEffect struct {
	mu   sync.Mutex
	cfg  geometryConfig
	zoom *zoomAnimation
}

func newZoomGeometryEffect(cfg geometryConfig) GeometryEffect {
	return &zoomGeometryEffect{cfg: cfg}
}

func (e *zoomGeometryEffect) ID() string { return "geometry-zoom" }

func (e *zoomGeometryEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.zoom != nil && e.zoom.active
}

func (e *zoomGeometryEffect) Update(now time.Time) {
	e.mu.Lock()
	if e.zoom != nil && e.zoom.active && now.Sub(e.zoom.startTime) >= e.zoom.duration {
		e.zoom.active = false
	}
	e.mu.Unlock()
}

func (e *zoomGeometryEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerWorkspaceZoom {
		return
	}
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	z := &zoomAnimation{
		paneID:    trigger.PaneID,
		start:     trigger.OldRect,
		end:       trigger.NewRect,
		startTime: now,
		duration:  e.cfg.ZoomDuration,
		active:    true,
	}
	if !trigger.Active {
		z.start, z.end = trigger.OldRect, trigger.NewRect
	}
	e.zoom = z
}

func (e *zoomGeometryEffect) ApplyGeometry(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.zoom == nil || !e.zoom.active {
		return
	}
	progress := calcProgress(time.Now(), e.zoom.startTime, e.zoom.duration)
	rect := lerpRect(e.zoom.start, e.zoom.end, progress)
	if state := panes[e.zoom.paneID]; state != nil {
		state.Rect = rect
		state.ZIndex = 1000
	}
}
