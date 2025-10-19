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
		"effects": []map[string]interface{}{
			{
				"id":          "inactive-overlay",
				"color":       "#141400",
				"intensity":   0.35,
				"duration_ms": 400,
			},
		},
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "workspace", Section{
		"effects": []map[string]interface{}{
			{
				"id":       "rainbow",
				"speed_hz": 0.5,
			},
			{
				"id":          "flash",
				"color":       "#ffffff",
				"duration_ms": 250,
			},
		},
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
