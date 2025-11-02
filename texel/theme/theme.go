// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/theme/theme.go
// Summary: Implements theme capabilities for the theme subsystem.
// Usage: Accessed by both server and client when reading theme from theme configurations.
// Notes: Ensures user theme files always contain required defaults.

package theme

import (
	"encoding/json"
	"errors"
	"github.com/gdamore/tcell/v2"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Config represents the entire theme and configuration structure,
// which is a map of section names to their configurations.
type Config map[string]Section

// Section represents a namespaced part of the configuration.
type Section map[string]interface{}

var (
	instance Config
	once     sync.Once
	loadErr  error
)

// Get returns the singleton instance of the configuration.
// It loads the configuration from a file on its first call.
func Get() Config {
	once.Do(func() {
		instance = make(Config)
		loadErr = instance.Load()
		if loadErr != nil {
			log.Printf("Theme info: Could not load theme file (%v). A new default theme will be created.", loadErr)
			// Defaults are now applied explicitly in main.go using RegisterDefaults.
			// This ensures we start with an empty but valid config map on error.
		}
	})
	return instance
}

// configPath returns the default path for the theme file.
func configPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "texelation", "theme.json"), nil
}

// Load reads the configuration from the default path.
func (c Config) Load() error {
	path, err := configPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	for sectionName, section := range c {
		for key, value := range section {
			if m, ok := value.(map[string]interface{}); ok {
				c[sectionName][key] = m
			}
		}
	}
	return nil
}

// Save writes the current configuration to the default path.
func (c Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GetColor retrieves a color from the theme.
func (c Config) GetColor(sectionName, key string, defaultColor tcell.Color) tcell.Color {
	if section, ok := c[sectionName]; ok {
		if val, ok := section[key]; ok {
			if colorStr, ok := val.(string); ok {
				return HexColor(colorStr).ToTcell()
			}
		}
	}
	return defaultColor
}

// GetString retrieves a string value from the theme/config.
func (c Config) GetString(sectionName, key, defaultValue string) string {
	if section, ok := c[sectionName]; ok {
		if val, ok := section[key]; ok {
			if strVal, ok := val.(string); ok {
				return strVal
			}
		}
	}
	return defaultValue
}

// GetFloat retrieves a float value from the theme/config.
func (c Config) GetFloat(sectionName, key string, defaultValue float64) float64 {
	if section, ok := c[sectionName]; ok {
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
	}
	return defaultValue
}

// GetBool retrieves a boolean value from the theme/config.
func (c Config) GetBool(sectionName, key string, defaultValue bool) bool {
	if section, ok := c[sectionName]; ok {
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
	}
	return defaultValue
}

// RegisterDefaults ensures that a section has default values for keys that are not already set by the user.
// This allows adding new theme options without overwriting user customizations.
func (c Config) RegisterDefaults(sectionName string, defaults Section) {
	if _, ok := c[sectionName]; !ok {
		c[sectionName] = make(Section)
	}

	for key, defaultValue := range defaults {
		if _, ok := c[sectionName][key]; !ok {
			// Key does not exist in the user's config, so set the default.
			c[sectionName][key] = defaultValue
		}
	}
}

// Err returns any error encountered during the initial load of the theme file.
func Err() error {
	return loadErr
}
