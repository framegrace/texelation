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
	charIdx   uint8
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

func (e *cryptEffect) nextChar() rune {
	ch := e.charTable[e.charIdx]
	e.charIdx++
	return ch
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

func (e *cryptEffect) ApplyWorkspace(buffer [][]client.Cell) {
	if !e.active {
		return
	}
	for y := range buffer {
		row := buffer[y]
		for x := range row {
			if isAlphanumeric(row[x].Ch) {
				row[x].Ch = e.nextChar()
			}
		}
	}
}

func init() {
	Register("crypt", func(cfg EffectConfig) (Effect, error) {
		return &cryptEffect{}, nil
	})
}
