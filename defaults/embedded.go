// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: defaults/embedded.go
// Summary: Embedded default configuration files.

package defaults

import (
	"embed"
	"fmt"
	"runtime"
)

//go:embed texelation.json apps/*/config.json keybindings-*.json
var fs embed.FS

// SystemConfig returns the embedded system config JSON.
func SystemConfig() ([]byte, error) {
	return fs.ReadFile("texelation.json")
}

// AppConfig returns the embedded config JSON for the named app.
func AppConfig(app string) ([]byte, error) {
	if app == "" {
		return nil, fmt.Errorf("app name is required")
	}
	return fs.ReadFile(fmt.Sprintf("apps/%s/config.json", app))
}

// KeybindingsConfig returns the embedded keybindings JSON for the given preset.
func KeybindingsConfig(preset string) ([]byte, error) {
	return fs.ReadFile(fmt.Sprintf("keybindings-%s.json", preset))
}

// DefaultKeybindingsPreset returns "linux" or "mac" based on the runtime OS.
func DefaultKeybindingsPreset() string {
	if runtime.GOOS == "darwin" {
		return "mac"
	}
	return "linux"
}
