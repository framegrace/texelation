package main

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
