// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/embedded.go
// Summary: Loads and caches parsed defaults from embedded JSON files.
// The embedded JSON files in defaults/ are the single source of truth.

package config

import (
	"encoding/json"
	"sync"

	"texelation/defaults"
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
			embeddedSystemErr = err
			return
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
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
func cloneConfig(cfg Config) Config {
	if cfg == nil {
		return nil
	}
	clone := make(Config, len(cfg))
	for k, v := range cfg {
		switch val := v.(type) {
		case Section:
			out := make(Section, len(val))
			for sk, sv := range val {
				out[sk] = sv
			}
			clone[k] = out
		case map[string]interface{}:
			out := make(Section, len(val))
			for sk, sv := range val {
				out[sk] = sv
			}
			clone[k] = out
		default:
			clone[k] = v
		}
	}
	return clone
}
