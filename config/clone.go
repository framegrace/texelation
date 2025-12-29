// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/clone.go
// Summary: Clone helpers for config maps.

package config

// Clone returns a shallow copy of the config and its sections.
func Clone(cfg Config) Config {
	if cfg == nil {
		return nil
	}
	clone := make(Config, len(cfg))
	for sectionName, section := range cfg {
		switch v := section.(type) {
		case map[string]interface{}:
			out := make(Section, len(v))
			for key, value := range v {
				out[key] = value
			}
			clone[sectionName] = out
		case Section:
			out := make(Section, len(v))
			for key, value := range v {
				out[key] = value
			}
			clone[sectionName] = out
		default:
			clone[sectionName] = v
		}
	}
	return clone
}
