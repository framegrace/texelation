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
	"strings"
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
	mu       sync.RWMutex // Protects access to the instance map
)

// Get returns the singleton instance of the configuration.
// It loads the configuration from a file on its first call.
func Get() Config {
	once.Do(func() {
		instance = make(Config)
		loadErr = instance.Load()
		if loadErr != nil {
			log.Printf("Theme info: Could not load theme file (%v). A new default theme will be created.", loadErr)
		}
		
		// Load Palette
		paletteName := instance.GetString("meta", "palette", "mocha")
		if err := LoadPalette(paletteName); err != nil {
			log.Printf("Theme warning: Could not load palette '%s' (%v), falling back to 'mocha'", paletteName, err)
			LoadPalette("mocha")
		}
		
		// Apply standard semantics (Catppuccin defaults)
		instance.LoadStandardSemantics()
	})
	
	mu.RLock()
	defer mu.RUnlock()
	return instance
}

// GetLoadError returns the error that occurred during theme loading, if any.
// This allows callers to check if the theme loaded successfully.
func GetLoadError() error {
	_ = Get() // Ensure theme has been loaded
	mu.RLock()
	defer mu.RUnlock()
	return loadErr
}

// Reload forces a re-read of the theme file and palette.
func Reload() error {
	mu.Lock()
	defer mu.Unlock()

	log.Println("Theme: Reloading configuration...")
	
	// Re-create instance to clear old state
	newInstance := make(Config)
	if err := newInstance.Load(); err != nil {
		log.Printf("Theme: Failed to reload theme file: %v", err)
		return err
	}
	
	instance = newInstance

	// Re-load Palette
	paletteName := instance.GetString("meta", "palette", "mocha")
	if err := LoadPalette(paletteName); err != nil {
		log.Printf("Theme warning: Could not load palette '%s' (%v), falling back to 'mocha'", paletteName, err)
		LoadPalette("mocha")
	}

	// Re-apply defaults
	instance.LoadStandardSemantics()
	ApplyDefaults(instance)

	return nil
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
// Note: This method is internal and not thread-safe on its own; callers must hold the lock.
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
	mu.Lock()
	defer mu.Unlock()
	
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
	mu.RLock()
	defer mu.RUnlock()

	val, ok := c.getRawValue(sectionName, key)
	if !ok {
		return defaultColor
	}

	if colorStr, ok := val.(string); ok {
		return c.resolveColorString(colorStr, 0)
	}
	return defaultColor
}

func (c Config) getRawValue(sectionName, key string) (interface{}, bool) {
	if section, ok := c[sectionName]; ok {
		if val, ok := section[key]; ok {
			return val, true
		}
	}
	return nil, false
}

func (c Config) resolveColorString(s string, depth int) tcell.Color {
	if depth > 5 { // Prevent infinite recursion
		return tcell.ColorDefault
	}

	// 1. Hex Color
	if strings.HasPrefix(s, "#") {
		return HexColor(s).ToTcell()
	}

	// 2. Palette Reference
	if strings.HasPrefix(s, "@") {
		return ResolveColorName(strings.TrimPrefix(s, "@"))
	}

	// 3. Indirection (section.key)
	if parts := strings.Split(s, "."); len(parts) == 2 {
		if refVal, ok := c.getRawValue(parts[0], parts[1]); ok {
			if refStr, ok := refVal.(string); ok {
				return c.resolveColorString(refStr, depth+1)
			}
		}
	}

	// 4. Implicit "ui" section Indirection
	// If "s" is something like "bg.base", check "ui.bg.base"
	if refVal, ok := c.getRawValue("ui", s); ok {
		if refStr, ok := refVal.(string); ok {
			return c.resolveColorString(refStr, depth+1)
		}
	}

	// 5. Try parsing as hex if it looks like one but missed # (legacy/fallback)
	if len(s) == 6 {
		// Simple check to see if it's hex-like
		if _, err := strconv.ParseInt(s, 16, 64); err == nil {
			return HexColor("#" + s).ToTcell()
		}
	}

	return tcell.ColorDefault
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

// GetInt retrieves an integer value from the theme/config.
func (c Config) GetInt(sectionName, key string, defaultValue int) int {
	if section, ok := c[sectionName]; ok {
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
