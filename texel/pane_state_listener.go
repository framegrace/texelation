// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/pane_state_listener.go
// Summary: Implements pane state listener capabilities for the core desktop engine.
// Usage: Used throughout the project to implement pane state listener inside the desktop and panes.

package texel

// PaneStateListener observes active/resizing changes so remotes can mirror visuals.
type PaneStateListener interface {
	PaneStateChanged(id [16]byte, active bool, resizing bool, z int, handlesMouse bool)
}
