// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/json_output.go
// Summary: JSON serialization for machine-parseable comparison output.
//
// This module generates JSON output from terminal comparisons, enabling
// automated debugging workflows and CI integration.
//
// Usage:
//   result, _ := cmp.CompareAtEndWithFullDiff()
//   jsonBytes, _ := result.ToJSON()
//   fmt.Println(string(jsonBytes))

package testutil

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// normalizeRune converts null runes to spaces for consistent comparison.
func normalizeRune(r rune) rune {
	if r == 0 {
		return ' '
	}
	return r
}

// DiffType categorizes the type of difference between cells.
type DiffType string

const (
	DiffTypeChar     DiffType = "char"
	DiffTypeFG       DiffType = "fg"
	DiffTypeBG       DiffType = "bg"
	DiffTypeAttr     DiffType = "attr"
	DiffTypeCombined DiffType = "combined"
	DiffTypeMatch    DiffType = "match"
)

// EnhancedComparisonResult holds complete comparison results including colors/attrs.
type EnhancedComparisonResult struct {
	Match          bool            `json:"match"`
	Summary        string          `json:"summary"`
	Differences    []EnhancedDiff  `json:"differences"`
	TotalCells     int             `json:"total_cells"`
	DiffCount      int             `json:"diff_count"`
	BytesProcessed int             `json:"bytes_processed"`
	CharDiffs      int             `json:"char_diffs"`
	ColorDiffs     int             `json:"color_diffs"`
	AttrDiffs      int             `json:"attr_diffs"`
	TmuxGrid       [][]CellInfo    `json:"tmux_grid,omitempty"`
	TexeltermGrid  [][]CellInfo    `json:"texelterm_grid,omitempty"`
}

// EnhancedDiff represents a complete cell-level difference.
type EnhancedDiff struct {
	X         int       `json:"x"`
	Y         int       `json:"y"`
	Reference CellInfo  `json:"reference"`
	Texelterm CellInfo  `json:"texelterm"`
	DiffType  DiffType  `json:"diff_type"`
	DiffDesc  string    `json:"diff_desc"`
}

// CellInfo holds complete cell information for JSON output.
type CellInfo struct {
	Rune    string    `json:"rune"`
	RuneHex string    `json:"rune_hex,omitempty"`
	FG      ColorInfo `json:"fg"`
	BG      ColorInfo `json:"bg"`
	Attr    AttrInfo  `json:"attr"`
}

// ColorInfo represents color for JSON serialization.
type ColorInfo struct {
	Mode    string `json:"mode"`
	Value   uint8  `json:"value,omitempty"`
	R       uint8  `json:"r,omitempty"`
	G       uint8  `json:"g,omitempty"`
	B       uint8  `json:"b,omitempty"`
	Display string `json:"display"`
}

// AttrInfo represents attributes for JSON serialization.
type AttrInfo struct {
	Bold      bool   `json:"bold"`
	Underline bool   `json:"underline"`
	Reverse   bool   `json:"reverse"`
	Display   string `json:"display"`
}

// EnhancedDivergence holds details about where outputs diverged with full cell info.
type EnhancedDivergence struct {
	ByteIndex      int                       `json:"byte_index"`
	ByteEndIndex   int                       `json:"byte_end_index"`
	ChunkHex       string                    `json:"chunk_hex"`
	ChunkReadable  string                    `json:"chunk_readable"`
	EscapeContext  string                    `json:"escape_context"`
	Comparison     *EnhancedComparisonResult `json:"comparison"`
}

// ToJSON serializes an EnhancedComparisonResult to compact JSON.
func (r *EnhancedComparisonResult) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// ToJSONPretty serializes with indentation for readability.
func (r *EnhancedComparisonResult) ToJSONPretty() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// DivergenceToJSON serializes a divergence point to JSON.
func DivergenceToJSON(d *EnhancedDivergence) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

// FilterDiffsByType returns only differences of specified types.
func FilterDiffsByType(diffs []EnhancedDiff, diffTypes ...DiffType) []EnhancedDiff {
	typeSet := make(map[DiffType]bool)
	for _, dt := range diffTypes {
		typeSet[dt] = true
	}
	var result []EnhancedDiff
	for _, d := range diffs {
		if typeSet[d.DiffType] {
			result = append(result, d)
		}
	}
	return result
}

// CellToInfo converts parser.Cell to CellInfo for JSON.
func CellToInfo(c parser.Cell) CellInfo {
	r := c.Rune
	runeStr := " "
	if r != 0 && r != ' ' {
		runeStr = string(r)
	}
	return CellInfo{
		Rune:    runeStr,
		RuneHex: fmt.Sprintf("0x%04x", r),
		FG:      ColorToInfo(c.FG),
		BG:      ColorToInfo(c.BG),
		Attr:    AttrToInfo(c.Attr),
	}
}

// ColorToInfo converts parser.Color to ColorInfo for JSON.
func ColorToInfo(c parser.Color) ColorInfo {
	info := ColorInfo{}
	switch c.Mode {
	case parser.ColorModeDefault:
		info.Mode = "default"
		info.Display = "default"
	case parser.ColorModeStandard:
		info.Mode = "standard"
		info.Value = c.Value
		names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
			"bright-black", "bright-red", "bright-green", "bright-yellow",
			"bright-blue", "bright-magenta", "bright-cyan", "bright-white"}
		if int(c.Value) < len(names) {
			info.Display = names[c.Value]
		} else {
			info.Display = fmt.Sprintf("std(%d)", c.Value)
		}
	case parser.ColorMode256:
		info.Mode = "256"
		info.Value = c.Value
		info.Display = fmt.Sprintf("256(%d)", c.Value)
	case parser.ColorModeRGB:
		info.Mode = "rgb"
		info.R = c.R
		info.G = c.G
		info.B = c.B
		info.Display = fmt.Sprintf("rgb(%d,%d,%d)", c.R, c.G, c.B)
	default:
		info.Mode = "unknown"
		info.Display = "unknown"
	}
	return info
}

// AttrToInfo converts parser.Attribute to AttrInfo for JSON.
func AttrToInfo(a parser.Attribute) AttrInfo {
	info := AttrInfo{
		Bold:      a&parser.AttrBold != 0,
		Underline: a&parser.AttrUnderline != 0,
		Reverse:   a&parser.AttrReverse != 0,
	}
	var parts []string
	if info.Bold {
		parts = append(parts, "bold")
	}
	if info.Underline {
		parts = append(parts, "underline")
	}
	if info.Reverse {
		parts = append(parts, "reverse")
	}
	if len(parts) == 0 {
		info.Display = "normal"
	} else {
		info.Display = strings.Join(parts, "+")
	}
	return info
}

// GridToCellInfoGrid converts a parser.Cell grid to CellInfo grid.
func GridToCellInfoGrid(grid [][]parser.Cell) [][]CellInfo {
	result := make([][]CellInfo, len(grid))
	for y := range grid {
		result[y] = make([]CellInfo, len(grid[y]))
		for x := range grid[y] {
			result[y][x] = CellToInfo(grid[y][x])
		}
	}
	return result
}

// CompareCells compares two cells and returns the diff type.
func CompareCells(ref, actual parser.Cell) DiffType {
	charDiff := (ref.Rune != actual.Rune)
	fgDiff := (ref.FG != actual.FG)
	bgDiff := (ref.BG != actual.BG)
	attrDiff := (ref.Attr != actual.Attr)

	if !charDiff && !fgDiff && !bgDiff && !attrDiff {
		return DiffTypeMatch
	}

	count := 0
	if charDiff {
		count++
	}
	if fgDiff || bgDiff {
		count++
	}
	if attrDiff {
		count++
	}

	if count > 1 {
		return DiffTypeCombined
	}
	if charDiff {
		return DiffTypeChar
	}
	if fgDiff {
		return DiffTypeFG
	}
	if bgDiff {
		return DiffTypeBG
	}
	if attrDiff {
		return DiffTypeAttr
	}
	return DiffTypeMatch
}

// DescribeCellDiff creates a human-readable description of cell differences.
func DescribeCellDiff(ref, actual parser.Cell) string {
	var parts []string

	// Normalize runes for comparison
	refRune := normalizeRune(ref.Rune)
	actRune := normalizeRune(actual.Rune)

	if refRune != actRune {
		parts = append(parts, fmt.Sprintf("char: %q vs %q", refRune, actRune))
	}
	if ref.FG != actual.FG {
		parts = append(parts, fmt.Sprintf("fg: %s vs %s",
			ColorToInfo(ref.FG).Display, ColorToInfo(actual.FG).Display))
	}
	if ref.BG != actual.BG {
		parts = append(parts, fmt.Sprintf("bg: %s vs %s",
			ColorToInfo(ref.BG).Display, ColorToInfo(actual.BG).Display))
	}
	if ref.Attr != actual.Attr {
		parts = append(parts, fmt.Sprintf("attr: %s vs %s",
			AttrToInfo(ref.Attr).Display, AttrToInfo(actual.Attr).Display))
	}

	if len(parts) == 0 {
		return "match"
	}
	return strings.Join(parts, "; ")
}

// EnhancedCompareGrids compares two cell grids with full color/attr support.
// Returns an EnhancedComparisonResult with detailed differences.
func EnhancedCompareGrids(reference, actual [][]parser.Cell, width, height int) *EnhancedComparisonResult {
	result := &EnhancedComparisonResult{
		Match:      true,
		TotalCells: width * height,
	}

	for y := 0; y < height && y < len(reference) && y < len(actual); y++ {
		for x := 0; x < width && x < len(reference[y]) && x < len(actual[y]); x++ {
			refCell := reference[y][x]
			actCell := actual[y][x]

			// Normalize null runes to space
			refCell.Rune = normalizeRune(refCell.Rune)
			actCell.Rune = normalizeRune(actCell.Rune)

			diffType := CompareCells(refCell, actCell)
			if diffType != DiffTypeMatch {
				result.Match = false
				result.DiffCount++

				if diffType == DiffTypeChar || diffType == DiffTypeCombined {
					result.CharDiffs++
				}
				if diffType == DiffTypeFG || diffType == DiffTypeBG || diffType == DiffTypeCombined {
					result.ColorDiffs++
				}
				if diffType == DiffTypeAttr || diffType == DiffTypeCombined {
					result.AttrDiffs++
				}

				result.Differences = append(result.Differences, EnhancedDiff{
					X:         x,
					Y:         y,
					Reference: CellToInfo(refCell),
					Texelterm: CellToInfo(actCell),
					DiffType:  diffType,
					DiffDesc:  DescribeCellDiff(refCell, actCell),
				})
			}
		}
	}

	if result.Match {
		result.Summary = "Outputs match"
	} else {
		result.Summary = fmt.Sprintf("Found %d differences (%d char, %d color, %d attr)",
			result.DiffCount, result.CharDiffs, result.ColorDiffs, result.AttrDiffs)
	}

	return result
}

// CreateEnhancedDivergence creates an EnhancedDivergence from divergence data.
func CreateEnhancedDivergence(byteIndex, byteEndIndex int, chunk []byte, comparison *EnhancedComparisonResult) *EnhancedDivergence {
	return &EnhancedDivergence{
		ByteIndex:     byteIndex,
		ByteEndIndex:  byteEndIndex,
		ChunkHex:      fmt.Sprintf("%x", chunk),
		ChunkReadable: EscapeSequenceLog(chunk),
		EscapeContext: EscapeSequenceLog(chunk),
		Comparison:    comparison,
	}
}
