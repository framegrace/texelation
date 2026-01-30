// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/text_extractor.go
// Summary: Extract plain text from terminal cells for search indexing.

package parser

import (
	"strings"
	"unicode"
)

// ExtractText converts a slice of Cells to plain text for FTS indexing.
// It strips formatting, handles wide characters, and trims trailing whitespace.
func ExtractText(cells []Cell) string {
	if len(cells) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.Grow(len(cells))

	for i, cell := range cells {
		r := cell.Rune

		// Skip null runes (empty cells or wide char continuations)
		if r == 0 {
			continue
		}

		// Skip control characters except space and tab
		if unicode.IsControl(r) && r != ' ' && r != '\t' {
			continue
		}

		// For wide characters, we only write the rune from the first cell.
		// The second cell has Wide=true but typically r=0 (handled above).
		// If somehow both have the same rune, skip the duplicate.
		if cell.Wide && i > 0 && cells[i-1].Rune == r {
			continue
		}

		sb.WriteRune(r)
	}

	// Trim trailing whitespace for storage efficiency
	return strings.TrimRight(sb.String(), " \t")
}

// ExtractTextFromLine extracts text from a LogicalLine.
func ExtractTextFromLine(line *LogicalLine) string {
	if line == nil {
		return ""
	}
	return ExtractText(line.Cells)
}
