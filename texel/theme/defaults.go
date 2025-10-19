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
		"effects":            defaultPaneEffects(),
	}) {
		changed = true
	}

	if applySectionDefaults(cfg, "workspace", Section{
		"effects": defaultWorkspaceEffects(),
	}) {
		changed = true
	}

	if normalizePaneEffects(cfg) {
		changed = true
	}

	if normalizeWorkspaceEffects(cfg) {
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

func defaultPaneEffects() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"id":          "inactive-overlay",
			"color":       "#141400",
			"intensity":   0.35,
			"duration_ms": 400,
		},
		{
			"id":          "resizing-overlay",
			"color":       "#ffb86c",
			"intensity":   0.2,
			"duration_ms": 160,
		},
	}
}

func defaultWorkspaceEffects() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"id":       "rainbow",
			"speed_hz": 0.5,
		},
		{
			"id":          "flash",
			"color":       "#ffffff",
			"duration_ms": 250,
		},
	}
}

func normalizePaneEffects(cfg Config) bool {
	sec, ok := cfg["pane"]
	if !ok || sec == nil {
		return false
	}
	raw, ok := sec["effects"]
	if !ok || raw == nil {
		sec["effects"] = defaultPaneEffects()
		return true
	}
	effects, ok := toEffectList(raw)
	if !ok {
		sec["effects"] = defaultPaneEffects()
		return true
	}
	if len(effects) == 0 {
		sec["effects"] = defaultPaneEffects()
		return true
	}
	return false
}

func normalizeWorkspaceEffects(cfg Config) bool {
	sec, ok := cfg["workspace"]
	if !ok || sec == nil {
		cfg["workspace"] = Section{"effects": defaultWorkspaceEffects()}
		return true
	}
	raw, ok := sec["effects"]
	if !ok || raw == nil {
		sec["effects"] = defaultWorkspaceEffects()
		return true
	}
	effects, ok := toEffectList(raw)
	if !ok || len(effects) == 0 {
		sec["effects"] = defaultWorkspaceEffects()
		return true
	}
	return false
}

func toEffectList(value interface{}) ([]map[string]interface{}, bool) {
	switch v := value.(type) {
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(v))
		for i, item := range v {
			out[i] = cloneEffect(item)
		}
		return out, true
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, false
			}
			out = append(out, cloneEffect(m))
		}
		return out, true
	default:
		return nil, false
	}
}

func cloneEffect(entry map[string]interface{}) map[string]interface{} {
	if entry == nil {
		return nil
	}
	clone := make(map[string]interface{}, len(entry))
	for k, v := range entry {
		clone[k] = v
	}
	return clone
}
