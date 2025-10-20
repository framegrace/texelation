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

	"texelation/client"
)

type rainbowEffect struct {
	active     bool
	speedHz    float64
	phase      float64
	lastUpdate time.Time
}

func newRainbowEffect(speedHz float64) Effect {
	if speedHz <= 0 {
		speedHz = 0.5
	}
	return &rainbowEffect{speedHz: speedHz}
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
	for y := 0; y < height; y++ {
		row := buffer[y]
		for x := 0; x < len(row); x++ {
			cell := &row[x]
			offset := float64(x+y) * 0.1
			color := hsvToRGB(float32(e.phase+offset), 1.0, 1.0)
			cell.Style = cell.Style.Foreground(color)
		}
	}
}

func (e *rainbowEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

func init() {
	Register("rainbow", func(cfg EffectConfig) (Effect, error) {
		speed := parseFloatOrDefault(cfg, "speed_hz", 0.5)
		return newRainbowEffect(speed), nil
	})
}
