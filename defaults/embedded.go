// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: defaults/embedded.go
// Summary: Embedded default configuration files.

package defaults

import (
	"embed"
	"fmt"
)

//go:embed texelation.json apps/*/config.json
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
