// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/keyflash.go
// Summary: Implements key flash capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate key flash visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

type keyFlashEffect struct {
	color    tcell.Color
	duration time.Duration
	timeline *fadeTimeline
	keys     map[rune]struct{}
}

func newKeyFlashEffect(color tcell.Color, duration time.Duration, keys []rune) Effect {
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
	return &keyFlashEffect{
		color:    color,
		duration: duration,
		timeline: &fadeTimeline{},
		keys:     upper,
	}
}

func (e *keyFlashEffect) ID() string { return "flash" }

func (e *keyFlashEffect) Active() bool {
	return e.timeline.animating || e.timeline.current > 0
}

func (e *keyFlashEffect) Update(now time.Time) {
	e.timeline.valueAt(now)
}

func (e *keyFlashEffect) HandleTrigger(trigger EffectTrigger) {
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

func (e *keyFlashEffect) ApplyWorkspace(buffer [][]client.Cell) {
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

func (e *keyFlashEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

func init() {
	Register("flash", func(cfg EffectConfig) (Effect, error) {
		color := parseColorOrDefault(cfg, "color", defaultFlashColor)
		duration := parseDurationOrDefault(cfg, "duration_ms", 250)
		keys := parseKeysOrDefault(cfg, "keys", []rune{'F'})
		return newKeyFlashEffect(color, duration, keys), nil
	})
}
