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
	PaneEffectBase
	color     tcell.Color
	intensity float32
	defaultFg tcell.Color
	defaultBg tcell.Color
}

func newFadeTintEffect(color tcell.Color, intensity float32, duration time.Duration, defaultFg, defaultBg tcell.Color) Effect {
	if intensity < 0 {
		intensity = 0
	} else if intensity > 1 {
		intensity = 1
	}
	if duration < 0 {
		duration = 0
	}
	return &fadeTintEffect{
		PaneEffectBase: NewPaneEffectBase(duration),
		color:          color,
		intensity:      intensity,
		defaultFg:      defaultFg,
		defaultBg:      defaultBg,
	}
}

func (e *fadeTintEffect) ID() string { return "fadeTint" }

// Active and Update are provided by PaneEffectBase

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

	// Use base helper - even simpler now!
	e.Animate(trigger.PaneID, target, trigger.Timestamp)
}

func (e *fadeTintEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {
	if pane == nil {
		return
	}

	// Get cached value (Update was already called this frame)
	intensity := e.GetCached(pane.ID)
	if intensity <= 0 {
		return
	}

	// Apply tint
	for y := range buffer {
		row := buffer[y]
		for x := range row {
			cell := &row[x]
			fgFallback := e.defaultFg
			if fgFallback == tcell.ColorDefault {
				fgFallback = tcell.ColorWhite
			}
			bgFallback := e.defaultBg
			if bgFallback == tcell.ColorDefault {
				bgFallback = tcell.ColorBlack
			}
			cell.Style = tintStyle(cell.Style, e.color, intensity, false, fgFallback, bgFallback)
		}
	}
}

func (e *fadeTintEffect) ApplyWorkspace(buffer [][]client.Cell) {}

func init() {
	Register("fadeTint", func(cfg EffectConfig) (Effect, error) {
		color := parseColorOrDefault(cfg, "color", defaultInactiveColor)
		intensity := float32(parseFloatOrDefault(cfg, "intensity", 0.35))
		duration := parseDurationOrDefault(cfg, "duration_ms", 400)
		defaultFg := parseColorOrDefault(cfg, "default_fg", tcell.ColorWhite)
		defaultBg := parseColorOrDefault(cfg, "default_bg", tcell.ColorBlack)
		return newFadeTintEffect(color, intensity, duration, defaultFg, defaultBg), nil
	})
}
