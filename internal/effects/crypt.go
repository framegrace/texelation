// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/crypt.go
// Summary: Implements crypt screen-scramble effect for the client effect subsystem.
// Usage: Replaces alphanumeric characters with random block characters while preserving styles.

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
	active bool
}

func (e *cryptEffect) ID() string { return "crypt" }

func (e *cryptEffect) Active() bool { return e.active }

func (e *cryptEffect) Update(now time.Time) {}

func (e *cryptEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerCryptToggle {
		return
	}
	e.active = !e.active
}

func (e *cryptEffect) ApplyWorkspace(buffer [][]client.Cell) {
	if !e.active {
		return
	}
	for y := range buffer {
		row := buffer[y]
		for x := range row {
			ch := row[x].Ch
			if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
				row[x].Ch = rune(brailleBase + rand.Intn(brailleCount))
			}
		}
	}
}

func (e *cryptEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

func init() {
	Register("crypt", func(cfg EffectConfig) (Effect, error) {
		return &cryptEffect{}, nil
	})
}
