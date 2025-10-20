// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/rainbow.go
// Summary: Implements rainbow capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate rainbow visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"math"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

type rainbowEffect struct {
	active     bool
	speedHz    float64
	mix        float32
	phase      float64
	lastUpdate time.Time
}

func newRainbowEffect(speedHz float64, mix float32) Effect {
	if speedHz <= 0 {
		speedHz = 0.5
	}
	if mix < 0 {
		mix = 0
	} else if mix > 1 {
		mix = 1
	}
	return &rainbowEffect{speedHz: speedHz, mix: mix}
}

func (e *rainbowEffect) ID() string { return "rainbow" }

func (e *rainbowEffect) Active() bool { return e.active }

func (e *rainbowEffect) Update(now time.Time) {
	if !e.active {
		return
	}
	if e.lastUpdate.IsZero() {
		e.lastUpdate = now
		return
	}
	delta := now.Sub(e.lastUpdate).Seconds()
	e.lastUpdate = now
	e.phase = math.Mod(e.phase+2*math.Pi*e.speedHz*delta, 2*math.Pi)
}

func (e *rainbowEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerWorkspaceControl {
		return
	}
	e.active = trigger.Active
	if trigger.Timestamp.IsZero() {
		e.lastUpdate = time.Now()
	} else {
		e.lastUpdate = trigger.Timestamp
	}
}

func (e *rainbowEffect) ApplyWorkspace(buffer [][]client.Cell) {
	if !e.active {
		return
	}
	height := len(buffer)
	if height == 0 {
		return
	}
	width := len(buffer[0])
	if width == 0 {
		return
	}
	mix := e.mix
	for y := 0; y < height; y++ {
		row := buffer[y]
		for x := 0; x < len(row); x++ {
			cell := &row[x]
			offset := float64(x+y) * 0.1
			tint := hsvToRGB(float32(e.phase+offset), 1.0, 1.0).TrueColor()
			fg, bg, attrs := cell.Style.Decompose()
			baseFg := fg.TrueColor()
			if fg == tcell.ColorDefault || !baseFg.Valid() {
				baseFg = defaultInactiveColor.TrueColor()
			}

			// Detect prompt/background hacks that encode background in the foreground colour.
			if matchesNeighborBackground(baseFg, row, x) {
				continue
			}

			mixed := blendColor(baseFg, tint, mix)
			style := cell.Style.Foreground(mixed)
			if bg != tcell.ColorDefault {
				style = style.Background(bg.TrueColor())
			}
			style = style.
				Bold(attrs&tcell.AttrBold != 0).
				Underline(attrs&tcell.AttrUnderline != 0).
				Reverse(attrs&tcell.AttrReverse != 0).
				Blink(attrs&tcell.AttrBlink != 0).
				Dim(attrs&tcell.AttrDim != 0).
				Italic(attrs&tcell.AttrItalic != 0)
			cell.Style = style
		}
	}
}

func (e *rainbowEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

func init() {
	Register("rainbow", func(cfg EffectConfig) (Effect, error) {
		speed := parseFloatOrDefault(cfg, "speed_hz", 0.5)
		mix := float32(parseFloatOrDefault(cfg, "mix", 0.6))
		return newRainbowEffect(speed, mix), nil
	})
}

func matchesNeighborBackground(base tcell.Color, row []client.Cell, x int) bool {
	if !base.Valid() {
		return false
	}
	if x > 0 {
		if _, bg, _ := row[x-1].Style.Decompose(); bg != tcell.ColorDefault {
			if base.TrueColor() == bg.TrueColor() {
				return true
			}
		}
	}
	if x+1 < len(row) {
		if _, bg, _ := row[x+1].Style.Decompose(); bg != tcell.ColorDefault {
			if base.TrueColor() == bg.TrueColor() {
				return true
			}
		}
	}
	return false
}
