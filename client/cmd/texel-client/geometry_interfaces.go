package main

import (
	"time"

	"texelation/client"
)

type GeometryEffect interface {
	ID() string
	Active() bool
	Update(now time.Time)
	HandleTrigger(trigger EffectTrigger)
	ApplyGeometry(panes map[[16]byte]*geometryPaneState, workspace *geometryWorkspaceState)
}

type geometryPaneState struct {
	Pane    *client.PaneState
	Base    PaneRect
	Rect    PaneRect
	Buffer  [][]client.Cell
	Ghost   bool
	ZIndex  int
	Opacity float32
}

type geometryWorkspaceState struct {
	Width      int
	Height     int
	Zoomed     bool
	ZoomedPane PaneID
}
