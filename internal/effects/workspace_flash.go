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
	"unicode"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

type workspaceFlashEffect struct {
	color    tcell.Color
	duration time.Duration
	timeline *fadeTimeline
	keys     map[rune]struct{}
}

func newWorkspaceFlashEffect(color tcell.Color, duration time.Duration, keys []rune) Effect {
	if duration < 0 {
		duration = 0
	}
	if len(keys) == 0 {
		keys = []rune{'F'}
	}
	upper := make(map[rune]struct{}, len(keys))
	for _, r := range keys {
		if r == 0 {
			continue
		}
		upper[unicode.ToUpper(r)] = struct{}{}
	}
	return &workspaceFlashEffect{
		color:    color,
		duration: duration,
		timeline: &fadeTimeline{},
		keys:     upper,
	}
}

func (e *workspaceFlashEffect) ID() string { return "flash" }

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
	if len(e.keys) > 0 {
		if _, ok := e.keys[unicode.ToUpper(trigger.Key)]; !ok {
			return
		}
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

func (e *workspaceFlashEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

func init() {
	Register("flash", func(cfg EffectConfig) (Effect, error) {
		color := parseColorOrDefault(cfg, "color", defaultFlashColor)
		duration := parseDurationOrDefault(cfg, "duration_ms", 250)
		keys := parseKeysOrDefault(cfg, "keys", []rune{'F'})
		return newWorkspaceFlashEffect(color, duration, keys), nil
	})
}
