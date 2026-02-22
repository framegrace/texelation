// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/screensaver_spiral.go
// Summary: Spiral fade blender for screensaver transitions.
// Usage: Reveals effect from center outward in a spiral on fade-in,
//        hides from outside inward on fade-out.

package effects

import (
	"math"
	"math/rand"

	"github.com/framegrace/texelation/client"
)

const (
	spiralTurns   = 3.0  // number of spiral rotations from center to edge
	spiralFeather = 0.05 // width of the soft transition zone [0..1]
)

type spiralBlender struct {
	revealMap [][]float32 // per-cell reveal time [0..1], 0=center first
	width     int
	height    int
}

func newSpiralBlender() *spiralBlender {
	return &spiralBlender{}
}

func (b *spiralBlender) Reset() {}

func (b *spiralBlender) ensureMap(width, height int) {
	if b.width == width && b.height == height {
		return
	}
	b.width = width
	b.height = height
	b.revealMap = make([][]float32, height)

	cx := float64(width) / 2
	cy := float64(height) / 2
	// Terminal cells are ~2x taller than wide; scale Y so the spiral looks circular.
	const aspect = 2.0

	maxR := math.Sqrt(cx*cx + (cy*aspect)*(cy*aspect))
	if maxR < 1 {
		maxR = 1
	}

	for y := range height {
		b.revealMap[y] = make([]float32, width)
		for x := range width {
			dx := float64(x) - cx
			dy := (float64(y) - cy) * aspect

			r := math.Sqrt(dx*dx+dy*dy) / maxR
			angle := math.Atan2(dy, dx)/(2*math.Pi) + 0.5 // [0, 1]

			// Archimedean spiral: combine radius and angle so points at the
			// same radius but different angles get staggered reveal times.
			t := (r + angle*spiralTurns) / (1 + spiralTurns)
			if t > 1 {
				t = 1
			}
			if t < 0 {
				t = 0
			}
			b.revealMap[y][x] = float32(t)
		}
	}
}

func (b *spiralBlender) Blend(orig, dst [][]client.Cell, intensity float32) {
	height := len(dst)
	if height == 0 {
		return
	}
	width := len(dst[0])
	if width == 0 {
		return
	}
	b.ensureMap(width, height)

	for y := range dst {
		srcRow := orig[y]
		dstRow := dst[y]
		rm := b.revealMap[y]
		for x := range dstRow {
			t := rm[x]
			if intensity >= t+spiralFeather {
				// Fully revealed — keep transformed.
				continue
			}
			if intensity <= t {
				// Not yet revealed — revert to original.
				dstRow[x] = srcRow[x]
				continue
			}
			// Feather zone — probabilistic soft edge.
			p := (intensity - t) / spiralFeather
			if rand.Float32() >= p {
				dstRow[x] = srcRow[x]
			}
		}
	}
}
