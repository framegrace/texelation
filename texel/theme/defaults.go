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

	// Load standard semantic definitions first to ensure "ui" section has basics
	cfg.LoadStandardSemantics()

	if applySectionDefaults(cfg, "desktop", Section{
		"default_fg": "text.primary",
		"default_bg": "bg.base",
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "pane", Section{
		"inactive_border_fg": "border.inactive",
		"inactive_border_bg": "bg.base",
		"active_border_fg":   "border.active",
		"active_border_bg":   "bg.base",
		"resizing_border_fg": "border.resizing",
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "effects", Section{
		"bindings": defaultEffectBindings(),
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "statusbar", Section{
		"base_fg":         "text.primary",
		"base_bg":         "bg.mantle",
		"inactive_tab_fg": "text.muted",
		"inactive_tab_bg": "bg.crust",
		"active_tab_fg":   "text.primary",
		"active_tab_bg":   "bg.base",
		"control_mode_fg": "text.inverse",
		"control_mode_bg": "action.danger",
		"title_fg":        "text.primary",
		"clock_fg":        "text.primary",
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "welcome", Section{
		"text_fg": "text.accent",
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "clock", Section{
		"text_fg": "text.primary",
	}) {
		changed = true
	}

    if applySectionDefaults(cfg, "selection", Section{
        "highlight_bg": "selection",
        "highlight_fg": "text.primary",
    }) {
        changed = true
    }

    // TexelUI (TUI widgets) default colors
    // Most widgets now rely on semantic keys (ui.*), but we can still
    // populate overrides here if we want specific UI element tweaks.
    if applySectionDefaults(cfg, "ui", Section{
        // These map the semantic names (defined in semantics.go) to palette colors.
        // We re-assert them here just in case, but semantics.go does the heavy lifting.
        // For specific widget overrides:
        "button_bg": "action.primary",
        "button_fg": "text.inverse",
        
        // Legacy keys (mapped to new system for backward compat)
        "surface_bg": "bg.surface",
        "surface_fg": "text.primary",
    }) {
        changed = true
    }

	if applySectionDefaults(cfg, "texelterm", Section{
		"visual_bell_enabled": false,
		"wrap_enabled":        true,
		"reflow_enabled":      true,
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "texelterm.scroll", Section{
		"velocity_decay":       0.6,   // Time window (seconds) for velocity accumulation
		"velocity_increment":   0.6,   // Velocity increase per scroll event
		"max_velocity":         15.0,  // Maximum velocity (multiplier will be 1 + max_velocity)
		"debounce_ms":          50,    // Milliseconds to debounce duplicate events
		"exponential_curve":    0.8,   // Exponential smoothing factor (velocity^curve)
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
				"color":       "bg.base", // Use semantic background
				"intensity":   0.35,
				"duration_ms": 400,
			},
		},
		{
			"event":  "pane.resizing",
			"target": "pane",
			"effect": "fadeTint",
			"params": map[string]interface{}{
				"color":       "border.resizing", // Match resizing border
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
