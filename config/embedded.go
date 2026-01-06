// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/embedded.go
// Summary: Loads and caches parsed defaults from embedded JSON files.
// The embedded JSON files in defaults/ are the single source of truth.

package config

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/framegrace/texelation/defaults"
)

var (
	embeddedSystemOnce sync.Once
	embeddedSystem     Config
	embeddedSystemErr  error

	embeddedApps   = make(map[string]Config)
	embeddedAppsMu sync.RWMutex
)

// embeddedSystemDefaults returns the parsed system defaults from embedded JSON.
// The result is cached after the first call.
func embeddedSystemDefaults() (Config, error) {
	embeddedSystemOnce.Do(func() {
		data, err := defaults.SystemConfig()
		if err != nil {
			log.Printf("Config: Failed to read embedded system defaults: %v", err)
			embeddedSystemErr = err
			return
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			log.Printf("Config: Failed to parse embedded system defaults: %v", err)
			embeddedSystemErr = err
			return
		}
		embeddedSystem = cfg
	})
	return embeddedSystem, embeddedSystemErr
}

// embeddedAppDefaults returns the parsed app defaults from embedded JSON.
// Results are cached per-app after the first call for each app.
func embeddedAppDefaults(app string) (Config, error) {
	// Check cache first
	embeddedAppsMu.RLock()
	if cfg, ok := embeddedApps[app]; ok {
		embeddedAppsMu.RUnlock()
		return cfg, nil
	}
	embeddedAppsMu.RUnlock()

	// Load from embedded
	data, err := defaults.AppConfig(app)
	if err != nil {
		// No embedded config for this app - not an error, just return nil
		return nil, nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("Config: Failed to parse embedded app defaults for %s: %v", app, err)
		return nil, err
	}

	// Cache the result
	embeddedAppsMu.Lock()
	embeddedApps[app] = cfg
	embeddedAppsMu.Unlock()

	return cfg, nil
}

// defaultSystemConfig returns a clone of the embedded system defaults.
// Used by store.go when writing initial config to disk.
func defaultSystemConfig() Config {
	cfg, err := embeddedSystemDefaults()
	if err != nil || cfg == nil {
		return nil
	}
	return cloneConfig(cfg)
}

// defaultAppConfig returns a clone of the embedded app defaults.
// Used by store.go when writing initial config to disk.
func defaultAppConfig(app string) Config {
	cfg, err := embeddedAppDefaults(app)
	if err != nil || cfg == nil {
		return nil
	}
	return cloneConfig(cfg)
}

// cloneConfig creates a deep copy of a config.
// All nested maps and slices are recursively cloned to prevent
// mutation of cached defaults.
func cloneConfig(cfg Config) Config {
	if cfg == nil {
		return nil
	}
	clone := make(Config, len(cfg))
	for k, v := range cfg {
		clone[k] = deepCloneValue(v)
	}
	return clone
}

// deepCloneValue recursively clones a value, handling maps and slices.
func deepCloneValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case Section:
		out := make(Section, len(val))
		for k, sv := range val {
			out[k] = deepCloneValue(sv)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, sv := range val {
			out[k] = deepCloneValue(sv)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, sv := range val {
			out[i] = deepCloneValue(sv)
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(val))
		for i, sv := range val {
			cloned := make(map[string]interface{}, len(sv))
			for k, v := range sv {
				cloned[k] = deepCloneValue(v)
			}
			out[i] = cloned
		}
		return out
	default:
		// Primitive types (string, int, float64, bool) are immutable
		return v
	}
}
