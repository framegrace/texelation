// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/fadetint.go
// Summary: Implements fade tint capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate fade tint visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
)

type fadeTintEffect struct {
	PaneEffectBase
	color     tcell.Color
	intensity float32
	defaultFg tcell.Color
	defaultBg tcell.Color
	// wsDuration is set at construction; never mutated.
	wsDuration time.Duration

	// mu guards all cross-goroutine state below. HandleTrigger runs on the
	// dispatcher goroutine; Update / ApplyPane / ApplyWorkspace / Active run
	// on the render goroutine.
	mu          sync.Mutex
	wsActive    bool
	wsIntensity float32 // current animated intensity
	wsTarget    float32
	wsStart     time.Time
	seenActive  map[[16]byte]bool
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
		wsDuration:     duration,
		seenActive:     make(map[[16]byte]bool),
	}
}

func (e *fadeTintEffect) ID() string { return "fadeTint" }

func (e *fadeTintEffect) Active(now time.Time) bool {
	if e.PaneEffectBase.Active(now) {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.wsIntensity > 0 || e.wsTarget > 0
}

func (e *fadeTintEffect) Update(now time.Time) {
	e.PaneEffectBase.Update(now)
	e.mu.Lock()
	defer e.mu.Unlock()
	// Animate workspace intensity toward target
	if e.wsDuration <= 0 {
		e.wsIntensity = e.wsTarget
		return
	}
	elapsed := float32(now.Sub(e.wsStart).Seconds())
	progress := elapsed / float32(e.wsDuration.Seconds())
	if progress >= 1 {
		e.wsIntensity = e.wsTarget
	} else {
		// Smoothstep
		t := progress * progress * (3 - 2*progress)
		if e.wsTarget > e.wsIntensity {
			e.wsIntensity = e.wsIntensity + (e.wsTarget-e.wsIntensity)*t
		} else {
			start := e.intensity // fade from full intensity
			if !e.wsActive {
				start = e.wsIntensity
			}
			e.wsIntensity = start + (e.wsTarget-start)*t
		}
	}
}

func (e *fadeTintEffect) HandleTrigger(trigger EffectTrigger) {
	switch trigger.Type {
	case TriggerPaneActive:
		if trigger.Active {
			e.mu.Lock()
			e.seenActive[trigger.PaneID] = true
			e.mu.Unlock()
		}
		target := float32(0)
		if !trigger.Active {
			target = e.intensity
		}
		e.Animate(trigger.PaneID, target, trigger.Timestamp)
	case TriggerPaneResizing:
		target := float32(0)
		if trigger.Resizing {
			target = e.intensity
		}
		e.Animate(trigger.PaneID, target, trigger.Timestamp)
	case TriggerWorkspaceControl:
		e.mu.Lock()
		e.wsActive = trigger.Active
		e.wsTarget = 0
		if trigger.Active {
			e.wsTarget = e.intensity
		}
		e.wsStart = trigger.Timestamp
		e.mu.Unlock()
	}
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

	// Skip panes that have never been active (e.g., status bar).
	// Only darken panes that participate in the focus system.
	e.mu.Lock()
	seen := e.seenActive[pane.ID]
	e.mu.Unlock()
	if !seen {
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

func (e *fadeTintEffect) ApplyWorkspace(buffer [][]client.Cell) {
	e.mu.Lock()
	intensity := e.wsIntensity
	e.mu.Unlock()
	if intensity <= 0 {
		return
	}
	fgFallback := e.defaultFg
	if fgFallback == tcell.ColorDefault {
		fgFallback = tcell.ColorWhite
	}
	bgFallback := e.defaultBg
	if bgFallback == tcell.ColorDefault {
		bgFallback = tcell.ColorBlack
	}
	for y := range buffer {
		for x := range buffer[y] {
			cell := &buffer[y][x]
			cell.Style = tintStyle(cell.Style, e.color, intensity, false, fgFallback, bgFallback)
		}
	}
}

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
