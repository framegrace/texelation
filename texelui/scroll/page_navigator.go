// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/scroll/page_navigator.go
// Summary: PageNavigator interface for selection-based page navigation.

package scroll

// PageNavigator is an interface for widgets that handle page navigation by moving selection
// rather than just scrolling the viewport. This is typically used by list and grid widgets
// where PgUp/PgDn should move the selected item by a page-worth of items.
//
// When a child widget implements PageNavigator, ScrollPane will delegate PgUp/PgDn
// handling to the child instead of scrolling the viewport directly.
type PageNavigator interface {
	// HandlePageNavigation handles PgUp (direction=-1) or PgDn (direction=+1).
	// pageSize is the viewport height (suggested number of rows/items to move).
	// Returns true if the navigation was handled.
	HandlePageNavigation(direction int, pageSize int) bool
}
