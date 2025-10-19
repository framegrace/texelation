// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/workspace_flash.go
// Summary: Implements workspace flash capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate workspace flash visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

type workspaceFlashEffect struct {
	color    tcell.Color
	duration time.Duration
	timeline *fadeTimeline
}

func newWorkspaceFlashEffect(color tcell.Color, duration time.Duration) *workspaceFlashEffect {
	eff := &workspaceFlashEffect{timeline: &fadeTimeline{}}
	eff.Configure(color, duration)
	return eff
}

func (e *workspaceFlashEffect) Configure(color tcell.Color, duration time.Duration) {
	e.color = color
	if duration < 0 {
		duration = 0
	}
	e.duration = duration
}

func (e *workspaceFlashEffect) ID() string { return "workspace-flash" }

func (e *workspaceFlashEffect) Active() bool {
	return e.timeline.animating || e.timeline.current > 0
}

func (e *workspaceFlashEffect) Update(now time.Time) {
	e.timeline.valueAt(now)
}

func (e *workspaceFlashEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerWorkspaceKey {
		return
	}
	if trigger.Key != 'f' && trigger.Key != 'F' {
		return
	}
	when := trigger.Timestamp
	if when.IsZero() {
		when = time.Now()
	}
	current, _ := e.timeline.valueAt(when)
	e.timeline.startAnimation(current, 1.0, e.duration, when)
}

func (e *workspaceFlashEffect) ApplyWorkspace(buffer [][]client.Cell) {
	intensity := e.timeline.current
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
	if !e.timeline.animating && intensity > 0 {
		e.timeline.startAnimation(intensity, 0, e.duration, time.Now())
	}
}
