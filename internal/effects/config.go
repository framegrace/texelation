// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/config.go
// Summary: Implements config capabilities for the client effect subsystem.
// Usage: Used by the client runtime to orchestrate config visuals before rendering.
// Notes: Centralises every pane and workspace overlay so they can be configured via themes.

package effects

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

type EffectConfig map[string]interface{}

type BindingSpec struct {
	Event  EffectTriggerType
	Target Target
	Effect string
	Config EffectConfig
}

func CreateEffect(id string, cfg EffectConfig) (Effect, error) {
	factory, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("unknown effect id %q", id)
	}
	return factory(cfg)
}

func ParseBindings(raw interface{}) ([]BindingSpec, error) {
	entries, err := normaliseEntries(raw)
	if err != nil {
		return nil, err
	}
	specs := make([]BindingSpec, 0, len(entries))
	for _, entry := range entries {
		effectID, _ := entry["effect"].(string)
		if effectID == "" {
			continue
		}
		eventName, _ := entry["event"].(string)
		evt, ok := ParseTrigger(eventName)
		if !ok {
			return nil, fmt.Errorf("unknown effect event %q", eventName)
		}
		targetName, _ := entry["target"].(string)
		tgt, ok := parseTarget(targetName)
		if !ok {
			return nil, fmt.Errorf("unknown effect target %q", targetName)
		}
		cfg := make(EffectConfig)
		if params, ok := entry["params"].(map[string]interface{}); ok {
			for k, v := range params {
				cfg[k] = v
			}
		}
		specs = append(specs, BindingSpec{Event: evt, Target: tgt, Effect: effectID, Config: cfg})
	}
	return specs, nil
}

func DefaultBindings() []BindingSpec {
	return []BindingSpec{
		{
			Event:  TriggerPaneActive,
			Target: TargetPane,
			Effect: "fadeTint",
			Config: EffectConfig{
				"color":       "#141400",
				"intensity":   0.35,
				"duration_ms": 400,
			},
		},
		{
			Event:  TriggerPaneResizing,
			Target: TargetPane,
			Effect: "fadeTint",
			Config: EffectConfig{
				"color":       "#ffb86c",
				"intensity":   0.2,
				"duration_ms": 160,
			},
		},
		{
			Event:  TriggerWorkspaceControl,
			Target: TargetWorkspace,
			Effect: "rainbow",
			Config: EffectConfig{
				"speed_hz": 0.5,
			},
		},
		{
			Event:  TriggerWorkspaceKey,
			Target: TargetWorkspace,
			Effect: "flash",
			Config: EffectConfig{
				"color":       "#ffffff",
				"duration_ms": 250,
				"keys":        []string{"F"},
			},
		},
	}
}

func ParseTrigger(name string) (EffectTriggerType, bool) {
	switch strings.ToLower(name) {
	case "pane.created":
		return TriggerPaneCreated, true
	case "pane.removed":
		return TriggerPaneRemoved, true
	case "pane.active":
		return TriggerPaneActive, true
	case "pane.resizing":
		return TriggerPaneResizing, true
	case "pane.geometry":
		return TriggerPaneGeometry, true
	case "pane.title":
		return TriggerPaneTitle, true
	case "pane.zorder":
		return TriggerPaneZOrder, true
	case "pane.key":
		return TriggerPaneKey, true
	case "workspace.control":
		return TriggerWorkspaceControl, true
	case "workspace.key":
		return TriggerWorkspaceKey, true
	case "workspace.switch":
		return TriggerWorkspaceSwitch, true
	case "workspace.resize":
		return TriggerWorkspaceResize, true
	case "workspace.layout":
		return TriggerWorkspaceLayout, true
	case "workspace.zoom":
		return TriggerWorkspaceZoom, true
	case "workspace.theme":
		return TriggerWorkspaceTheme, true
	case "clipboard.changed":
		return TriggerClipboardChanged, true
	case "clock.tick":
		return TriggerClockTick, true
	case "session.state":
		return TriggerSessionState, true
	default:
		return 0, false
	}
}

// TriggerNames returns the supported trigger names for effect bindings.
func TriggerNames() []string {
	return []string{
		"pane.created",
		"pane.removed",
		"pane.active",
		"pane.resizing",
		"pane.geometry",
		"pane.title",
		"pane.zorder",
		"pane.key",
		"workspace.control",
		"workspace.key",
		"workspace.switch",
		"workspace.resize",
		"workspace.layout",
		"workspace.zoom",
		"workspace.theme",
		"clipboard.changed",
		"clock.tick",
		"session.state",
	}
}

func parseTarget(value string) (Target, bool) {
	switch strings.ToLower(value) {
	case "pane", "panes":
		return TargetPane, true
	case "workspace", "workspaces":
		return TargetWorkspace, true
	default:
		return 0, false
	}
}

func normaliseEntries(raw interface{}) ([]map[string]interface{}, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if v == "" {
			return nil, nil
		}
		var out []map[string]interface{}
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, err
		}
		return out, nil
	case []interface{}:
		bytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var out []map[string]interface{}
		if err := json.Unmarshal(bytes, &out); err != nil {
			return nil, err
		}
		return out, nil
	case []map[string]interface{}:
		return v, nil
	default:
		return nil, fmt.Errorf("invalid effect bindings format: %T", raw)
	}
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

func parseKeysOrDefault(cfg EffectConfig, key string, fallback []rune) []rune {
	if cfg == nil {
		return append([]rune(nil), fallback...)
	}
	raw, ok := cfg[key]
	if !ok || raw == nil {
		return append([]rune(nil), fallback...)
	}
	switch v := raw.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []rune(v)
	case []interface{}:
		out := make([]rune, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				r, _ := utf8.DecodeRuneInString(s)
				if r != utf8.RuneError {
					out = append(out, r)
				}
			}
		}
		return out
	case []string:
		out := make([]rune, 0, len(v))
		for _, s := range v {
			if s == "" {
				continue
			}
			r, _ := utf8.DecodeRuneInString(s)
			if r != utf8.RuneError {
				out = append(out, r)
			}
		}
		return out
	default:
		return append([]rune(nil), fallback...)
	}
}
