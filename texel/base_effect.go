package texel

import (
	"context"
	"sync"
	"time"
)

// BaseEffect provides common state and animation logic for effects.
type BaseEffect struct {
	mu               sync.Mutex
	state            EffectState
	currentIntensity float32
	targetIntensity  float32
	duration         time.Duration
	animCancel       context.CancelFunc
	desktop          *Desktop // Changed from screen to desktop
	screen           *Screen
}

// newBaseEffect creates the common part of an effect.
func newBaseEffect(scr *Screen, targetIntensity float32) BaseEffect {
	return BaseEffect{
		screen:          scr,
		desktop:         scr.desktop,
		state:           StateOff,
		targetIntensity: targetIntensity,
		duration:        200 * time.Millisecond,
	}
}

// setDuration allows an Option to configure the effect's duration.
func (b *BaseEffect) setDuration(d time.Duration) {
	b.duration = d
}

// setTargetIntensity allows an Option to configure the effect's intensity.
func (b *BaseEffect) setTargetIntensity(i float32) {
	b.targetIntensity = i
}

func (b *BaseEffect) setState(s EffectState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = s
}

func (b *BaseEffect) getState() EffectState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *BaseEffect) setIntensity(i float32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentIntensity = i
}

func (b *BaseEffect) getIntensity() float32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.currentIntensity
}

func (b *BaseEffect) activate() {
	currentState := b.getState()
	if currentState == StateOn || currentState == StateFadingIn {
		return
	}
	b.animate(b.targetIntensity, StateFadingIn, StateOn)
}

func (b *BaseEffect) inactivate() {
	currentState := b.getState()
	if currentState == StateOff || currentState == StateFadingOut {
		return
	}
	b.animate(0.0, StateFadingOut, StateOff)
}

func (b *BaseEffect) animate(to float32, duringState, endState EffectState) {
	b.mu.Lock()
	if b.animCancel != nil {
		b.animCancel()
	}
	from := b.currentIntensity
	b.state = duringState
	ctx, cancel := context.WithCancel(context.Background())
	b.animCancel = cancel
	b.mu.Unlock()

	go func() {
		startTime := time.Now()
		ticker := time.NewTicker(32 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(startTime)
				if elapsed >= b.duration {
					b.setIntensity(to)
					b.setState(endState)
					b.screen.Refresh()
					return
				}
				progress := float32(elapsed) / float32(b.duration)
				newIntensity := from + (to-from)*progress
				b.setIntensity(newIntensity)
				b.screen.Refresh()
			}
		}
	}()
}
