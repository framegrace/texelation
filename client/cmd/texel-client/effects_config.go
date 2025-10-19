package main

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"
)

type EffectConfig map[string]interface{}

type paneEffectSpec struct {
	ID     string
	Config EffectConfig
}

type workspaceEffectSpec struct {
	ID     string
	Config EffectConfig
}

type effectRegistry struct {
	paneFactories      map[string]func(EffectConfig) (PaneEffect, error)
	workspaceFactories map[string]func(EffectConfig) (WorkspaceEffect, error)
}

func newEffectRegistry() *effectRegistry {
	reg := &effectRegistry{
		paneFactories:      make(map[string]func(EffectConfig) (PaneEffect, error)),
		workspaceFactories: make(map[string]func(EffectConfig) (WorkspaceEffect, error)),
	}
	reg.paneFactories["inactive-overlay"] = func(cfg EffectConfig) (PaneEffect, error) {
		color := parseColorOrDefault(cfg, "color", defaultInactiveColor)
		intensity := float32(parseFloatOrDefault(cfg, "intensity", 0.35))
		duration := parseDurationOrDefault(cfg, "duration_ms", 400)
		return newInactiveOverlayEffect(color, intensity, duration), nil
	}
	reg.workspaceFactories["rainbow"] = func(cfg EffectConfig) (WorkspaceEffect, error) {
		speed := parseFloatOrDefault(cfg, "speed_hz", 0.5)
		return newWorkspaceRainbowEffect(speed), nil
	}
	reg.workspaceFactories["flash"] = func(cfg EffectConfig) (WorkspaceEffect, error) {
		color := parseColorOrDefault(cfg, "color", defaultFlashColor)
		duration := parseDurationOrDefault(cfg, "duration_ms", 250)
		return newWorkspaceFlashEffect(color, duration), nil
	}
	return reg
}

func (r *effectRegistry) createPaneEffect(spec paneEffectSpec) PaneEffect {
	if factory, ok := r.paneFactories[spec.ID]; ok {
		if eff, err := factory(spec.Config); err == nil {
			return eff
		}
	}
	return nil
}

func (r *effectRegistry) createWorkspaceEffect(spec workspaceEffectSpec) WorkspaceEffect {
	if factory, ok := r.workspaceFactories[spec.ID]; ok {
		if eff, err := factory(spec.Config); err == nil {
			return eff
		}
	}
	return nil
}

func parsePaneEffectSpecs(raw interface{}) ([]paneEffectSpec, error) {
	var entries []map[string]interface{}
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if v == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(v), &entries); err != nil {
			return nil, err
		}
	case []interface{}:
		bytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(bytes, &entries); err != nil {
			return nil, err
		}
	case []map[string]interface{}:
		entries = v
	default:
		return nil, nil
	}
	specs := make([]paneEffectSpec, 0, len(entries))
	for _, entry := range entries {
		idVal, _ := entry["id"].(string)
		if idVal == "" {
			continue
		}
		cfg := make(EffectConfig)
		for k, v := range entry {
			if k == "id" {
				continue
			}
			cfg[k] = v
		}
		specs = append(specs, paneEffectSpec{ID: idVal, Config: cfg})
	}
	return specs, nil
}

func parseWorkspaceEffectSpecs(raw interface{}) ([]workspaceEffectSpec, error) {
	var entries []map[string]interface{}
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if v == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(v), &entries); err != nil {
			return nil, err
		}
	case []interface{}:
		bytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(bytes, &entries); err != nil {
			return nil, err
		}
	case []map[string]interface{}:
		entries = v
	default:
		return nil, nil
	}
	specs := make([]workspaceEffectSpec, 0, len(entries))
	for _, entry := range entries {
		idVal, _ := entry["id"].(string)
		if idVal == "" {
			continue
		}
		cfg := make(EffectConfig)
		for k, v := range entry {
			if k == "id" {
				continue
			}
			cfg[k] = v
		}
		specs = append(specs, workspaceEffectSpec{ID: idVal, Config: cfg})
	}
	return specs, nil
}

func parseColorOrDefault(cfg EffectConfig, key string, fallback tcell.Color) tcell.Color {
	if cfg == nil {
		return fallback
	}
	if raw, ok := cfg[key]; ok {
		if str, ok := raw.(string); ok {
			if color, ok := parseHexColor(str); ok {
				return color
			}
		}
	}
	return fallback
}

func parseFloatOrDefault(cfg EffectConfig, key string, fallback float64) float64 {
	if cfg == nil {
		return fallback
	}
	if raw, ok := cfg[key]; ok {
		switch v := raw.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func parseDurationOrDefault(cfg EffectConfig, key string, fallbackMS int64) time.Duration {
	if cfg == nil {
		return time.Duration(fallbackMS) * time.Millisecond
	}
	if raw, ok := cfg[key]; ok {
		switch v := raw.(type) {
		case int:
			return time.Duration(v) * time.Millisecond
		case int64:
			return time.Duration(v) * time.Millisecond
		case float64:
			return time.Duration(v) * time.Millisecond
		case string:
			if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
				return time.Duration(parsed) * time.Millisecond
			}
		}
	}
	return time.Duration(fallbackMS) * time.Millisecond
}
