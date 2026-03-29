// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/statusbar/register.go
// Summary: Registers the status bar app with the Texelation registry.

package statusbar

import "github.com/framegrace/texelation/registry"

func init() {
	registry.RegisterBuiltInProvider(func(_ *registry.Registry) (*registry.Manifest, registry.AppFactory) {
		return &registry.Manifest{
				Name:        "statusbar",
				DisplayName: "Status Bar",
				Description: "Workspace tabs, mode indicator, and system info",
				Version:     "2.0.0",
				Type:        registry.AppTypeBuiltIn,
				Icon:        "bar",
				Category:    "system",
			}, func() interface{} {
				return New()
			}
	})
}
