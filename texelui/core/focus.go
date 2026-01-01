// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/core/focus.go
// Summary: Focus-related utility functions for widget trees.

package core

// IsDescendantFocused recursively checks if widget or any descendant has focus.
// Returns true if the widget itself is focused, or if any child widget is focused.
func IsDescendantFocused(w Widget) bool {
	if w == nil {
		return false
	}
	// Check if widget itself is focused
	if fs, ok := w.(FocusState); ok && fs.IsFocused() {
		return true
	}
	// Check children recursively
	if cc, ok := w.(ChildContainer); ok {
		var found bool
		cc.VisitChildren(func(child Widget) {
			if found {
				return
			}
			if IsDescendantFocused(child) {
				found = true
			}
		})
		return found
	}
	return false
}
