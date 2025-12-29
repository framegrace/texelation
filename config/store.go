// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/store.go
// Summary: Load, reload, and migration logic for config store.

package config

import "log"

func loadSystemLocked() error {
	path, err := systemConfigPath()
	if err != nil {
		log.Printf("Config: Failed to resolve system config path: %v", err)
		system = make(Config)
		applySystemDefaults(system)
		return err
	}

	cfg, exists, readErr := readConfig(path)
	if readErr != nil {
		log.Printf("Config: Failed to read system config %s: %v", path, readErr)
		cfg = make(Config)
	}

	if exists && len(cfg) == 0 {
		if def := defaultSystemConfig(); def != nil {
			cfg = def
			if err := writeConfig(path, cfg); err != nil {
				log.Printf("Config: Failed to write default system config: %v", err)
				if readErr == nil {
					readErr = err
				}
			}
		}
	}

	if !exists {
		cfg = make(Config)
		migrated, migrateErr := migrateSystemFromLegacy(cfg)
		if migrateErr != nil {
			log.Printf("Config: Legacy system migration error: %v", migrateErr)
			if readErr == nil {
				readErr = migrateErr
			}
		}
		if !migrated {
			if def := defaultSystemConfig(); def != nil {
				cfg = def
				migrated = true
			}
		}
		applySystemDefaults(cfg)
		if migrated {
			if err := writeConfig(path, cfg); err != nil {
				log.Printf("Config: Failed to write migrated system config: %v", err)
				if readErr == nil {
					readErr = err
				}
			}
		}
	} else {
		applySystemDefaults(cfg)
	}

	system = cfg
	if readErr == nil && exists {
		log.Printf("Config: Loaded system config from %s", path)
	}
	return readErr
}

func loadAppLocked(name string) (Config, error) {
	path, err := appConfigPath(name)
	if err != nil {
		return nil, err
	}

	cfg, exists, readErr := readConfig(path)
	if readErr != nil {
		log.Printf("Config: Failed to read app config %s: %v", path, readErr)
		cfg = make(Config)
	}

	if exists && len(cfg) == 0 {
		if def := defaultAppConfig(name); def != nil {
			cfg = def
			if err := writeConfig(path, cfg); err != nil {
				log.Printf("Config: Failed to write default app config: %v", err)
				if readErr == nil {
					readErr = err
				}
			}
		}
	}

	if !exists {
		cfg = make(Config)
		migrated, migrateErr := migrateAppFromLegacy(name, cfg)
		if migrateErr != nil {
			log.Printf("Config: Legacy app migration error: %v", migrateErr)
			if readErr == nil {
				readErr = migrateErr
			}
		}
		if !migrated {
			if def := defaultAppConfig(name); def != nil {
				cfg = def
				migrated = true
			}
		}
		applyAppDefaults(name, cfg)
		if migrated {
			if err := writeConfig(path, cfg); err != nil {
				log.Printf("Config: Failed to write migrated app config: %v", err)
				if readErr == nil {
					readErr = err
				}
			}
		}
	} else {
		applyAppDefaults(name, cfg)
	}

	if readErr == nil && exists {
		log.Printf("Config: Loaded app %q config from %s", name, path)
	}
	return cfg, readErr
}
