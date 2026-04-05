// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/defaults.go
// Summary: Default values for system and app configuration files.
// Defaults are loaded from embedded JSON files in defaults/ package.

package config

// applySystemDefaults merges embedded system defaults into cfg.
// Only missing keys are filled from the embedded defaults.
func applySystemDefaults(cfg Config) {
	if cfg == nil {
		return
	}
	embedded, err := embeddedSystemDefaults()
	if err != nil || embedded == nil {
		return
	}
	mergeDefaults(cfg, embedded)
}

// applyAppDefaults merges embedded app defaults into cfg.
// Only missing keys are filled from the embedded defaults.
func applyAppDefaults(app string, cfg Config) {
	if cfg == nil {
		return
	}
	embedded, err := embeddedAppDefaults(app)
	if err != nil || embedded == nil {
		return
	}
	mergeDefaults(cfg, embedded)
}

// AppDefaults returns a clone of the embedded default config for the named app.
// Returns nil if no defaults exist for that app.
func AppDefaults(app string) Config {
	return defaultAppConfig(app)
}

// SystemDefaults returns a clone of the embedded default system config.
func SystemDefaults() Config {
	return defaultSystemConfig()
}

// ValidateAgainstDefaults compares a config against its defaults and returns
// a list of unknown keys (present in cfg but not in defaults).
// Keys are formatted as "section.key" for section-level keys.
func ValidateAgainstDefaults(defaults, cfg Config) []string {
	if defaults == nil || cfg == nil {
		return nil
	}
	var unknown []string
	for sectionName, sectionVal := range cfg {
		defSection, hasDef := defaults[sectionName]
		if !hasDef {
			unknown = append(unknown, sectionName)
			continue
		}
		// Both must be maps for key-level comparison
		cfgMap, cfgOk := toMap(sectionVal)
		defMap, defOk := toMap(defSection)
		if !cfgOk || !defOk {
			continue
		}
		for key := range cfgMap {
			if _, ok := defMap[key]; !ok {
				unknown = append(unknown, sectionName+"."+key)
			}
		}
	}
	return unknown
}

func toMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case Section:
		return map[string]interface{}(m), true
	}
	return nil, false
}

// mergeDefaults copies missing keys from defaults into cfg.
// Existing keys in cfg are preserved.
// Values are deep-cloned to prevent mutation of cached defaults.
func mergeDefaults(cfg, defaults Config) {
	for sectionName, section := range defaults {
		if section == nil {
			continue
		}
		switch s := section.(type) {
		case Section:
			cfg.RegisterDefaults(sectionName, cloneSection(s))
		case map[string]interface{}:
			cfg.RegisterDefaults(sectionName, cloneSection(Section(s)))
		default:
			// Top-level non-section value (like "defaultApp": "launcher")
			if _, exists := cfg[sectionName]; !exists {
				cfg[sectionName] = deepCloneValue(section)
			}
		}
	}
}

// cloneSection creates a deep copy of a Section.
func cloneSection(s Section) Section {
	if s == nil {
		return nil
	}
	clone := make(Section, len(s))
	for k, v := range s {
		clone[k] = deepCloneValue(v)
	}
	return clone
}
