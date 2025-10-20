// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/theme/defaults.go
// Summary: Implements defaults capabilities for the theme subsystem.
// Usage: Accessed by both server and client when reading defaults from theme configurations.
// Notes: Ensures user theme files always contain required defaults.

package theme

import "log"

// ApplyDefaults ensures all built-in theme keys exist and persists new values.
func ApplyDefaults(cfg Config) {
	if cfg == nil {
		return
	}
	changed := false

	if applySectionDefaults(cfg, "desktop", Section{
		"default_fg": "#f8f8f2",
		"default_bg": "#282a36",
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "pane", Section{
		"inactive_border_fg": "#6272a4",
		"inactive_border_bg": "#282a36",
		"active_border_fg":   "#50fa7b",
		"active_border_bg":   "#282a36",
		"resizing_border_fg": "#ffb86c",
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "effects", Section{
		"bindings": defaultEffectBindings(),
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "geometry", Section{
		"split_effect":       "stretch",
		"remove_effect":      "ghost_n_grow",
		"zoom_effect":        "expand",
		"split_duration_ms":  160,
		"remove_duration_ms": 160,
		"zoom_duration_ms":   220,
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "statusbar", Section{
		"base_fg":         "#f8f8f2",
		"base_bg":         "#21222C",
		"inactive_tab_fg": "#6272a4",
		"inactive_tab_bg": "#383a46",
		"active_tab_fg":   "#f8f8f2",
		"active_tab_bg":   "#44475a",
		"control_mode_fg": "#f8f8f2",
		"control_mode_bg": "#ff5555",
		"title_fg":        "#8be9fd",
		"clock_fg":        "#f1fa8c",
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "welcome", Section{
		"text_fg": "#bd93f9",
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "clock", Section{
		"text_fg": "#f1fa8c",
	}) {
		changed = true
	}

	if !changed {
		return
	}
	if err := cfg.Save(); err != nil {
		log.Printf("theme: failed to save defaults: %v", err)
	}
}

func applySectionDefaults(cfg Config, section string, defaults Section) bool {
	if defaults == nil {
		return false
	}
	if _, ok := cfg[section]; !ok {
		cfg[section] = make(Section)
	}
	changed := false
	for key, value := range defaults {
		if _, ok := cfg[section][key]; !ok {
			cfg[section][key] = value
			changed = true
		}
	}
	return changed
}
func defaultEffectBindings() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"event":  "pane.active",
			"target": "pane",
			"effect": "fadeTint",
			"params": map[string]interface{}{
				"color":       "#141400",
				"intensity":   0.35,
				"duration_ms": 400,
			},
		},
		{
			"event":  "pane.resizing",
			"target": "pane",
			"effect": "resizeTint",
			"params": map[string]interface{}{
				"color":       "#ffb86c",
				"intensity":   0.2,
				"duration_ms": 160,
			},
		},
		{
			"event":  "workspace.control",
			"target": "workspace",
			"effect": "rainbow",
			"params": map[string]interface{}{
				"speed_hz": 0.5,
			},
		},
		{
			"event":  "workspace.key",
			"target": "workspace",
			"effect": "flash",
			"params": map[string]interface{}{
				"color":       "#ffffff",
				"duration_ms": 250,
				"keys":        []string{"F"},
			},
		},
	}
}
