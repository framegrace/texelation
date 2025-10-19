// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/focus_listener.go
// Summary: Implements focus listener capabilities for the core desktop engine.
// Usage: Used throughout the project to implement focus listener inside the desktop and panes.
// Notes: Legacy desktop logic migrated from the monolithic application.

package texel

// DesktopFocusListener describes consumers interested in focus changes.
type DesktopFocusListener interface {
	PaneFocused(paneID [16]byte)
}
