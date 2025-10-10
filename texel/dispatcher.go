package texel

import "sync"
import "github.com/gdamore/tcell/v2"

// EventType defines the type of an event.
type EventType int

const (
	// Control Events
	EventControlOn EventType = iota
	EventControlOff
	// Pane Events
	EventPaneActiveChanged
	EventPaneClosed
	// Screen/Global Events
	EventStateUpdate
	// Add other event types here as needed
)

// Event represents a message passed through the system.
// It has a type and can carry an arbitrary data payload.
type Event struct {
	Type    EventType
	Payload interface{}
}

// StatePayload is the data associated with a MsgStateUpdate.
type StatePayload struct {
	AllWorkspaces  []int
	WorkspaceID    int
	InControlMode  bool
	SubMode        rune
	ActiveTitle    string
	DesktopBgColor tcell.Color // Added: Desktop's default background color
	Zoomed         bool
	ZoomedPaneID   [16]byte
}

// Listener is an interface that any component can implement to receive events.
type Listener interface {
	// OnEvent is the callback method for receiving events.
	OnEvent(event Event)
}

// EventDispatcher manages a list of listeners and broadcasts events to them.
type EventDispatcher struct {
	mu        sync.RWMutex
	listeners []Listener
}

// NewEventDispatcher creates a new dispatcher.
func NewEventDispatcher() *EventDispatcher {
	return &EventDispatcher{
		listeners: make([]Listener, 0),
	}
}

// Subscribe adds a new listener to receive events.
func (d *EventDispatcher) Subscribe(listener Listener) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listeners = append(d.listeners, listener)
}

// Unsubscribe removes a listener.
func (d *EventDispatcher) Unsubscribe(listener Listener) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, l := range d.listeners {
		if l == listener {
			d.listeners = append(d.listeners[:i], d.listeners[i+1:]...)
			break
		}
	}
}

// Broadcast sends an event to all subscribed listeners.
func (d *EventDispatcher) Broadcast(event Event) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, l := range d.listeners {
		l.OnEvent(event)
	}
}
