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

	"texelation/client"
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
	Active() bool
	Update(now time.Time)
	HandleTrigger(trigger EffectTrigger)
	ApplyPane(pane *client.PaneState, buffer [][]client.Cell)
	ApplyWorkspace(buffer [][]client.Cell)
}

// PaneAdapter wraps an effect and suppresses workspace application.
type PaneAdapter struct{ Effect }

// ApplyWorkspace for pane-bound effects is a no-op.
func (PaneAdapter) ApplyWorkspace(buffer [][]client.Cell) {}

// WorkspaceAdapter wraps an effect and suppresses pane application.
type WorkspaceAdapter struct{ Effect }

// ApplyPane for workspace-bound effects is a no-op.
func (WorkspaceAdapter) ApplyPane(pane *client.PaneState, buffer [][]client.Cell) {}
