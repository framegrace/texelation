// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/launcher/register.go
// Summary: Registers the launcher app with the Texelation registry.

package launcher

import "github.com/framegrace/texelation/registry"

func init() {
	registry.RegisterBuiltInProvider(func(reg *registry.Registry) (*registry.Manifest, registry.AppFactory) {
		return &registry.Manifest{
				Name:        "launcher",
				DisplayName: "Launcher",
				Description: "Application launcher",
				Icon:        "🚀",
				Category:    "system",
				ThemeSchema: registry.ThemeSchema{
					"ui": {"bg.surface", "text.primary", "text.inverse", "accent"},
				},
			}, func() interface{} {
				return New(reg)
			}
	})
}
