package theme

import (
	"encoding/json"
	"github.com/gdamore/tcell/v2"
	"log"
	"os"
	"path/filepath"
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
)

// Get returns the singleton instance of the configuration.
// It loads the configuration from a file on its first call.
func Get() Config {
	once.Do(func() {
		instance = make(Config)
		err := instance.Load()
		if err != nil {
			log.Printf("Theme info: Could not load theme file (%v). A new default theme will be created.", err)
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
		return err
	}

	return json.Unmarshal(data, &c)
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
