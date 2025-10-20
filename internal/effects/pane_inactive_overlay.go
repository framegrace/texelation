// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/pane_inactive_overlay.go
// Summary: Implements pane inactive overlay capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate pane inactive overlay visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

type paneTintEffect struct {
	color     tcell.Color
	intensity float32
	duration  time.Duration
	timelines map[[16]byte]*fadeTimeline
	mu        sync.Mutex
}

func newPaneTintEffect(color tcell.Color, intensity float32, duration time.Duration) Effect {
	if intensity < 0 {
		intensity = 0
	} else if intensity > 1 {
		intensity = 1
	}
	if duration < 0 {
		duration = 0
	}
	return &paneTintEffect{
		color:     color,
		intensity: intensity,
		duration:  duration,
		timelines: make(map[[16]byte]*fadeTimeline),
	}
}

func (e *paneTintEffect) ID() string { return "fadeTint" }

func (e *paneTintEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tl := range e.timelines {
		if tl.animating || tl.current > 0 {
			return true
		}
	}
	return false
}

func (e *paneTintEffect) Update(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, timeline := range e.timelines {
		timeline.valueAt(now)
	}
}

func (e *paneTintEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerPaneActive && trigger.Type != TriggerPaneResizing {
		return
	}
	e.mu.Lock()
	timeline := e.timelines[trigger.PaneID]
	if timeline == nil {
		timeline = &fadeTimeline{}
		e.timelines[trigger.PaneID] = timeline
	}

	target := float32(0)
	switch trigger.Type {
	case TriggerPaneActive:
		if !trigger.Active {
			target = e.intensity
		}
	case TriggerPaneResizing:
		if trigger.Resizing {
			target = e.intensity
		}
	}

	when := trigger.Timestamp
	if when.IsZero() {
		when = time.Now()
	}
	if !timeline.initialized {
		timeline.setInstant(target, e.duration, when)
	} else {
		current, _ := timeline.valueAt(when)
		timeline.startAnimation(current, target, e.duration, when)
	}
	e.mu.Unlock()
}

func (e *paneTintEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {
	if pane == nil {
		return
	}
	e.mu.Lock()
	timeline := e.timelines[pane.ID]
	if timeline == nil {
		if pane.Active {
			e.mu.Unlock()
			return
		}
		timeline = &fadeTimeline{}
		e.timelines[pane.ID] = timeline
		timeline.setInstant(e.intensity, e.duration, time.Now())
	}
	intensity := timeline.current
	e.mu.Unlock()
	if intensity <= 0 {
		return
	}
	for y := range buffer {
		row := buffer[y]
		for x := range row {
			cell := &row[x]
			cell.Style = tintStyle(cell.Style, e.color, intensity)
		}
	}
}

func (e *paneTintEffect) ApplyWorkspace(buffer [][]client.Cell) {}

func init() {
	Register("fadeTint", func(cfg EffectConfig) (Effect, error) {
		color := parseColorOrDefault(cfg, "color", defaultInactiveColor)
		intensity := float32(parseFloatOrDefault(cfg, "intensity", 0.35))
		duration := parseDurationOrDefault(cfg, "duration_ms", 400)
		return newPaneTintEffect(color, intensity, duration), nil
	})
}
