// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/theme/app_overrides.go
// Summary: App-specific theme overlay helpers.

package theme

import "texelation/config"

// ForApp returns the base theme merged with any per-app overrides.
func ForApp(app string) Config {
	base := Get()
	overrides := overridesForApp(app)
	if len(overrides) == 0 {
		return base
	}
	return WithOverrides(base, overrides)
}

func overridesForApp(app string) Config {
	if app == "" {
		return nil
	}
	cfg := config.App(app)
	if cfg == nil {
		return nil
	}
	return ParseOverrides(cfg["theme_overrides"])
}
