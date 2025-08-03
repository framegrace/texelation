// texel/effects_v2.go
package texel

import (
	"context"
	"log"
	"sync"
	"time"
)

// Effect is now a pure rendering transformation
// It receives a buffer reference and applies visual changes in-place
type Effect interface {
	// Apply transforms the buffer in-place (no copying needed)
	Apply(buffer *[][]Cell)
	// Clone creates a new instance of this effect
	Clone() Effect
}

// AnimatedEffect extends Effect with animation capabilities
type AnimatedEffect interface {
	Effect
	// GetIntensity returns the current animation intensity (0.0 to 1.0)
	GetIntensity() float32
	// SetIntensity sets the animation intensity
	SetIntensity(intensity float32)
	// IsAnimating returns true if the effect is currently animating
	IsAnimating() bool
}

// EffectPipeline manages a collection of effects that are applied in sequence
type EffectPipeline struct {
	mu      sync.RWMutex
	effects []Effect
}

// NewEffectPipeline creates a new effect pipeline
func NewEffectPipeline() *EffectPipeline {
	return &EffectPipeline{
		effects: make([]Effect, 0),
	}
}

// AddEffect adds an effect to the end of the pipeline
func (ep *EffectPipeline) AddEffect(effect Effect) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.effects = append(ep.effects, effect)
}

// RemoveEffect removes an effect from the pipeline
func (ep *EffectPipeline) RemoveEffect(effect Effect) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	for i, e := range ep.effects {
		if e == effect {
			ep.effects = append(ep.effects[:i], ep.effects[i+1:]...)
			break
		}
	}
}

// Clear removes all effects from the pipeline
func (ep *EffectPipeline) Clear() {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.effects = make([]Effect, 0)
}

// Apply runs all effects in the pipeline sequentially
func (ep *EffectPipeline) Apply(buffer *[][]Cell) {
	ep.mu.RLock()
	defer ep.mu.RUnlock()

	for _, effect := range ep.effects {
		effect.Apply(buffer)
	}
}

// EffectAnimator handles animation of effects
type EffectAnimator struct {
	mu        sync.Mutex
	animators map[AnimatedEffect]context.CancelFunc
}

// NewEffectAnimator creates a new effect animator
func NewEffectAnimator() *EffectAnimator {
	return &EffectAnimator{
		animators: make(map[AnimatedEffect]context.CancelFunc),
	}
}

// AnimateTo animates an effect to a target intensity over the given duration
func (ea *EffectAnimator) AnimateTo(effect AnimatedEffect, targetIntensity float32, duration time.Duration, onComplete func()) {
	ea.mu.Lock()

	// Cancel any existing animation for this effect
	if cancel, exists := ea.animators[effect]; exists {
		log.Printf("AnimateTo: Cancelling existing animation for effect")
		cancel()
	}

	startIntensity := effect.GetIntensity()
	log.Printf("AnimateTo: Starting animation from %.3f to %.3f over %v", startIntensity, targetIntensity, duration)

	ctx, cancel := context.WithCancel(context.Background())
	ea.animators[effect] = cancel
	ea.mu.Unlock()

	go func() {
		defer func() {
			ea.mu.Lock()
			delete(ea.animators, effect)
			ea.mu.Unlock()
			log.Printf("AnimateTo: Animation completed, final intensity: %.3f", effect.GetIntensity())
			if onComplete != nil {
				onComplete()
			}
		}()

		startTime := time.Now()
		ticker := time.NewTicker(16 * time.Millisecond) // ~60fps
		defer ticker.Stop()

		frameCount := 0
		for {
			select {
			case <-ctx.Done():
				log.Printf("AnimateTo: Animation cancelled after %d frames", frameCount)
				return
			case <-ticker.C:
				elapsed := time.Since(startTime)
				if elapsed >= duration {
					effect.SetIntensity(targetIntensity)
					log.Printf("AnimateTo: Animation finished, set final intensity to %.3f", targetIntensity)
					return
				}

				progress := float32(elapsed) / float32(duration)
				// Smooth easing
				progress = progress * progress * (3.0 - 2.0*progress)

				newIntensity := startIntensity + (targetIntensity-startIntensity)*progress
				effect.SetIntensity(newIntensity)

				frameCount++
				if frameCount%30 == 0 { // Log every ~0.5 seconds
					log.Printf("AnimateTo: Frame %d, progress=%.3f, intensity=%.3f", frameCount, progress, newIntensity)
				}
			}
		}
	}()
}

func (f *FadeEffect) SetIntensity(intensity float32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	oldIntensity := f.intensity

	if intensity < 0.0 {
		intensity = 0.0
	} else if intensity > 1.0 {
		intensity = 1.0
	}
	f.intensity = intensity

	if oldIntensity != f.intensity {
		log.Printf("FadeEffect.SetIntensity: Changed from %.3f to %.3f", oldIntensity, f.intensity)
	}
}

// FadeIn animates an effect to full intensity
func (ea *EffectAnimator) FadeIn(effect AnimatedEffect, duration time.Duration, onComplete func()) {
	ea.AnimateTo(effect, 1.0, duration, onComplete)
}

// FadeOut animates an effect to zero intensity
func (ea *EffectAnimator) FadeOut(effect AnimatedEffect, duration time.Duration, onComplete func()) {
	ea.AnimateTo(effect, 0.0, duration, onComplete)
}

// Stop stops all animations for the given effect
func (ea *EffectAnimator) Stop(effect AnimatedEffect) {
	ea.mu.Lock()
	defer ea.mu.Unlock()
	if cancel, exists := ea.animators[effect]; exists {
		cancel()
		delete(ea.animators, effect)
	}
}

// StopAll stops all animations
func (ea *EffectAnimator) StopAll() {
	ea.mu.Lock()
	defer ea.mu.Unlock()
	for _, cancel := range ea.animators {
		cancel()
	}
	ea.animators = make(map[AnimatedEffect]context.CancelFunc)
}
