// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/types.go
// Summary: Typed access helpers for config store data.

package config

import (
	"encoding/json"
	"strconv"
)

// Section returns the named section or nil if missing.
func (c Config) Section(sectionName string) Section {
	if c == nil {
		return nil
	}
	if sectionName == "" {
		return Section(c)
	}
	if raw, ok := c[sectionName]; ok {
		switch v := raw.(type) {
		case Section:
			return v
		case map[string]interface{}:
			return Section(v)
		}
	}
	return nil
}

// RegisterDefaults ensures a section has defaults without overwriting existing keys.
func (c Config) RegisterDefaults(sectionName string, defaults Section) {
	if c == nil || defaults == nil {
		return
	}
	section := c.Section(sectionName)
	if section == nil {
		section = make(Section)
		if sectionName == "" {
			for k, v := range defaults {
				if _, ok := c[k]; !ok {
					c[k] = v
				}
			}
			return
		}
		c[sectionName] = section
	}

	for key, value := range defaults {
		if _, ok := section[key]; !ok {
			section[key] = value
		}
	}
}

// GetString retrieves a string value from the config.
func (c Config) GetString(sectionName, key, defaultValue string) string {
	section := c.Section(sectionName)
	if section == nil {
		return defaultValue
	}
	if val, ok := section[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return defaultValue
}

// GetFloat retrieves a float value from the config.
func (c Config) GetFloat(sectionName, key string, defaultValue float64) float64 {
	section := c.Section(sectionName)
	if section == nil {
		return defaultValue
	}
	if val, ok := section[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case json.Number:
			if parsed, err := v.Float64(); err == nil {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				return parsed
			}
		}
	}
	return defaultValue
}

// GetInt retrieves an integer value from the config.
func (c Config) GetInt(sectionName, key string, defaultValue int) int {
	section := c.Section(sectionName)
	if section == nil {
		return defaultValue
	}
	if val, ok := section[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case float32:
			return int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				return int(parsed)
			}
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				return parsed
			}
		}
	}
	return defaultValue
}

// GetBool retrieves a boolean value from the config.
func (c Config) GetBool(sectionName, key string, defaultValue bool) bool {
	section := c.Section(sectionName)
	if section == nil {
		return defaultValue
	}
	if val, ok := section[key]; ok {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			if parsed, err := strconv.ParseBool(v); err == nil {
				return parsed
			}
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				return parsed != 0
			}
		case float64:
			return v != 0
		case int:
			return v != 0
		}
	}
	return defaultValue
}
