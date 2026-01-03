// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/core/keyhints.go
// Summary: Key hints interface for status bar integration.

package core

import "strings"

// KeyHint represents a single keyboard shortcut hint.
type KeyHint struct {
	Key   string // Display string: "Tab", "↑↓", "Ctrl+S"
	Label string // Action description: "Next", "Move", "Save"
}

// KeyHintsProvider allows widgets to expose their keyboard shortcuts
// for display in the status bar.
type KeyHintsProvider interface {
	// GetKeyHints returns the keyboard shortcuts available for this widget.
	// Returns nil or empty slice if no hints are available.
	GetKeyHints() []KeyHint
}

// FormatKeyHints formats a slice of KeyHints into a display string.
// Example output: "Tab:Next │ ↑↓:Move │ Enter:Select"
func FormatKeyHints(hints []KeyHint) string {
	return FormatKeyHintsWithSeparator(hints, " │ ")
}

// FormatKeyHintsWithSeparator formats hints with a custom separator.
func FormatKeyHintsWithSeparator(hints []KeyHint, separator string) string {
	if len(hints) == 0 {
		return ""
	}

	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		if h.Key != "" && h.Label != "" {
			parts = append(parts, h.Key+":"+h.Label)
		}
	}

	return strings.Join(parts, separator)
}

// FindDeepFocused finds the most deeply focused widget starting from w.
// Returns w if no focused descendant is found.
// Returns nil if w is nil.
func FindDeepFocused(w Widget) Widget {
	if w == nil {
		return nil
	}

	// Check if w has focused descendants
	if cc, ok := w.(ChildContainer); ok {
		var deepest Widget
		cc.VisitChildren(func(child Widget) {
			if deepest != nil {
				return
			}
			if fs, ok := child.(FocusState); ok && fs.IsFocused() {
				// Found a focused child - recurse to find deepest
				deepest = FindDeepFocused(child)
			} else if cc2, ok := child.(ChildContainer); ok {
				// Not focused but might contain focused descendant
				if IsDescendantFocused(child) {
					deepest = FindDeepFocused(child)
				}
				_ = cc2 // silence unused warning
			}
		})
		if deepest != nil {
			return deepest
		}
	}

	return w
}
