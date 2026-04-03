// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/interfaces.go
// Summary: Implements interfaces capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate interfaces visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"time"

	"github.com/framegrace/texelation/client"
)

// Target identifies where an effect applies its visual changes.
type Target int

const (
	TargetPane Target = iota
	TargetWorkspace
)

// Effect represents a visual overlay that can react to triggers and mutate pane/workspace buffers.
type Effect interface {
	ID() string
	Active(now time.Time) bool
	Update(now time.Time)
	HandleTrigger(trigger EffectTrigger)
	ApplyPane(pane *client.PaneState, buffer [][]client.Cell)
	ApplyWorkspace(buffer [][]client.Cell)
}

// FrameSkipper is an optional interface for effects that don't need to render
// every tick. FrameSkip returns N where only 1-in-N ticks produce a render.
// Effects that change fewer cells per frame can return 1 (no skip) while
// heavy full-screen effects can return 3 (~10fps) to reduce terminal output.
type FrameSkipper interface {
	FrameSkip() int
}

// PaneAdapter wraps an effect and suppresses workspace application.
type PaneAdapter struct{ Effect }

// ApplyWorkspace for pane-bound effects is a no-op.
func (PaneAdapter) ApplyWorkspace(buffer [][]client.Cell) {}

// WorkspaceAdapter wraps an effect and suppresses pane application.
type WorkspaceAdapter struct{ Effect }

// ApplyPane for workspace-bound effects is a no-op.
func (WorkspaceAdapter) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}
