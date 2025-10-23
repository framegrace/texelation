// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/fadetint.go
// Summary: Implements fade tint capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate fade tint visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

type fadeTintEffect struct {
	color    tcell.Color
	intensity float32
	duration time.Duration
	timeline *Timeline
}

func newFadeTintEffect(color tcell.Color, intensity float32, duration time.Duration) Effect {
	if intensity < 0 {
		intensity = 0
	} else if intensity > 1 {
		intensity = 1
	}
	if duration < 0 {
		duration = 0
	}
	return &fadeTintEffect{
		color:    color,
		intensity: intensity,
		duration: duration,
		timeline: NewTimeline(0.0), // Default to 0 (no tint)
	}
}

func (e *fadeTintEffect) ID() string { return "fadeTint" }

func (e *fadeTintEffect) Active() bool {
	return e.timeline.HasActiveAnimations() || e.timeline.Get(nil) > 0
}

func (e *fadeTintEffect) Update(now time.Time) {
	e.timeline.Update(now)
}

func (e *fadeTintEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerPaneActive && trigger.Type != TriggerPaneResizing {
		return
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

	// Simple! Just animate to the target - Timeline handles everything
	e.timeline.AnimateTo(trigger.PaneID, target, e.duration)
}

func (e *fadeTintEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {
	if pane == nil {
		return
	}

	// Get current animated value - Timeline handles initialization, locking, everything
	intensity := e.timeline.Get(pane.ID)
	if intensity <= 0 {
		return
	}

	// Apply tint
	for y := range buffer {
		row := buffer[y]
		for x := range row {
			cell := &row[x]
			cell.Style = tintStyle(cell.Style, e.color, intensity)
		}
	}
}

func (e *fadeTintEffect) ApplyWorkspace(buffer [][]client.Cell) {}

func init() {
	Register("fadeTint", func(cfg EffectConfig) (Effect, error) {
		color := parseColorOrDefault(cfg, "color", defaultInactiveColor)
		intensity := float32(parseFloatOrDefault(cfg, "intensity", 0.35))
		duration := parseDurationOrDefault(cfg, "duration_ms", 400)
		return newFadeTintEffect(color, intensity, duration), nil
	})
}
