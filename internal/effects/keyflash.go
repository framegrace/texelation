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
	color        tcell.Color
	duration     time.Duration
	timeline     *Timeline
	keys         map[rune]struct{}
	defaultFg    tcell.Color
	defaultBg    tcell.Color
	maxIntensity float32
}

func newKeyFlashEffect(color tcell.Color, duration time.Duration, keys []rune, defaultFg, defaultBg tcell.Color, maxIntensity float32) Effect {
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
		color:        color,
		duration:     duration,
		timeline:     NewTimeline(0.0), // Default to 0 (no flash)
		keys:         upper,
		defaultFg:    defaultFg,
		defaultBg:    defaultBg,
		maxIntensity: maxIntensity,
	}
}

func (e *keyFlashEffect) ID() string { return "flash" }

func (e *keyFlashEffect) Active() bool {
	// Use a dummy key for workspace-wide flash
	return e.timeline.Get("flash") > 0
}

func (e *keyFlashEffect) Update(now time.Time) {
	e.timeline.Update(now)
}

func (e *keyFlashEffect) HandleTrigger(trigger EffectTrigger) {
	switch trigger.Type {
	case TriggerWorkspaceKey:
		if len(e.keys) > 0 {
			if _, ok := e.keys[unicode.ToUpper(trigger.Key)]; !ok {
				return
			}
		}
		e.timeline.AnimateTo("flash", 1.0, e.duration)
	case TriggerWorkspaceControl:
		if !trigger.Active {
			return
		}
		e.timeline.AnimateTo("flash", 1.0, e.duration)
	}
}

func (e *keyFlashEffect) ApplyWorkspace(buffer [][]client.Cell) {
	baseIntensity := e.timeline.Get("flash")
	if baseIntensity <= 0 {
		return
	}
	intensity := baseIntensity
	if e.maxIntensity > 0 && e.maxIntensity < 1 {
		intensity = baseIntensity * e.maxIntensity
	}

	// Apply flash tint
	for y := range buffer {
		row := buffer[y]
		for x := range row {
			cell := &row[x]
			swap := isFakeBackgroundCell(row, x)
			fgFallback := e.defaultFg
			if fgFallback == tcell.ColorDefault {
				fgFallback = tcell.ColorWhite
			}
			bgFallback := e.defaultBg
			if bgFallback == tcell.ColorDefault {
				bgFallback = tcell.ColorBlack
			}
			cell.Style = tintStyle(cell.Style, e.color, intensity, swap, fgFallback, bgFallback)
		}
	}

	// Auto-fade back to zero after reaching peak
	// If we're at or near peak and not animating back, start fade-out
	if baseIntensity >= 0.99 && !e.timeline.IsAnimating("flash") {
		e.timeline.AnimateTo("flash", 0.0, e.duration)
	}
}

func (e *keyFlashEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

func init() {
	Register("flash", func(cfg EffectConfig) (Effect, error) {
		color := parseColorOrDefault(cfg, "color", defaultFlashColor)
		duration := parseDurationOrDefault(cfg, "duration_ms", 250)
		keys := parseKeysOrDefault(cfg, "keys", []rune{'F'})
		defaultFg := parseColorOrDefault(cfg, "default_fg", tcell.ColorWhite)
		defaultBg := parseColorOrDefault(cfg, "default_bg", tcell.ColorBlack)
		maxIntensity := float32(parseFloatOrDefault(cfg, "max_intensity", 1.0))
		if maxIntensity < 0 {
			maxIntensity = 0
		} else if maxIntensity > 1 {
			maxIntensity = 1
		}
		return newKeyFlashEffect(color, duration, keys, defaultFg, defaultBg, maxIntensity), nil
	})
}

func isFakeBackgroundCell(row []client.Cell, idx int) bool {
	if idx < 0 || idx >= len(row) {
		return false
	}
	fg, bg, _ := row[idx].Style.Decompose()
	trueFg := fg.TrueColor()
	if fg == tcell.ColorDefault || !trueFg.Valid() {
		return false
	}
	if bg.TrueColor() == trueFg {
		return true
	}
	if bg != tcell.ColorDefault {
		return false
	}
	if idx > 0 {
		if _, neighborBg, _ := row[idx-1].Style.Decompose(); neighborBg != tcell.ColorDefault {
			if neighborBg.TrueColor() == trueFg {
				return true
			}
		}
	}
	if idx+1 < len(row) {
		if _, neighborBg, _ := row[idx+1].Style.Decompose(); neighborBg != tcell.ColorDefault {
			if neighborBg.TrueColor() == trueFg {
				return true
			}
		}
	}
	return false
}
