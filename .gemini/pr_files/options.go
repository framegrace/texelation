// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/configeditor/options.go
// Summary: Provides options registry for combo boxes in the config editor.
// Usage: Used by the config editor to dynamically retrieve options for fields.

package configeditor

import (
	"github.com/framegrace/texelation/internal/effects"
	"github.com/framegrace/texelation/texel"
)

// EasingFunctions returns available easing function names.
func EasingFunctions() []string {
	return texel.EasingFunctionNames
}

// EffectIDs returns registered effect IDs from the effects registry.
func EffectIDs() []string {
	return effects.RegisteredIDs()
}

// ComboOptionsFor returns options for a given section/key combination.
// Returns nil if no options are applicable.
func ComboOptionsFor(target *configTarget, section, key string) []string {
	switch {
	case target.kind == targetSystem && section == "" && key == "defaultApp":
		return target.appOptions
	case section == "layout_transitions" && key == "easing":
		return EasingFunctions()
	default:
		return nil
	}
}
