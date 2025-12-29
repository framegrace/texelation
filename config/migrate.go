// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/migrate.go
// Summary: Legacy config migration helpers.

package config

func migrateSystemFromLegacy(cfg Config) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	migrated := false
	var firstErr error

	if legacyPath, err := legacyConfigPath(); err == nil {
		legacyCfg, exists, err := readConfig(legacyPath)
		if err != nil {
			firstErr = err
		}
		if exists {
			if _, ok := cfg["defaultApp"]; !ok {
				if val, ok := legacyCfg["defaultApp"]; ok {
					cfg["defaultApp"] = val
					migrated = true
				}
			}
		}
	} else if firstErr == nil {
		firstErr = err
	}

	if legacyPath, err := legacyThemePath(); err == nil {
		legacyTheme, exists, err := readConfig(legacyPath)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if exists && legacyTheme != nil {
			if copySection(cfg, legacyTheme, "layout_transitions") {
				migrated = true
			}
			if copySection(cfg, legacyTheme, "effects") {
				migrated = true
			}
			if section, ok := legacyTheme["meta"]; ok {
				if meta, ok := section.(map[string]interface{}); ok {
					if val, ok := meta["palette"]; ok {
						if _, ok := cfg["activeTheme"]; !ok {
							cfg["activeTheme"] = val
							migrated = true
						}
					}
				}
			}
		}
	} else if firstErr == nil {
		firstErr = err
	}

	return migrated, firstErr
}

func migrateAppFromLegacy(app string, cfg Config) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	if app != "texelterm" {
		return false, nil
	}
	legacyPath, err := legacyThemePath()
	if err != nil {
		return false, err
	}
	legacyTheme, exists, err := readConfig(legacyPath)
	if err != nil {
		return false, err
	}
	if !exists || legacyTheme == nil {
		return false, nil
	}
	migrated := false
	if copySection(cfg, legacyTheme, "texelterm") {
		migrated = true
	}
	if copySection(cfg, legacyTheme, "texelterm.scroll") {
		migrated = true
	}
	if copySection(cfg, legacyTheme, "texelterm.selection") {
		migrated = true
	}
	if copySection(cfg, legacyTheme, "texelterm.history") {
		migrated = true
	}
	return migrated, nil
}

func copySection(dst Config, src Config, name string) bool {
	if dst == nil || src == nil || name == "" {
		return false
	}
	if _, ok := dst[name]; ok {
		return false
	}
	if section, ok := src[name]; ok {
		dst[name] = section
		return true
	}
	return false
}
