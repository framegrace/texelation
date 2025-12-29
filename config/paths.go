// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: config/paths.go
// Summary: Path helpers for texelation configuration.

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func configRoot() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "texelation"), nil
}

func systemConfigPath() (string, error) {
	root, err := configRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, systemConfigName), nil
}

func legacyConfigPath() (string, error) {
	root, err := configRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, legacyConfigName), nil
}

func legacyThemePath() (string, error) {
	root, err := configRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, legacyThemeName), nil
}

func appConfigPath(app string) (string, error) {
	if app == "" {
		return "", fmt.Errorf("app name is required")
	}
	root, err := configRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "apps", app, "config.json"), nil
}
