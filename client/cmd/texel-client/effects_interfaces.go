package main

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
