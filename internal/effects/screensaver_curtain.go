// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/screensaver_curtain.go
// Summary: Curtain fade blender for screensaver transitions.
// Usage: Sweeps the effect in like a curtain. Direction is randomized on each
//        activation: left-to-right, right-to-left, top-to-bottom, or bottom-to-top.

package effects

import (
	"math/rand"

	"github.com/framegrace/texelation/client"
)

const curtainFeather = 0.08 // soft edge width for the curtain sweep

type curtainDir int

const (
	curtainLeftToRight curtainDir = iota
	curtainRightToLeft
	curtainTopToBottom
	curtainBottomToTop
)

type curtainBlender struct {
	dir curtainDir
}

func newCurtainBlender() *curtainBlender {
	return &curtainBlender{dir: curtainDir(rand.Intn(4))}
}

func (b *curtainBlender) Reset() {
	b.dir = curtainDir(rand.Intn(4))
}

func (b *curtainBlender) Blend(orig, dst [][]client.Cell, intensity float32) {
	height := len(dst)
	if height == 0 {
		return
	}
	width := len(dst[0])
	if width == 0 {
		return
	}

	// Precompute denominator to avoid repeated division.
	var denom float32
	switch b.dir {
	case curtainLeftToRight, curtainRightToLeft:
		denom = float32(width - 1)
	case curtainTopToBottom, curtainBottomToTop:
		denom = float32(height - 1)
	}
	if denom < 1 {
		denom = 1
	}

	for y := range dst {
		srcRow := orig[y]
		dstRow := dst[y]
		for x := range dstRow {
			var t float32
			switch b.dir {
			case curtainLeftToRight:
				t = float32(x) / denom
			case curtainRightToLeft:
				t = 1 - float32(x)/denom
			case curtainTopToBottom:
				t = float32(y) / denom
			case curtainBottomToTop:
				t = 1 - float32(y)/denom
			}

			if intensity >= t+curtainFeather {
				continue
			}
			if intensity <= t {
				dstRow[x] = srcRow[x]
				continue
			}
			// Feather zone — probabilistic soft edge.
			p := (intensity - t) / curtainFeather
			if rand.Float32() >= p {
				dstRow[x] = srcRow[x]
			}
		}
	}
}
