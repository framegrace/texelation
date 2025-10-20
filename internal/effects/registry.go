// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/registry.go
// Summary: Implements registry capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate registry visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import "sync"

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Factory constructs an effect given its configuration map.
type Factory func(EffectConfig) (Effect, error)

// Register associates an effect ID with a factory. It panics on duplicate IDs.
func Register(id string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[id]; exists {
		panic("effects: duplicate registration for " + id)
	}
	registry[id] = factory
}

// Lookup fetches a factory by ID.
func Lookup(id string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[id]
	return f, ok
}

// RegisteredIDs returns the set of effect identifiers currently registered.
func RegisteredIDs() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	return ids
}
