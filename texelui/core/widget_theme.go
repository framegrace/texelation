// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/core/widget_theme.go
// Summary: Centralized widget color resolution from theme.
// This provides a single place to map semantic colors to widget roles.

package core

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
)

// WidgetColors contains all colors used by TexelUI widgets.
// This centralizes theme resolution so widget API changes can be made
// in one place rather than scattered across all widgets.
type WidgetColors struct {
	// Surface colors (default widget background/foreground)
	SurfaceBg tcell.Color
	SurfaceFg tcell.Color

	// Base background (for overlays, modals)
	BaseBg tcell.Color

	// Text variants
	TextPrimary   tcell.Color
	TextSecondary tcell.Color
	TextMuted     tcell.Color
	TextInverse   tcell.Color

	// Input fields
	InputBg    tcell.Color
	InputFg    tcell.Color
	InputCaret tcell.Color

	// Buttons
	ButtonBg tcell.Color
	ButtonFg tcell.Color

	// Border colors
	BorderDefault tcell.Color
	BorderActive  tcell.Color
	BorderFocus   tcell.Color

	// Selection/highlight
	SelectionBg tcell.Color
	SelectionFg tcell.Color

	// Accent color
	Accent tcell.Color

	// Action colors
	ActionPrimary tcell.Color
}

// GetWidgetColors returns the current widget colors from the theme.
// This should be called when creating widgets to get current theme values.
func GetWidgetColors() WidgetColors {
	tm := theme.Get()
	return WidgetColors{
		// Surface
		SurfaceBg: tm.GetSemanticColor("bg.surface"),
		SurfaceFg: tm.GetSemanticColor("text.primary"),

		// Base
		BaseBg: tm.GetSemanticColor("bg.base"),

		// Text variants
		TextPrimary:   tm.GetSemanticColor("text.primary"),
		TextSecondary: tm.GetSemanticColor("text.secondary"),
		TextMuted:     tm.GetSemanticColor("text.muted"),
		TextInverse:   tm.GetSemanticColor("text.inverse"),

		// Input fields
		InputBg:    tm.GetSemanticColor("bg.surface"),
		InputFg:    tm.GetSemanticColor("text.primary"),
		InputCaret: tm.GetSemanticColor("caret"),

		// Buttons
		ButtonBg: tm.GetSemanticColor("action.primary"),
		ButtonFg: tm.GetSemanticColor("text.inverse"),

		// Border
		BorderDefault: tm.GetSemanticColor("border.default"),
		BorderActive:  tm.GetSemanticColor("border.active"),
		BorderFocus:   tm.GetSemanticColor("border.focus"),

		// Selection
		SelectionBg: tm.GetSemanticColor("selection"),
		SelectionFg: tm.GetSemanticColor("text.primary"),

		// Accent
		Accent: tm.GetSemanticColor("accent"),

		// Actions
		ActionPrimary: tm.GetSemanticColor("action.primary"),
	}
}

// DefaultStyle returns the default tcell style for widget surfaces.
func (c WidgetColors) DefaultStyle() tcell.Style {
	return tcell.StyleDefault.Background(c.SurfaceBg).Foreground(c.SurfaceFg)
}

// InputStyle returns the style for input fields.
func (c WidgetColors) InputStyle() tcell.Style {
	return tcell.StyleDefault.Background(c.InputBg).Foreground(c.InputFg)
}

// ButtonStyle returns the style for buttons.
func (c WidgetColors) ButtonStyle() tcell.Style {
	return tcell.StyleDefault.Background(c.ButtonBg).Foreground(c.ButtonFg)
}

// SelectionStyle returns the style for selected items.
func (c WidgetColors) SelectionStyle() tcell.Style {
	return tcell.StyleDefault.Background(c.SelectionBg).Foreground(c.SelectionFg)
}

// BorderStyle returns the style for normal borders.
func (c WidgetColors) BorderStyle() tcell.Style {
	return tcell.StyleDefault.Background(c.SurfaceBg).Foreground(c.BorderDefault)
}

// FocusedBorderStyle returns the style for focused borders.
func (c WidgetColors) FocusedBorderStyle() tcell.Style {
	return tcell.StyleDefault.Background(c.SurfaceBg).Foreground(c.BorderActive)
}
