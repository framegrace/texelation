// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/cell.go
// Summary: Implements cell capabilities for the core desktop engine.
// Usage: Used throughout the project to implement cell inside the desktop and panes.
// Notes: Legacy desktop logic migrated from the monolithic application.

package texel

import "github.com/gdamore/tcell/v2"

// Cell represents a single character cell on the terminal screen.
// It now uses tcell.Style to handle all formatting.
type Cell struct {
	Ch    rune
	Style tcell.Style
}
