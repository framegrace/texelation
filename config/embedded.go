// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/embedded.go
// Summary: Embedded defaults loader for config store.

package config

import (
	"encoding/json"
	"log"

	configdefaults "texelation/defaults"
)

func defaultSystemConfig() Config {
	data, err := configdefaults.SystemConfig()
	return decodeDefault("system", data, err)
}

func defaultAppConfig(app string) Config {
	data, err := configdefaults.AppConfig(app)
	return decodeDefault("app "+app, data, err)
}

func decodeDefault(label string, data []byte, readErr error) Config {
	if readErr != nil {
		log.Printf("Config: Default %s config unavailable: %v", label, readErr)
		return nil
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("Config: Default %s config invalid: %v", label, err)
		return nil
	}
	return cfg
}
