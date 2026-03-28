// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/timeline.go
// Summary: Re-exports animation primitives from texelui/animation.
// The canonical implementation now lives in texelui. This file provides
// backward-compatible aliases so existing effects code compiles unchanged.

package effects

import "github.com/framegrace/texelui/animation"

// EasingFunc defines an easing function that maps progress [0,1] to eased value [0,1]
type EasingFunc = animation.EasingFunc

// Common easing functions — re-exported from texelui/animation.
var (
	EaseLinear       = animation.EaseLinear
	EaseSmoothstep   = animation.EaseSmoothstep
	EaseSmootherstep = animation.EaseSmootherstep
	EaseInQuad       = animation.EaseInQuad
	EaseOutQuad      = animation.EaseOutQuad
	EaseInOutQuad    = animation.EaseInOutQuad
	EaseInCubic      = animation.EaseInCubic
	EaseOutCubic     = animation.EaseOutCubic
	EaseInOutCubic   = animation.EaseInOutCubic
)

// AnimateOptions configures an animation transition.
type AnimateOptions = animation.AnimateOptions

// DefaultAnimateOptions returns options with smoothstep easing.
var DefaultAnimateOptions = animation.DefaultAnimateOptions

// Timeline provides thread-safe, per-key animation timelines.
type Timeline = animation.Timeline

// NewTimeline creates a new timeline manager.
var NewTimeline = animation.NewTimeline
