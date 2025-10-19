package main

import (
	"time"
)

type geometryConfig struct {
	SplitMode      string
	RemoveMode     string
	SplitDuration  time.Duration
	RemoveDuration time.Duration
	ZoomDuration   time.Duration
}

const (
	splitModeGhost   = "ghost"
	splitModeStretch = "stretch"

	removeModeGhost   = "ghost"
	removeModeStretch = "stretch"
)

func parseGeometryConfig(section map[string]interface{}) geometryConfig {
	cfg := geometryConfig{
		SplitMode:      splitModeStretch,
		RemoveMode:     removeModeGhost,
		SplitDuration:  160 * time.Millisecond,
		RemoveDuration: 160 * time.Millisecond,
		ZoomDuration:   220 * time.Millisecond,
	}
	if section == nil {
		return cfg
	}
	if raw, ok := section["split_mode"].(string); ok && raw != "" {
		cfg.SplitMode = raw
	}
	if raw, ok := section["remove_mode"].(string); ok && raw != "" {
		cfg.RemoveMode = raw
	}
	if dur := parseDurationOrDefault(EffectConfig(section), "split_duration_ms", cfg.SplitDuration.Milliseconds()); dur > 0 {
		cfg.SplitDuration = dur
	}
	if dur := parseDurationOrDefault(EffectConfig(section), "remove_duration_ms", cfg.RemoveDuration.Milliseconds()); dur > 0 {
		cfg.RemoveDuration = dur
	}
	if dur := parseDurationOrDefault(EffectConfig(section), "zoom_duration_ms", cfg.ZoomDuration.Milliseconds()); dur > 0 {
		cfg.ZoomDuration = dur
	}
	return cfg
}
