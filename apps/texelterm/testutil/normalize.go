// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Helpers for normalizing blank rows during replay comparisons.

package testutil

import "github.com/framegrace/texelation/apps/texelterm/parser"

func isBlankRow(row []parser.Cell) bool {
	if len(row) == 0 {
		return true
	}
	base := row[0]
	for _, c := range row {
		if c.Rune != ' ' && c.Rune != 0 {
			return false
		}
		if c.FG != base.FG || c.BG != base.BG {
			return false
		}
	}
	return true
}

func normalizeRowToDefault(row []parser.Cell) {
	for i := range row {
		row[i].FG = parser.DefaultFG
		row[i].BG = parser.DefaultBG
		row[i].Attr = 0
	}
}
