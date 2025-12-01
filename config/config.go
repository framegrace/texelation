// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/config.go
// Summary: Server configuration loading from ~/.config/texelation/config.json

package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// Config holds the server configuration.
type Config struct {
	// DefaultApp is the app to launch on startup and in new panes/splits
	// Valid values: "launcher", "texelterm", "welcome"
	DefaultApp string `json:"defaultApp"`
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		DefaultApp: "launcher",
	}
}

// Load loads configuration from ~/.config/texelation/config.json
// If the file doesn't exist, returns default config.
// Command-line flags override config file values.
func Load() (*Config, error) {
	cfg := Default()

	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("Config: Failed to get user config dir: %v", err)
		return cfg, nil
	}

	configPath := filepath.Join(configDir, "texelation", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Config: No config file at %s, using defaults", configPath)
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	log.Printf("Config: Loaded from %s", configPath)
	return cfg, nil
}

// Save saves the configuration to ~/.config/texelation/config.json
func (c *Config) Save() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	texelationDir := filepath.Join(configDir, "texelation")
	if err := os.MkdirAll(texelationDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(texelationDir, "config.json")

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return err
	}

	log.Printf("Config: Saved to %s", configPath)
	return nil
}
