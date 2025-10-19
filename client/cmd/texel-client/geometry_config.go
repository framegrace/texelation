package main

import (
	"time"
)

type geometryConfig struct {
	SplitEffect    string
	RemoveEffect   string
	ZoomEffect     string
	SplitDuration  time.Duration
	RemoveDuration time.Duration
	ZoomDuration   time.Duration
}

const (
	geometryEffectGhost   = "ghost_n_grow"
	geometryEffectStretch = "stretch"
	geometryEffectExpand  = "expand"
)

func parseGeometryConfig(section map[string]interface{}) geometryConfig {
	cfg := geometryConfig{
		SplitEffect:    geometryEffectStretch,
		RemoveEffect:   geometryEffectGhost,
		ZoomEffect:     geometryEffectExpand,
		SplitDuration:  160 * time.Millisecond,
		RemoveDuration: 160 * time.Millisecond,
		ZoomDuration:   220 * time.Millisecond,
	}
	if section == nil {
		return cfg
	}
	if raw, ok := section["split_effect"].(string); ok && raw != "" {
		cfg.SplitEffect = raw
	}
	if raw, ok := section["remove_effect"].(string); ok && raw != "" {
		cfg.RemoveEffect = raw
	}
	if raw, ok := section["zoom_effect"].(string); ok && raw != "" {
		cfg.ZoomEffect = raw
	}
	if raw, ok := section["split_mode"].(string); ok && raw != "" {
		cfg.SplitEffect = legacySplitModeToEffect(raw)
	}
	if raw, ok := section["remove_mode"].(string); ok && raw != "" {
		cfg.RemoveEffect = legacyRemoveModeToEffect(raw)
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

func legacySplitModeToEffect(mode string) string {
	switch mode {
	case "ghost":
		return geometryEffectGhost
	case "stretch":
		return geometryEffectStretch
	default:
		return geometryEffectStretch
	}
}

func legacyRemoveModeToEffect(mode string) string {
	switch mode {
	case "ghost":
		return geometryEffectGhost
	case "stretch":
		return geometryEffectStretch
	default:
		return geometryEffectGhost
	}
}
