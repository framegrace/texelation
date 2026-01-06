// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/configeditor/register.go
// Summary: Registers the config editor app with the Texelation registry.

package configeditor

import "github.com/framegrace/texelation/registry"

func init() {
	registry.RegisterBuiltInProvider(func(reg *registry.Registry) (*registry.Manifest, registry.AppFactory) {
		return &registry.Manifest{
				Name:        "config-editor",
				DisplayName: "Settings",
				Description: "Configuration editor",
				Icon:        "⚙️",
				Category:    "system",
			}, func() interface{} {
				return New(reg)
			}
	})
}
