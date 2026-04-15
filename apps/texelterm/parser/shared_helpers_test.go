// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/shared_helpers_test.go
// Summary: Shared test helper functions used across multiple test files.

package parser

// logicalLineToString converts a LogicalLine to a string, mapping null runes
// to spaces. Returns "" for a nil line.
func logicalLineToString(line *LogicalLine) string {
	if line == nil {
		return ""
	}
	return cellsToString(line.Cells)
}

// trimLogicalLine trims trailing spaces and null characters from a line.
func trimLogicalLine(s string) string {
	end := len(s)
	for end > 0 && (s[end-1] == ' ' || s[end-1] == 0) {
		end--
	}
	return s[:end]
}

// cellsToString converts a []Cell slice to a rune string, mapping the
// null-rune sentinel (Rune == 0) to a space so sparse-grid padding prints as
// blanks rather than NULs.
func cellsToString(cells []Cell) string {
	runes := make([]rune, len(cells))
	for i, c := range cells {
		if c.Rune == 0 {
			runes[i] = ' '
		} else {
			runes[i] = c.Rune
		}
	}
	return string(runes)
}

// parseString feeds every rune of s through p, shorthand for the common
// `for _, ch := range s { p.Parse(ch) }` test pattern.
func parseString(p *Parser, s string) {
	for _, r := range s {
		p.Parse(r)
	}
}

// trimRight removes trailing whitespace (space, tab, null) from a string.
// Used to normalize grid/line output where cells are padded with spaces.
func trimRight(s string) string {
	end := len(s)
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == 0) {
		end--
	}
	return s[:end]
}

// readAllSparseLines reads every line from globalIdx 0 through ContentEnd
// from the sparse main screen, returning trimmed strings. Gaps (nil cells)
// become empty strings. Used by reload tests to compare full-session content.
func readAllSparseLines(v *VTerm) []string {
	if v.mainScreen == nil {
		return nil
	}
	end := v.mainScreen.ContentEnd()
	if end < 0 {
		return nil
	}
	lines := make([]string, 0, end+1)
	for gi := int64(0); gi <= end; gi++ {
		cells := v.mainScreen.ReadLine(gi)
		if cells == nil {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, trimLogicalLine(cellsToString(cells)))
	}
	return lines
}

// readAllPageStoreLines reads every line from the PageStore backing the
// sparse terminal, returning trimmed strings. Used to verify that disk
// content matches what's in the sparse store after reload.
func readAllPageStoreLines(ps *PageStore) []string {
	count := ps.LineCount()
	if count == 0 {
		return nil
	}
	lines := make([]string, 0, count)
	for i := int64(0); i < count; i++ {
		line, err := ps.ReadLine(i)
		if err != nil || line == nil {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, trimRight(logicalLineToString(line)))
	}
	return lines
}
