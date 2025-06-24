package texel

import (
	"fmt"
)

type Effect interface {
	Apply(buffer [][]Cell) [][]Cell
}

type EventType int

const (
	EventControlOn EventType = iota
	EventControlOff
	EventActivePaneChanged
)

func (e EventType) String() string {
	switch e {
	case EventControlOn:
		return "ControlOn"
	case EventControlOff:
		return "ControlOff"
	case EventActivePaneChanged:
		return "ActivePaneChanged"
	default:
		return "UnknownEvent"
	}
}

func (e Event) String() string {
	return fmt.Sprintf("Event: %s", e.Type)
}

// Event represents an event that occurs on the screen.
type Event struct {
	Type EventType
}

// EventListener defines an interface for objects that can respond to screen events.
type EventListener interface {
	OnEvent(owner *Pane, event Event)
}

type EffectState int

const (
	StateOff EffectState = iota
	StateFadingIn
	StateOn
	StateFadingOut
)
