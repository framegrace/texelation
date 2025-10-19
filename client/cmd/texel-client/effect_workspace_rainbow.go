package main

import (
	"math"
	"time"

	"texelation/client"
)

type workspaceRainbowEffect struct {
	active     bool
	speedHz    float64
	phase      float64
	lastUpdate time.Time
}

func newWorkspaceRainbowEffect(speedHz float64) *workspaceRainbowEffect {
	eff := &workspaceRainbowEffect{}
	eff.Configure(speedHz)
	return eff
}

func (e *workspaceRainbowEffect) ID() string { return "workspace-rainbow" }

func (e *workspaceRainbowEffect) Active() bool { return e.active }

func (e *workspaceRainbowEffect) Configure(speedHz float64) {
	if speedHz <= 0 {
		speedHz = 0.5
	}
	e.speedHz = speedHz
}

func (e *workspaceRainbowEffect) Update(now time.Time) {
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

func (e *workspaceRainbowEffect) HandleTrigger(trigger EffectTrigger) {
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

func (e *workspaceRainbowEffect) ApplyWorkspace(buffer [][]client.Cell) {
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
