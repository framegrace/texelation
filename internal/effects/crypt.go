// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/crypt.go
// Summary: Implements crypt screen-scramble effect for the client effect subsystem.
// Usage: Replaces alphanumeric characters with random braille characters while preserving styles.
// Notes: Lifecycle is managed by the screensaver_fade wrapper.

package effects

import (
	"math/rand"
	"time"
	"unicode"

	"github.com/framegrace/texelation/client"
)

// Braille patterns U+2801..U+28FF (skip U+2800 blank).
const brailleBase = 0x2801
const brailleCount = 0x28FF - brailleBase + 1

type cryptEffect struct {
	active    bool
	charTable [256]rune // pre-generated braille chars
	frame     uint32    // frame counter for partial updates
}

func (e *cryptEffect) ID() string   { return "crypt" }
func (e *cryptEffect) Active(_ time.Time) bool { return e.active }
func (e *cryptEffect) Update(now time.Time) {}
func (e *cryptEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

func (e *cryptEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerScreensaver {
		return
	}
	e.active = trigger.Active
	if trigger.Active {
		for i := range e.charTable {
			e.charTable[i] = rune(brailleBase + rand.Intn(brailleCount))
		}
	}
}

// isAlphanumeric is a fast ASCII-optimized check that falls back to unicode for non-ASCII.
func isAlphanumeric(ch rune) bool {
	if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
		return true
	}
	if ch < 128 {
		return false // ASCII but not alphanumeric
	}
	return unicode.IsLetter(ch) || unicode.IsDigit(ch)
}

// cellHash returns a pseudo-random but deterministic hash for a cell position.
// Uses a simple integer hash to avoid diagonal banding artifacts from linear coefficients.
func cellHash(x, y int) uint8 {
	h := uint32(x) ^ (uint32(y) * 2654435761) // Knuth multiplicative hash
	h ^= h >> 16
	h *= 0x45d9f3b
	h ^= h >> 16
	return uint8(h)
}

func (e *cryptEffect) ApplyWorkspace(buffer [][]client.Cell) {
	if !e.active {
		return
	}
	e.frame++
	// Use position-based lookup so each cell gets a stable braille char.
	// Only re-roll a fraction of cells per frame (~12%) to create a
	// shimmer effect without forcing SetContent on every cell.
	frame := uint8(e.frame)
	for y := range buffer {
		row := buffer[y]
		for x := range row {
			if !isAlphanumeric(row[x].Ch) {
				continue
			}
			h := cellHash(x, y)
			// Re-roll this cell when its hash phase matches the frame.
			// Threshold 100/256 ≈ 39% of cells change per frame.
			if uint8(h+frame) < 100 {
				e.charTable[h] = rune(brailleBase + rand.Intn(brailleCount))
			}
			row[x].Ch = e.charTable[h]
		}
	}
}

// FrameSkip returns 2 — crypt only changes ~40% of cells per frame,
// so 15fps is smooth enough.
func (e *cryptEffect) FrameSkip() int { return 2 }

func init() {
	Register("crypt", func(cfg EffectConfig) (Effect, error) {
		return &cryptEffect{}, nil
	})
}
