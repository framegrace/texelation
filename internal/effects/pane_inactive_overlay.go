package effects

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

type inactiveOverlayEffect struct {
	color     tcell.Color
	intensity float32
	duration  time.Duration
	timelines map[[16]byte]*fadeTimeline
	mu        sync.Mutex
}

func newInactiveOverlayEffect(color tcell.Color, intensity float32, duration time.Duration) *inactiveOverlayEffect {
	eff := &inactiveOverlayEffect{timelines: make(map[[16]byte]*fadeTimeline)}
	eff.Configure(color, intensity, duration)
	return eff
}

func (e *inactiveOverlayEffect) Configure(color tcell.Color, intensity float32, duration time.Duration) {
	if intensity < 0 {
		intensity = 0
	} else if intensity > 1 {
		intensity = 1
	}
	if duration < 0 {
		duration = 0
	}
	e.color = color
	e.intensity = intensity
	e.duration = duration
}

func (e *inactiveOverlayEffect) ID() string { return "pane-inactive-overlay" }

func (e *inactiveOverlayEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tl := range e.timelines {
		if tl.animating || tl.current > 0 {
			return true
		}
	}
	return false
}

func (e *inactiveOverlayEffect) Update(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, timeline := range e.timelines {
		timeline.valueAt(now)
	}
}

func (e *inactiveOverlayEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerPaneActive {
		return
	}
	e.mu.Lock()
	timeline := e.timelines[trigger.PaneID]
	if timeline == nil {
		timeline = &fadeTimeline{}
		e.timelines[trigger.PaneID] = timeline
	}
	target := float32(0)
	if !trigger.Active {
		target = e.intensity
	}
	when := trigger.Timestamp
	if when.IsZero() {
		when = time.Now()
	}
	if !timeline.initialized {
		timeline.setInstant(target, e.duration, when)
	} else {
		current, _ := timeline.valueAt(when)
		timeline.startAnimation(current, target, e.duration, when)
	}
	e.mu.Unlock()
}

func (e *inactiveOverlayEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {
	if pane == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	timeline := e.timelines[pane.ID]
	if timeline == nil {
		if pane.Active {
			return
		}
		timeline = &fadeTimeline{}
		e.timelines[pane.ID] = timeline
		timeline.setInstant(e.intensity, e.duration, time.Now())
	}
	intensity := timeline.current
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
}
