// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/register.go
// Summary: Registers the texelbrowse app with the Texelation registry.

package texelbrowse

import "github.com/framegrace/texelation/registry"

func init() {
	registry.RegisterBuiltInProvider(func(_ *registry.Registry) (*registry.Manifest, registry.AppFactory) {
		return &registry.Manifest{
				Name:        "texelbrowse",
				DisplayName: "TexelBrowse",
				Description: "Semantic terminal browser",
				Icon:        "🌐",
				Category:    "utility",
				ThemeSchema: registry.ThemeSchema{
					"desktop": {"default_bg"},
					"ui":      {"text.primary", "text.secondary", "text.active"},
				},
			}, func() interface{} {
				return New("")
			}
	})
}
