// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/matrix.go
// Summary: Matrix digital rain screensaver effect.
// Usage: Replaces screen content with cascading vertical streams of green katakana characters.
// Notes: Lifecycle is managed by the screensaver_fade wrapper.

package effects

import (
	"math/rand"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
)

// Half-width Katakana U+FF66..U+FF9F.
const katakanaBase = 0xFF66
const katakanaCount = 0xFF9F - katakanaBase + 1

type matrixStream struct {
	head    int // current head row
	length  int // trail length in rows
	speed   int // frames per advance step
	counter int // frame counter for speed
	delay   int // frames before stream starts
}

type matrixEffect struct {
	active   bool
	streams  []matrixStream
	cols     int
	rows     int
	charTable [256]rune // pre-generated random katakana for fast lookup
	charIdx   uint8     // cycling index into charTable
}

func (e *matrixEffect) ID() string   { return "matrix" }
func (e *matrixEffect) Active(_ time.Time) bool { return e.active }
func (e *matrixEffect) Update(now time.Time) {}
func (e *matrixEffect) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}

func (e *matrixEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerScreensaver {
		return
	}
	e.active = trigger.Active
	if trigger.Active {
		e.streams = nil
	}
}

func (e *matrixEffect) newStream() matrixStream {
	length := 4 + rand.Intn(e.rows/2+1)
	return matrixStream{
		head:   -rand.Intn(e.rows),
		length: length,
		speed:  1 + rand.Intn(4),
		delay:  rand.Intn(e.rows),
	}
}

func (e *matrixEffect) initStreams(cols, rows int) {
	e.cols = cols
	e.rows = rows
	e.streams = make([]matrixStream, cols)
	for i := range e.streams {
		e.streams[i] = e.newStream()
	}
	// Pre-generate character lookup table
	for i := range e.charTable {
		e.charTable[i] = rune(katakanaBase + rand.Intn(katakanaCount))
	}
}

// nextChar returns the next pre-generated character, cycling through the table.
func (e *matrixEffect) nextChar() rune {
	ch := e.charTable[e.charIdx]
	e.charIdx++
	return ch
}

func (e *matrixEffect) ApplyWorkspace(buffer [][]client.Cell) {
	if !e.active || len(buffer) == 0 || len(buffer[0]) == 0 {
		return
	}

	rows := len(buffer)
	cols := len(buffer[0])

	if e.cols != cols || e.rows != rows || e.streams == nil {
		e.initStreams(cols, rows)
	}

	// Advance streams.
	for i := range e.streams {
		s := &e.streams[i]
		if s.delay > 0 {
			s.delay--
			continue
		}
		s.counter++
		if s.counter >= s.speed {
			s.counter = 0
			s.head++
			if s.head-s.length > rows {
				e.streams[i] = e.newStream()
				e.streams[i].delay = rand.Intn(e.rows / 2)
			}
		}
	}

	// Pre-build styles for trail segments.
	headStyle := tcell.StyleDefault.
		Foreground(tcell.NewRGBColor(220, 255, 220)).
		Background(tcell.ColorBlack).
		Bold(true)
	brightStyle := tcell.StyleDefault.
		Foreground(tcell.NewRGBColor(0, 230, 0)).
		Background(tcell.ColorBlack)
	midStyle := tcell.StyleDefault.
		Foreground(tcell.NewRGBColor(0, 160, 0)).
		Background(tcell.ColorBlack)
	dimStyle := tcell.StyleDefault.
		Foreground(tcell.NewRGBColor(0, 90, 0)).
		Background(tcell.ColorBlack)
	bgStyle := tcell.StyleDefault.
		Foreground(tcell.NewRGBColor(0, 30, 0)).
		Background(tcell.ColorBlack)

	// Paint every cell.
	for y := 0; y < rows; y++ {
		row := buffer[y]
		for x := 0; x < cols; x++ {
			s := &e.streams[x]
			dist := s.head - y // 0 = head, positive = deeper into trail

			if s.delay > 0 || dist < 0 || dist >= s.length {
				// Not in a stream: faint random char on black.
				row[x] = client.Cell{
					Ch:    e.nextChar(),
					Style: bgStyle,
				}
				continue
			}

			ch := e.nextChar()
			frac := float32(dist) / float32(s.length)

			var style tcell.Style
			switch {
			case dist == 0:
				style = headStyle
			case frac < 0.25:
				style = brightStyle
			case frac < 0.55:
				style = midStyle
			default:
				style = dimStyle
			}

			row[x] = client.Cell{Ch: ch, Style: style}
		}
	}
}

func init() {
	Register("matrix", func(cfg EffectConfig) (Effect, error) {
		return &matrixEffect{}, nil
	})
}
