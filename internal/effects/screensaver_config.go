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
	FadeStyle   string
	FadeIn      time.Duration
	FadeOut     time.Duration
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
		FadeStyle:   "dissolve",
		FadeIn:      5 * time.Second,
		FadeOut:     500 * time.Millisecond,
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
		cfg.Timeout = time.Duration(v * float64(time.Minute))
	}
	if v, ok := section["effect"].(string); ok && v != "" {
		cfg.EffectID = v
	}
	if v, ok := section["fade_style"].(string); ok && v != "" {
		cfg.FadeStyle = v
	}
	if v, ok := section["fade_in_seconds"].(float64); ok && v > 0 {
		cfg.FadeIn = time.Duration(v * float64(time.Second))
	}
	if v, ok := section["fade_out_seconds"].(float64); ok && v > 0 {
		cfg.FadeOut = time.Duration(v * float64(time.Second))
	}
	if v, ok := section["lock_enabled"].(bool); ok {
		cfg.LockEnabled = v
	}
	if v, ok := section["lock_timeout_minutes"].(float64); ok && v > 0 {
		cfg.LockTimeout = time.Duration(v * float64(time.Minute))
	}
	return cfg
}
