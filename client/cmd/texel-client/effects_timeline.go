package main

import "time"

type fadeTimeline struct {
	initialized bool
	current     float32
	start       float32
	target      float32
	startTime   time.Time
	duration    time.Duration
	animating   bool
}

func (t *fadeTimeline) valueAt(now time.Time) (float32, bool) {
	if !t.initialized {
		return t.current, false
	}
	if !t.animating {
		t.current = t.target
		return t.current, false
	}
	if t.duration <= 0 {
		t.current = t.target
		t.animating = false
		return t.current, false
	}
	if now.Before(t.startTime) {
		return t.start, true
	}
	elapsed := now.Sub(t.startTime)
	if elapsed >= t.duration {
		t.current = t.target
		t.animating = false
		return t.current, false
	}
	progress := float32(elapsed) / float32(t.duration)
	if progress < 0 {
		progress = 0
	} else if progress > 1 {
		progress = 1
	}
	// ease in/out via smoothstep
	progress = progress * progress * (3.0 - 2.0*progress)
	t.current = t.start + (t.target-t.start)*progress
	return t.current, true
}

func (t *fadeTimeline) setInstant(target float32, duration time.Duration, when time.Time) {
	t.initialized = true
	t.animating = false
	t.start = target
	t.target = target
	t.current = target
	t.startTime = when
	t.duration = duration
}

func (t *fadeTimeline) startAnimation(current, target float32, duration time.Duration, when time.Time) {
	t.initialized = true
	t.start = current
	t.current = current
	t.target = target
	t.startTime = when
	t.duration = duration
	t.animating = current != target
	if duration <= 0 {
		t.current = target
		t.animating = false
	}
}
