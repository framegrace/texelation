// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/commands.go
// Summary: URL bar helpers and URL normalization utilities.

package texelbrowse

import "strings"

// normalizeURL cleans up raw user input into a navigable URL.
// If the input has no scheme, "https://" is prepended.
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		return raw
	}
	return "https://" + raw
}
