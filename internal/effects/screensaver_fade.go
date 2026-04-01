// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/screensaver_fade.go
// Summary: Wraps a screensaver effect with fade-in/fade-out blending.
// Usage: Wraps the inner effect and is the only binding registered with the manager.
//        Controls the inner effect's lifecycle: proxies activate immediately, but delays
//        deactivate until fade-out completes.

package effects

import (
	"math/rand"
	"time"

	"github.com/framegrace/texelation/client"
)

const defaultFadeOut = 500 * time.Millisecond

// fadeBlender controls how the screensaver transition looks.
type fadeBlender interface {
	// Blend selectively reverts cells in dst back to orig based on intensity [0..1].
	// intensity=0: all original, intensity=1: all transformed.
	Blend(orig, dst [][]client.Cell, intensity float32)
	// Reset is called on each screensaver activation so blenders can
	// re-randomize direction or other per-activation state.
	Reset()
}

type screensaverFade struct {
	inner       Effect
	effectIDs   []string // random mode: pick from these on each activation
	blender     fadeBlender
	timeline    *Timeline
	active      bool
	fadingOut   bool
	fadeOut      time.Duration
	snapshotBuf [][]client.Cell // pooled buffer for blend snapshot
}

func blenderForStyle(style string) fadeBlender {
	switch style {
	case "spiral":
		return newSpiralBlender()
	case "curtain":
		return newCurtainBlender()
	case "random":
		return &randomBlender{}
	default:
		return &dissolveBlender{}
	}
}

// randomBlender picks a random concrete blender on each Reset().
type randomBlender struct {
	inner fadeBlender
}

var concreteStyles = []string{"dissolve", "curtain", "spiral"}

func (b *randomBlender) Reset() {
	b.inner = blenderForStyle(concreteStyles[rand.Intn(len(concreteStyles))])
	b.inner.Reset()
}

func (b *randomBlender) Blend(orig, dst [][]client.Cell, intensity float32) {
	if b.inner == nil {
		b.inner = blenderForStyle(concreteStyles[rand.Intn(len(concreteStyles))])
	}
	b.inner.Blend(orig, dst, intensity)
}

func NewScreensaverFade(inner Effect, fadeStyle string) Effect {
	return &screensaverFade{
		inner:    inner,
		blender:  blenderForStyle(fadeStyle),
		timeline: NewTimeline(0.0),
	}
}

// NewScreensaverFadeRandom creates a screensaver fade that picks a random
// effect from effectIDs on each activation.
func NewScreensaverFadeRandom(effectIDs []string, fadeStyle string) Effect {
	return &screensaverFade{
		effectIDs: effectIDs,
		blender:   blenderForStyle(fadeStyle),
		timeline:  NewTimeline(0.0),
	}
}

func (e *screensaverFade) ID() string { return "screensaver_fade" }

func (e *screensaverFade) Active() bool {
	return e.active || e.fadingOut
}

func (e *screensaverFade) Update(now time.Time) {
	e.timeline.Update(now)
	if e.inner != nil {
		e.inner.Update(now)
	}

	// Fade-out complete: deactivate the inner effect now.
	if e.fadingOut && e.timeline.GetCached("fade") <= 0 {
		e.fadingOut = false
		if e.inner != nil {
			e.inner.HandleTrigger(EffectTrigger{
				Type:      TriggerScreensaver,
				Active:    false,
				Timestamp: now,
			})
		}
	}
}

func (e *screensaverFade) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerScreensaver {
		if e.inner != nil {
			e.inner.HandleTrigger(trigger)
		}
		return
	}

	fadeIn := trigger.FadeIn
	if fadeIn <= 0 {
		fadeIn = 5 * time.Second
	}

	if trigger.Active {
		e.active = true
		e.fadingOut = false
		e.timeline.Reset("fade")
		e.timeline.AnimateTo("fade", 1.0, fadeIn, trigger.Timestamp)
		e.blender.Reset()
		// Random mode: pick a new effect each activation.
		if len(e.effectIDs) > 0 {
			id := e.effectIDs[rand.Intn(len(e.effectIDs))]
			if eff, err := CreateEffect(id, nil); err == nil {
				e.inner = eff
			}
		}
		e.inner.HandleTrigger(trigger)
	} else {
		// Start fade-out but keep inner effect running.
		e.active = false
		e.fadingOut = true
		fadeOut := trigger.FadeOut
		if fadeOut <= 0 {
			fadeOut = defaultFadeOut
		}
		e.fadeOut = fadeOut
		e.timeline.AnimateTo("fade", 0.0, fadeOut, trigger.Timestamp)
		// Inner effect stays active — deactivated in Update() when fade reaches 0.
	}
}

func (e *screensaverFade) ApplyWorkspace(buffer [][]client.Cell) {
	if !e.active && !e.fadingOut {
		return
	}

	intensity := e.timeline.GetCached("fade")
	if intensity <= 0 {
		return
	}

	// Snapshot buffer before the inner effect transforms it (reuse pooled buffer).
	if len(e.snapshotBuf) != len(buffer) {
		e.snapshotBuf = make([][]client.Cell, len(buffer))
	}
	for y := range buffer {
		if len(e.snapshotBuf[y]) != len(buffer[y]) {
			e.snapshotBuf[y] = make([]client.Cell, len(buffer[y]))
		}
		copy(e.snapshotBuf[y], buffer[y])
	}

	// Let the inner effect transform the buffer at full intensity.
	if e.inner == nil {
		return
	}
	e.inner.ApplyWorkspace(buffer)

	if intensity >= 1.0 {
		return
	}

	e.blender.Blend(e.snapshotBuf, buffer, float32(intensity))
}

func (e *screensaverFade) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

// cellEqual compares two client.Cell values for equality.
// client.Cell contains protocol.DynColorDesc which has a slice field (Stops),
// making it incomparable with ==.
func cellEqual(a, b client.Cell) bool {
	return a.Ch == b.Ch && a.Style == b.Style
}

// dissolveBlender uses per-cell random dithering (the default).
type dissolveBlender struct{}

func (b *dissolveBlender) Reset() {}

func (b *dissolveBlender) Blend(orig, dst [][]client.Cell, intensity float32) {
	for y := range dst {
		srcRow := orig[y]
		dstRow := dst[y]
		for x := range dstRow {
			if !cellEqual(dstRow[x], srcRow[x]) && rand.Float32() >= intensity {
				dstRow[x] = srcRow[x]
			}
		}
	}
}
