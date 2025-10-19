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

type PaneEffect interface {
	ID() string
	Active() bool
	Update(now time.Time)
	HandleTrigger(trigger EffectTrigger)
	ApplyPane(pane *client.PaneState, buffer [][]client.Cell)
}

type WorkspaceEffect interface {
	ID() string
	Active() bool
	Update(now time.Time)
	HandleTrigger(trigger EffectTrigger)
	ApplyWorkspace(buffer [][]client.Cell)
}
