// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/theme/overrides.go
// Summary: Helpers for applying per-app theme overrides.

package theme

import (
	"encoding/json"
	"strings"
)

// Clone returns a shallow copy of the theme config and its sections.
func Clone(cfg Config) Config {
	if cfg == nil {
		return nil
	}
	clone := make(Config, len(cfg))
	for sectionName, section := range cfg {
		if section == nil {
			continue
		}
		out := make(Section, len(section))
		for key, value := range section {
			out[key] = value
		}
		clone[sectionName] = out
	}
	return clone
}

// WithOverrides merges theme overrides on top of a base theme config.
func WithOverrides(base Config, overrides Config) Config {
	if len(overrides) == 0 {
		return base
	}
	merged := Clone(base)
	if merged == nil {
		merged = make(Config)
	}
	for sectionName, section := range overrides {
		if section == nil {
			continue
		}
		dest := merged[sectionName]
		if dest == nil {
			dest = make(Section)
			merged[sectionName] = dest
		}
		for key, value := range section {
			dest[key] = value
		}
	}
	return merged
}

// ParseOverrides converts raw config data into a theme config override map.
func ParseOverrides(raw interface{}) Config {
	switch v := raw.(type) {
	case nil:
		return nil
	case Config:
		return v
	case map[string]Section:
		cfg := make(Config, len(v))
		for key, section := range v {
			cfg[key] = section
		}
		return cfg
	case map[string]interface{}:
		return overridesFromMap(v)
	case Section:
		return overridesFromMap(map[string]interface{}(v))
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return nil
		}
		return overridesFromMap(decoded)
	case []byte:
		if len(v) == 0 {
			return nil
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(v, &decoded); err != nil {
			return nil
		}
		return overridesFromMap(decoded)
	default:
		return nil
	}
}

func overridesFromMap(raw map[string]interface{}) Config {
	if len(raw) == 0 {
		return nil
	}
	cfg := make(Config)
	for sectionName, sectionRaw := range raw {
		switch section := sectionRaw.(type) {
		case Section:
			if len(section) > 0 {
				cfg[sectionName] = section
			}
		case map[string]interface{}:
			if len(section) > 0 {
				cfg[sectionName] = Section(section)
			}
		}
	}
	if len(cfg) == 0 {
		return nil
	}
	return cfg
}
