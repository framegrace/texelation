// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/events.go
// Summary: Implements events capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate events visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"time"

	"texelation/client"
)

type EffectTriggerType int

type PaneID = [16]byte

type PaneRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

type EffectTrigger struct {
	Type                 EffectTriggerType
	PaneID               PaneID
	RelatedPaneID        PaneID
	WorkspaceID          int
	Key                  rune
	Modifiers            uint16
	Active               bool
	Resizing             bool
	NewRect              PaneRect
	OldRect              PaneRect
	Title                string
	ZOrder               int
	DeltaCols, DeltaRows int
	Timestamp            time.Time
	PaneBuffer           [][]client.Cell
	Ghost                bool

	// Layout animation fields (Phase 2)
	SplitOrientation int     // 0=Horizontal, 1=Vertical (matches texel.SplitType)
	TargetWeight     float64 // Target weight after animation [0..1]
}

const (
	TriggerPaneCreated EffectTriggerType = iota
	TriggerPaneRemoved
	TriggerPaneActive
	TriggerPaneResizing
	TriggerPaneGeometry
	TriggerPaneTitle
	TriggerPaneZOrder
	TriggerPaneKey

	// Layout animation triggers (Phase 2)
	TriggerPaneSplit     // Pane is being split into two
	TriggerPaneRemoving  // Pane is being removed (animate out)
	TriggerPaneReplaced  // Pane is being replaced by another

	TriggerWorkspaceControl
	TriggerWorkspaceKey
	TriggerWorkspaceSwitch
	TriggerWorkspaceResize
	TriggerWorkspaceLayout
	TriggerWorkspaceZoom
	TriggerWorkspaceTheme

	TriggerClipboardChanged
	TriggerClockTick
	TriggerSessionState
)
