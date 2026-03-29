// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/statusbar/consts.go
// Summary: Powerline/Nerd Font constants used by status bar widgets.

package statusbar

// Powerline characters for creating the tab effect.
// These require a Powerline-patched font or a Nerd Font to render correctly.
const (
	rightTabSeparator     = '\uE0B8' // Left half circle thick separator
	leftTabSeparator      = '\uE0BA' // Right half circle thick separator
	leftLineTabSeparator  = '\uE0BB'
	rightLineTabSeparator = '\uE0B9'
	keyboardIcon          = " \uF11C "
	ctrlIcon              = " \uF085 "
)
