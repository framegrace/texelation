package main

import (
	"sync"
	"time"
)

type stretchGeometryEffect struct {
	mu    sync.Mutex
	cfg   geometryConfig
	panes map[PaneID]*paneAnimation
	peers map[PaneID]PaneID
}

func newStretchEffect(cfg geometryConfig) GeometryEffect {
	return &stretchGeometryEffect{
		cfg:   cfg,
		panes: make(map[PaneID]*paneAnimation),
		peers: make(map[PaneID]PaneID),
	}
}

func (e *stretchGeometryEffect) ID() string { return geometryEffectStretch }

func (e *stretchGeometryEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.panes) > 0
}

func (e *stretchGeometryEffect) Update(now time.Time) {
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

func (e *stretchGeometryEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerPaneCreated && trigger.Type != TriggerPaneGeometry {
		return
	}
	now := time.Now()
	e.mu.Lock()
	anim := &paneAnimation{
		start:     trigger.OldRect,
		end:       trigger.NewRect,
		startTime: now,
		duration:  e.cfg.SplitDuration,
	}
	e.panes[trigger.PaneID] = anim
	e.peers[trigger.PaneID] = trigger.RelatedPaneID
	e.mu.Unlock()
}

func (e *stretchGeometryEffect) ApplyGeometry(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState) {
	e.mu.Lock()
	now := time.Now()
	for id, anim := range e.panes {
		if anim == nil {
			continue
		}
		progress := calcProgress(now, anim.startTime, anim.duration)
		rect := lerpRect(anim.start, anim.end, progress)
		if state := panes[id]; state != nil {
			state.Rect = rect
			state.Dirty = true
		}
		if peerID := e.peers[id]; peerID != ([16]byte{}) {
			if peerState := panes[peerID]; peerState != nil {
				peerRect := adjustPeerRect(peerState.Base, rect)
				peerState.Rect = peerRect
				peerState.Dirty = true
			}
		}
	}
	e.mu.Unlock()
}
