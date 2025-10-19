package effects

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

type resizingOverlayEffect struct {
	color     tcell.Color
	intensity float32
	duration  time.Duration
	timelines map[[16]byte]*fadeTimeline
	mu        sync.Mutex
}

func newResizingOverlayEffect(color tcell.Color, intensity float32, duration time.Duration) *resizingOverlayEffect {
	eff := &resizingOverlayEffect{timelines: make(map[[16]byte]*fadeTimeline)}
	eff.Configure(color, intensity, duration)
	return eff
}

func (e *resizingOverlayEffect) Configure(color tcell.Color, intensity float32, duration time.Duration) {
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

func (e *resizingOverlayEffect) ID() string { return "pane-resizing-overlay" }

func (e *resizingOverlayEffect) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tl := range e.timelines {
		if tl.animating || tl.current > 0 {
			return true
		}
	}
	return false
}

func (e *resizingOverlayEffect) Update(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, timeline := range e.timelines {
		timeline.valueAt(now)
	}
}

func (e *resizingOverlayEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerPaneResizing {
		return
	}
	e.mu.Lock()
	timeline := e.timelines[trigger.PaneID]
	if timeline == nil {
		timeline = &fadeTimeline{}
		e.timelines[trigger.PaneID] = timeline
	}
	target := float32(0)
	if trigger.Resizing {
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

func (e *resizingOverlayEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {
	if pane == nil || len(buffer) == 0 {
		return
	}
	e.mu.Lock()
	timeline := e.timelines[pane.ID]
	if timeline == nil {
		if !pane.Resizing {
			e.mu.Unlock()
			return
		}
		timeline = &fadeTimeline{}
		e.timelines[pane.ID] = timeline
		timeline.setInstant(e.intensity, e.duration, time.Now())
	}
	intensity := timeline.current
	e.mu.Unlock()
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
