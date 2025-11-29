// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/theme/semantics.go
// Summary: Defines standard semantic color bindings for the application.
// Usage: Used to map high-level UI concepts (e.g., "action.primary") to palette colors.

package theme

import "github.com/gdamore/tcell/v2"

// StandardSemantics defines the default mappings from semantic names to palette colors.
// These align with the Catppuccin Style Guide.
var StandardSemantics = Section{
	// Global Accent (The pivot color for the theme)
	"accent":           "@mauve",    // Main brand color
	"accent_secondary": "@lavender", // Secondary/Dimmed accent

	// Background Surfaces
	"bg.base":    "@base",     // Main application background
	"bg.mantle":  "@mantle",   // Sidebars, lower-level backgrounds
	"bg.crust":   "@crust",    // Lowest level (maybe for terminal background)
	"bg.surface": "@surface0", // Cards, inputs, elements

	// Text
	"text.primary":   "@text",     // Primary content
	"text.secondary": "@subtext1", // Secondary descriptions
	"text.muted":     "@overlay0", // Comments, disabled text
	"text.inverse":   "@base",     // Text on top of primary/accent colors
	"text.accent":    "accent",    // Brand/Logo text (Points to ui.accent)

	// Actions & States
	"action.primary": "accent",  // Call to action (Buttons) -> Points to ui.accent
	"action.success": "@green",  // Success states
	"action.warning": "@yellow", // Warnings
	"action.danger":  "@red",    // Destructive actions
	"selection":      "@surface2", // Selection highlight

	// Borders & Focus
	"border.active":   "accent",           // Active pane/element border -> Points to ui.accent
	"border.inactive": "@overlay0",        // Inactive pane/element border
	"border.resizing": "accent_secondary", // Resizing state -> Points to ui.accent_secondary
	"border.focus":    "accent_secondary", // Focused element ring -> Points to ui.accent_secondary

	// Components specific
	"caret": "@rosewater", // Input caret
}

// LoadStandardSemantics registers the standard semantic definitions into the "ui" section.
// This ensures that even if the user hasn't defined them, they are available.
func (c Config) LoadStandardSemantics() {
	c.RegisterDefaults("ui", StandardSemantics)
}

// GetSemanticColor retrieves a color from the "ui" section by its semantic name.
// Example: GetSemanticColor("text.primary") -> resolves to @text -> #cdd6f4
func (c Config) GetSemanticColor(key string) tcell.Color {
	return c.GetColor("ui", key, tcell.ColorDefault)
}
