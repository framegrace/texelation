package main

import (
	"time"

	"texelation/client"
)

type paneAnimation struct {
	start     PaneRect
	end       PaneRect
	startTime time.Time
	duration  time.Duration
	buffer    [][]client.Cell
	ghost     bool
	forceTop  bool
}

type zoomAnimation struct {
	paneID    PaneID
	start     PaneRect
	end       PaneRect
	startTime time.Time
	duration  time.Duration
	active    bool
}

func calcProgress(now, start time.Time, duration time.Duration) float32 {
	if duration <= 0 {
		return 1
	}
	elapsed := now.Sub(start)
	if elapsed <= 0 {
		return 0
	}
	if elapsed >= duration {
		return 1
	}
	return easeInOutQuad(float32(elapsed) / float32(duration))
}

func lerpRect(a, b PaneRect, t float32) PaneRect {
	return PaneRect{
		X:      int(float32(a.X) + float32(b.X-a.X)*t),
		Y:      int(float32(a.Y) + float32(b.Y-a.Y)*t),
		Width:  int(float32(a.Width) + float32(b.Width-a.Width)*t),
		Height: int(float32(a.Height) + float32(b.Height-a.Height)*t),
	}
}

func easeInOutQuad(t float32) float32 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	if t < 0.5 {
		return 2 * t * t
	}
	return -1 + (4-2*t)*t
}
