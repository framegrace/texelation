// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/config.go
// Summary: System + app configuration store for texelation.

package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	systemConfigName = "texelation.json"
	legacyConfigName = "config.json"
	legacyThemeName  = "theme.json"
)

// Config stores configuration sections as JSON-compatible data.
type Config map[string]interface{}

// Section stores key/value pairs for a configuration section.
type Section map[string]interface{}

var (
	mu      sync.RWMutex
	once    sync.Once
	system  Config
	apps    map[string]Config
	loadErr error
)

// Err returns the most recent system config load error.
func Err() error {
	once.Do(initStore)
	mu.RLock()
	defer mu.RUnlock()
	return loadErr
}

// System returns the system configuration (texelation.json).
func System() Config {
	once.Do(initStore)
	mu.RLock()
	defer mu.RUnlock()
	return system
}

// App returns the config for a named app (apps/<app>/config.json).
func App(name string) Config {
	if name == "" {
		return nil
	}
	once.Do(initStore)

	mu.RLock()
	cfg := apps[name]
	mu.RUnlock()
	if cfg != nil {
		return cfg
	}

	mu.Lock()
	defer mu.Unlock()
	if cfg, ok := apps[name]; ok {
		return cfg
	}

	loaded, err := loadAppLocked(name)
	if err != nil {
		log.Printf("Config: Failed to load app %q config: %v", name, err)
		loaded = make(Config)
		applyAppDefaults(name, loaded)
	}
	apps[name] = loaded
	return loaded
}

// Reload refreshes the system config and all cached app configs.
func Reload() error {
	once.Do(initStore)

	mu.Lock()
	defer mu.Unlock()

	loadErr = loadSystemLocked()
	for name := range apps {
		if _, err := loadAppLocked(name); err != nil {
			log.Printf("Config: Failed to reload app %q config: %v", name, err)
		}
	}
	return loadErr
}

// ReloadSystem refreshes the system config.
func ReloadSystem() error {
	once.Do(initStore)
	mu.Lock()
	defer mu.Unlock()
	loadErr = loadSystemLocked()
	return loadErr
}

// ReloadApp refreshes a single app config.
func ReloadApp(name string) error {
	if name == "" {
		return nil
	}
	once.Do(initStore)
	mu.Lock()
	defer mu.Unlock()
	loaded, err := loadAppLocked(name)
	if err != nil {
		return err
	}
	apps[name] = loaded
	return nil
}

// SaveSystem persists the current system config to disk.
func SaveSystem() error {
	once.Do(initStore)
	mu.Lock()
	defer mu.Unlock()
	path, err := systemConfigPath()
	if err != nil {
		return err
	}
	return writeConfig(path, system)
}

// SaveApp persists a named app config to disk.
func SaveApp(name string) error {
	if name == "" {
		return nil
	}
	once.Do(initStore)
	mu.Lock()
	defer mu.Unlock()
	cfg := apps[name]
	if cfg == nil {
		cfg = make(Config)
		applyAppDefaults(name, cfg)
		apps[name] = cfg
	}
	path, err := appConfigPath(name)
	if err != nil {
		return err
	}
	return writeConfig(path, cfg)
}

// SetSystem replaces the in-memory system config with the provided config.
func SetSystem(cfg Config) {
	once.Do(initStore)
	mu.Lock()
	defer mu.Unlock()
	if cfg == nil {
		cfg = make(Config)
	}
	system = Clone(cfg)
}

// SetApp replaces the in-memory app config with the provided config.
func SetApp(name string, cfg Config) {
	if name == "" {
		return
	}
	once.Do(initStore)
	mu.Lock()
	defer mu.Unlock()
	if cfg == nil {
		cfg = make(Config)
	}
	apps[name] = Clone(cfg)
}

func initStore() {
	mu.Lock()
	defer mu.Unlock()
	system = make(Config)
	apps = make(map[string]Config)
	loadErr = loadSystemLocked()
}

func readConfig(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, true, err
	}
	return cfg, true, nil
}

func writeConfig(path string, cfg Config) error {
	if cfg == nil {
		cfg = make(Config)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
