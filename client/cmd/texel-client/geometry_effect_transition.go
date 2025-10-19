package main

import (
	"sync"
	"time"

	"texelation/client"
)

type paneAnimation struct {
	start     PaneRect
	end       PaneRect
	startTime time.Time
	duration  time.Duration
	buffer    [][]client.Cell
	ghost     bool
	forceTop  bool
}

type zoomAnimation struct {
	paneID    PaneID
	start     PaneRect
	end       PaneRect
	startTime time.Time
	duration  time.Duration
	active    bool
}

type geometryTransitionEffect struct {
	mu         sync.Mutex
	panes      map[[16]byte]*paneAnimation
	zoom       *zoomAnimation
	lastUpdate time.Time
	cfg        geometryConfig
}

func newGeometryTransitionEffect(cfg geometryConfig) *geometryTransitionEffect {
	return &geometryTransitionEffect{
		panes: make(map[[16]byte]*paneAnimation),
		cfg:   cfg,
	}
}

func (e *geometryTransitionEffect) ID() string { return "geometry-transitions" }

func (e *geometryTransitionEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.zoom != nil && e.zoom.active {
		return true
	}
	return len(e.panes) > 0
}

func (e *geometryTransitionEffect) Update(now time.Time) {
	e.mu.Lock()
	e.lastUpdate = now
	for id, anim := range e.panes {
		if anim == nil {
			continue
		}
		if now.Sub(anim.startTime) >= anim.duration {
			delete(e.panes, id)
		}
	}
	if e.zoom != nil && e.zoom.active && now.Sub(e.zoom.startTime) >= e.zoom.duration {
		e.zoom.active = false
	}
	e.mu.Unlock()
}

func (e *geometryTransitionEffect) HandleTrigger(trigger EffectTrigger) {
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	switch trigger.Type {
	case TriggerPaneCreated, TriggerPaneGeometry:
		anim := &paneAnimation{
			start:     trigger.OldRect,
			end:       trigger.NewRect,
			startTime: now,
			duration:  e.cfg.SplitDuration,
		}
		if e.cfg.SplitMode == splitModeGhost && trigger.Type == TriggerPaneCreated {
			anim.ghost = true
			anim.forceTop = true
		}
		e.panes[trigger.PaneID] = anim
	case TriggerPaneRemoved:
		anim := &paneAnimation{
			start:     trigger.OldRect,
			end:       trigger.NewRect,
			startTime: now,
			duration:  e.cfg.RemoveDuration,
			buffer:    trigger.PaneBuffer,
			ghost:     trigger.Ghost,
			forceTop:  trigger.Ghost,
		}
		if !anim.ghost && len(anim.buffer) > 0 {
			anim.ghost = true
		}
		e.panes[trigger.PaneID] = anim
	case TriggerWorkspaceZoom:
		zoom := &zoomAnimation{
			paneID:    trigger.PaneID,
			start:     trigger.OldRect,
			end:       trigger.NewRect,
			startTime: now,
			duration:  e.cfg.ZoomDuration,
			active:    true,
		}
		e.zoom = zoom
		if !trigger.Active {
			zoom.start, zoom.end = trigger.OldRect, trigger.NewRect
		}
	}
}

func (e *geometryTransitionEffect) ApplyGeometry(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState) {
	e.mu.Lock()
	now := e.lastUpdate
	if now.IsZero() {
		now = time.Now()
	}
	for id, anim := range e.panes {
		if anim == nil {
			continue
		}
		progress := calcProgress(now, anim.startTime, anim.duration)
		rect := lerpRect(anim.start, anim.end, progress)
		if anim.ghost {
			state := panes[id]
			if state == nil {
				state = &geometryPaneState{Pane: nil, Base: anim.end, Rect: rect, Buffer: anim.buffer, Ghost: true}
				panes[id] = state
			}
			state.Rect = rect
			state.Buffer = anim.buffer
			if anim.forceTop {
				state.ZIndex = 900
			}
		} else if state := panes[id]; state != nil {
			state.Rect = rect
			if anim.forceTop {
				state.ZIndex = 900
			}
		}
	}
	if e.zoom != nil && e.zoom.active {
		progress := calcProgress(now, e.zoom.startTime, e.zoom.duration)
		rect := lerpRect(e.zoom.start, e.zoom.end, progress)
		if state := panes[e.zoom.paneID]; state != nil {
			state.Rect = rect
			state.ZIndex = 1000
		}
	}
	e.mu.Unlock()
}

func calcProgress(now, start time.Time, duration time.Duration) float32 {
	if duration <= 0 {
		return 1
	}
	elapsed := now.Sub(start)
	if elapsed <= 0 {
		return 0
	}
	if elapsed >= duration {
		return 1
	}
	return easeInOutQuad(float32(elapsed) / float32(duration))
}

func lerpRect(a, b PaneRect, t float32) PaneRect {
	return PaneRect{
		X:      int(float32(a.X) + (float32(b.X-a.X) * t)),
		Y:      int(float32(a.Y) + (float32(b.Y-a.Y) * t)),
		Width:  int(float32(a.Width) + (float32(b.Width-a.Width) * t)),
		Height: int(float32(a.Height) + (float32(b.Height-a.Height) * t)),
	}
}

func easeInOutQuad(t float32) float32 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	if t < 0.5 {
		return 2 * t * t
	}
	return -1 + (4-2*t)*t
}
