// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/help/register.go
// Summary: Registers the help app with the Texelation registry.

package help

import "github.com/framegrace/texelation/registry"

func init() {
	registry.RegisterBuiltInProvider(func(_ *registry.Registry) (*registry.Manifest, registry.AppFactory) {
		return &registry.Manifest{
				Name:        "help",
				DisplayName: "Help",
				Description: "Help viewer",
				Icon:        "❓",
				Category:    "system",
				ThemeSchema: registry.ThemeSchema{
					"desktop": {"default_bg"},
					"ui":      {"text.primary", "text.secondary", "text.active"},
				},
			}, func() interface{} {
				return NewHelpApp()
			}
	})
}
