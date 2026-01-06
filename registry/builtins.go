// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: registry/builtins.go
// Summary: Supports init-time registration of built-in apps.

package registry

import "sync"

// BuiltInProvider returns a manifest and factory for a registry instance.
type BuiltInProvider func(reg *Registry) (*Manifest, AppFactory)

var (
	builtInMu        sync.RWMutex
	builtInProviders []BuiltInProvider
)

// RegisterBuiltInProvider registers an init-time built-in provider.
func RegisterBuiltInProvider(provider BuiltInProvider) {
	if provider == nil {
		return
	}
	builtInMu.Lock()
	builtInProviders = append(builtInProviders, provider)
	builtInMu.Unlock()
}

// RegisterBuiltIns registers all init-time built-ins into the provided registry.
func RegisterBuiltIns(reg *Registry) {
	if reg == nil {
		return
	}
	builtInMu.RLock()
	providers := append([]BuiltInProvider(nil), builtInProviders...)
	builtInMu.RUnlock()

	for _, provider := range providers {
		manifest, factory := provider(reg)
		if manifest == nil || factory == nil {
			continue
		}
		reg.RegisterBuiltIn(manifest, factory)
	}
}
