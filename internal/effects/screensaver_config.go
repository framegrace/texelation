// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/screensaver_config.go
// Summary: Parses the "screensaver" section from system config (texelation.json).

package effects

import "time"

// ScreensaverConfig holds parsed screensaver settings.
type ScreensaverConfig struct {
	Enabled     bool
	Timeout     time.Duration
	EffectID    string
	LockEnabled bool
	LockTimeout time.Duration
}

// ParseScreensaverConfig parses the "screensaver" section from system config.
// All fields are optional with sensible defaults.
func ParseScreensaverConfig(section map[string]interface{}) ScreensaverConfig {
	cfg := ScreensaverConfig{
		Enabled:     false,
		Timeout:     5 * time.Minute,
		EffectID:    "crypt",
		LockEnabled: false,
		LockTimeout: 15 * time.Minute,
	}
	if section == nil {
		return cfg
	}
	if v, ok := section["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if v, ok := section["timeout_minutes"].(float64); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Minute
	}
	if v, ok := section["effect"].(string); ok && v != "" {
		cfg.EffectID = v
	}
	if v, ok := section["lock_enabled"].(bool); ok {
		cfg.LockEnabled = v
	}
	if v, ok := section["lock_timeout_minutes"].(float64); ok && v > 0 {
		cfg.LockTimeout = time.Duration(v) * time.Minute
	}
	return cfg
}
