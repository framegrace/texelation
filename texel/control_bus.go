package texel

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ControlHandler processes a control trigger with an optional payload.
type ControlHandler func(payload interface{}) error

// ControlCapability describes a control trigger registered on a control bus.
type ControlCapability struct {
	ID          string
	Description string
}

// ControlBus allows apps to trigger or register control hooks for event signaling.
// This is the primary communication pattern used throughout Texelation for apps
// to signal events to their containers and for cards to expose controls.
type ControlBus interface {
	Trigger(id string, payload interface{}) error
	Capabilities() []ControlCapability
	Register(id, description string, handler ControlHandler) error
	Unregister(id string)
}

// ControlRegistry is implemented by control bus providers to let components publish controls.
type ControlRegistry interface {
	Register(id, description string, handler ControlHandler) error
}

type controlBus struct {
	mu        sync.RWMutex
	handlers  map[string]ControlHandler
	capByID   map[string]ControlCapability
	capSorted []ControlCapability
}

// NewControlBus creates a new control bus for event signaling.
func NewControlBus() ControlBus {
	return &controlBus{
		handlers: make(map[string]ControlHandler),
		capByID:  make(map[string]ControlCapability),
	}
}

func (b *controlBus) Register(id, description string, handler ControlHandler) error {
	if id == "" {
		return errors.New("texel: control id must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("texel: control %q must provide a handler", id)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.handlers[id]; exists {
		return fmt.Errorf("texel: control %q already registered", id)
	}
	b.handlers[id] = handler
	cap := ControlCapability{ID: id, Description: description}
	b.capByID[id] = cap
	b.rebuildCapabilities()
	return nil
}

func (b *controlBus) Unregister(id string) {
	if id == "" {
		return
	}
	b.mu.Lock()
	delete(b.handlers, id)
	delete(b.capByID, id)
	b.rebuildCapabilities()
	b.mu.Unlock()
}

func (b *controlBus) rebuildCapabilities() {
	b.capSorted = b.capSorted[:0]
	for _, cap := range b.capByID {
		b.capSorted = append(b.capSorted, cap)
	}
	sort.Slice(b.capSorted, func(i, j int) bool {
		return b.capSorted[i].ID < b.capSorted[j].ID
	})
}

func (b *controlBus) Trigger(id string, payload interface{}) error {
	b.mu.RLock()
	handler, ok := b.handlers[id]
	b.mu.RUnlock()
	if !ok {
		return fmt.Errorf("texel: unknown control %q", id)
	}
	return handler(payload)
}

func (b *controlBus) Capabilities() []ControlCapability {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]ControlCapability, len(b.capSorted))
	copy(out, b.capSorted)
	return out
}
