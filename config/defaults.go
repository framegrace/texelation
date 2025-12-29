// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/defaults.go
// Summary: Default values for system and app configuration files.

package config

func applySystemDefaults(cfg Config) {
	if cfg == nil {
		return
	}
	cfg.RegisterDefaults("", Section{
		"defaultApp":  "launcher",
		"activeTheme": "mocha",
	})
	cfg.RegisterDefaults("layout_transitions", Section{
		"enabled":       true,
		"duration_ms":   300,
		"easing":        "smoothstep",
		"min_threshold": 3,
	})
	cfg.RegisterDefaults("effects", Section{
		"bindings": defaultEffectBindings(),
	})
}

func applyAppDefaults(app string, cfg Config) {
	if cfg == nil {
		return
	}
	switch app {
	case "texelterm":
		cfg.RegisterDefaults("texelterm", Section{
			"visual_bell_enabled":    false,
			"wrap_enabled":           true,
			"reflow_enabled":         true,
			"display_buffer_enabled": true,
		})
		cfg.RegisterDefaults("texelterm.scroll", Section{
			"velocity_decay":     0.6,
			"velocity_increment": 0.6,
			"max_velocity":       15.0,
			"debounce_ms":        50,
			"exponential_curve":  0.8,
		})
		cfg.RegisterDefaults("texelterm.selection", Section{
			"edge_zone":        2,
			"max_scroll_speed": 15,
		})
		cfg.RegisterDefaults("texelterm.history", Section{
			"memory_lines": 100000,
			"persist_dir":  "",
		})
	}
}

func defaultEffectBindings() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"event":  "pane.active",
			"target": "pane",
			"effect": "fadeTint",
			"params": map[string]interface{}{
				"color":       "bg.base",
				"intensity":   0.35,
				"duration_ms": 400,
			},
		},
		{
			"event":  "pane.resizing",
			"target": "pane",
			"effect": "fadeTint",
			"params": map[string]interface{}{
				"color":       "border.resizing",
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
	}
}
